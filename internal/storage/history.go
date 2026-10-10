package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// SIMULATION HISTORY

const simulationColumns = `id, service, mode, severity, intensity, duration_ns, state, started_at, ends_at, ended_at`

const selectRecentSimulations = `
SELECT ` + simulationColumns + `
FROM simulations
ORDER BY started_at DESC, id DESC
LIMIT $1`

const selectSimulation = `
SELECT ` + simulationColumns + `
FROM simulations
WHERE id = $1`

// ListRecentSimulations gives back the newest simulations, finished or not, newest first.
func (s *Store) ListRecentSimulations(ctx context.Context, limit int) ([]chaos.Simulation, error) {
	rows, err := s.pool.Query(ctx, selectRecentSimulations, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent simulations: %w", err)
	}
	defer rows.Close()

	var out []chaos.Simulation
	for rows.Next() {
		sim, err := scanSimulation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recent simulation: %w", err)
		}
		out = append(out, sim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read recent simulations: %w", err)
	}
	return out, nil
}

// GetSimulation gives back one saved simulation, or chaos.ErrNotFound when it was never saved.
func (s *Store) GetSimulation(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	sim, err := scanSimulation(s.pool.QueryRow(ctx, selectSimulation, string(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return chaos.Simulation{}, fmt.Errorf("get saved simulation %s: %w", id, chaos.ErrNotFound)
	}
	if err != nil {
		return chaos.Simulation{}, fmt.Errorf("get saved simulation %s: %w", id, err)
	}
	return sim, nil
}

// scanSimulation reads one simulation from a row that holds the simulation columns.
func scanSimulation(row pgx.Row) (chaos.Simulation, error) {
	var (
		id, service, mode, severity, state string
		intensity                          float64
		durationNS                         int64
		startedAt, endsAt                  time.Time
		endedAt                            *time.Time
	)
	if err := row.Scan(&id, &service, &mode, &severity, &intensity, &durationNS, &state, &startedAt, &endsAt, &endedAt); err != nil {
		return chaos.Simulation{}, err
	}
	return chaos.Simulation{
		ID: chaos.SimulationID(id),
		Scenario: chaos.Scenario{
			Service:   service,
			Mode:      chaos.FailureMode(mode),
			Severity:  chaos.Severity(severity),
			Intensity: intensity,
			Duration:  time.Duration(durationNS),
		},
		State:     chaos.State(state),
		StartedAt: startedAt.UTC(),
		EndsAt:    endsAt.UTC(),
		EndedAt:   utcOrNil(endedAt),
	}, nil
}

// INCIDENT HISTORY

const incidentColumns = `id, simulation_id, service, mode, severity, status, breaches, declared_at,
       acknowledged_at, acknowledged_by, mitigated_at, resolved_at, slack_channel_id, slack_card_ts`

const selectRecentIncidents = `
SELECT ` + incidentColumns + `
FROM incidents
ORDER BY declared_at DESC, id DESC
LIMIT $1`

const selectIncident = `
SELECT ` + incidentColumns + `
FROM incidents
WHERE id = $1`

// ListRecentIncidents gives back the newest incidents with their stories, open or closed, newest first.
func (s *Store) ListRecentIncidents(ctx context.Context, limit int) ([]incident.Incident, error) {
	rows, err := s.pool.Query(ctx, selectRecentIncidents, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent incidents: %w", err)
	}
	defer rows.Close()

	var out []incident.Incident
	for rows.Next() {
		inc, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recent incident: %w", err)
		}
		out = append(out, inc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read recent incidents: %w", err)
	}
	if err := s.attachTimelines(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetIncident gives back one saved incident with its story, or incident.ErrNotFound when it was never saved.
func (s *Store) GetIncident(ctx context.Context, id incident.IncidentID) (incident.Incident, error) {
	inc, err := scanIncident(s.pool.QueryRow(ctx, selectIncident, string(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return incident.Incident{}, fmt.Errorf("get saved incident %s: %w", id, incident.ErrNotFound)
	}
	if err != nil {
		return incident.Incident{}, fmt.Errorf("get saved incident %s: %w", id, err)
	}
	one := []incident.Incident{inc}
	if err := s.attachTimelines(ctx, one); err != nil {
		return incident.Incident{}, err
	}
	return one[0], nil
}

// scanIncident reads one incident from a row that holds the incident columns.
func scanIncident(row pgx.Row) (incident.Incident, error) {
	var (
		id, simID, service, mode, severity, status string
		acknowledgedBy, channelID, cardTS          string
		breaches                                   []string
		declaredAt                                 time.Time
		acknowledgedAt, mitigatedAt, resolvedAt    *time.Time
	)
	err := row.Scan(&id, &simID, &service, &mode, &severity, &status, &breaches, &declaredAt,
		&acknowledgedAt, &acknowledgedBy, &mitigatedAt, &resolvedAt, &channelID, &cardTS)
	if err != nil {
		return incident.Incident{}, err
	}
	return incident.Incident{
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
	}, nil
}

// attachTimelines fills in the stories of all the given incidents with one query.
func (s *Store) attachTimelines(ctx context.Context, incidents []incident.Incident) error {
	if len(incidents) == 0 {
		return nil
	}
	ids := make([]string, len(incidents))
	byID := make(map[string]int, len(incidents))
	for i, inc := range incidents {
		ids[i] = string(inc.ID)
		byID[ids[i]] = i
	}

	rows, err := s.pool.Query(ctx, selectTimelines, ids)
	if err != nil {
		return fmt.Errorf("query incident timelines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id, actor, body string
			at              time.Time
		)
		if err := rows.Scan(&id, &at, &actor, &body); err != nil {
			return fmt.Errorf("scan timeline entry: %w", err)
		}
		i := byID[id]
		incidents[i].Timeline = append(incidents[i].Timeline, incident.TimelineEntry{At: at.UTC(), Actor: actor, Text: body})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read incident timelines: %w", err)
	}
	return nil
}
