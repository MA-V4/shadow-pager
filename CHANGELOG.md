# Changelog

All notable changes to Shadow-Pager are written down here. The project follows
[Semantic Versioning](https://semver.org/).

## 1.0.0, 2026-10-11

The first release. It is grouped by the phases the project was built in.

### Phase 1: foundation

- HTTP server with structured JSON logs, configuration from the environment, and a
  graceful shutdown that drains requests before it stops anything else.
- `/healthz` for liveness and `/readyz` for readiness.
- The chaos engine: scenarios, simulations, a tick loop, and subscriptions that never
  block the engine.

### Phase 2: telemetry

- One metric generator driven by a table of impact and curve shape per failure mode:
  latency spike, error rate surge, memory leak, CPU saturation and database pool exhaustion.
- SLO limits, and samples that say which limits they break.
- Deterministic sample timestamps and injectable noise, so tests are exact.

### Phase 3: HTTP API

- Create, list, get and halt simulations under `/api/v1`.
- Live metric samples over Server-Sent Events, with heartbeats and a clean end on shutdown.
- One error shape for every failure, and DTOs that own the wire format.
- CI with gofmt, vet and race tests.

### Phase 4: incidents and Slack

- Lifecycle events from the engine.
- The incident domain: declared, acknowledged, mitigated, resolved, moving forward only.
- An incident is declared on the first sample that breaks an SLO, never twice for one
  simulation, and is marked mitigated when its simulation ends.
- Slack over Socket Mode: a channel per incident, a Block Kit incident card with buttons,
  timeline messages, and the `/shadowpager` slash command.
- Incident list and detail endpoints.
- The server runs without Slack when no tokens are set.

### Phase 5: persistence

- Postgres storage with embedded migrations behind an advisory lock.
- A pgx pool sized for free plans, in a mode that works behind transaction poolers.
- Simulations are saved from engine events and incidents from manager events, on their
  own queues, so the database never slows the engine.
- Running simulations and open incidents are restored at startup.
- The server runs in memory when no database is configured.

### Phase 6: protection, status and the web app

- A hand-written token bucket rate limiter, an optional admin token and hand-written CORS.
- Readiness reads a remembered database ping, so a free database can sleep.
- History: lists merge live items with rows from the database.
- A status aggregator and the public `/api/v1/status` and `/api/v1/slo` endpoints.
- The web app: dashboard with live charts, incident list and detail, public status page.
- A redesign from the "Night Shift" design system: design tokens for two themes, IBM
  Plex, a drill composer, service cards with a signal rail, charts that only turn red
  above the SLO, a flight recorder timeline, and a layout that works from 360 pixels up.

### Phase 7: launch readiness

- Benchmarks for broadcast, sample generation and the rate limiter, and a test that
  subscriptions leave no goroutines behind.
- Security headers on the API and the web app, and a Content-Security-Policy that the web
  build writes for the API address it was built with.
- A smoke test script for a deployed stack.
- CI: vulnerability scan, npm audit and a benchmark artifact.
- Nine decision records and a README written for a reviewer.

### Phase 8: security hardening and release

Dependencies and toolchain

- Go minimum raised from 1.22 to 1.25, with the toolchain pinned to Go 1.26.9.
- chi v5.2.3 to v5.3.2, pgx v5.7.4 to v5.11.0 and golang.org/x/text v0.21.0 to v0.41.0,
  which clears every finding of the vulnerability scan in code this program calls.
- The vulnerability scan now fails the build, and every CI job checks that it builds with
  the pinned toolchain. Render is told to use the same toolchain.

Application

- Public demo mode (`PUBLIC_DRILLS`): visitors may start drills without the admin token.
  Public drills are tagged in the API and the web app, have their own cap, and never
  reach Slack. A visitor can halt public drills only.
- Open streams are capped in total and per caller, and a slot is always handed back.
- Reads have their own generous rate limit. Health probes have none.
- The admin token must be at least 32 characters, or the server refuses to start.
- In production the server refuses a database address without `sslmode=require` or stricter.
- `Strict-Transport-Security` on the API in production and on the web app.
- The caller's address is taken from proxy headers only for a configured number of
  trusted proxies (`TRUSTED_PROXY_HOPS`). chi's `RealIP` is no longer used.
- Logs name only the host and the database of the database address, and an unreadable
  address no longer leaks into an error.
- New endpoint `GET /api/v1/config`, and a `public` field on simulations and incidents.

Polish

- Render's health check calls `/healthz`, so a database blip cannot restart the instance.
- The SLO label of a chart always stays inside the plot, clear of the time axis.
- Sparklines of healthy metrics use their own range and line up across a card.
- The status page says "Simulated services" above its overall state, and the incident list
  says what its duration means.
- The README is cut down to a 30 second read, and the guides moved to `docs/`.

Repository

- Every GitHub Action pinned to a full commit, Dependabot for Go, npm and Actions.
- SECURITY.md, an MIT license, issue templates and this changelog.

### Known limits

- The demo runs on free plans: the API sleeps after 15 minutes without traffic, and the
  first request after that is slow.
- Rate limits and stream counts live in the memory of one process.
- The peak value in "Recently finished" is what the browser saw, so it is empty after a reload.
