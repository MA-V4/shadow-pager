package api

import "net/http"

// SECURITY HEADERS

const (
	// This server only sends JSON and event streams, so a browser that shows an answer as a page may load nothing at all.
	contentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
	referrerPolicy        = "strict-origin-when-cross-origin"
)

// SecurityHeaders tells browsers to treat every answer as plain data: no guessing the type, no framing, no loading.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", referrerPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		// The writer is passed on untouched, so a live stream can still flush and clear its deadline.
		next.ServeHTTP(w, r)
	})
}
