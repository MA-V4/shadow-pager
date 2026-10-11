# 4. Shutdown drains HTTP before it stops the engine

## Context

Render stops an instance with SIGTERM and gives it 30 seconds. At that moment requests
can be in flight, streams are open, notifications to Slack can be queued and database
writes can be waiting. Stopping the parts in the wrong order loses work or answers
requests with errors.

## Decision

On a signal the server stops its parts in this order:

1. Mark the server as draining, so `/readyz` returns 503 and the platform stops routing.
2. Drain HTTP with `http.Server.Shutdown`. Open streams are ended first through a context
   that is cancelled by `RegisterOnShutdown`.
3. Stop the engine.
4. Stop the incident manager and the Slack listener together.
5. Stop the storage recorder.
6. Close the database pool, last, in a deferred call.

The engine runs on a context made with `context.WithoutCancel`, so the signal that starts
the shutdown does not stop the engine before the HTTP drain has finished.

## Consequences

- A request that was accepted is answered by a working engine. A halt that arrives during
  the drain still halts.
- The incident manager and the recorder finish what is in their queues before they stop,
  and the pool is still open while they do.
- The order is tested: a test reads the log lines of a real shutdown and checks their
  sequence.
- All steps share one deadline, `SHUTDOWN_TIMEOUT` (20 seconds by default). A step that
  uses it up is reported as an error, and the steps after it get no time.

## Alternative rejected

Cancelling one root context and letting every goroutine stop at once. It is less code,
but the engine would stop while requests are still being served, the pool could close
under a queued write, and the result would depend on scheduling.
