# 1. Interfaces are declared by the package that uses them

## Context

The engine in `internal/chaos` is used by three other packages: the HTTP API, the Slack
listener and the incident manager. Each of them needs only a few of its methods. The API
needs inject, halt, get, list and subscribe. Slack needs inject, halt and list. The
incident manager needs the two subscribe methods.

## Decision

The `chaos` package exports the concrete `*Engine` and declares only one interface,
`Generator`, which is the one thing it consumes itself. Every other package declares the
small interface it needs, next to the code that uses it: `simulator` in `internal/api`,
`simulations` and `incidents` in `internal/slack`, `engine` and `Notifier` in
`internal/incident`. `cmd/server/main.go` is the only place that knows the concrete types
and passes them in.

## Consequences

- Each package can be tested against a fake of three or four methods. The API tests never
  start a real engine.
- `internal/incident` does not import Slack, and `internal/api` never names
  `*chaos.Engine`. The dependency arrows all point at `main.go`.
- Adding a method to the engine breaks nobody, because no interface lists every method.
- The same method signature is written in more than one place. When a signature changes,
  the compiler points at every consumer, which is the wanted behaviour.

## Alternative rejected

One exported `Simulator` interface in the `chaos` package that lists every engine method.
It is the habit carried over from Java and Python abstract base classes. Every consumer
would then depend on methods it never calls, every fake would have to implement all of
them, and the interface would have to grow with the engine.
