package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// HELPERS

// wantSecurityHeaders is what every answer of the server must carry.
var wantSecurityHeaders = map[string]string{
	"X-Content-Type-Options":  "nosniff",
	"Referrer-Policy":         "strict-origin-when-cross-origin",
	"X-Frame-Options":         "DENY",
	"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'",
}

// checkSecurityHeaders fails the test for every header that is missing or wrong.
func checkSecurityHeaders(t *testing.T, got http.Header) {
	t.Helper()
	for name, want := range wantSecurityHeaders {
		if values := got.Values(name); len(values) != 1 || values[0] != want {
			t.Errorf("%s = %q, want exactly %q", name, values, want)
		}
	}
}

// TESTS

func TestSecurityHeadersOnEveryKindOfAnswer(t *testing.T) {
	tests := []struct {
		name   string
		status int
		write  func(w http.ResponseWriter)
	}{
		{name: "JSON answer", status: http.StatusOK, write: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}},
		{name: "error answer", status: http.StatusNotFound, write: func(w http.ResponseWriter) {
			http.Error(w, "nope", http.StatusNotFound)
		}},
		{name: "answer with no body", status: http.StatusNoContent, write: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusNoContent)
		}},
		{name: "handler that writes nothing at all", status: http.StatusOK, write: func(http.ResponseWriter) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tt.write(w) }))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
			checkSecurityHeaders(t, rec.Header())
		})
	}
}

func TestSecurityHeadersPolicyAllowsNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(false)(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	policy := rec.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Errorf("policy %q is missing %q", policy, directive)
		}
	}
	for _, loose := range []string{"*", "unsafe-inline", "unsafe-eval", "http:", "https:", "data:"} {
		if strings.Contains(policy, loose) {
			t.Errorf("policy %q contains %q, which would let something load", policy, loose)
		}
	}
}

func TestSecurityHeadersKeepCORSAnswers(t *testing.T) {
	reached := 0
	handler := SecurityHeaders(false)(corsHandler(&reached))

	// A refused preflight never reaches the real handler, and it still has to carry the headers.
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, corsRequest(http.MethodOptions, strangerOrigin, map[string]string{"Access-Control-Request-Method": "POST"}))
	if refused.Code != http.StatusForbidden || reached != 0 {
		t.Fatalf("preflight from a stranger = %d and reached the handler %d times, want 403 and 0", refused.Code, reached)
	}
	checkSecurityHeaders(t, refused.Header())

	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, corsRequest(http.MethodPost, dashboardOrigin, nil))
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != dashboardOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q, so the security headers broke CORS", got, dashboardOrigin)
	}
	checkSecurityHeaders(t, allowed.Header())
}

func TestSecurityHeadersDoNotWrapTheWriter(t *testing.T) {
	var seen http.ResponseWriter
	handler := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen = w
		// A live stream sets its own type and flushes, and both must still work behind the middleware.
		w.Header().Set("Content-Type", "text/event-stream")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush behind the middleware: %v", err)
		}
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil))

	if seen != http.ResponseWriter(rec) {
		t.Fatal("the handler got a different writer than the server gave, so a stream could lose its flush and deadline control")
	}
	if !rec.Flushed {
		t.Error("the flush did not reach the real writer")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want the stream to keep its own type", got)
	}
	checkSecurityHeaders(t, rec.Header())
}

func TestStrictTransportSecurityOnlyInProduction(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	off := httptest.NewRecorder()
	SecurityHeaders(false)(next).ServeHTTP(off, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := off.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q outside production, want none, because local servers speak plain HTTP", got)
	}
	checkSecurityHeaders(t, off.Header())

	on := httptest.NewRecorder()
	SecurityHeaders(true)(next).ServeHTTP(on, httptest.NewRequest(http.MethodGet, "/", nil))
	// One year is 31,536,000 seconds.
	if got, want := on.Header().Get("Strict-Transport-Security"), "max-age=31536000; includeSubDomains"; got != want {
		t.Errorf("Strict-Transport-Security = %q in production, want %q", got, want)
	}
	checkSecurityHeaders(t, on.Header())
}
