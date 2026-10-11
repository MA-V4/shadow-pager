# Shadow-Pager

Inject a fake infrastructure failure, watch an incident run its course in Slack, and see the telemetry live on a status page.

[![CI](https://github.com/MA-V4/shadow-pager/actions/workflows/ci.yml/badge.svg)](https://github.com/MA-V4/shadow-pager/actions/workflows/ci.yml)

## Live demo

These are placeholders until the project is deployed.

| What        | Link                                              |
|-------------|---------------------------------------------------|
| Dashboard   | `https://<your-app>.vercel.app/`                  |
| Status page | `https://<your-app>.vercel.app/status`            |
| API         | `https://<your-api>.onrender.com/api/v1/status`   |

The API runs on a free plan that sleeps when idle, so the first load can take up to a minute.

## Screenshots

The image files are placeholders and are not in the repository yet.

| Dashboard | Incident detail |
|-----------|-----------------|
| ![Dashboard with a breaching service](docs/images/dashboard.png) | ![Incident detail with its timeline](docs/images/incident-detail.png) |

| Incidents | Status page |
|-----------|-------------|
| ![Incident list](docs/images/incidents.png) | ![Public status page](docs/images/status.png) |

## Architecture

```mermaid
flowchart LR
    subgraph Browser
        WEB[Web app<br/>dashboard and incidents]
        STATUS[Status page]
    end

    subgraph Server["Go server"]
        API[HTTP API<br/>/api/v1]
        SSE[SSE stream<br/>/api/v1/stream]
        ENGINE[Chaos engine]
        MANAGER[Incident manager]
        AGG[Status aggregator]
        SLACK[Slack adapter]
        RECORDER[Storage recorder]
    end

    SLACKAPP[(Slack workspace)]
    DB[(Postgres)]

    WEB ==>|start and halt drills| API
    API ==>|inject, halt, list| ENGINE
    ENGINE ==>|metric samples| SSE
    SSE ==>|live samples| WEB
    ENGINE ==>|samples and lifecycle events| MANAGER
    ENGINE ==>|samples| AGG
    ENGINE ==>|lifecycle events| RECORDER
    MANAGER ==>|incidents| API
    MANAGER ==>|open incidents| AGG
    AGG ==>|status snapshot| API
    API ==>|status and incidents| STATUS
    API ==>|incidents| WEB
    MANAGER ==>|declared, status changed| SLACK
    SLACK ==>|channel, card, timeline| SLACKAPP
    SLACKAPP ==>|buttons and /shadowpager| SLACK
    SLACK ==>|acknowledge, mitigate, resolve| MANAGER
    SLACK ==>|inject and halt| ENGINE
    RECORDER ==>|simulations| DB
    MANAGER ==>|incidents and timeline| DB
    DB ==>|restore at startup| ENGINE
    DB ==>|restore at startup| MANAGER
```

Slack and Postgres are both optional. Without them the server runs in memory and still
declares incidents.

## How it works

1. **Inject.** A drill is started from the dashboard, the API or `/shadowpager` in Slack.
   It names a service, a failure mode, a severity, an intensity and a duration.
2. **Simulate.** Once per second the engine makes a metric sample for every running
   drill: p99 latency, error rate, CPU and memory. Each failure mode is a table row of
   impact and curve shape, not separate code.
3. **Declare.** The first sample that breaks an SLO declares an incident. With Slack
   switched on, a channel is created and an incident card is posted.
4. **Respond.** The buttons on the card move the incident forward: acknowledge, mitigate
   (which halts the drill), resolve. Statuses only move forward.
5. **Observe.** Samples stream to the dashboard over Server-Sent Events, and the public
   status page shows each service as operational, degraded or in a major outage.

## Go design decisions

Each decision has a short record with its context, its consequences and the alternative
that was rejected.

| Decision | Record |
|----------|--------|
| Interfaces are declared by the package that uses them | [ADR 1](docs/adr/0001-consumer-side-interfaces.md) |
| Server-Sent Events for the dashboard, not WebSockets | [ADR 2](docs/adr/0002-sse-over-websockets.md) |
| Broadcast never blocks and slow subscribers drop samples | [ADR 3](docs/adr/0003-non-blocking-broadcast.md) |
| Shutdown drains HTTP before it stops the engine | [ADR 4](docs/adr/0004-shutdown-ordering.md) |
| A pgx exec mode that works behind transaction poolers | [ADR 5](docs/adr/0005-pooler-safe-pgx-exec-mode.md) |
| Readiness reads a remembered ping, so Neon can sleep | [ADR 6](docs/adr/0006-passive-readiness.md) |
| Persistence is driven by events, not by calls in the engine | [ADR 7](docs/adr/0007-events-drive-persistence.md) |
| The rate limiter and CORS are written by hand | [ADR 8](docs/adr/0008-hand-written-rate-limiter-and-cors.md) |
| Slack and storage are optional | [ADR 9](docs/adr/0009-optional-slack-and-storage.md) |

Where things live:

    cmd/server          configuration, wiring, health probes, graceful shutdown
    internal/chaos      simulation engine, failure shapes, metric generators
    internal/incident   incident lifecycle and its notification and save queues
    internal/slack      Socket Mode listener, Block Kit card, slash command
    internal/storage    Postgres pool, migrations, repositories, recorder
    internal/api        HTTP handlers, SSE stream, CORS, security headers, write protection
    internal/status     per service status for the public page
    internal/ratelimit  token bucket per caller
    web                 the web app (Vite, React, TypeScript, Tailwind)
    docs/adr            the decision records
    scripts             the smoke test for a deployed stack

## Running locally

Requires Go 1.22 or newer. Slack and Postgres are not needed.

    cp .env.example .env              # optional, the defaults work as they are
    set -a; source .env; set +a       # the binary reads real environment variables
    go run ./cmd/server
    curl localhost:8080/healthz
    curl localhost:8080/readyz

Then start the web app in a second terminal. It needs Node 20.19 or newer.

    cd web
    npm install
    npm run dev

Open http://localhost:5173. The dev server forwards every `/api` call to
`http://localhost:8080`, so `VITE_API_URL` stays empty in local development.

| URL          | What it shows                                                    |
|--------------|------------------------------------------------------------------|
| `/`          | Dashboard: run a drill, live charts with SLO lines, halt.        |
| `/incidents` | Every incident, open ones first, with a detail page and timeline. |
| `/status`    | Public status page: overall state, services, active incidents.   |

Stop the server with Ctrl+C and watch the shutdown sequence in the logs.

### Endpoints

| Path                              | Purpose                                                    |
|-----------------------------------|------------------------------------------------------------|
| `GET /healthz`                    | Liveness. The process is up. Never checks dependencies.    |
| `GET /readyz`                     | Readiness. Engine ticking, not draining, database reachable. |
| `GET /api/v1/simulations`         | List simulations, newest first.                            |
| `POST /api/v1/simulations`        | Start a drill.                                             |
| `GET /api/v1/simulations/{id}`    | One simulation.                                            |
| `POST /api/v1/simulations/{id}/halt` | Stop a running drill.                                   |
| `GET /api/v1/stream`              | Live metric samples as Server-Sent Events.                 |
| `GET /api/v1/incidents`           | List incidents, newest first.                              |
| `GET /api/v1/incidents/{id}`      | One incident with its timeline.                            |
| `GET /api/v1/status`              | Public status of every service.                            |
| `GET /api/v1/slo`                 | The SLO limits that count as a breach.                     |

### API protection

The API is meant to be shown in public, so the endpoints that change something are guarded.

- **Rate limit.** Injecting and halting are limited per caller IP address with a token
  bucket: `RATE_LIMIT_BURST` requests in a row (default 5), then one more every
  `RATE_LIMIT_REFILL` (default 30s). A limited request gets `429` with a `Retry-After`
  header. A second, shared bucket caps what all callers can do together. Slack users get
  the same limit on `/shadowpager inject` and `/shadowpager halt`.
- **Admin token.** When `ADMIN_TOKEN` is set, injecting and halting need the header
  `Authorization: Bearer <token>`. When it is not set they stay open, but rate limited.
  Reading is always open.
- **CORS.** Only the websites listed in `CORS_ALLOWED_ORIGINS` (comma separated) may call
  the API from a browser.
- **Security headers.** Every API answer carries `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, a `Referrer-Policy` and a `Content-Security-Policy` that loads
  nothing. The web app sends the same headers from `web/vercel.json`, and its build writes
  a policy into the page that allows only its own files and the API address it was built with.

### Slack setup

Slack is optional. With no Slack tokens the server logs one warning, still declares
incidents, and serves them at `/api/v1/incidents`. To see the full lifecycle in Slack:

1. Open https://api.slack.com/apps, choose **Create New App**, then **From a manifest**,
   pick your workspace, and paste the contents of `deploy/slack-manifest.yaml`.
2. Open **Install App**, install it to the workspace, and copy the **Bot User OAuth Token**.
   It starts with `xoxb-`.
3. Open **Basic Information**, find **App-Level Tokens**, generate a token with the
   `connections:write` scope, and copy it. It starts with `xapp-`. The manifest cannot
   create this token for you.
4. Put both tokens in your `.env` file as `SLACK_APP_TOKEN` and `SLACK_BOT_TOKEN`.
   Both must be set together; one without the other stops the server at startup.
5. Optional: set `SLACK_INVITE_USER_IDS` to a comma separated list of member IDs
   (in Slack, open a profile, choose the three dots, then **Copy member ID**) so those
   people are invited into every incident channel. Set `SLACK_ANNOUNCE_CHANNEL_ID` to a
   public channel ID to get a one line announcement with a link for each new incident.
6. Start the server and look for `connected to slack` in the logs.

Then, in any Slack channel:

    /shadowpager inject latency_spike payments-api 0.9 2m
    /shadowpager list
    /shadowpager halt <simulation_id>
    /shadowpager help

When the simulation breaks its SLO, the bot creates a public channel named like
`inc-20260102-payments-api-1a2b`, posts an incident card, and the buttons on the card
move the incident along: **Acknowledge**, **Mitigate (halt simulation)**, **Resolve**.
A simulation that ends by itself marks the incident as mitigated, and a person still
has to resolve it.

The bot scopes in the manifest are the smallest set that works:

| Scope             | Why it is needed                                              |
|-------------------|---------------------------------------------------------------|
| `channels:manage` | Create the public incident channel and invite responders.     |
| `channels:join`   | Join the announce channel before posting there.               |
| `chat:write`      | Post and update the incident card, timeline and replies.      |
| `commands`        | Receive the `/shadowpager` slash command.                     |

### Database setup

Postgres is optional. With no `DATABASE_URL` the server logs one warning and keeps
everything in memory. With it, simulations and incidents survive restarts, including a
Render free tier spin-down, so the buttons on old Slack incident cards keep working.

Set one variable, `DATABASE_URL`, and start the server. The tables are created and
updated automatically at startup; look for `migration applied` in the logs.

**Neon.** In the Neon console open **Connect**, switch **Connection pooling** on, and copy
the connection string. The pooled host name contains `-pooler`. Keep `sslmode=require`.

**Supabase.** Open **Connect** and copy the **Transaction pooler** string, which uses port
`6543`. The direct connection on port `5432` only has an IPv6 address on the free tier,
which Render cannot reach.

Both poolers share one database connection between many clients, which breaks named
prepared statements. The server therefore uses a pgx mode that never creates them, so
the pooled strings above work as they are.

What is stored: one row per simulation, one row per incident with its Slack channel and
card message, and the incident timeline. Raw metric samples are never stored.

When a database is configured, `/readyz` also reports `"database": "ok"` or
`"unreachable"` from a ping that is remembered for 60 seconds. `/healthz` never touches
the database.

## Deploying

The whole stack runs on free plans: Neon for Postgres, Render for the API, Vercel for the
web app. Do the steps in this order, because each one needs an address from the one before.

### 1. Neon (database)

1. Create a project in the Neon console.
2. Open **Connect**, switch **Connection pooling** on, and copy the connection string.
   This is your `DATABASE_URL`.

### 2. Render (API)

1. In Render choose **New**, then **Blueprint**, and pick this repository. Render reads
   `render.yaml` and creates the web service.
2. Fill in the variables that Render asks for (the ones marked `sync: false`):

| Variable                    | Value                                                              |
|-----------------------------|--------------------------------------------------------------------|
| `DATABASE_URL`              | The pooled Neon connection string from step 1.                     |
| `ADMIN_TOKEN`               | A long random text. Leave empty to keep writes open.               |
| `CORS_ALLOWED_ORIGINS`      | Your Vercel address from step 3, for example `https://shadow-pager.vercel.app`. Set it after step 3. |
| `SLACK_APP_TOKEN`           | Optional. The `xapp-` token from "Slack setup".                    |
| `SLACK_BOT_TOKEN`           | Optional. The `xoxb-` token from "Slack setup".                    |
| `SLACK_INVITE_USER_IDS`     | Optional. Comma separated Slack member IDs.                        |
| `SLACK_ANNOUNCE_CHANNEL_ID` | Optional. A public Slack channel ID.                               |

   `APP_ENV`, `LOG_LEVEL`, `RATE_LIMIT_BURST` and `RATE_LIMIT_REFILL` already have values
   in `render.yaml`. `PORT` is set by Render itself. `HOST` must stay unset on Render.
3. Wait for the first deploy and note the service address, for example
   `https://shadow-pager-api.onrender.com`. Check it with `/healthz` and `/readyz`.

### 3. Vercel (web app)

1. In Vercel choose **Add New**, then **Project**, and import this repository.
2. Set **Root Directory** to `web`. Vercel then finds `web/vercel.json`, which sets the
   security headers and makes addresses like `/incidents/inc_123` work when the page is
   refreshed.
3. Add one environment variable:

| Variable       | Value                                                                    |
|----------------|--------------------------------------------------------------------------|
| `VITE_API_URL` | The Render address from step 2, without a slash at the end.              |

   The build reads this address twice: the app calls it, and the page's
   Content-Security-Policy allows it. If the address changes, redeploy the web app.
4. Deploy, then copy the Vercel address into `CORS_ALLOWED_ORIGINS` on Render and let
   Render redeploy. Without this the browser blocks every call from the web app.

### 4. Check it

1. Run the smoke test against the live addresses (see "Smoke test" below).
2. Open the Vercel address. The first load can show "Waking server" for up to a
   minute, because the free Render plan sleeps when idle.
3. On the dashboard, open the settings in the header and enter the admin token if you set
   one, then run a drill.
4. Watch the charts cross the dashed SLO lines, then open `/incidents` and `/status`.

## Testing

### Go

    gofmt -l .                  # prints nothing when every file is formatted
    go vet ./...
    go test -count=1 ./...      # CI adds -race on Linux

The storage tests need a real Postgres and are skipped when `TEST_DATABASE_URL` is not
set. With Docker:

    docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:17
    export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
    go test -count=1 ./...

`TEST_DATABASE_URL` must be written as a URL. Every test creates its own schema named
`test_...` and drops it afterwards, so tests never see the rows of another test. Do not
point it at a database you care about.

### Benchmarks

    go test -run '^$' -bench . -benchmem ./internal/chaos ./internal/ratelimit

They cover `Engine.broadcast` with 1, 10 and 100 subscribers, the path where every
subscriber is full, `ProfileGenerator.Sample`, and the rate limiter under parallel load.
On a 12th generation Core i5 a broadcast to 100 subscribers takes about 2.7 microseconds
and allocates nothing. CI keeps the benchmark output of every push to `main` as a build
artifact named `go-benchmarks`.

`internal/chaos/leak_test.go` checks that `Subscribe` and `SubscribeEvents` leave no
goroutine behind after their context is cancelled.

### Web

    cd web
    npm run typecheck
    npm run lint
    npm test
    npm run build

### Smoke test

`scripts/smoke.sh` checks a deployed stack. It needs only bash and curl and works in Git
Bash and on Linux.

    API_URL=https://<your-api>.onrender.com \
    WEB_ORIGIN=https://<your-app>.vercel.app \
    ADMIN_TOKEN=<your token> \
    bash scripts/smoke.sh

It waits up to 90 seconds for a sleeping instance to wake, then checks `/healthz`,
`/readyz`, `/api/v1/status`, the security headers and a CORS preflight from the web
origin. Only when `ADMIN_TOKEN` is given, it starts a tiny 10 second drill, finds it in
the list, halts it, and checks that a write without the token is refused. The drill is
too weak to break an SLO, so it declares no incident and posts nothing to Slack. Every
check prints `PASS`, `FAIL` or `SKIP`, and the script exits with a non-zero code when
anything failed. The token is never printed.

### CI

Every push and pull request to `main` runs gofmt, `go vet`, `go test -race` with a
Postgres service, the four web checks and `npm audit` at high severity. A separate job
runs `govulncheck`; it reports its findings but does not block the build yet.
