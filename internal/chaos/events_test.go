package chaos

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// HELPERS

// eventWait is the longest a test waits before it gives up.
const eventWait = 2 * time.Second

// fakeClock is a pretend clock that only moves when a test tells it to.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newEventEngine builds a quiet engine that uses the pretend clock.
func newEventEngine(t *testing.T, cfg Config, clock *fakeClock) *Engine {
	t.Helper()
	e, err := NewEngine(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// nextEvent waits for one event and fails the test if none comes.
func nextEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("event channel closed while waiting for an event")
		}
		return ev
	case <-time.After(eventWait):
		t.Fatal("timed out waiting for an event")
	}
	return Event{}
}

func eventScenario() Scenario {
	return Scenario{
		Service:   "payments-api",
		Mode:      FailureLatencySpike,
		Severity:  SeverityMajor,
		Intensity: 0.9,
		Duration:  time.Minute,
	}
}

// TESTS

func TestEngineEmitsLifecycleEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &fakeClock{now: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)}
	e := newEventEngine(t, Config{}, clock)

	events, err := e.SubscribeEvents(ctx)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}

	halted, err := e.Inject(ctx, eventScenario())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	resolved, err := e.Inject(ctx, eventScenario())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if _, err := e.Halt(ctx, halted.ID); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	// Moving the clock past the end and ticking once makes the second simulation finish by itself.
	clock.Advance(2 * time.Minute)
	e.tick(ctx)

	want := []struct {
		typ   EventType
		id    SimulationID
		state State
	}{
		{EventSimulationStarted, halted.ID, StateRunning},
		{EventSimulationStarted, resolved.ID, StateRunning},
		{EventSimulationHalted, halted.ID, StateHalted},
		{EventSimulationResolved, resolved.ID, StateResolved},
	}
	for i, w := range want {
		got := nextEvent(t, events)
		if got.Type != w.typ || got.Simulation.ID != w.id || got.Simulation.State != w.state {
			t.Errorf("event %d = %s for %s in state %s, want %s for %s in state %s",
				i, got.Type, got.Simulation.ID, got.Simulation.State, w.typ, w.id, w.state)
		}
		if w.state.Terminal() && got.Simulation.EndedAt == nil {
			t.Errorf("event %d has no EndedAt but the simulation is over", i)
		}
	}
}

func TestSubscribeEventsClosesOnCancel(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	e := newEventEngine(t, Config{}, clock)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := e.SubscribeEvents(ctx)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}
	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected a closed channel, got an event")
		}
	case <-time.After(eventWait):
		t.Fatal("event channel was not closed after cancel")
	}
}

func TestSubscribeEventsRejectsDoneContext(t *testing.T) {
	e := newEventEngine(t, Config{}, &fakeClock{now: time.Now()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.SubscribeEvents(ctx); err == nil {
		t.Fatal("SubscribeEvents with a done context returned no error")
	}
}

func TestEventsNeverBlockTheEngine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := newEventEngine(t, Config{MaxActive: eventBuffer + 10}, &fakeClock{now: time.Now()})

	// Nobody reads from this channel, so it fills up.
	events, err := e.SubscribeEvents(ctx)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}

	const extra = 3
	done := make(chan error, 1)
	go func() {
		for i := 0; i < eventBuffer+extra; i++ {
			if _, err := e.Inject(ctx, eventScenario()); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Inject: %v", err)
		}
	case <-time.After(eventWait):
		t.Fatal("Inject blocked on a full event listener")
	}

	if got := len(events); got != eventBuffer {
		t.Errorf("buffered events = %d, want %d", got, eventBuffer)
	}
	if got := e.eventsDropped.Load(); got != extra {
		t.Errorf("dropped events = %d, want %d", got, extra)
	}
}
