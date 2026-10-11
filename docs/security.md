# Security

How the API protects itself and how secrets are handled. To report a vulnerability, and
for the risks that are accepted on purpose, see [SECURITY.md](../SECURITY.md).

## API protection

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
  from the right, so a caller cannot invent one. With `0` the header is ignored. The
  right number depends on the hosting platform, and [the deploy guide](deploy.md#checking-trusted_proxy_hops)
  says how to check it.
- **CORS.** Only the websites listed in `CORS_ALLOWED_ORIGINS` may call the API from a
  browser.
- **Security headers.** Every API answer carries `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, a `Referrer-Policy` and a `Content-Security-Policy` that loads
  nothing. In production it also carries `Strict-Transport-Security` for one year. The web
  app sends the same headers from `web/vercel.json`, and its build writes a policy into
  the page that allows only its own files and the API address it was built with.
- **Secrets in logs.** Tokens and the `Authorization` header are never logged. Of the
  database address only the host and the database name are logged.

## Dependencies

Dependencies are checked on every push: `govulncheck` for Go, `npm audit` for the web
app, and both fail the build on a finding. Dependabot proposes updates once a week, and
every GitHub Action is pinned to a full commit.

## Secrets

- The server reads its secrets (`ADMIN_TOKEN`, the Slack tokens, `DATABASE_URL`) from the
  environment only. None of them is in the repository, and the git history was scanned
  for them.
- A weak or malformed secret stops the server at startup with an error that names the
  problem and never the value.
- The web app is built with one variable, `VITE_API_URL`, which is public. No secret can
  reach the files that browsers download, because only variables that start with `VITE_`
  are ever put into them.
- The admin token exists in the browser only where a person typed it. It is kept in
  `localStorage` and sent as a header to the API.
