# Configuration

Everything the server can be told, and everything it answers. For how to put it online,
see [deploy.md](deploy.md). For how it protects itself, see [security.md](security.md).

## How it works

1. **Inject.** A drill is started from the dashboard, the API or `/shadowpager` in Slack.
   It names a service, a failure mode, a severity, an intensity and a duration.
2. **Simulate.** Once per second the engine makes a metric sample for every running
   drill: p99 latency, error rate, CPU and memory. Each failure mode is a table row of
   impact and curve shape, not separate code.
3. **Declare.** The first sample that breaks an SLO declares an incident. With Slack
   switched on, and for a drill that is not public, a channel is created and an incident
   card is posted.
4. **Respond.** The buttons on the card move the incident forward: acknowledge, mitigate
   (which halts the drill), resolve. Statuses only move forward.
5. **Observe.** Samples stream to the dashboard over Server-Sent Events, and the public
   status page shows each service as operational, degraded or in a major outage.

## Where things live

    cmd/server          configuration, wiring, health probes, graceful shutdown
    internal/api        HTTP handlers, SSE stream, CORS, security headers, limits, public drills
    internal/chaos      simulation engine, failure shapes, metric generators
    internal/incident   incident lifecycle and its notification and save queues
    internal/ratelimit  token bucket per caller
    internal/slack      Socket Mode listener, Block Kit card, slash command
    internal/status     per service status for the public page
    internal/storage    Postgres pool, migrations, repositories, recorder
    web                 the web app (Vite, React, TypeScript, Tailwind)
    docs/adr            the decision records
    scripts             the smoke test for a deployed stack

## Web pages

| URL          | What it shows                                                    |
|--------------|------------------------------------------------------------------|
| `/`          | Dashboard: run a drill, live charts with SLO lines, halt.        |
| `/incidents` | Every incident, open ones first, with a detail page and timeline. |
| `/status`    | Public status page: overall state, services, active incidents.   |

## Endpoints

| Path                                 | Purpose                                                    |
|--------------------------------------|------------------------------------------------------------|
| `GET /healthz`                       | Liveness. The process is up. Never checks dependencies, never rate limited. Render's health check calls this one. |
| `GET /readyz`                        | Readiness. 200 when the engine ticks, the server is not draining and the last database ping worked. 503 otherwise. Never rate limited. |
| `GET /api/v1/simulations`            | List simulations, newest first. `?limit=` defaults to 50 and is capped at 200. |
| `POST /api/v1/simulations`           | Start a drill.                                             |
| `GET /api/v1/simulations/{id}`       | One simulation.                                            |
| `POST /api/v1/simulations/{id}/halt` | Stop a running drill.                                      |
| `GET /api/v1/stream`                 | Live metric samples as Server-Sent Events. `?simulation_id=` keeps one simulation. |
| `GET /api/v1/incidents`              | List incidents, newest first, with the same `?limit=`.     |
| `GET /api/v1/incidents/{id}`         | One incident with its timeline.                            |
| `GET /api/v1/status`                 | Public status of every service.                            |
| `GET /api/v1/slo`                    | The SLO limits that count as a breach.                     |
| `GET /api/v1/config`                 | Whether public drills are on.                              |

Every error has the body `{"error": "message"}`. A request over a limit gets `429` with a
`Retry-After` header.

## Environment variables

The server reads every setting from the environment. All of them are optional.

| Variable                    | Default       | Meaning |
|-----------------------------|---------------|---------|
| `HOST`                      | empty         | Address to listen on. Empty means every network. Use `127.0.0.1` locally. |
| `PORT`                      | `8080`        | Port to listen on. Render sets it. |
| `APP_ENV`                   | `development` | `production` switches on the TLS check for the database and the HSTS header. |
| `LOG_LEVEL`                 | `info`        | `debug`, `info`, `warn` or `error`. |
| `SHUTDOWN_TIMEOUT`          | `20s`         | How long a graceful shutdown may take. |
| `CHAOS_TICK_INTERVAL`       | `1s`          | How often the engine makes samples. |
| `CHAOS_MAX_ACTIVE`          | `10`          | How many drills may run at once. |
| `DATABASE_URL`              | empty         | Postgres connection string. Empty keeps everything in memory. Secret. |
| `ADMIN_TOKEN`               | empty         | Bearer token for writes, at least 32 characters. Secret. |
| `RATE_LIMIT_BURST`          | `5`           | Writes one caller may do in a row. |
| `RATE_LIMIT_REFILL`         | `30s`         | How long a caller waits to earn one more write. |
| `CORS_ALLOWED_ORIGINS`      | empty         | Comma separated websites that may call the API from a browser. |
| `PUBLIC_DRILLS`             | `false`       | `true` lets visitors start drills without the token. |
| `PUBLIC_MAX_ACTIVE`         | `5`           | How many public drills may run at once. Must be smaller than `CHAOS_MAX_ACTIVE`. |
| `STREAM_MAX_TOTAL`          | `100`         | How many live streams may be open in total. |
| `STREAM_MAX_PER_IP`         | `5`           | How many live streams one caller may hold open. |
| `TRUSTED_PROXY_HOPS`        | `0`           | How many proxies in front of the server may report the caller's address. `render.yaml` sets `1`; see [how to check it](deploy.md#checking-trusted_proxy_hops). |
| `SLACK_APP_TOKEN`           | empty         | Slack app-level token, starts with `xapp-`. Secret. |
| `SLACK_BOT_TOKEN`           | empty         | Slack bot token, starts with `xoxb-`. Secret. |
| `SLACK_INVITE_USER_IDS`     | empty         | Comma separated Slack member IDs to invite into incident channels. |
| `SLACK_ANNOUNCE_CHANNEL_ID` | empty         | Slack channel ID where new incidents are announced. |

Two more are read outside the server: `TEST_DATABASE_URL` by the storage tests, and
`VITE_API_URL` by the web build. `render.yaml` also sets `GOTOOLCHAIN` for the build.
