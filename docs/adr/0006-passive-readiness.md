# 6. Readiness reads a remembered ping, so Neon can sleep

## Context

`/readyz` says whether traffic should be sent to the server, and that includes whether
the database can be reached. A health check or a monitor may call it every few seconds.
The database is a free Neon instance that suspends itself when no query arrives for a
few minutes and takes a moment to wake. A probe that pings the database on every call
would never let it suspend, and a slow ping would make the probe itself slow.

## Decision

The probe reads a remembered answer from `pingCache`. The cache starts healthy, because
the database was just reached at startup. When a probe arrives and the answer is older
than 60 seconds, the cache starts one ping in the background, with a two second limit,
and the probe returns the old answer at once. The cache has no timer of its own: it only
pings when a probe asks.

`/healthz` never touches any dependency. It is the path Render's health check calls, so
a database problem can never make the platform restart a healthy process. `/readyz` is
for people and for monitoring.

## Consequences

- The probe never waits for the database, so a slow database cannot make the platform
  think the server is dead.
- The database is pinged at most once per minute, and only while something is calling
  `/readyz`. Render's own probe calls `/healthz`, so with no monitor watching `/readyz`
  the database is not pinged at all and Neon can suspend whenever the app is quiet.
- A monitor that polls `/readyz` keeps Neon awake with one ping per minute for as long as
  it runs. That is the price of watching the database.
- A database failure shows up in `/readyz` up to a minute late, plus one probe interval.

## Alternative rejected

Pinging the database inside every probe. It is the obvious implementation and it is
correct, but it couples the probe latency to the database and keeps a free database awake
for as long as the health check runs.
