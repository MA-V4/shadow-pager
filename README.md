# Shadow-Pager

Inject a fake infrastructure failure, watch an incident run its course in Slack, and see the telemetry live on a status page.

[![CI](https://github.com/MA-V4/shadow-pager/actions/workflows/ci.yml/badge.svg)](https://github.com/MA-V4/shadow-pager/actions/workflows/ci.yml)

## Live demo

The links are placeholders until the project is deployed.

| What        | Link                                              |
|-------------|---------------------------------------------------|
| Dashboard   | `https://<your-app>.vercel.app/`                  |
| Status page | `https://<your-app>.vercel.app/status`            |
| API         | `https://<your-api>.onrender.com/api/v1/status`   |

The API runs on a free plan that sleeps when idle, so the first load can take up to a minute.

The demo has two kinds of drills.

- **Public drills.** Anyone can start a drill from the dashboard without a token. It
  shows up on the dashboard and the status page, and it declares an incident like any
  other. It is tagged "public drill" and it never posts to Slack. At most five public
  drills run at once, each caller may start five in a row and then one every 30 seconds,
  and a visitor can halt public drills only.
- **Admin drills.** A drill started with the admin token (entered under the settings icon
  in the header) also runs the Slack side: a channel per incident, an incident card with
  buttons, and a timeline. This is what the recording below shows, because the Slack
  workspace of the demo is private.

![An admin drill running through its incident lifecycle in Slack](docs/images/slack-demo.gif)

The recording is a placeholder and is not in the repository yet.

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
    MANAGER ==>|declared, status changed, admin drills only| SLACK
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
   switched on, and for a drill that is not public, a channel is created and an incident
   card is posted.
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

## Running locally

Requires Go 1.25 or newer. `go.mod` pins the toolchain to Go 1.26.9, and an older Go
downloads that toolchain by itself on the first build. Slack and Postgres are not needed.

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

| Path                                 | Purpose                                                    |
|--------------------------------------|------------------------------------------------------------|
| `GET /healthz`                       | Liveness. The process is up. Never checks dependencies, never rate limited. |
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

### Environment variables

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
| `TRUSTED_PROXY_HOPS`        | `0`           | How many proxies in front of the server may report the caller's address. `1` on Render. |
| `SLACK_APP_TOKEN`           | empty         | Slack app-level token, starts with `xapp-`. Secret. |
| `SLACK_BOT_TOKEN`           | empty         | Slack bot token, starts with `xoxb-`. Secret. |
| `SLACK_INVITE_USER_IDS`     | empty         | Comma separated Slack member IDs to invite into incident channels. |
| `SLACK_ANNOUNCE_CHANNEL_ID` | empty         | Slack channel ID where new incidents are announced. |

Two more are read outside the server: `TEST_DATABASE_URL` by the storage tests, and
`VITE_API_URL` by the web build. `render.yaml` also sets `GOTOOLCHAIN` for the build.

### API protection

The API is meant to be shown in public, so it is guarded in layers.

- **Write limit.** Injecting and halting are limited per caller IP address with a token
  bucket: `RATE_LIMIT_BURST` requests in a row, then one more every `RATE_LIMIT_REFILL`.
  A second, shared bucket caps what all callers can do together. Slack users get the same
  limit on `/shadowpager inject` and `/shadowpager halt`.
- **Read limit.** Reads have their own, generous bucket per caller: 120 in a row, then
  four per second. It is separate from the write limit. The health probes are exempt.
- **Stream limit.** At most `STREAM_MAX_TOTAL` live streams are open at once, and at most
  `STREAM_MAX_PER_IP` per caller. A slot is handed back when its stream ends for any reason.
- **Admin token.** When `ADMIN_TOKEN` is set, injecting and halting need the header
  `Authorization: Bearer <token>`. The token must be at least 32 characters, or the server
  refuses to start. When no token is set, writes stay open but rate limited.
- **Public drills.** With `PUBLIC_DRILLS=true`, a request without a token is let through
  as a visitor. Its drill is marked public, counts against `PUBLIC_MAX_ACTIVE`, and never
  reaches Slack. A visitor can only halt public drills. A wrong token is refused with 401.
- **Caller address.** Limits are keyed by IP address. The address comes from the
  `X-Forwarded-For` header only for as many proxies as `TRUSTED_PROXY_HOPS` says, counted
  from the right, so a caller cannot invent one. With `0` the header is ignored.
- **CORS.** Only the websites listed in `CORS_ALLOWED_ORIGINS` may call the API from a
  browser.
- **Security headers.** Every API answer carries `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, a `Referrer-Policy` and a `Content-Security-Policy` that loads
  nothing. In production it also carries `Strict-Transport-Security` for one year. The web
  app sends the same headers from `web/vercel.json`, and its build writes a policy into
  the page that allows only its own files and the API address it was built with.
- **Secrets in logs.** Tokens and the `Authorization` header are never logged. Of the
  database address only the host and the database name are logged.

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

A drill started from Slack or with the admin token posts to Slack. A public drill never
does, whatever it breaks.

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
`6543`, and add `?sslmode=require` if it is not there. The direct connection on port
`5432` only has an IPv6 address on the free tier, which Render cannot reach.

With `APP_ENV=production` the server refuses to start unless `DATABASE_URL` has
`sslmode=require`, `verify-ca` or `verify-full`. A missing sslmode is refused too.

Both poolers share one database connection between many clients, which breaks named
prepared statements. The server therefore uses a pgx mode that never creates them, so
the pooled strings above work as they are.

What is stored: one row per simulation, one row per incident with its Slack channel and
card message, and the incident timeline. Raw metric samples are never stored.

When a database is configured, `/readyz` also reports `"database": "ok"` or
`"unreachable"`. The probe never waits for the database: it reads the answer of the last
ping, and when that answer is older than 60 seconds it starts a new ping in the
background. `/healthz` never touches the database.

## Deploying

The whole stack runs on free plans: Neon for Postgres, Render for the API, Vercel for the
web app. Do the steps in this order, because each one needs an address from the one before.

### 1. Neon (database)

1. Create a project in the Neon console.
2. Open **Connect**, switch **Connection pooling** on, and copy the connection string.
   This is your `DATABASE_URL`. It must contain `sslmode=require`.

### 2. Render (API)

1. In Render choose **New**, then **Blueprint**, and pick this repository. Render reads
   `render.yaml` and creates the web service.
2. Fill in the variables that Render asks for (the ones marked `sync: false`):

| Variable                    | Value                                                              |
|-----------------------------|--------------------------------------------------------------------|
| `DATABASE_URL`              | The pooled Neon connection string from step 1.                     |
| `ADMIN_TOKEN`               | A random text of at least 32 characters, for example from `openssl rand -hex 32`. |
| `CORS_ALLOWED_ORIGINS`      | Your Vercel address from step 3, for example `https://shadow-pager.vercel.app`. Set it after step 3. |
| `SLACK_APP_TOKEN`           | Optional. The `xapp-` token from "Slack setup".                    |
| `SLACK_BOT_TOKEN`           | Optional. The `xoxb-` token from "Slack setup".                    |
| `SLACK_INVITE_USER_IDS`     | Optional. Comma separated Slack member IDs.                        |
| `SLACK_ANNOUNCE_CHANNEL_ID` | Optional. A public Slack channel ID.                               |

   The other variables already have values in `render.yaml`: `APP_ENV=production`,
   `PUBLIC_DRILLS=true`, the limits, `TRUSTED_PROXY_HOPS=1` and `GOTOOLCHAIN`. `PORT` is
   set by Render itself. `HOST` must stay unset on Render.
3. Wait for the first deploy and note the service address, for example
   `https://shadow-pager-api.onrender.com`. Check it with `/healthz` and `/readyz`.

Render's native Go runtime always installs the newest stable Go and has no setting to pin
one. `render.yaml` therefore sets `GOTOOLCHAIN` to the version pinned in `go.mod`, which
makes the Go command download and use exactly that toolchain. The build prints
`go version` first, and the server logs `go_version` in its first line, so both logs show
which Go built and runs the binary.

The free Render instance sleeps after 15 minutes without inbound HTTP traffic and loses
what it held in memory, which is why the database matters. Slack slash commands and
buttons do not wake it, because Socket Mode is a connection that the server opens to
Slack, not a request that Slack sends to the server. While the instance sleeps, that
connection is gone and Slack has nowhere to deliver `/shadowpager`. Opening the web app
or calling `/healthz` wakes the instance, and it then reconnects to Slack.

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
   No secret belongs in the web app's environment: everything named `VITE_` ends up in
   the files that browsers download.
4. Deploy, then copy the Vercel address into `CORS_ALLOWED_ORIGINS` on Render and let
   Render redeploy. Without this the browser blocks every call from the web app.

### 4. Check it

1. Run the smoke test against the live addresses (see "Smoke test" below).
2. Open the Vercel address. The first load can show "Waking server" for up to a
   minute, because the free Render plan sleeps when idle.
3. Run a drill without a token and see the "public drill" tag. Then open the settings in
   the header, enter the admin token, and run another one, which also posts to Slack.
4. Watch the charts cross the dashed SLO lines, then open `/incidents` and `/status`.

## Security

How to report a problem, what is in scope, and which risks are accepted on purpose for a
public demo are written down in [SECURITY.md](SECURITY.md). In short: the demo instance
is shared by everyone, public drills are open by design, and no secret ever reaches the
browser bundle or the logs.

Dependencies are checked on every push: `govulncheck` for Go, `npm audit` for the web
app, and both fail the build on a finding. Dependabot proposes updates once a week, and
every GitHub Action is pinned to a full commit.

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

To scan for known vulnerabilities the way CI does:

    go run golang.org/x/vuln/cmd/govulncheck@latest ./...

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
`/readyz`, `/api/v1/status`, the security headers (and `Strict-Transport-Security` when
the address is HTTPS), `/api/v1/config`, and a CORS preflight from the web origin.

Only when `ADMIN_TOKEN` is given, it starts a tiny 10 second drill with the token, finds
it in the list, checks that a request without the token cannot halt it, and halts it with
the token. Then it sends a write without the token: on a server with public drills on,
that must start a public drill, which the script halts again; on a server with them off,
it must be refused.

The drills are too weak to break an SLO, so they declare no incident and post nothing to
Slack. Every check prints `PASS`, `FAIL` or `SKIP`, and the script exits with a non-zero
code when anything failed. The token is never printed.

### CI

Every push and pull request to `main` runs gofmt, `go vet`, `go test -race` with a
Postgres service, `govulncheck`, the four web checks and `npm audit` at high severity.
Every Go job builds with the toolchain named in `go.mod` and fails if any other Go is in
use. A push to `main` also runs the benchmarks and keeps their output.

## License

[MIT](LICENSE)
