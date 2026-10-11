# 7. Persistence is driven by events, not by calls in the engine

## Context

Simulations and incidents should survive a restart, because a free Render instance loses
its memory every time it spins down. The engine holds a lock while it changes state, and
a database on a free plan can take seconds to answer or be asleep.

## Decision

The engine knows nothing about storage. It emits lifecycle events (started, resolved,
halted) while it holds its write lock, so the order of events always matches the order of
state changes. `storage.Recorder` subscribes to those events and upserts simulations. The
incident manager queues a save on each of its own lifecycle changes.

Each writer has its own goroutine and queue. Every write has a five second limit. A write
that fails is logged and dropped, and memory stays the source of truth. Both writers
finish what is still queued before they stop.

## Consequences

- A slow or broken database cannot stall a tick, an API request or a Slack button.
- The `chaos` package has no storage import and can be tested with no database at all.
- The database can lag behind memory, and a failed write is lost. `SaveIncident` writes
  the incident row and all its timeline entries in one transaction with
  `ON CONFLICT DO NOTHING` on the timeline, so the next save of the same incident repairs
  an earlier failed one.
- The recorder and the manager write independently, so `incidents.simulation_id` has no
  foreign key to `simulations`. A foreign key would make an incident save fail whenever
  it overtakes the save of its simulation.
- A full event buffer drops the event. This is logged at error level and counted.

## Alternative rejected

Calling the store from inside `Engine.Inject`, `Engine.Halt` and `Engine.tick`. Every
state change would then wait for the database while holding the engine lock, the engine
would need a store to be tested, and "storage is optional" would turn into nil checks
through the engine.
