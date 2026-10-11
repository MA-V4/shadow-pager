package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// HELPERS

// send makes one HTTP request with extra headers and gives back the whole answer.
func send(t *testing.T, method, url, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, url, err)
	}
	return res, string(raw)
}

const injectBody = `{"service":"payments-api","mode":"latency_spike","severity":"minor","intensity":0.1,"duration":"10s"}`

// CONFIG TESTS

func TestLoadConfigProtection(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantErr     bool
		wantToken   string
		wantBurst   int
		wantRefill  time.Duration
		wantOrigins string
	}{
		{name: "defaults", env: nil, wantBurst: 5, wantRefill: 30 * time.Second},
		{
			name: "everything set",
			env: map[string]string{
				"ADMIN_TOKEN":          " hunter2secret-that-is-long-enough-0123 ",
				"RATE_LIMIT_BURST":     "2",
				"RATE_LIMIT_REFILL":    "1m",
				"CORS_ALLOWED_ORIGINS": "https://a.example, http://localhost:5173 ,,",
			},
			wantToken:   "hunter2secret-that-is-long-enough-0123",
			wantBurst:   2,
			wantRefill:  time.Minute,
			wantOrigins: "https://a.example|http://localhost:5173",
		},
		{name: "burst is not a number", env: map[string]string{"RATE_LIMIT_BURST": "lots"}, wantErr: true},
		{name: "burst is zero", env: map[string]string{"RATE_LIMIT_BURST": "0"}, wantErr: true},
		{name: "refill is not a time", env: map[string]string{"RATE_LIMIT_REFILL": "soon"}, wantErr: true},
		{name: "refill is negative", env: map[string]string{"RATE_LIMIT_REFILL": "-5s"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadConfig(mapEnv(tc.env))

			if (err != nil) != tc.wantErr {
				t.Fatalf("loadConfig error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				if !errors.Is(err, errProtectionConfig) {
					t.Errorf("error %v does not wrap errProtectionConfig", err)
				}
				return
			}
			if cfg.AdminToken != tc.wantToken || cfg.RateLimitBurst != tc.wantBurst || cfg.RateLimitRefill != tc.wantRefill {
				t.Errorf("token %q, burst %d, refill %s, want %q, %d, %s", cfg.AdminToken, cfg.RateLimitBurst, cfg.RateLimitRefill, tc.wantToken, tc.wantBurst, tc.wantRefill)
			}
			if got := strings.Join(cfg.CORSAllowedOrigins, "|"); got != tc.wantOrigins {
				t.Errorf("CORSAllowedOrigins = %q, want %q", got, tc.wantOrigins)
			}
		})
	}
}

// END TO END TESTS

func TestRunProtectsWrites(t *testing.T) {
	s := startServer(t, map[string]string{
		"ADMIN_TOKEN":       "hunter2secret-that-is-long-enough-0123",
		"RATE_LIMIT_BURST":  "3",
		"RATE_LIMIT_REFILL": "1h",
	})
	auth := map[string]string{"Authorization": "Bearer hunter2secret-that-is-long-enough-0123"}

	// Reading is open to everyone, with or without the token.
	for _, path := range []string{"/simulations", "/incidents", "/status", "/slo"} {
		if res, body := send(t, http.MethodGet, s.base+path, "", nil); res.StatusCode != http.StatusOK {
			t.Errorf("GET %s without a token = %d %s, want 200", path, res.StatusCode, body)
		}
	}

	// The first try has no token, which costs one of the three turns.
	res, body := send(t, http.MethodPost, s.base+"/simulations", injectBody, nil)
	if res.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(res.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("POST without a token = %d %s, want 401 that asks for a bearer token", res.StatusCode, body)
	}

	res, body = send(t, http.MethodPost, s.base+"/simulations", injectBody, auth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST with the token = %d %s, want 201", res.StatusCode, body)
	}
	location := res.Header.Get("Location")

	if res, body := send(t, http.MethodPost, s.root+location+"/halt", "", auth); res.StatusCode != http.StatusOK {
		t.Fatalf("POST halt with the token = %d %s, want 200", res.StatusCode, body)
	}

	// All three turns are used up now, and one hour must pass to earn the next.
	res, body = send(t, http.MethodPost, s.base+"/simulations", injectBody, auth)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("fourth write = %d %s, want 429", res.StatusCode, body)
	}
	if got := res.Header.Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want 3600", got)
	}
	if !strings.Contains(body, `"error":"too many requests, try again in 3600 seconds"`) {
		t.Errorf("body = %s, want the retry time in the error", body)
	}

	if res, _ := send(t, http.MethodGet, s.base+"/simulations", "", nil); res.StatusCode != http.StatusOK {
		t.Errorf("GET after the limit = %d, want 200 because reading is never limited", res.StatusCode)
	}

	s.stop()
	for _, m := range s.logs.messages() {
		if strings.Contains(m, "hunter2secret-that-is-long-enough-0123") {
			t.Errorf("log line %q repeats the admin token", m)
		}
	}
}

func TestRunWritesAreOpenWithoutAdminToken(t *testing.T) {
	s := startServer(t, nil)

	if res, body := send(t, http.MethodPost, s.base+"/simulations", injectBody, nil); res.StatusCode != http.StatusCreated {
		t.Errorf("POST without a configured token = %d %s, want 201", res.StatusCode, body)
	}
}

func TestRunCORS(t *testing.T) {
	const allowed = "https://shadow-pager.vercel.app"
	s := startServer(t, map[string]string{"CORS_ALLOWED_ORIGINS": allowed + ",http://localhost:5173"})

	preflight := map[string]string{
		"Origin":                         allowed,
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization, content-type",
	}
	res, _ := send(t, http.MethodOptions, s.base+"/simulations", "", preflight)
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != allowed {
		t.Errorf("preflight = %d with allow origin %q, want 204 for %s", res.StatusCode, res.Header.Get("Access-Control-Allow-Origin"), allowed)
	}
	if got := res.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("Access-Control-Allow-Headers = %q, want it to allow the admin token header", got)
	}

	preflight["Origin"] = "https://evil.example"
	if res, _ := send(t, http.MethodOptions, s.base+"/simulations", "", preflight); res.StatusCode != http.StatusForbidden {
		t.Errorf("preflight from a stranger = %d, want 403", res.StatusCode)
	}

	res, _ = send(t, http.MethodGet, s.base+"/status", "", map[string]string{"Origin": "http://localhost:5173"})
	if res.StatusCode != http.StatusOK || res.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("GET /status from the local dashboard = %d with allow origin %q, want 200 and the origin echoed", res.StatusCode, res.Header.Get("Access-Control-Allow-Origin"))
	}
	res, _ = send(t, http.MethodGet, s.base+"/status", "", map[string]string{"Origin": "https://evil.example"})
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("GET /status from a stranger got allow origin %q, want none", res.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestRunServesStatusFromLiveSamples(t *testing.T) {
	s := startServer(t, nil)

	if _, body := send(t, http.MethodGet, s.base+"/status", "", nil); !strings.Contains(body, `"status":"operational"`) || !strings.Contains(body, `"services":[]`) {
		t.Fatalf("GET /status before any failure = %s, want operational with no services", body)
	}

	scenario := `{"service":"payments-api","mode":"latency_spike","severity":"critical","intensity":1,"duration":"10s"}`
	if res, body := send(t, http.MethodPost, s.base+"/simulations", scenario, nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /simulations = %d %s, want 201", res.StatusCode, body)
	}

	// The incident manager and the status page read the same samples on their own, so we wait until both have caught up.
	s.logs.waitFor(t, "incident declared")
	var body string
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	timeout := time.After(testWait)
	for {
		_, body = send(t, http.MethodGet, s.base+"/status", "", nil)
		if strings.Contains(body, `"status":"major_outage"`) {
			break
		}
		select {
		case <-tick.C:
		case <-timeout:
			t.Fatalf("GET /status never reported the outage, last answer: %s", body)
		}
	}
	for _, want := range []string{`"name":"payments-api","status":"major_outage","metrics":{`, `"severity":"critical"`, `"updates":[{`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /status during the outage = %s, want it to contain %s", body, want)
		}
	}
}
