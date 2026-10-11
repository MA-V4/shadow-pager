# 3. Broadcast never blocks and slow subscribers drop samples

## Context

The engine ticks once per second and hands every sample to every subscriber: each open
dashboard stream, the incident manager and the status aggregator. A subscriber can be
slow for reasons the engine cannot control, such as a browser on a bad connection.

## Decision

Every subscriber gets a buffered channel (64 samples by default). `Engine.broadcast` sends
with `select` and a `default` case. When a buffer is full the sample for that subscriber is
dropped and counted in `Engine.dropped`. Lifecycle events use the same pattern with a
buffer of 256, and a dropped event is also logged at error level, because losing one of
those is a real problem.

`Engine.tick` changes simulation state under the write lock, releases it, and only then
generates and broadcasts samples under the read lock.

## Consequences

- One stuck subscriber cannot delay the tick or any other subscriber. The cost of a
  broadcast grows with the number of subscribers and with nothing else (see the
  benchmarks in `internal/chaos/bench_test.go`).
- A slow dashboard sees gaps in its chart instead of slowing everyone down. Samples are
  a live picture, not a record, so a gap is acceptable. Raw samples are never stored.
- A subscriber that must not miss anything cannot rely on samples. The incident manager
  declares an incident on the first unhealthy sample it sees, so a dropped sample can
  delay a declaration by one tick but cannot prevent it.
- The channel is closed under the write lock after the subscriber is removed from the
  map, so a close can never race a send.

## Alternative rejected

A blocking send, with or without a timeout. A blocking send lets the slowest client set
the pace of the whole engine. A send with a timeout bounds the damage but still makes the
tick time depend on the number of slow clients, and it needs a timer per send.
