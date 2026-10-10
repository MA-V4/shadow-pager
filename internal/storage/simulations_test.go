package storage

import (
	"context"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// FIXTURES

// testStart has no parts smaller than a microsecond, because that is all Postgres keeps.
var testStart = time.Date(2026, 1, 2, 15, 4, 5, 123456000, time.UTC)

// testSimulation builds a simulation that started at the given offset from the test start.
func testSimulation(id chaos.SimulationID, state chaos.State, offset time.Duration) chaos.Simulation {
	started := testStart.Add(offset)
	sim := chaos.Simulation{
		ID: id,
		Scenario: chaos.Scenario{
			Service:   "payments-api",
			Mode:      chaos.FailureLatencySpike,
			Severity:  chaos.SeverityMajor,
			Intensity: 0.9,
			Duration:  2 * time.Minute,
		},
		State:     state,
		StartedAt: started,
		EndsAt:    started.Add(2 * time.Minute),
	}
	if state.Terminal() {
		ended := started.Add(30 * time.Second)
		sim.EndedAt = &ended
	}
	return sim
}

// TESTS

func TestUpsertAndLoadRunningSimulations(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// A time in another zone must come back as the same moment in UTC.
	paris := time.FixedZone("CET", 3600)
	second := testSimulation("sim_2", chaos.StateRunning, time.Minute)
	second.StartedAt = second.StartedAt.In(paris)
	second.EndsAt = second.EndsAt.In(paris)

	for _, sim := range []chaos.Simulation{
		second,
		testSimulation("sim_1", chaos.StateRunning, 0),
		testSimulation("sim_done", chaos.StateResolved, 0),
	} {
		if err := store.UpsertSimulation(ctx, sim); err != nil {
			t.Fatalf("UpsertSimulation %s: %v", sim.ID, err)
		}
	}

	got, err := store.LoadRunningSimulations(ctx)
	if err != nil {
		t.Fatalf("LoadRunningSimulations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("loaded %d simulations, want the 2 that are running", len(got))
	}
	if got[0].ID != "sim_1" || got[1].ID != "sim_2" {
		t.Errorf("order = %s, %s, want oldest first", got[0].ID, got[1].ID)
	}

	want := testSimulation("sim_1", chaos.StateRunning, 0)
	first := got[0]
	if first.Scenario != want.Scenario {
		t.Errorf("scenario = %+v, want %+v", first.Scenario, want.Scenario)
	}
	if first.State != chaos.StateRunning || first.EndedAt != nil {
		t.Errorf("state = %s, ended at %v, want running with no end", first.State, first.EndedAt)
	}
	if !first.StartedAt.Equal(want.StartedAt) || !first.EndsAt.Equal(want.EndsAt) {
		t.Errorf("times = %s to %s, want %s to %s", first.StartedAt, first.EndsAt, want.StartedAt, want.EndsAt)
	}
	for _, sim := range got {
		if sim.StartedAt.Location() != time.UTC || sim.EndsAt.Location() != time.UTC {
			t.Errorf("simulation %s times are in %s, want UTC", sim.ID, sim.StartedAt.Location())
		}
	}
	if !got[1].StartedAt.Equal(testStart.Add(time.Minute)) {
		t.Errorf("sim_2 started at %s, want the same moment it was saved with", got[1].StartedAt)
	}
}

func TestUpsertSimulationUpdatesState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertSimulation(ctx, testSimulation("sim_1", chaos.StateRunning, 0)); err != nil {
		t.Fatalf("UpsertSimulation running: %v", err)
	}
	if err := store.UpsertSimulation(ctx, testSimulation("sim_1", chaos.StateHalted, 0)); err != nil {
		t.Fatalf("UpsertSimulation halted: %v", err)
	}

	got, err := store.LoadRunningSimulations(ctx)
	if err != nil {
		t.Fatalf("LoadRunningSimulations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("loaded %d running simulations, want 0 after the halt", len(got))
	}
	if n := countRows(t, store, "simulations"); n != 1 {
		t.Errorf("simulations table has %d rows, want 1 because the second save is an update", n)
	}

	var state string
	var endedAt *time.Time
	if err := store.pool.QueryRow(ctx, `SELECT state, ended_at FROM simulations WHERE id = $1`, "sim_1").Scan(&state, &endedAt); err != nil {
		t.Fatalf("read simulation back: %v", err)
	}
	if state != "halted" || endedAt == nil || !endedAt.Equal(testStart.Add(30*time.Second)) {
		t.Errorf("stored state = %s, ended at %v, want halted 30 seconds after the start", state, endedAt)
	}
}

func TestUpsertSimulationNeverRevivesAFinishedOne(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertSimulation(ctx, testSimulation("sim_1", chaos.StateHalted, 0)); err != nil {
		t.Fatalf("UpsertSimulation halted: %v", err)
	}
	// A late or repeated event must not bring a finished simulation back to life.
	if err := store.UpsertSimulation(ctx, testSimulation("sim_1", chaos.StateRunning, 0)); err != nil {
		t.Fatalf("UpsertSimulation running: %v", err)
	}

	got, err := store.LoadRunningSimulations(ctx)
	if err != nil {
		t.Fatalf("LoadRunningSimulations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("loaded %d running simulations, want 0 because the simulation was already over", len(got))
	}
}
