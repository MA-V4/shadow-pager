package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	const direct = "10.0.0.5:44321"

	tests := []struct {
		name    string
		hops    int
		headers []string
		want    string
	}{
		{name: "no trusted proxy ignores the header", hops: 0, headers: []string{"203.0.113.9"}, want: direct},
		{name: "one proxy, one address", hops: 1, headers: []string{"203.0.113.9"}, want: "203.0.113.9:0"},
		{name: "one proxy takes the last address", hops: 1, headers: []string{"198.51.100.1, 203.0.113.9"}, want: "203.0.113.9:0"},
		{name: "a forged address in front is not believed", hops: 1, headers: []string{"1.2.3.4, 5.6.7.8, 203.0.113.9"}, want: "203.0.113.9:0"},
		{name: "two proxies take the one before the last", hops: 2, headers: []string{"1.2.3.4, 203.0.113.9, 172.16.0.1"}, want: "203.0.113.9:0"},
		{name: "the header split over two lines", hops: 2, headers: []string{"1.2.3.4, 203.0.113.9", "172.16.0.1"}, want: "203.0.113.9:0"},
		{name: "spaces around the address", hops: 1, headers: []string{"  203.0.113.9  "}, want: "203.0.113.9:0"},
		{name: "IPv6", hops: 1, headers: []string{"2001:db8::1"}, want: "[2001:db8::1]:0"},
		{name: "IPv4 written as IPv6", hops: 1, headers: []string{"::ffff:203.0.113.9"}, want: "203.0.113.9:0"},
		{name: "fewer addresses than trusted proxies", hops: 3, headers: []string{"203.0.113.9"}, want: direct},
		{name: "no header at all", hops: 1, headers: nil, want: direct},
		{name: "garbage where the address should be", hops: 1, headers: []string{"203.0.113.9, not-an-address"}, want: direct},
		{name: "an address with a port is not an address", hops: 1, headers: []string{"203.0.113.9:8080"}, want: direct},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			handler := ClientIP(tt.hops)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
			req.RemoteAddr = direct
			for _, h := range tt.headers {
				req.Header.Add("X-Forwarded-For", h)
			}
			// These headers are easy to forge, and nothing may ever read them.
			req.Header.Set("X-Real-IP", "6.6.6.6")
			req.Header.Set("True-Client-IP", "6.6.6.6")

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if got != tt.want {
				t.Errorf("RemoteAddr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientIPFeedsTheRateLimitKey(t *testing.T) {
	var key string
	handler := ClientIP(1)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { key = clientKey(r) }))

	for header, want := range map[string]string{
		"9.9.9.9, 203.0.113.9":       "203.0.113.9",
		"2001:db8:1:2:aaaa:bbbb:c:d": "2001:db8:1:2::/64",
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/simulations", nil)
		req.Header.Set("X-Forwarded-For", header)
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if key != want {
			t.Errorf("rate limit key for %q = %q, want %q", header, key, want)
		}
	}
}
