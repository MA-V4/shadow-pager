# Security policy

## Supported versions

Only the latest release gets security fixes.

| Version | Supported |
|---------|-----------|
| 1.0.x   | Yes       |
| older   | No        |

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately through GitHub: open the **Security** tab of this repository, choose
**Advisories**, then **Report a vulnerability**. This creates a private report that only
the maintainer can read.

Please include what you found, the steps to reproduce it, and what an attacker could do
with it. You can expect a first answer within seven days. This is a personal project
with one maintainer, so there is no bounty.

## In scope

- The Go server in this repository: the HTTP API, the event stream, the rate limits, the
  admin token check, CORS, and the handling of Slack and database credentials.
- The web app in `web/`: the Content-Security-Policy, the handling of the admin token in
  the browser, and anything that lets one visitor affect what another visitor sees beyond
  what public drills allow by design.
- The CI workflows and the deploy configuration in this repository.

## Out of scope

- Denial of service against the shared demo instance by sheer volume. It runs on free
  plans and will fall over under load. The limits in the code exist to keep that cheap to
  recover from, not to make it impossible.
- Problems that need access to the maintainer's Render, Vercel, Neon or Slack accounts.
- Findings in third party services themselves.
- The content of simulated incidents. Every service, metric and incident shown is made up.

## Known and accepted risks

These are deliberate trade-offs for a public demo, not oversights.

- **Shared demo instance.** Everyone who opens the demo sees the same simulations and
  incidents. There are no accounts and no private data. Do not type anything secret into
  the service name field, because it is shown to every visitor.
- **Public drills.** When `PUBLIC_DRILLS` is on, anyone can start a drill without a
  token. Such drills are rate limited per caller and in total, have their own cap that is
  smaller than the engine's, and never post to Slack. Any visitor can halt any public
  drill, and no visitor can halt a drill of the admin. A visitor can still fill the
  public slots and annoy other visitors.
- **Caller addresses.** Per caller limits use the address reported by the proxy in front
  of the server, for as many proxies as `TRUSTED_PROXY_HOPS` says. If that number is
  wrong for the hosting platform, the per caller limits become weaker or coarser. A
  second limit shared by all callers does not depend on addresses at all.
- **Admin token in the browser.** The web app keeps the admin token in `localStorage` so
  it does not have to be typed again. Anyone with access to that browser profile can read
  it. The strict Content-Security-Policy is what keeps scripts from other origins away
  from it.
- **Free tier limits.** The API sleeps after 15 minutes without traffic and loses its
  in-memory state, the database sleeps too, and the first request after a sleep is slow.
  Open streams are capped in total and per caller, because each one holds a connection.
- **Rate limits live in one process.** They are kept in memory and reset on every
  restart. The project runs a single instance.
- **Incident history is public.** The read endpoints need no token. They never return
  Slack tokens, and the public status endpoint leaves out Slack channel IDs and the names
  of the people who acted.
