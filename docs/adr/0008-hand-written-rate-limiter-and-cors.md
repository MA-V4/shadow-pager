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
  Reads are never limited.
- `internal/api/cors.go` allows only the origins in `CORS_ALLOWED_ORIGINS`, answers a
  preflight from any other origin with 403, never allows credentials, and does not wrap
  the response writer.

## Consequences

- No new dependency, and each piece is about a hundred lines with its own tests and
  benchmark.
- Memory is bounded even when a caller invents a new address for every request.
- The caller's IP comes from chi's `RealIP` middleware, which trusts proxy headers. A
  caller can lie about them, so the per caller limit is a courtesy and the shared bucket
  is the real limit.
- The limiter lives in the memory of one process. With more than one instance each would
  count on its own. The project runs one instance.
- The code has to be maintained here. It covers what this API needs and nothing more.

## Alternative rejected

`golang.org/x/time/rate` with a map of limiters, and `github.com/go-chi/cors`. Both are
good libraries. The first still needs the map, the eviction and the memory bound to be
written by hand, which is most of the work. The second is a full CORS implementation for
an API with two methods and one allowed origin.
