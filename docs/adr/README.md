# Decision records

Each file records one design decision: the context, what was decided, what follows from
it, and the alternative that was rejected. They are short on purpose.

| No. | Decision |
|-----|----------|
| 1 | [Interfaces are declared by the package that uses them](0001-consumer-side-interfaces.md) |
| 2 | [Server-Sent Events for the dashboard, not WebSockets](0002-sse-over-websockets.md) |
| 3 | [Broadcast never blocks and slow subscribers drop samples](0003-non-blocking-broadcast.md) |
| 4 | [Shutdown drains HTTP before it stops the engine](0004-shutdown-ordering.md) |
| 5 | [A pgx exec mode that works behind transaction poolers](0005-pooler-safe-pgx-exec-mode.md) |
| 6 | [Readiness reads a remembered ping, so Neon can sleep](0006-passive-readiness.md) |
| 7 | [Persistence is driven by events, not by calls in the engine](0007-events-drive-persistence.md) |
| 8 | [The rate limiter and CORS are written by hand](0008-hand-written-rate-limiter-and-cors.md) |
| 9 | [Slack and storage are optional](0009-optional-slack-and-storage.md) |
