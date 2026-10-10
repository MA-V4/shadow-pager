package api

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// HELPERS

const (
	dashboardOrigin = "https://shadow-pager.vercel.app"
	strangerOrigin  = "https://evil.example"
)

// corsHandler wraps a tiny handler with CORS for the dashboard origin and counts how often it is reached.
func corsHandler(reached *int) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached++
		w.Header().Set("Location", "/simulations/sim_abc")
		w.WriteHeader(http.StatusCreated)
	})
	return CORS([]string{dashboardOrigin + "/", " http://localhost:5173 "})(next)
}

func corsRequest(method, origin string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(method, "/api/v1/simulations", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// TESTS

func TestCORSSimpleRequests(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		wantAllowed string
		wantVary    bool
	}{
		{name: "allowed origin", origin: dashboardOrigin, wantAllowed: dashboardOrigin, wantVary: true},
		{name: "second allowed origin", origin: "http://localhost:5173", wantAllowed: "http://localhost:5173", wantVary: true},
		{name: "allowed origin in capitals", origin: "HTTPS://Shadow-Pager.Vercel.App", wantAllowed: "HTTPS://Shadow-Pager.Vercel.App", wantVary: true},
		{name: "origin that is not on the list", origin: strangerOrigin, wantVary: true},
		{name: "origin that only starts like an allowed one", origin: dashboardOrigin + ".evil.example", wantVary: true},
		{name: "no origin at all, like curl", origin: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached := 0
			rec := httptest.NewRecorder()

			corsHandler(&reached).ServeHTTP(rec, corsRequest(http.MethodPost, tc.origin, nil))

			if reached != 1 || rec.Code != http.StatusCreated {
				t.Fatalf("handler reached %d times with status %d, want the request to go through once with 201", reached, rec.Code)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.wantAllowed {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tc.wantAllowed)
			}
			wantExpose := ""
			if tc.wantAllowed != "" {
				wantExpose = "Location, Retry-After"
			}
			if got := rec.Header().Get("Access-Control-Expose-Headers"); got != wantExpose {
				t.Errorf("Access-Control-Expose-Headers = %q, want %q", got, wantExpose)
			}
			if got := rec.Header().Get("Vary") == "Origin"; got != tc.wantVary {
				t.Errorf("Vary is Origin = %v, want %v", got, tc.wantVary)
			}
			if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
				t.Errorf("Access-Control-Allow-Credentials = %q, want it never set because no cookies are used", got)
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	preflight := map[string]string{
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization, content-type",
	}

	t.Run("allowed origin gets permission", func(t *testing.T) {
		reached := 0
		rec := httptest.NewRecorder()

		corsHandler(&reached).ServeHTTP(rec, corsRequest(http.MethodOptions, dashboardOrigin, preflight))

		if rec.Code != http.StatusNoContent || reached != 0 {
			t.Fatalf("status = %d, handler reached %d times, want 204 answered by the middleware itself", rec.Code, reached)
		}
		want := map[string]string{
			"Access-Control-Allow-Origin":  dashboardOrigin,
			"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
			"Access-Control-Allow-Headers": "Authorization, Content-Type",
			"Access-Control-Max-Age":       "600",
			"Vary":                         "Origin",
		}
		for header, value := range want {
			if got := rec.Header().Get(header); got != value {
				t.Errorf("%s = %q, want %q", header, got, value)
			}
		}
	})

	t.Run("origin that is not on the list is refused", func(t *testing.T) {
		reached := 0
		rec := httptest.NewRecorder()

		corsHandler(&reached).ServeHTTP(rec, corsRequest(http.MethodOptions, strangerOrigin, preflight))

		if rec.Code != http.StatusForbidden || reached != 0 {
			t.Fatalf("status = %d, handler reached %d times, want 403 and no call", rec.Code, reached)
		}
		for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
			if got := rec.Header().Get(header); got != "" {
				t.Errorf("%s = %q, want it missing for a stranger", header, got)
			}
		}
	})

	t.Run("plain OPTIONS without a preflight header goes to the handler", func(t *testing.T) {
		reached := 0
		rec := httptest.NewRecorder()

		corsHandler(&reached).ServeHTTP(rec, corsRequest(http.MethodOptions, dashboardOrigin, nil))

		if reached != 1 {
			t.Errorf("handler reached %d times, want 1", reached)
		}
	})
}

func TestCORSWithNoAllowedOrigins(t *testing.T) {
	reached := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++ })
	rec := httptest.NewRecorder()

	CORS(nil)(next).ServeHTTP(rec, corsRequest(http.MethodGet, dashboardOrigin, nil))

	if reached != 1 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("handler reached %d times, allow origin = %q, want the request served with no permission for any website", reached, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSWorksForTheEventStream(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample, 1)}
	fake.stream <- testSample("sim_a", 950)
	h := NewHandler(fake, slog.New(slog.NewTextHandler(io.Discard, nil)), context.Background())
	srv := httptest.NewServer(CORS([]string{dashboardOrigin})(h.Routes()))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Origin", dashboardOrigin)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Access-Control-Allow-Origin"); got != dashboardOrigin {
		t.Errorf("Access-Control-Allow-Origin on the stream = %q, want %q", got, dashboardOrigin)
	}
	if got := res.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}

	// The first event must arrive while the stream is still open, which proves nothing is held back.
	line := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(res.Body)
		if sc.Scan() {
			line <- sc.Text()
		}
	}()
	select {
	case got := <-line:
		if !strings.HasPrefix(got, "event: metric") {
			t.Errorf("first line = %q, want the metric event", got)
		}
	case <-time.After(streamWait):
		t.Fatal("no event arrived through the CORS middleware")
	}
}
