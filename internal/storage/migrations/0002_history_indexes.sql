/* The API lists the newest simulations first, and this index keeps that quick as the table grows. */
CREATE INDEX simulations_started_idx ON simulations (started_at DESC, id DESC);

/* The API lists the newest incidents first, and this index keeps that quick as the table grows. */
CREATE INDEX incidents_declared_idx ON incidents (declared_at DESC, id DESC);
