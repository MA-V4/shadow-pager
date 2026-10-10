package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// LIMITS

const (
	defaultListLimit = 50              // how many items a list gives back when the caller does not say
	maxListLimit     = 200             // the most items a list ever gives back
	archiveTimeout   = 3 * time.Second // how long the database may take to answer a list or a lookup
)

// parseLimit reads the limit from the URL, and uses the default when there is none.
func parseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultListLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 0, fmt.Errorf("%w: limit must be a whole number from 1 to %d", errBadRequest, maxListLimit)
	}
	return min(limit, maxListLimit), nil
}

// ARCHIVE

// archive lists the only things this package needs the database to do.
type archive interface {
	ListRecentSimulations(ctx context.Context, limit int) ([]chaos.Simulation, error)
	GetSimulation(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error)
	ListRecentIncidents(ctx context.Context, limit int) ([]incident.Incident, error)
	GetIncident(ctx context.Context, id incident.IncidentID) (incident.Incident, error)
}

// WithHistory adds finished items from the database to the lists and lookups, and must come after WithIncidents.
func WithHistory(a archive) Option {
	return func(h *Handler) {
		h.sim = simulatorWithHistory{simulator: h.sim, archive: a, log: h.log}
		if h.incidents != nil {
			h.incidents = incidentsWithHistory{live: h.incidents, archive: a, log: h.log}
		}
	}
}

// SIMULATIONS

// simulatorWithHistory answers from the engine first and fills the gaps from the database.
type simulatorWithHistory struct {
	simulator // everything we do not change goes straight to the engine
	archive   archive
	log       *slog.Logger
}

// List gives back what the engine holds plus older simulations from the database, newest first.
func (s simulatorWithHistory) List(ctx context.Context) ([]chaos.Simulation, error) {
	live, err := s.simulator.List(ctx)
	if err != nil {
		return nil, err
	}

	archiveCtx, cancel := context.WithTimeout(ctx, archiveTimeout)
	defer cancel()
	saved, err := s.archive.ListRecentSimulations(archiveCtx, maxListLimit)
	if err != nil {
		// A sick database must not take the live list down with it.
		s.log.WarnContext(ctx, "simulation history is missing from this answer", slog.Any("error", err))
		return live, nil
	}
	return mergeSimulations(live, saved), nil
}

// Get asks the engine first, and looks in the database when the engine does not know the simulation.
func (s simulatorWithHistory) Get(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	sim, err := s.simulator.Get(ctx, id)
	if !errors.Is(err, chaos.ErrNotFound) {
		return sim, err
	}
	archiveCtx, cancel := context.WithTimeout(ctx, archiveTimeout)
	defer cancel()
	return s.archive.GetSimulation(archiveCtx, id)
}

// mergeSimulations joins both lists, keeps the live copy when both know a simulation, and sorts newest first.
func mergeSimulations(live, saved []chaos.Simulation) []chaos.Simulation {
	seen := make(map[chaos.SimulationID]bool, len(live))
	out := make([]chaos.Simulation, 0, len(live)+len(saved))
	for _, sim := range live {
		seen[sim.ID] = true
		out = append(out, sim)
	}
	for _, sim := range saved {
		if !seen[sim.ID] {
			out = append(out, sim)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// INCIDENTS

// incidentsWithHistory answers from the incident manager first and fills the gaps from the database.
type incidentsWithHistory struct {
	live    incidentSource
	archive archive
	log     *slog.Logger
}

// List gives back what the manager holds plus older incidents from the database, newest first.
func (s incidentsWithHistory) List(ctx context.Context) ([]incident.Incident, error) {
	live, err := s.live.List(ctx)
	if err != nil {
		return nil, err
	}

	archiveCtx, cancel := context.WithTimeout(ctx, archiveTimeout)
	defer cancel()
	saved, err := s.archive.ListRecentIncidents(archiveCtx, maxListLimit)
	if err != nil {
		s.log.WarnContext(ctx, "incident history is missing from this answer", slog.Any("error", err))
		return live, nil
	}
	return mergeIncidents(live, saved), nil
}

// Get asks the manager first, and looks in the database when the manager does not know the incident.
func (s incidentsWithHistory) Get(ctx context.Context, id incident.IncidentID) (incident.Incident, error) {
	inc, err := s.live.Get(ctx, id)
	if !errors.Is(err, incident.ErrNotFound) {
		return inc, err
	}
	archiveCtx, cancel := context.WithTimeout(ctx, archiveTimeout)
	defer cancel()
	return s.archive.GetIncident(archiveCtx, id)
}

// mergeIncidents joins both lists, keeps the live copy when both know an incident, and sorts newest first.
func mergeIncidents(live, saved []incident.Incident) []incident.Incident {
	seen := make(map[incident.IncidentID]bool, len(live))
	out := make([]incident.Incident, 0, len(live)+len(saved))
	for _, inc := range live {
		seen[inc.ID] = true
		out = append(out, inc)
	}
	for _, inc := range saved {
		if !seen[inc.ID] {
			out = append(out, inc)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DeclaredAt.Equal(out[j].DeclaredAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].DeclaredAt.After(out[j].DeclaredAt)
	})
	return out
}
