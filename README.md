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
