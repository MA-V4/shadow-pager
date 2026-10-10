# Shadow-Pager

[![CI](https://github.com/MA-V4/shadow-pager/actions/workflows/ci.yml/badge.svg)](https://github.com/MA-V4/shadow-pager/actions/workflows/ci.yml)

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

## Slack setup

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

## Database setup

Postgres is optional. With no `DATABASE_URL` the server logs one warning and keeps
everything in memory, exactly as before. With it, simulations and incidents survive
restarts, including a Render free tier spin-down, so the buttons on old Slack incident
cards keep working.

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

When a database is configured, `/readyz` also pings it with a two second limit and
reports `"database": "ok"` or `"unreachable"`. `/healthz` never touches the database.

### Running the database tests

The storage tests need a real Postgres and are skipped when `TEST_DATABASE_URL` is not
set. With Docker:

    docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:17
    export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
    go test -count=1 ./...

`TEST_DATABASE_URL` must be written as a URL. Every test creates its own schema named
`test_...` and drops it afterwards, so tests never see the rows of another test. Do not
point it at a database you care about.

## Screenshots

### Dashboard

### Incidents

### Incident detail

### Status page

## Frontend

The web app lives in `web/`: Vite, React, TypeScript, Tailwind and shadcn/ui style
components. It has three areas:

| URL              | What it shows                                                      |
|------------------|--------------------------------------------------------------------|
| `/`              | Dashboard: inject a failure, live charts with SLO lines, halt.     |
| `/incidents`     | Every incident, newest first, with a detail page and its timeline. |
| `/status`        | Public status page: overall banner, services, active incidents.    |

### Run it locally

Start the API first (see "Run locally" above), then in a second terminal:

    cd web
    npm install
    npm run dev

Open http://localhost:5173. The dev server forwards every `/api` call to
`http://localhost:8080`, so `VITE_API_URL` stays empty in local development. The four
checks that CI runs are:

    npm run typecheck
    npm run lint
    npm test
    npm run build

## API protection

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

## Deploy guide

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
2. Set **Root Directory** to `web`. Vercel then finds `web/vercel.json`, which also makes
   addresses like `/incidents/inc_123` work when the page is refreshed.
3. Add one environment variable:

| Variable       | Value                                                                    |
|----------------|--------------------------------------------------------------------------|
| `VITE_API_URL` | The Render address from step 2, without a slash at the end.              |

4. Deploy, then copy the Vercel address into `CORS_ALLOWED_ORIGINS` on Render and let
   Render redeploy. Without this the browser blocks every call from the web app.

### 4. Check it

1. Open the Vercel address. The first load can show "Waking up the server" for up to a
   minute, because the free Render plan sleeps when idle.
2. On the dashboard, enter the admin token if you set one, and inject a failure.
3. Watch the charts cross the dashed SLO lines, then open `/incidents` and `/status`.
