package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// CLIENT ADDRESS

// ClientIP finds the real address of the caller behind a known number of proxies, and trusts nothing a caller can forge.
func ClientIP(trustedHops int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if addr, ok := forwardedClient(r.Header.Values("X-Forwarded-For"), trustedHops); ok {
				// The port is gone by now, so a zero keeps the usual "host:port" shape for everyone who reads it later.
				r.RemoteAddr = net.JoinHostPort(addr.String(), "0")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forwardedClient picks the address that the outermost trusted proxy wrote down, counting from the right.
func forwardedClient(headers []string, trustedHops int) (netip.Addr, bool) {
	// With no trusted proxy the header is only something a caller typed, so it is ignored.
	if trustedHops < 1 {
		return netip.Addr{}, false
	}
	var parts []string
	for _, header := range headers {
		parts = append(parts, strings.Split(header, ",")...)
	}
	// Each trusted proxy adds one address at the end, so the caller sits that many places from the right.
	index := len(parts) - trustedHops
	if index < 0 {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(parts[index]))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
