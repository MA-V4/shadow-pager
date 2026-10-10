package api

import (
	"net/http"
	"strings"
)

// CORS

const (
	corsAllowMethods  = "GET, POST, OPTIONS"
	corsAllowHeaders  = "Authorization, Content-Type"
	corsExposeHeaders = "Location, Retry-After"
	corsMaxAge        = "600" // seconds a browser may remember the answer to a preflight
)

// CORS tells browsers which other websites may talk to this API, and says no to every website that is not on the list.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin = normalizeOrigin(origin); origin != "" {
			allowed[origin] = true
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			// Requests without an Origin do not come from a web page on another site, so there is nothing to decide.
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// The answer depends on who asks, and caches must know that.
			w.Header().Add("Vary", "Origin")
			ok := allowed[normalizeOrigin(origin)]
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""

			if preflight {
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
				w.Header().Set("Access-Control-Max-Age", corsMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Without these headers the browser refuses to show the answer to a page that is not on the list.
			if ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Expose-Headers", corsExposeHeaders)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// normalizeOrigin makes two spellings of the same website look the same.
func normalizeOrigin(origin string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(origin), "/"))
}
