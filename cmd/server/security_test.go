package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// HELPERS

// checkServerSecurityHeaders fails the test for every security header that an answer is missing.
func checkServerSecurityHeaders(t *testing.T, what string, h http.Header) {
	t.Helper()
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"X-Frame-Options":        "DENY",
	}
	for name, value := range want {
		if got := h.Get(name); got != value {
			t.Errorf("%s: %s = %q, want %q", what, name, got, value)
		}
	}
	if got := h.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") || !strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("%s: Content-Security-Policy = %q, want one that loads nothing and blocks framing", what, got)
	}
}

// TESTS

func TestRunSendsSecurityHeaders(t *testing.T) {
	s := startServer(t, map[string]string{"CORS_ALLOWED_ORIGINS": "https://shadow-pager.vercel.app"})

	for _, path := range []string{s.root + "/healthz", s.root + "/readyz", s.base + "/status", s.base + "/simulations", s.base + "/nowhere"} {
		res, _ := send(t, http.MethodGet, path, "", nil)
		checkServerSecurityHeaders(t, "GET "+path, res.Header)
	}

	// A preflight that is refused never reaches a handler, and it must carry the headers too.
	res, _ := send(t, http.MethodOptions, s.base+"/simulations", "", map[string]string{
		"Origin":                        "https://evil.example",
		"Access-Control-Request-Method": "POST",
	})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("preflight from a stranger = %d, want 403", res.StatusCode)
	}
	checkServerSecurityHeaders(t, "refused preflight", res.Header)
}

func TestRunStreamStillWorksWithSecurityHeaders(t *testing.T) {
	s := startServer(t, nil)
	res0, body := send(t, http.MethodPost, s.base+"/simulations", injectBody, map[string]string{"Content-Type": "application/json"})
	if res0.StatusCode != http.StatusCreated {
		t.Fatalf("inject = %d %s, want 201", res0.StatusCode, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.ID == "" {
		t.Fatalf("read the id of the new simulation from %q: %v", body, err)
	}
	sim := created.ID

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/stream", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want an event stream", got)
	}
	checkServerSecurityHeaders(t, "GET /stream", res.Header)

	// A real sample must still arrive, which proves the stream can flush behind the new middleware.
	found := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") && strings.Contains(sc.Text(), sim) {
				found <- true
				return
			}
		}
		found <- false
	}()
	select {
	case ok := <-found:
		if !ok {
			t.Fatal("the stream ended before a sample of the injected simulation arrived")
		}
	case <-time.After(testWait):
		t.Fatal("timed out waiting for a sample on the stream")
	}
}
