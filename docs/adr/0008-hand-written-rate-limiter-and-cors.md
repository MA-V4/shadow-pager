# 8. The rate limiter and CORS are written by hand

## Context

The API is shown in public, and two of its endpoints change something: starting a drill
and halting one. They need a rate limit. The web app is served from another origin, so
the API needs CORS. The project keeps its dependency list to three modules (chi, slack-go
and pgx) on purpose, so a reviewer can read everything that runs.

## Decision

Both are written in the project, with the standard library only.

- `internal/ratelimit` is a token bucket per key behind one mutex. It remembers at most
  10,000 keys, forgets a key once its bucket is full again, and turns strangers away when
  the table is full.
- `internal/api/guard.go` applies three checks to a write, in this order: the bucket of
  the caller's IP address (IPv6 by /64), the optional admin token compared in constant
  time, and a bucket shared by all callers. A refused request gets 429 with `Retry-After`.
  Reads have a separate and much larger bucket per caller, and the health probes have none.
- `internal/api/cors.go` allows only the origins in `CORS_ALLOWED_ORIGINS`, answers a
  preflight from any other origin with 403, never allows credentials, and does not wrap
  the response writer.

## Consequences

- No new dependency, and each piece is about a hundred lines with its own tests and
  benchmark.
- Memory is bounded even when a caller invents a new address for every request.
- The caller's IP is read by `internal/api/clientip.go`, also written by hand. It takes
  the address from `X-Forwarded-For` only for as many proxies as `TRUSTED_PROXY_HOPS`
  says, counted from the right, and ignores the header otherwise. chi's `RealIP` was
  used at first and was replaced, because it believes whatever a caller sends. If the
  number of proxies is set wrong, the per caller limits get weaker, and the bucket shared
  by all callers is still the backstop.
- The limiter lives in the memory of one process. With more than one instance each would
  count on its own. The project runs one instance.
- The code has to be maintained here. It covers what this API needs and nothing more.

## Alternative rejected

`golang.org/x/time/rate` with a map of limiters, and `github.com/go-chi/cors`. Both are
good libraries. The first still needs the map, the eviction and the memory bound to be
written by hand, which is most of the work. The second is a full CORS implementation for
an API with two methods and one allowed origin.
