# 9. Slack and storage are optional

## Context

The full system needs a Slack workspace with an installed app and a Postgres database.
A reviewer who clones the repository has neither. The project should start with
`go run ./cmd/server` and nothing else, and it should behave the same way in tests.

## Decision

Both integrations are switched on by configuration and replaced by a do-nothing part when
it is missing.

- With no Slack tokens the server logs one warning and uses `incident.NopNotifier`.
  Incidents are still declared and served by the API. One token without the other is a
  configuration error and stops the server at startup.
- With no `DATABASE_URL` the server logs one warning and keeps everything in memory.
- With `DATABASE_URL` set, a database that cannot be reached at startup stops the server,
  after a retry of about 15 seconds for a database that is waking up.
- A Slack connection that fails while running is logged and never stops the server.

## Consequences

- The project runs with zero setup, and the test suite needs no network. The storage
  tests are the exception: they need a real Postgres and skip when
  `TEST_DATABASE_URL` is not set.
- The incident manager talks to a `Notifier` and a `Store` interface and has no branch
  for "Slack is off" or "storage is off".
- A typo in a variable name silently gives the in-memory mode. The warning at startup is
  the only sign, so it is logged at warn level and names the missing variables.
- A half configured integration fails loudly at startup instead of working partly.

## Alternative rejected

Requiring both and failing at startup when either is missing. It removes the do-nothing
parts and one class of surprise, but it turns a one minute first run into an hour of
account setup, and it pushes tests towards mocks of Slack and the database.
