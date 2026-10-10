package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// RECORDER

// eventSource lists the only thing the recorder needs the engine to do.
type eventSource interface {
	SubscribeEvents(ctx context.Context) (<-chan chaos.Event, error)
}

// simulationWriter lists the only thing the recorder needs the store to do.
type simulationWriter interface {
	UpsertSimulation(ctx context.Context, sim chaos.Simulation) error
}

// recorderWriteTimeout is how long one save of a simulation may take.
const recorderWriteTimeout = 5 * time.Second

// Recorder listens to the engine and saves every simulation when it starts and when it ends.
type Recorder struct {
	events  <-chan chaos.Event
	store   simulationWriter
	log     *slog.Logger
	timeout time.Duration
	failed  atomic.Uint64 // saves that did not work
}

// NewRecorder starts listening to the engine right away, so no event can slip past before Run.
func NewRecorder(ctx context.Context, source eventSource, store simulationWriter, logger *slog.Logger) (*Recorder, error) {
	if source == nil || store == nil || logger == nil {
		return nil, errors.New("storage: recorder needs an event source, a store and a logger")
	}
	events, err := source.SubscribeEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("subscribe recorder to events: %w", err)
	}
	return &Recorder{
		events:  events,
		store:   store,
		log:     logger.With(slog.String("component", "recorder")),
		timeout: recorderWriteTimeout,
	}, nil
}

// Run saves simulations until the context given to NewRecorder is done and every waiting event is written.
func (r *Recorder) Run() {
	r.log.Info("recorder started")
	// The engine closes this channel when the context ends, and the events still waiting in it are read first.
	for ev := range r.events {
		r.record(ev)
	}
	r.log.Info("recorder stopped", slog.Uint64("failed_writes", r.failed.Load()))
}

// record saves one simulation with a time limit, and only writes a log line when it fails.
func (r *Recorder) record(ev chaos.Event) {
	// A fresh context lets the last saves go through while the program shuts down.
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	if err := r.store.UpsertSimulation(ctx, ev.Simulation); err != nil {
		r.failed.Add(1)
		r.log.ErrorContext(ctx, "simulation not saved, the engine carries on",
			slog.String("event", string(ev.Type)),
			slog.String("simulation_id", string(ev.Simulation.ID)),
			slog.Any("error", err),
		)
	}
}
