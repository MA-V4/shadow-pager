/* Simulations are saved as small snapshots, and raw metric samples are never stored. */
CREATE TABLE simulations (
    id          text PRIMARY KEY,
    service     text NOT NULL,
    mode        text NOT NULL,
    severity    text NOT NULL,
    intensity   double precision NOT NULL,
    duration_ns bigint NOT NULL,
    state       text NOT NULL,
    started_at  timestamptz NOT NULL,
    ends_at     timestamptz NOT NULL,
    ended_at    timestamptz,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

/* Startup only asks for the simulations that were still running. */
CREATE INDEX simulations_running_idx ON simulations (started_at) WHERE state = 'running';

/* One simulation can have at most one incident, which the unique rule enforces. */
CREATE TABLE incidents (
    id               text PRIMARY KEY,
    simulation_id    text NOT NULL UNIQUE,
    service          text NOT NULL,
    mode             text NOT NULL,
    severity         text NOT NULL,
    status           text NOT NULL,
    breaches         text[] NOT NULL DEFAULT '{}',
    declared_at      timestamptz NOT NULL,
    acknowledged_at  timestamptz,
    acknowledged_by  text NOT NULL DEFAULT '',
    mitigated_at     timestamptz,
    resolved_at      timestamptz,
    slack_channel_id text NOT NULL DEFAULT '',
    slack_card_ts    text NOT NULL DEFAULT '',
    updated_at       timestamptz NOT NULL DEFAULT now()
);

/* Startup only asks for the incidents that people are still working on. */
CREATE INDEX incidents_open_idx ON incidents (declared_at) WHERE status <> 'resolved';

/* The primary key keeps each story line once and also serves the load by incident. */
CREATE TABLE incident_timeline (
    incident_id text NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    seq         integer NOT NULL,
    at          timestamptz NOT NULL,
    actor       text NOT NULL,
    body        text NOT NULL,
    PRIMARY KEY (incident_id, seq)
);
