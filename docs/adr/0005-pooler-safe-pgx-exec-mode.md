# 5. A pgx exec mode that works behind transaction poolers

## Context

The free Postgres plans this project targets (Neon pooled URLs, Supabase on port 6543)
put a transaction-mode pooler in front of the database. Such a pooler can give every
transaction a different server connection. By default pgx prepares a named statement on
first use and reuses it later. Behind a transaction pooler the later use can land on a
connection that has never seen that statement, and the query fails.

## Decision

The pool is configured with `pgx.QueryExecModeExec`. In this mode pgx sends parameters
with the extended protocol but never creates a named prepared statement. Only the
migration runner uses the simple protocol, because one migration file holds many
statements.

Migrations take `pg_advisory_xact_lock` inside each transaction, not a session lock,
because a session lock does not survive a transaction pooler either.

## Consequences

- The same binary works with a direct connection, with Neon and with Supabase, and no
  flag has to be set per provider.
- Parameters are still sent separately from the SQL text for every normal query.
- Each query costs a little more than a cached prepared statement would. The app writes
  a few rows per incident, so this is not measurable here.
- This has only been run against a local PostgreSQL 17. Neon and Supabase themselves, and
  a real pooler, are not yet verified.

## Alternative rejected

Keeping the default mode and asking users to connect directly, without the pooler. On
Supabase the direct host has only an IPv6 address on the free plan, which Render cannot
reach, and on Neon a direct connection uses up the small connection limit.
