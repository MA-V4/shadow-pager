package chaos

import (
	"context"
	"errors"
	"testing"
	"time"
)

// publicScenario is a drill that a visitor without the admin token started.
func publicScenario() Scenario {
	sc := eventScenario()
	sc.Public = true
	return sc
}

func TestPublicDrillsHaveTheirOwnLimit(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)}
	e := newEventEngine(t, Config{MaxActive: 4, MaxPublicActive: 2}, clock)

	first, err := e.Inject(ctx, publicScenario())
	if err != nil {
		t.Fatalf("first public drill: %v", err)
	}
	if _, err := e.Inject(ctx, publicScenario()); err != nil {
		t.Fatalf("second public drill: %v", err)
	}

	_, err = e.Inject(ctx, publicScenario())
	if !errors.Is(err, ErrPublicCapacity) {
		t.Fatalf("third public drill error = %v, want ErrPublicCapacity", err)
	}
	// Callers that only know the general limit must still see this as one.
	if !errors.Is(err, ErrCapacity) {
		t.Errorf("error %v is not an ErrCapacity", err)
	}

	// The admin still has the two slots that visitors cannot take.
	for i := 0; i < 2; i++ {
		if _, err := e.Inject(ctx, eventScenario()); err != nil {
			t.Fatalf("admin drill %d with the public limit reached: %v", i+1, err)
		}
	}
	_, err = e.Inject(ctx, eventScenario())
	if !errors.Is(err, ErrCapacity) || errors.Is(err, ErrPublicCapacity) {
		t.Fatalf("fifth drill error = %v, want the general ErrCapacity", err)
	}

	// Halting a public drill frees a public slot, but every slot is still taken, so the general limit answers.
	if _, err := e.Halt(ctx, first.ID); err != nil {
		t.Fatalf("halt: %v", err)
	}
	if _, err := e.Inject(ctx, publicScenario()); err != nil {
		t.Fatalf("public drill after one was halted: %v", err)
	}
}

func TestPublicLimitCountsOnlyRunningPublicDrills(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)}
	e := newEventEngine(t, Config{MaxActive: 10, MaxPublicActive: 1}, clock)

	// Drills of the admin never count against the public limit.
	for i := 0; i < 3; i++ {
		if _, err := e.Inject(ctx, eventScenario()); err != nil {
			t.Fatalf("admin drill %d: %v", i+1, err)
		}
	}
	sim, err := e.Inject(ctx, publicScenario())
	if err != nil {
		t.Fatalf("public drill next to three admin drills: %v", err)
	}
	if _, err := e.Inject(ctx, publicScenario()); !errors.Is(err, ErrPublicCapacity) {
		t.Fatalf("second public drill error = %v, want ErrPublicCapacity", err)
	}

	// A public drill that ended by itself no longer takes a slot.
	clock.Advance(sim.Scenario.Duration)
	e.tick(ctx)
	if _, err := e.Inject(ctx, publicScenario()); err != nil {
		t.Fatalf("public drill after the first one ended: %v", err)
	}
}

func TestPublicLimitOfZeroMeansNoExtraLimit(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)}
	e := newEventEngine(t, Config{MaxActive: 3}, clock)

	for i := 0; i < 3; i++ {
		if _, err := e.Inject(ctx, publicScenario()); err != nil {
			t.Fatalf("public drill %d: %v", i+1, err)
		}
	}
	if _, err := e.Inject(ctx, publicScenario()); !errors.Is(err, ErrCapacity) || errors.Is(err, ErrPublicCapacity) {
		t.Fatalf("fourth drill error = %v, want only the general ErrCapacity", err)
	}
}

func TestPublicMarkTravelsWithTheSimulation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &fakeClock{now: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)}
	e := newEventEngine(t, Config{}, clock)
	events, err := e.SubscribeEvents(ctx)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}

	sim, err := e.Inject(ctx, publicScenario())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if !sim.Scenario.Public {
		t.Error("the injected simulation lost its public mark")
	}
	// The incident manager and the recorder learn about it from this event.
	if ev := nextEvent(t, events); !ev.Simulation.Scenario.Public {
		t.Error("the started event lost the public mark")
	}
	got, err := e.Get(ctx, sim.ID)
	if err != nil || !got.Scenario.Public {
		t.Errorf("Get = public %v, error %v, want a public simulation", got.Scenario.Public, err)
	}

	// A restart puts the simulation back with its mark.
	fresh := newEventEngine(t, Config{MaxActive: 5, MaxPublicActive: 1}, clock)
	restored, err := fresh.Restore(ctx, []Simulation{sim})
	if err != nil || len(restored) != 1 || !restored[0].Scenario.Public {
		t.Fatalf("Restore = %+v, error %v, want one public simulation", restored, err)
	}
	if _, err := fresh.Inject(ctx, publicScenario()); !errors.Is(err, ErrPublicCapacity) {
		t.Errorf("public drill next to a restored public one error = %v, want ErrPublicCapacity", err)
	}
}
