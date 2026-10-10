package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/MA-V4/shadow-pager/internal/incident"
)

// INCIDENTS

// This line makes the compiler check that Store can be used as an incident.Store.
var _ incident.Store = (*Store)(nil)

const upsertIncident = `
INSERT INTO incidents (
    id, simulation_id, service, mode, severity, status, breaches, declared_at,
    acknowledged_at, acknowledged_by, mitigated_at, resolved_at, slack_channel_id, slack_card_ts, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now())
ON CONFLICT (id) DO UPDATE
SET status = EXCLUDED.status,
    acknowledged_at = EXCLUDED.acknowledged_at,
    acknowledged_by = EXCLUDED.acknowledged_by,
    mitigated_at = EXCLUDED.mitigated_at,
    resolved_at = EXCLUDED.resolved_at,
    slack_channel_id = EXCLUDED.slack_channel_id,
    slack_card_ts = EXCLUDED.slack_card_ts,
    updated_at = now()`

// insertTimelineEntry adds one story line and quietly skips a line that is already saved.
const insertTimelineEntry = `
INSERT INTO incident_timeline (incident_id, seq, at, actor, body)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (incident_id, seq) DO NOTHING`

// SaveIncident writes the incident and all of its story lines together, or nothing at all.
func (s *Store) SaveIncident(ctx context.Context, inc incident.Incident) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("save incident %s: begin transaction: %w", inc.ID, err)
	}
	// Rolling back after a commit does nothing, so this is safe on every path.
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			s.log.ErrorContext(ctx, "rollback incident save", slog.String("incident_id", string(inc.ID)), slog.Any("error", err))
		}
	}()

	// The breaches list must never be nil, because the column does not allow a missing value.
	breaches := inc.Breaches
	if breaches == nil {
		breaches = []string{}
	}
	_, err = tx.Exec(ctx, upsertIncident,
		string(inc.ID),
		string(inc.SimulationID),
		inc.Service,
		string(inc.Mode),
		string(inc.Severity),
		string(inc.Status),
		breaches,
		inc.DeclaredAt.UTC(),
		utcOrNil(inc.AcknowledgedAt),
		inc.AcknowledgedBy,
		utcOrNil(inc.MitigatedAt),
		utcOrNil(inc.ResolvedAt),
		inc.ChannelID,
		inc.CardTimestamp,
	)
	if err != nil {
		return fmt.Errorf("save incident %s: upsert incident: %w", inc.ID, err)
	}

	// A batch sends every story line in one trip to the database.
	if len(inc.Timeline) > 0 {
		batch := &pgx.Batch{}
		for seq, entry := range inc.Timeline {
			batch.Queue(insertTimelineEntry, string(inc.ID), seq, entry.At.UTC(), entry.Actor, entry.Text)
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return fmt.Errorf("save incident %s: insert timeline: %w", inc.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("save incident %s: commit: %w", inc.ID, err)
	}
	return nil
}

// AppendTimeline writes one story line of an incident that is already saved.
func (s *Store) AppendTimeline(ctx context.Context, id incident.IncidentID, seq int, entry incident.TimelineEntry) error {
	if _, err := s.pool.Exec(ctx, insertTimelineEntry, string(id), seq, entry.At.UTC(), entry.Actor, entry.Text); err != nil {
		return fmt.Errorf("append timeline entry %d to incident %s: %w", seq, id, err)
	}
	return nil
}

// selectOpenIncidents also picks closed incidents whose simulation still runs, so that simulation never gets a second incident.
const selectOpenIncidents = `
SELECT ` + incidentColumns + `
FROM incidents i
WHERE i.status <> 'resolved'
   OR EXISTS (SELECT 1 FROM simulations s WHERE s.id = i.simulation_id AND s.state = 'running')
ORDER BY i.declared_at, i.id`

const selectTimelines = `
SELECT incident_id, at, actor, body
FROM incident_timeline
WHERE incident_id = ANY($1)
ORDER BY incident_id, seq`

// LoadOpenIncidents gives back every incident the manager must know about after a restart, with its story.
func (s *Store) LoadOpenIncidents(ctx context.Context) ([]incident.Incident, error) {
	rows, err := s.pool.Query(ctx, selectOpenIncidents)
	if err != nil {
		return nil, fmt.Errorf("query open incidents: %w", err)
	}
	defer rows.Close()

	var out []incident.Incident
	for rows.Next() {
		inc, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("scan open incident: %w", err)
		}
		out = append(out, inc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read open incidents: %w", err)
	}
	if err := s.attachTimelines(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}
