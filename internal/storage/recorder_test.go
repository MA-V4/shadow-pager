package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// FAKES

// recorderWait is the longest a recorder test waits before it gives up.
const recorderWait = 2 * time.Second

// fakeEvents pretends to be the engine and hands out a channel the test controls.
type fakeEvents struct {
	ch  chan chaos.Event
	err error
}

func (f *fakeEvents) SubscribeEvents(context.Context) (<-chan chaos.Event, error) {
	return f.ch, f.err
}

// fakeWriter pretends to be the store and writes down every simulation it is asked to save.
type fakeWriter struct {
	saved chan chaos.Simulation

	mu       sync.Mutex
	failNext int // how many of the next saves fail
}

func (f *fakeWriter) UpsertSimulation(_ context.Context, sim chaos.Simulation) error {
	f.mu.Lock()
	fail := f.failNext > 0
	if fail {
		f.failNext--
	}
	f.mu.Unlock()
	if fail {
		return errors.New("database is on fire")
	}
	f.saved <- sim
	return nil
}

// startRecorder builds a recorder on fakes and runs it in the background.
func startRecorder(t *testing.T, events *fakeEvents, writer *fakeWriter) (*Recorder, <-chan struct{}) {
	t.Helper()
	rec, err := NewRecorder(context.Background(), events, writer, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		rec.Run()
	}()
	return rec, done
}

func nextSaved(t *testing.T, writer *fakeWriter) chaos.Simulation {
	t.Helper()
	select {
	case sim := <-writer.saved:
		return sim
	case <-time.After(recorderWait):
		t.Fatal("timed out waiting for a simulation to be saved")
	}
	return chaos.Simulation{}
}

// TESTS

func TestRecorderSavesEveryEvent(t *testing.T) {
	events := &fakeEvents{ch: make(chan chaos.Event, 4)}
	writer := &fakeWriter{saved: make(chan chaos.Simulation, 4)}
	_, done := startRecorder(t, events, writer)

	events.ch <- chaos.Event{Type: chaos.EventSimulationStarted, Simulation: testSimulation("sim_1", chaos.StateRunning, 0)}
	events.ch <- chaos.Event{Type: chaos.EventSimulationHalted, Simulation: testSimulation("sim_1", chaos.StateHalted, 0)}

	if got := nextSaved(t, writer); got.ID != "sim_1" || got.State != chaos.StateRunning {
		t.Errorf("first save = %s in state %s, want sim_1 running", got.ID, got.State)
	}
	if got := nextSaved(t, writer); got.State != chaos.StateHalted || got.EndedAt == nil {
		t.Errorf("second save = state %s ended at %v, want halted with an end time", got.State, got.EndedAt)
	}

	close(events.ch)
	select {
	case <-done:
	case <-time.After(recorderWait):
		t.Fatal("Run did not return after the event channel closed")
	}
}

func TestRecorderKeepsGoingWhenTheStoreFails(t *testing.T) {
	events := &fakeEvents{ch: make(chan chaos.Event, 4)}
	writer := &fakeWriter{saved: make(chan chaos.Simulation, 4), failNext: 2}
	rec, done := startRecorder(t, events, writer)

	for _, id := range []chaos.SimulationID{"sim_1", "sim_2", "sim_3"} {
		events.ch <- chaos.Event{Type: chaos.EventSimulationStarted, Simulation: testSimulation(id, chaos.StateRunning, 0)}
	}

	// The first two saves fail, and the recorder must still be alive for the third.
	if got := nextSaved(t, writer); got.ID != "sim_3" {
		t.Errorf("saved %s, want sim_3 after two failed saves", got.ID)
	}

	close(events.ch)
	<-done
	if got := rec.failed.Load(); got != 2 {
		t.Errorf("failed saves = %d, want 2", got)
	}
}

func TestRecorderWritesWaitingEventsBeforeItStops(t *testing.T) {
	events := &fakeEvents{ch: make(chan chaos.Event, 4)}
	writer := &fakeWriter{saved: make(chan chaos.Simulation, 4)}

	// These events are already waiting when the channel closes, as happens at shutdown.
	events.ch <- chaos.Event{Type: chaos.EventSimulationHalted, Simulation: testSimulation("sim_1", chaos.StateHalted, 0)}
	events.ch <- chaos.Event{Type: chaos.EventSimulationHalted, Simulation: testSimulation("sim_2", chaos.StateHalted, 0)}
	close(events.ch)

	_, done := startRecorder(t, events, writer)
	select {
	case <-done:
	case <-time.After(recorderWait):
		t.Fatal("Run did not return")
	}

	if len(writer.saved) != 2 {
		t.Errorf("saved %d simulations, want both waiting events written before stopping", len(writer.saved))
	}
}

func TestNewRecorderErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	writer := &fakeWriter{saved: make(chan chaos.Simulation, 1)}

	subscribeErr := errors.New("engine is gone")
	if _, err := NewRecorder(context.Background(), &fakeEvents{err: subscribeErr}, writer, logger); !errors.Is(err, subscribeErr) {
		t.Errorf("NewRecorder error = %v, want one that wraps the subscribe error", err)
	}
	if _, err := NewRecorder(context.Background(), nil, writer, logger); err == nil {
		t.Error("NewRecorder without an event source returned no error")
	}
}

func TestRecorderWithRealEngineAndDatabase(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine, err := chaos.NewEngine(chaos.Config{}, logger)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec, err := NewRecorder(ctx, engine, store, logger)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		rec.Run()
	}()

	scenario := testSimulation("unused", chaos.StateRunning, 0).Scenario
	kept, err := engine.Inject(ctx, scenario)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	halted, err := engine.Inject(ctx, scenario)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if _, err := engine.Halt(ctx, halted.ID); err != nil {
		t.Fatalf("Halt: %v", err)
	}

	// Stopping makes the recorder write everything that is still waiting, so no polling is needed.
	cancel()
	select {
	case <-done:
	case <-time.After(recorderWait):
		t.Fatal("Run did not return after cancel")
	}

	running, err := store.LoadRunningSimulations(context.Background())
	if err != nil {
		t.Fatalf("LoadRunningSimulations: %v", err)
	}
	if len(running) != 1 || running[0].ID != kept.ID {
		t.Fatalf("running simulations in the database = %+v, want only %s", running, kept.ID)
	}
	if n := countRows(t, store, "simulations"); n != 2 {
		t.Errorf("simulations table has %d rows, want 2", n)
	}
}
