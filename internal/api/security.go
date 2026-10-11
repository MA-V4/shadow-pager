package api

import "net/http"

// SECURITY HEADERS

const (
	// This server only sends JSON and event streams, so a browser that shows an answer as a page may load nothing at all.
	contentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
	referrerPolicy        = "strict-origin-when-cross-origin"
	// One year, and it covers every subdomain of the address the API is served from.
	strictTransportSecurity = "max-age=31536000; includeSubDomains"
)

// SecurityHeaders tells browsers to treat every answer as plain data: no guessing the type, no framing, no loading.
func SecurityHeaders(https bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", referrerPolicy)
			h.Set("X-Frame-Options", "DENY")
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			// Browsers are only told to insist on HTTPS where the server really is behind it, which is production.
			if https {
				h.Set("Strict-Transport-Security", strictTransportSecurity)
			}
			// The writer is passed on untouched, so a live stream can still flush and clear its deadline.
			next.ServeHTTP(w, r)
		})
	}
}
