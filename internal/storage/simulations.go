package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// SIMULATIONS

// upsertSimulation saves a new simulation, or updates one that is still running and never one that is over.
const upsertSimulation = `
INSERT INTO simulations (id, service, mode, severity, intensity, duration_ns, state, started_at, ends_at, ended_at, public, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
ON CONFLICT (id) DO UPDATE
SET state = EXCLUDED.state, ended_at = EXCLUDED.ended_at, updated_at = now()
WHERE simulations.state = 'running'`

// UpsertSimulation saves the newest snapshot of one simulation.
func (s *Store) UpsertSimulation(ctx context.Context, sim chaos.Simulation) error {
	_, err := s.pool.Exec(ctx, upsertSimulation,
		string(sim.ID),
		sim.Scenario.Service,
		string(sim.Scenario.Mode),
		string(sim.Scenario.Severity),
		sim.Scenario.Intensity,
		int64(sim.Scenario.Duration),
		string(sim.State),
		sim.StartedAt.UTC(),
		sim.EndsAt.UTC(),
		utcOrNil(sim.EndedAt),
		sim.Scenario.Public,
	)
	if err != nil {
		return fmt.Errorf("upsert simulation %s: %w", sim.ID, err)
	}
	return nil
}

const selectRunningSimulations = `
SELECT ` + simulationColumns + `
FROM simulations
WHERE state = 'running'
ORDER BY started_at, id`

// LoadRunningSimulations gives back every simulation that was still running when it was last saved.
func (s *Store) LoadRunningSimulations(ctx context.Context) ([]chaos.Simulation, error) {
	rows, err := s.pool.Query(ctx, selectRunningSimulations)
	if err != nil {
		return nil, fmt.Errorf("query running simulations: %w", err)
	}
	defer rows.Close()

	var out []chaos.Simulation
	for rows.Next() {
		sim, err := scanSimulation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan running simulation: %w", err)
		}
		out = append(out, sim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read running simulations: %w", err)
	}
	return out, nil
}

// HELPERS

// utcOrNil turns a time that might be missing into UTC, and keeps a missing one missing.
func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
