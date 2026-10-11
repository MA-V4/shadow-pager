# Deploying

The whole stack runs on free plans: Neon for Postgres, Render for the API, Vercel for the
web app. Do the steps in this order, because each one needs an address from the one before.

## 1. Neon (database)

1. Create a project in the Neon console.
2. Open **Connect**, switch **Connection pooling** on, and copy the connection string.
   This is your `DATABASE_URL`. It must contain `sslmode=require`.

## 2. Render (API)

1. In Render choose **New**, then **Blueprint**, and pick this repository. Render reads
   `render.yaml` and creates the web service.
2. Fill in the variables that Render asks for (the ones marked `sync: false`):

| Variable                    | Value                                                              |
|-----------------------------|--------------------------------------------------------------------|
| `DATABASE_URL`              | The pooled Neon connection string from step 1.                     |
| `ADMIN_TOKEN`               | A random text of at least 32 characters, for example from `openssl rand -hex 32`. |
| `CORS_ALLOWED_ORIGINS`      | Your Vercel address from step 3, for example `https://shadow-pager.vercel.app`. Set it after step 3. |
| `SLACK_APP_TOKEN`           | Optional. The `xapp-` token from [Slack setup](#slack-setup).      |
| `SLACK_BOT_TOKEN`           | Optional. The `xoxb-` token from [Slack setup](#slack-setup).      |
| `SLACK_INVITE_USER_IDS`     | Optional. Comma separated Slack member IDs.                        |
| `SLACK_ANNOUNCE_CHANNEL_ID` | Optional. A public Slack channel ID.                               |

   The other variables already have values in `render.yaml`: `APP_ENV=production`,
   `PUBLIC_DRILLS=true`, the limits, `TRUSTED_PROXY_HOPS=1` and `GOTOOLCHAIN`. `PORT` is
   set by Render itself. `HOST` must stay unset on Render.
3. Wait for the first deploy and note the service address, for example
   `https://shadow-pager-api.onrender.com`. Check it with `/healthz` and `/readyz`.

### Health check

Render calls `/healthz`, which only says that the process is up and never touches the
database. A database that is slow, asleep or briefly gone therefore cannot make Render
restart the instance, which would lose the in-memory state for nothing. `/readyz` stays
as it is for people and for monitoring: it also reports whether the engine runs, whether
the server is draining and whether the last database ping worked.

### Go version

Render's native Go runtime always installs the newest stable Go and has no setting to pin
one. `render.yaml` therefore sets `GOTOOLCHAIN` to the version pinned in `go.mod`, which
makes the Go command download and use exactly that toolchain. The build prints
`go version` first, and the server logs `go_version` in its first line, so both logs show
which Go built and runs the binary.

### Checking TRUSTED_PROXY_HOPS

The rate limits and the stream limit are counted per caller address. Behind a proxy the
server only sees the proxy, so it reads the caller from the `X-Forwarded-For` header,
taking the entry that is `TRUSTED_PROXY_HOPS` places from the right. `render.yaml` sets
`1`, which assumes that exactly one proxy adds an address in front of the service. That
is an assumption about Render and not a fact this project can prove, so check it once
after the first deploy:

1. Look up your own public address, for example with `curl https://api.ipify.org`.
2. Use up your write turns on purpose: start six drills in a row from the dashboard, or
   send six `POST /api/v1/simulations` requests. The sixth is refused with 429.
3. In the Render logs find the line `write request rate limited` and read its `client`
   field.
4. If `client` is your own address, `1` is right. If it is some other address, a proxy
   sits one step further out: raise `TRUSTED_PROXY_HOPS` by one, redeploy and check again.

A number that is too small makes many visitors share one bucket. A number that is too
large lets a caller choose the address the server believes. When unsure, the smaller
number is the safe one, because the limit shared by all callers still holds.

### Sleeping

The free Render instance sleeps after 15 minutes without inbound HTTP traffic and loses
what it held in memory, which is why the database matters. Slack slash commands and
buttons do not wake it, because Socket Mode is a connection that the server opens to
Slack, not a request that Slack sends to the server. While the instance sleeps, that
connection is gone and Slack has nowhere to deliver `/shadowpager`. Opening the web app
or calling `/healthz` wakes the instance, and it then reconnects to Slack.

## 3. Vercel (web app)

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

## 4. Check it

1. Run the [smoke test](#smoke-test) against the live addresses.
2. Open the Vercel address. The first load can show "Waking server" for up to a
   minute, because the free Render plan sleeps when idle.
3. Run a drill without a token and see the "public drill" tag. Then open the settings in
   the header, enter the admin token, and run another one, which also posts to Slack.
4. Watch the charts cross the dashed SLO lines, then open `/incidents` and `/status`.

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

A drill started from Slack or with the admin token posts to Slack. A public drill never
does, whatever it breaks.

The bot scopes in the manifest are the smallest set that works:

| Scope             | Why it is needed                                              |
|-------------------|---------------------------------------------------------------|
| `channels:manage` | Create the public incident channel and invite responders.     |
| `channels:join`   | Join the announce channel before posting there.               |
| `chat:write`      | Post and update the incident card, timeline and replies.      |
| `commands`        | Receive the `/shadowpager` slash command.                     |

## Database setup

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

## Smoke test

`scripts/smoke.sh` checks a deployed stack. It needs only bash and curl and works in Git
Bash and on Linux.

    API_URL=https://shadow-pager-api.onrender.com \
    WEB_ORIGIN=https://shadow-pager.vercel.app \
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

## Checks before a release

`make test` runs the Go tests the way CI does, without the race detector, so it also
works on Windows. `make race` adds the race detector, which needs cgo and a C compiler.

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

### CI

Every push and pull request to `main` runs gofmt, `go vet`, `go test -race` with a
Postgres service, `govulncheck`, the four web checks and `npm audit` at high severity.
Every Go job builds with the toolchain named in `go.mod` and fails if any other Go is in
use. A push to `main` also runs the benchmarks and keeps their output.
