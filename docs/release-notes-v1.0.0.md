# Shadow-Pager v1.0.0

Shadow-Pager is an incident simulation engine. You inject a fake infrastructure failure,
an incident runs its course in Slack, and the telemetry streams live to a dashboard and a
public status page.

## What is in this release

- **Chaos engine.** Five failure modes (latency spike, error rate surge, memory leak, CPU
  saturation, database pool exhaustion), each a row of data and not separate code.
- **Incidents.** Declared on the first SLO breach, then acknowledged, mitigated and
  resolved, moving forward only.
- **Slack.** Socket Mode, a channel per incident, an incident card with buttons, and the
  `/shadowpager` slash command.
- **Web app.** A dashboard with live charts, an incident list with a flight recorder
  timeline, and a public status page. Light and dark, from 360 pixels up.
- **Persistence.** Postgres behind a pooler, restored at startup. Optional.
- **Public demo mode.** Visitors can run drills without a token. Those drills are tagged
  as public, capped, and never post to Slack.

## Built to be read

- Three Go dependencies: chi, slack-go and pgx. The rate limiter, CORS and the client
  address handling are written by hand.
- Nine short decision records in `docs/adr`.
- Benchmarks, a goroutine leak test, and a smoke test for the deployed stack.

## Security

- No known vulnerabilities in called code, checked by `govulncheck` on every push.
- Rate limits on writes, reads and open streams. An admin token of at least 32 characters.
- TLS required for the database in production. HSTS, a strict Content-Security-Policy
  and no secrets in the logs or the browser bundle.
- See `SECURITY.md` for how to report a problem and which risks are accepted on purpose.

## Running it

    go run ./cmd/server
    cd web && npm install && npm run dev

No Slack workspace and no database are needed to try it. The README has the full deploy
guide for Neon, Render and Vercel.

## Known limits

- The demo runs on free plans, so the first request after 15 idle minutes is slow.
- Slack slash commands do not wake a sleeping instance.
- Rate limits live in the memory of one process.

The full list of changes is in `CHANGELOG.md`.
