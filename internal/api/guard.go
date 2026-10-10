package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// LIMITER

// limiter lists the only thing the guards need a rate limiter to do.
type limiter interface {
	Allow(key string) (ok bool, retryAfter time.Duration)
}

// globalKey is the one bucket that all callers share in the global limiter.
const globalKey = "everyone"

// WRITE PROTECTION

// WriteProtection holds the settings that keep strangers from flooding the endpoints that change something.
type WriteProtection struct {
	PerClient  limiter // turns for each caller, named by IP address, or nil for no limit
	Global     limiter // turns shared by all callers together, or nil for no limit
	AdminToken string  // when set, callers must send it as a bearer token
}

// WithWriteProtection puts a rate limit and an optional admin token in front of every request that changes something.
func WithWriteProtection(p WriteProtection) Option {
	return func(h *Handler) {
		// The order matters: a flood is stopped first, then strangers, and only real work uses up the shared turns.
		if p.PerClient != nil {
			h.writeGuards = append(h.writeGuards, h.limitWrites(p.PerClient, clientKey))
		}
		if p.AdminToken != "" {
			h.writeGuards = append(h.writeGuards, h.requireAdminToken(p.AdminToken))
		}
		if p.Global != nil {
			h.writeGuards = append(h.writeGuards, h.limitWrites(p.Global, func(*http.Request) string { return globalKey }))
		}
	}
}

// limitWrites lets a request through when its caller still has a turn, and says when to come back otherwise.
func (h *Handler) limitWrites(l limiter, key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retryAfter := l.Allow(key(r))
			if ok {
				next.ServeHTTP(w, r)
				return
			}

			// Retry-After counts whole seconds, so we round up and never say zero.
			seconds := max(1, int(math.Ceil(retryAfter.Seconds())))
			h.log.WarnContext(r.Context(), "write request rate limited",
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("client", key(r)),
				slog.Int("retry_after_seconds", seconds),
			)
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			h.writeJSON(w, r, http.StatusTooManyRequests, errorResponse{
				Error: fmt.Sprintf("too many requests, try again in %d seconds", seconds),
			})
		})
	}
}

// requireAdminToken only lets a request through when it carries the right bearer token.
func (h *Handler) requireAdminToken(token string) func(http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Both sides are hashed first, so comparing takes the same time whatever the caller sent.
			got := sha256.Sum256([]byte(bearerToken(r)))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="shadow-pager"`)
				h.writeJSON(w, r, http.StatusUnauthorized, errorResponse{Error: "a valid admin token is required for this request"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken pulls the token out of the Authorization header, or gives back nothing when there is none.
func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// clientKey names the caller by IP address, and treats one IPv6 home network as one caller.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	// One customer usually gets a whole /64 of IPv6 addresses, so the bucket belongs to the /64.
	prefix, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}
