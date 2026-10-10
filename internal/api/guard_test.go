package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/ratelimit"
)

// FAKE

// fakeLimiter answers with whatever the test put in its fields and writes down who asked.
type fakeLimiter struct {
	deny       bool
	retryAfter time.Duration
	keys       []string
}

func (f *fakeLimiter) Allow(key string) (bool, time.Duration) {
	f.keys = append(f.keys, key)
	return !f.deny, f.retryAfter
}

// HELPERS

const testAdminToken = "s3cret-admin-token"

// guarded sends one request to a handler with the given protection and tells us if the engine was reached.
func guarded(t *testing.T, p WriteProtection, logs *bytes.Buffer, method, path, remoteAddr, authorization string) (*httptest.ResponseRecorder, *fakeSimulator) {
	t.Helper()
	if logs == nil {
		logs = &bytes.Buffer{}
	}
	fake := &fakeSimulator{sim: testSimulation("sim_abc", chaos.StateRunning)}
	h := NewHandler(fake, slog.New(slog.NewJSONHandler(logs, nil)), context.Background(), WithWriteProtection(p))

	body := ""
	if method == http.MethodPost && path == "/simulations" {
		body = validBody
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec, fake
}

// reachedEngine tells us if the request made it past the guards.
func reachedEngine(f *fakeSimulator) bool {
	return f.gotScenario != (chaos.Scenario{}) || f.gotID != ""
}

// RATE LIMIT TESTS

func TestRateLimitedWriteGets429WithRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		retryAfter time.Duration
		wantHeader string
	}{
		{name: "inject", method: http.MethodPost, path: "/simulations", retryAfter: 6200 * time.Millisecond, wantHeader: "7"},
		{name: "halt", method: http.MethodPost, path: "/simulations/sim_abc/halt", retryAfter: 30 * time.Second, wantHeader: "30"},
		{name: "a tiny wait still says one second", method: http.MethodPost, path: "/simulations", retryAfter: 10 * time.Millisecond, wantHeader: "1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lim := &fakeLimiter{deny: true, retryAfter: tc.retryAfter}

			rec, fake := guarded(t, WriteProtection{PerClient: lim}, nil, tc.method, tc.path, "203.0.113.9:4711", "")

			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want 429 (body %s)", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Retry-After"); got != tc.wantHeader {
				t.Errorf("Retry-After = %q, want %q", got, tc.wantHeader)
			}
			if want := `{"error":"too many requests, try again in ` + tc.wantHeader + ` seconds"}`; strings.TrimSpace(rec.Body.String()) != want {
				t.Errorf("body = %s, want %s", rec.Body, want)
			}
			if reachedEngine(fake) {
				t.Error("a rate limited request reached the engine")
			}
			if len(lim.keys) != 1 || lim.keys[0] != "203.0.113.9" {
				t.Errorf("limiter was asked about %v, want the caller's IP 203.0.113.9", lim.keys)
			}
		})
	}
}

func TestReadsAreNeverRateLimited(t *testing.T) {
	lim := &fakeLimiter{deny: true, retryAfter: time.Minute}

	for _, path := range []string{"/simulations", "/simulations/sim_abc"} {
		rec, _ := guarded(t, WriteProtection{PerClient: lim, Global: lim, AdminToken: testAdminToken}, nil, http.MethodGet, path, "", "")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 because reading is always open", path, rec.Code)
		}
	}
	if len(lim.keys) != 0 {
		t.Errorf("limiter was asked %d times for read requests, want 0", len(lim.keys))
	}
}

func TestRateLimitIsPerCaller(t *testing.T) {
	clock := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	lim := ratelimit.New(2, time.Minute, ratelimit.WithClock(func() time.Time { return clock }))
	p := WriteProtection{PerClient: lim}

	want := []struct {
		addr   string
		status int
	}{
		{"198.51.100.1:1000", http.StatusCreated},
		{"198.51.100.1:1001", http.StatusCreated},
		{"198.51.100.1:1002", http.StatusTooManyRequests},
		{"198.51.100.2:1000", http.StatusCreated},
		{"[2001:db8:1:2::aaaa]:1000", http.StatusCreated},
		{"[2001:db8:1:2::bbbb]:1000", http.StatusCreated},
		// A third address from the same IPv6 home network shares the bucket of the first two.
		{"[2001:db8:1:2::cccc]:1000", http.StatusTooManyRequests},
	}
	for i, w := range want {
		rec, _ := guarded(t, p, nil, http.MethodPost, "/simulations", w.addr, "")
		if rec.Code != w.status {
			t.Errorf("request %d from %s = %d, want %d", i+1, w.addr, rec.Code, w.status)
		}
	}
}

// ADMIN TOKEN TESTS

func TestAdminToken(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		wantStatus    int
	}{
		{name: "right token", authorization: "Bearer " + testAdminToken, wantStatus: http.StatusCreated},
		{name: "scheme in lower case", authorization: "bearer " + testAdminToken, wantStatus: http.StatusCreated},
		{name: "no header", authorization: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong token", authorization: "Bearer not-the-token", wantStatus: http.StatusUnauthorized},
		{name: "token that only starts the same", authorization: "Bearer s3cret", wantStatus: http.StatusUnauthorized},
		{name: "token without the scheme", authorization: testAdminToken, wantStatus: http.StatusUnauthorized},
		{name: "another scheme", authorization: "Basic " + testAdminToken, wantStatus: http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer

			rec, fake := guarded(t, WriteProtection{AdminToken: testAdminToken}, &logs, http.MethodPost, "/simulations", "", tc.authorization)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
					t.Errorf("WWW-Authenticate = %q, want it to ask for a bearer token", got)
				}
				if want := `{"error":"a valid admin token is required for this request"}`; strings.TrimSpace(rec.Body.String()) != want {
					t.Errorf("body = %s, want %s", rec.Body, want)
				}
				if reachedEngine(fake) {
					t.Error("a request without the right token reached the engine")
				}
			}
			if strings.Contains(rec.Body.String()+logs.String(), testAdminToken) {
				t.Error("the admin token shows up in the answer or in the logs")
			}
		})
	}

	t.Run("halt needs the token too", func(t *testing.T) {
		rec, fake := guarded(t, WriteProtection{AdminToken: testAdminToken}, nil, http.MethodPost, "/simulations/sim_abc/halt", "", "")
		if rec.Code != http.StatusUnauthorized || reachedEngine(fake) {
			t.Errorf("halt without a token = %d, engine reached = %v, want 401 and no call", rec.Code, reachedEngine(fake))
		}
	})

	t.Run("without a configured token writes stay open", func(t *testing.T) {
		rec, _ := guarded(t, WriteProtection{}, nil, http.MethodPost, "/simulations", "", "")
		if rec.Code != http.StatusCreated {
			t.Errorf("status = %d, want 201", rec.Code)
		}
	})
}

func TestGuardOrder(t *testing.T) {
	perClient, global := &fakeLimiter{}, &fakeLimiter{}
	p := WriteProtection{PerClient: perClient, Global: global, AdminToken: testAdminToken}

	// A stranger uses up one of their own turns, but none of the turns everyone shares.
	rec, _ := guarded(t, p, nil, http.MethodPost, "/simulations", "203.0.113.9:1", "Bearer wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(perClient.keys) != 1 || len(global.keys) != 0 {
		t.Errorf("per caller limiter asked %d times and shared limiter %d times, want 1 and 0", len(perClient.keys), len(global.keys))
	}

	rec, _ = guarded(t, p, nil, http.MethodPost, "/simulations", "203.0.113.9:1", "Bearer "+testAdminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if len(global.keys) != 1 || global.keys[0] != globalKey {
		t.Errorf("shared limiter was asked about %v, want one question for the shared bucket", global.keys)
	}

	t.Run("the shared limit stops everyone", func(t *testing.T) {
		full := &fakeLimiter{deny: true, retryAfter: 20 * time.Second}
		rec, fake := guarded(t, WriteProtection{Global: full}, nil, http.MethodPost, "/simulations", "", "")
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "20" || reachedEngine(fake) {
			t.Errorf("status = %d, Retry-After = %q, engine reached = %v, want 429, 20 and no call", rec.Code, rec.Header().Get("Retry-After"), reachedEngine(fake))
		}
	})
}

// CLIENT KEY TESTS

func TestClientKey(t *testing.T) {
	tests := []struct {
		remoteAddr string
		want       string
	}{
		{remoteAddr: "203.0.113.9:4711", want: "203.0.113.9"},
		{remoteAddr: "203.0.113.9", want: "203.0.113.9"},
		{remoteAddr: "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:443", want: "2001:db8:1:2::/64"},
		{remoteAddr: "2001:db8:1:2::1", want: "2001:db8:1:2::/64"},
		{remoteAddr: "[::ffff:203.0.113.9]:80", want: "203.0.113.9"},
		{remoteAddr: "not an address", want: "not an address"},
	}

	for _, tc := range tests {
		t.Run(tc.remoteAddr, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/simulations", nil)
			req.RemoteAddr = tc.remoteAddr
			if got := clientKey(req); got != tc.want {
				t.Errorf("clientKey = %q, want %q", got, tc.want)
			}
		})
	}
}
