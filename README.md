# Shadow-Pager

A ChatOps incident simulation engine. Inject infrastructure failures from a
dashboard, watch an automated incident lifecycle unfold in Slack, and stream
live telemetry to a status page.

## Status

Phase 1: project bootstrap. HTTP server, health probes, graceful shutdown,
chaos engine core types and lifecycle loop.

## Run locally

Requires Go 1.22 or newer.

    cp .env.example .env              # optional, defaults work out of the box
    set -a; source .env; set +a       # the binary reads real env vars, no dotenv dependency
    make run                          # or: go run ./cmd/server
    curl localhost:8080/healthz
    curl localhost:8080/readyz

Stop with Ctrl+C and watch the structured shutdown sequence in the logs.

## Test

    make test               # go test -race ./...

## Layout

    cmd/server          process bootstrap and lifecycle
    internal/chaos      simulation engine and telemetry generators
    internal/slack      Socket Mode incident lifecycle
    internal/storage    Postgres persistence

## Endpoints

| Path       | Purpose                                                   |
|------------|-----------------------------------------------------------|
| `/healthz` | Liveness. Process is up. Never checks dependencies.       |
| `/readyz`  | Readiness. Engine ticking and not draining. 503 otherwise. |
