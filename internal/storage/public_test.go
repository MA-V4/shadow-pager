package storage

import (
	"context"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

func TestPublicMarkSurvivesTheDatabase(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	public := testSimulation("sim_public", chaos.StateRunning, 0)
	public.Scenario.Public = true
	admin := testSimulation("sim_admin", chaos.StateRunning, time.Second)
	for _, sim := range []chaos.Simulation{public, admin} {
		if err := s.UpsertSimulation(ctx, sim); err != nil {
			t.Fatalf("UpsertSimulation(%s): %v", sim.ID, err)
		}
	}

	want := map[chaos.SimulationID]bool{"sim_public": true, "sim_admin": false}

	running, err := s.LoadRunningSimulations(ctx)
	if err != nil || len(running) != 2 {
		t.Fatalf("LoadRunningSimulations = %d simulations, error %v, want 2", len(running), err)
	}
	for _, sim := range running {
		if sim.Scenario.Public != want[sim.ID] {
			t.Errorf("restored %s public = %v, want %v", sim.ID, sim.Scenario.Public, want[sim.ID])
		}
	}

	recent, err := s.ListRecentSimulations(ctx, 10)
	if err != nil || len(recent) != 2 {
		t.Fatalf("ListRecentSimulations = %d simulations, error %v, want 2", len(recent), err)
	}
	for _, sim := range recent {
		if sim.Scenario.Public != want[sim.ID] {
			t.Errorf("listed %s public = %v, want %v", sim.ID, sim.Scenario.Public, want[sim.ID])
		}
	}

	for id, wantPublic := range want {
		got, err := s.GetSimulation(ctx, id)
		if err != nil || got.Scenario.Public != wantPublic {
			t.Errorf("GetSimulation(%s) public = %v, error %v, want %v", id, got.Scenario.Public, err, wantPublic)
		}
	}

	// A simulation that ends keeps its mark, because the update only touches the state.
	ended := public
	ended.State = chaos.StateHalted
	at := public.StartedAt.Add(30 * time.Second)
	ended.EndedAt = &at
	if err := s.UpsertSimulation(ctx, ended); err != nil {
		t.Fatalf("UpsertSimulation(halted): %v", err)
	}
	if got, err := s.GetSimulation(ctx, "sim_public"); err != nil || !got.Scenario.Public || got.State != chaos.StateHalted {
		t.Errorf("halted public simulation = public %v, state %s, error %v, want a halted public one", got.Scenario.Public, got.State, err)
	}
}

func TestPublicIncidentSurvivesTheDatabase(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	public := testIncident(t, "inc_public", "sim_public")
	public.Public = true
	admin := testIncident(t, "inc_admin", "sim_admin")
	for _, inc := range []incident.Incident{public, admin} {
		if err := s.SaveIncident(ctx, inc); err != nil {
			t.Fatalf("SaveIncident(%s): %v", inc.ID, err)
		}
	}

	want := map[incident.IncidentID]bool{"inc_public": true, "inc_admin": false}

	// These are the incidents the manager gets back after a restart, so Slack stays silent for the public one.
	open, err := s.LoadOpenIncidents(ctx)
	if err != nil || len(open) != 2 {
		t.Fatalf("LoadOpenIncidents = %d incidents, error %v, want 2", len(open), err)
	}
	for _, inc := range open {
		if inc.Public != want[inc.ID] {
			t.Errorf("restored %s public = %v, want %v", inc.ID, inc.Public, want[inc.ID])
		}
	}

	recent, err := s.ListRecentIncidents(ctx, 10)
	if err != nil || len(recent) != 2 {
		t.Fatalf("ListRecentIncidents = %d incidents, error %v, want 2", len(recent), err)
	}
	for _, inc := range recent {
		if inc.Public != want[inc.ID] {
			t.Errorf("listed %s public = %v, want %v", inc.ID, inc.Public, want[inc.ID])
		}
	}

	for id, wantPublic := range want {
		if got := loadOne(t, s, id); got.Public != wantPublic {
			t.Errorf("incident %s public = %v, want %v", id, got.Public, wantPublic)
		}
	}
}
