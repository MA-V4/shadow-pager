# 6. Readiness reads a remembered ping, so Neon can sleep

## Context

`/readyz` is the health check that Render calls every few seconds. It should say whether
the database can be reached. The database is a free Neon instance that suspends itself
when no query arrives for a few minutes and takes a moment to wake. A probe that pings
the database on every call would never let it suspend, and a slow ping would make the
probe itself slow.

## Decision

The probe reads a remembered answer from `pingCache`. The cache starts healthy, because
the database was just reached at startup. When a probe arrives and the answer is older
than 60 seconds, the cache starts one ping in the background, with a two second limit,
and the probe returns the old answer at once. The cache has no timer of its own: it only
pings when a probe asks.

`/healthz` never touches any dependency.

## Consequences

- The probe never waits for the database, so a slow database cannot make the platform
  think the server is dead.
- The database is pinged at most once per minute, and only while something is probing the
  server. When Render puts the idle server to sleep, the pings stop, and Neon can suspend
  too.
- While the server is awake, one ping per minute still keeps Neon awake. That is
  accepted: an awake server with a sleeping database would answer its first write slowly.
- A database failure shows up in `/readyz` up to a minute late, plus one probe interval.

## Alternative rejected

Pinging the database inside every probe. It is the obvious implementation and it is
correct, but it couples the probe latency to the database and keeps a free database awake
for as long as the health check runs.
