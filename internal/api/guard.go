package api

import (
	"context"
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
	// PublicDrills lets callers without the admin token start drills, which are then marked as public.
	PublicDrills bool
}

// WithWriteProtection puts a rate limit and an optional admin token in front of every request that changes something.
func WithWriteProtection(p WriteProtection) Option {
	return func(h *Handler) {
		h.publicDrills = p.PublicDrills
		// The order matters: a flood is stopped first, then strangers, and only real work uses up the shared turns.
		if p.PerClient != nil {
			h.writeGuards = append(h.writeGuards, h.limitRequests(p.PerClient, clientKey, "write request rate limited"))
		}
		switch {
		case p.PublicDrills:
			h.writeGuards = append(h.writeGuards, h.identifyCaller(p.AdminToken))
		case p.AdminToken != "":
			h.writeGuards = append(h.writeGuards, h.requireAdminToken(p.AdminToken))
		}
		if p.Global != nil {
			h.writeGuards = append(h.writeGuards, h.limitRequests(p.Global, func(*http.Request) string { return globalKey }, "write request rate limited"))
		}
	}
}

// WithReadLimit gives every caller a generous number of reads, so one scraper cannot wear the server out.
func WithReadLimit(l limiter) Option {
	return func(h *Handler) {
		if l != nil {
			h.readGuard = h.limitRequests(l, clientKey, "read request rate limited")
		}
	}
}

// limitRequests lets a request through when its caller still has a turn, and says when to come back otherwise.
func (h *Handler) limitRequests(l limiter, key func(*http.Request) string, logMessage string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retryAfter := l.Allow(key(r))
			if ok {
				next.ServeHTTP(w, r)
				return
			}

			// Retry-After counts whole seconds, so we round up and never say zero.
			seconds := max(1, int(math.Ceil(retryAfter.Seconds())))
			h.log.WarnContext(r.Context(), logMessage,
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

// onlyReads applies a guard to requests that only look, and lets every other request pass it by.
func onlyReads(guard func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		guarded := guard(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				guarded.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ADMIN TOKEN

// tokenMatcher gives back a function that checks a token in the same time whatever the caller sent.
func tokenMatcher(token string) func(candidate string) bool {
	want := sha256.Sum256([]byte(token))
	return func(candidate string) bool {
		// Both sides are hashed first, so comparing takes the same time whatever the caller sent.
		got := sha256.Sum256([]byte(candidate))
		return subtle.ConstantTimeCompare(got[:], want[:]) == 1
	}
}

// refuseToken answers a request whose admin token is missing or wrong, and never repeats what was sent.
func (h *Handler) refuseToken(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="shadow-pager"`)
	h.writeJSON(w, r, http.StatusUnauthorized, errorResponse{Error: message})
}

// requireAdminToken only lets a request through when it carries the right bearer token.
func (h *Handler) requireAdminToken(token string) func(http.Handler) http.Handler {
	matches := tokenMatcher(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !matches(bearerToken(r)) {
				h.refuseToken(w, r, "a valid admin token is required for this request")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PUBLIC DRILLS

// callerKey is the context key under which a request remembers that its caller is a visitor.
type callerKey struct{}

// identifyCaller lets visitors through as visitors, lets the admin through as the admin, and refuses a wrong token.
func (h *Handler) identifyCaller(token string) func(http.Handler) http.Handler {
	matches := tokenMatcher(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sent := bearerToken(r)
			switch {
			case sent == "":
				// No token at all is a visitor, and what a visitor starts is a public drill.
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, true)))
			case token != "" && matches(sent):
				next.ServeHTTP(w, r)
			default:
				// A wrong token is refused instead of being treated as a visitor, so a typo never goes unnoticed.
				h.refuseToken(w, r, "the admin token is not valid, so send the right one or none at all")
			}
		})
	}
}

// isVisitor tells us if the caller of this request came in without the admin token while public drills are on.
func isVisitor(r *http.Request) bool {
	visitor, _ := r.Context().Value(callerKey{}).(bool)
	return visitor
}

// HELPERS

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
