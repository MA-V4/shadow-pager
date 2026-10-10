package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MA-V4/shadow-pager/internal/chaos"
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
SELECT i.id, i.simulation_id, i.service, i.mode, i.severity, i.status, i.breaches, i.declared_at,
       i.acknowledged_at, i.acknowledged_by, i.mitigated_at, i.resolved_at, i.slack_channel_id, i.slack_card_ts
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
	var ids []string
	for rows.Next() {
		var (
			id, simID, service, mode, severity, status string
			acknowledgedBy, channelID, cardTS          string
			breaches                                   []string
			declaredAt                                 time.Time
			acknowledgedAt, mitigatedAt, resolvedAt    *time.Time
		)
		err := rows.Scan(&id, &simID, &service, &mode, &severity, &status, &breaches, &declaredAt,
			&acknowledgedAt, &acknowledgedBy, &mitigatedAt, &resolvedAt, &channelID, &cardTS)
		if err != nil {
			return nil, fmt.Errorf("scan open incident: %w", err)
		}
		out = append(out, incident.Incident{
			ID:             incident.IncidentID(id),
			SimulationID:   chaos.SimulationID(simID),
			Service:        service,
			Mode:           chaos.FailureMode(mode),
			Severity:       chaos.Severity(severity),
			Status:         incident.Status(status),
			Breaches:       breaches,
			DeclaredAt:     declaredAt.UTC(),
			AcknowledgedAt: utcOrNil(acknowledgedAt),
			AcknowledgedBy: acknowledgedBy,
			MitigatedAt:    utcOrNil(mitigatedAt),
			ResolvedAt:     utcOrNil(resolvedAt),
			ChannelID:      channelID,
			CardTimestamp:  cardTS,
		})
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read open incidents: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}

	// One more query fetches the stories of all those incidents, instead of one query per incident.
	byID := make(map[string]int, len(out))
	for i, id := range ids {
		byID[id] = i
	}
	entries, err := s.pool.Query(ctx, selectTimelines, ids)
	if err != nil {
		return nil, fmt.Errorf("query incident timelines: %w", err)
	}
	defer entries.Close()

	for entries.Next() {
		var (
			id, actor, body string
			at              time.Time
		)
		if err := entries.Scan(&id, &at, &actor, &body); err != nil {
			return nil, fmt.Errorf("scan timeline entry: %w", err)
		}
		i := byID[id]
		out[i].Timeline = append(out[i].Timeline, incident.TimelineEntry{At: at.UTC(), Actor: actor, Text: body})
	}
	if err := entries.Err(); err != nil {
		return nil, fmt.Errorf("read incident timelines: %w", err)
	}
	return out, nil
}
