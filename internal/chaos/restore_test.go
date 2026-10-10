package chaos

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// HELPERS

// savedSimulation builds a simulation the way it would come back from the database.
func savedSimulation(id SimulationID, startedAt time.Time, duration time.Duration) Simulation {
	sc := eventScenario()
	sc.Duration = duration
	return Simulation{
		ID:        id,
		Scenario:  sc,
		State:     StateRunning,
		StartedAt: startedAt,
		EndsAt:    startedAt.Add(duration),
	}
}

// recordingGenerator writes down how much time had passed for every sample it was asked for.
type recordingGenerator struct {
	elapsed chan time.Duration
}

func (g recordingGenerator) Sample(sim Simulation, elapsed time.Duration) MetricSample {
	g.elapsed <- elapsed
	return MetricSample{SimulationID: sim.ID, Timestamp: sim.StartedAt.Add(elapsed), Healthy: true}
}

// TESTS

func TestRestoreResumesAndExpiresSimulations(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	// The program was off for a while, and the clock now reads five minutes after the start.
	clock := &fakeClock{now: start.Add(5 * time.Minute)}
	e := newEventEngine(t, Config{}, clock)

	events, err := e.SubscribeEvents(ctx)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}

	alreadyDone := savedSimulation("sim_done", start, time.Minute)
	endedAt := start.Add(30 * time.Second)
	alreadyDone.State, alreadyDone.EndedAt = StateHalted, &endedAt

	saved := []Simulation{
		savedSimulation("sim_running", start, 10*time.Minute),
		savedSimulation("sim_expired", start, 2*time.Minute),
		savedSimulation("sim_just_ended", start, 5*time.Minute),
		alreadyDone,
		savedSimulation("", start, 10*time.Minute),
		savedSimulation("sim_running", start, 10*time.Minute),
	}

	got, err := e.Restore(ctx, saved)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	want := []struct {
		id      SimulationID
		state   State
		endedAt *time.Time
	}{
		{id: "sim_running", state: StateRunning},
		{id: "sim_expired", state: StateResolved, endedAt: ptr(start.Add(2 * time.Minute))},
		{id: "sim_just_ended", state: StateResolved, endedAt: ptr(start.Add(5 * time.Minute))},
		{id: "sim_done", state: StateHalted, endedAt: &endedAt},
	}
	if len(got) != len(want) {
		t.Fatalf("Restore gave back %d simulations, want %d (the empty id and the repeat are skipped)", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.id || g.State != w.state {
			t.Errorf("restored %d = %s in state %s, want %s in state %s", i, g.ID, g.State, w.id, w.state)
		}
		switch {
		case w.endedAt == nil && g.EndedAt != nil:
			t.Errorf("%s ended at %s, want it still running", g.ID, g.EndedAt)
		case w.endedAt != nil && (g.EndedAt == nil || !g.EndedAt.Equal(*w.endedAt)):
			t.Errorf("%s ended at %v, want %s", g.ID, g.EndedAt, w.endedAt)
		}

		stored, err := e.Get(ctx, w.id)
		if err != nil {
			t.Fatalf("Get %s: %v", w.id, err)
		}
		if stored.State != w.state || !stored.StartedAt.Equal(start) {
			t.Errorf("engine holds %s in state %s started at %s, want state %s with the original start", w.id, stored.State, stored.StartedAt, w.state)
		}
	}

	// The caller's slice must stay as it was.
	if saved[1].State != StateRunning || saved[1].EndedAt != nil {
		t.Errorf("Restore changed the caller's slice: %+v", saved[1])
	}
	// Restoring is quiet, because the caller already knows what it restored.
	if len(events) != 0 {
		t.Errorf("Restore sent %d events, want 0", len(events))
	}
}

func TestRestoredSimulationTicksFromItsOriginalStart(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start.Add(5 * time.Minute)}
	gen := recordingGenerator{elapsed: make(chan time.Duration, 4)}
	e, err := NewEngine(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithClock(clock.Now), WithGenerator(FailureLatencySpike, gen))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	events, err := e.SubscribeEvents(ctx)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}

	if _, err := e.Restore(ctx, []Simulation{savedSimulation("sim_running", start, 10*time.Minute)}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	e.tick(ctx)
	select {
	case got := <-gen.elapsed:
		if got != 5*time.Minute {
			t.Errorf("elapsed at first tick = %s, want 5m0s counted from the original start", got)
		}
	case <-time.After(eventWait):
		t.Fatal("the restored simulation produced no sample")
	}

	// Once the clock passes the original end, the simulation finishes like any other.
	clock.Advance(6 * time.Minute)
	e.tick(ctx)
	if ev := nextEvent(t, events); ev.Type != EventSimulationResolved || ev.Simulation.ID != "sim_running" {
		t.Errorf("event = %s for %s, want the restored simulation to resolve", ev.Type, ev.Simulation.ID)
	}
}

func TestRestoreRefusesBadTiming(t *testing.T) {
	t.Run("after Run has started", func(t *testing.T) {
		e := newEventEngine(t, Config{TickInterval: time.Hour}, &fakeClock{now: time.Now()})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- e.Run(ctx) }()
		t.Cleanup(func() {
			cancel()
			<-done
		})

		// Run flips the ready flag first thing, so we wait for it with a ticker instead of sleeping.
		ready := time.NewTicker(time.Millisecond)
		defer ready.Stop()
		timeout := time.After(eventWait)
		for !e.Ready() {
			select {
			case <-ready.C:
			case <-timeout:
				t.Fatal("engine never became ready")
			}
		}

		if _, err := e.Restore(context.Background(), nil); !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("Restore error = %v, want ErrAlreadyRunning", err)
		}
	})

	t.Run("with a finished context", func(t *testing.T) {
		e := newEventEngine(t, Config{}, &fakeClock{now: time.Now()})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := e.Restore(ctx, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("Restore error = %v, want context.Canceled", err)
		}
	})
}

// ptr gives back a pointer to a copy of the value.
func ptr[T any](v T) *T { return &v }
