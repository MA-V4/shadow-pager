// Package chaos pretends that computers are breaking so people can practice fixing them.
package chaos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ERRORS

var (
	ErrInvalidScenario = errors.New("invalid scenario")
	ErrNotFound        = errors.New("simulation not found")
	ErrCapacity        = errors.New("active simulation limit reached")
	ErrAlreadyRunning  = errors.New("engine already running")
	ErrNotActive       = errors.New("simulation is not active")
)

// DOMAIN TYPES

// FailureMode is the kind of thing that breaks.
type FailureMode string

const (
	FailureLatencySpike     FailureMode = "latency_spike"
	FailureErrorRateSurge   FailureMode = "error_rate_surge"
	FailureMemoryLeak       FailureMode = "memory_leak"
	FailureCPUSaturation    FailureMode = "cpu_saturation"
	FailureDBPoolExhaustion FailureMode = "db_pool_exhaustion"
)

// Valid tells us if the engine knows how to pretend this kind of break.
func (m FailureMode) Valid() bool {
	switch m {
	case FailureLatencySpike, FailureErrorRateSurge, FailureMemoryLeak,
		FailureCPUSaturation, FailureDBPoolExhaustion:
		return true
	}
	return false
}

// Severity is how bad the problem is.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityMajor    Severity = "major"
	SeverityMinor    Severity = "minor"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityCritical, SeverityMajor, SeverityMinor:
		return true
	}
	return false
}

// State tells us if a simulation is still going, finished by itself, or was stopped early.
type State string

const (
	StateRunning  State = "running"
	StateResolved State = "resolved"
	StateHalted   State = "halted"
)

// Terminal tells us if a simulation is all done and can never change again.
func (s State) Terminal() bool { return s == StateResolved || s == StateHalted }

// SimulationID is the special name of one simulation.
type SimulationID string

// Scenario is the wish list that says what breaks, how badly, and for how long.
type Scenario struct {
	Service   string        `json:"service"`
	Mode      FailureMode   `json:"mode"`
	Severity  Severity      `json:"severity"`
	Intensity float64       `json:"intensity"` // 0 is a tiny problem and 1 is everything broken
	Duration  time.Duration `json:"duration"`
}

const (
	minDuration = 10 * time.Second
	maxDuration = 30 * time.Minute // a limit so our free computer does not get tired
)

// Validate checks the whole wish list and tells us everything that is wrong with it.
func (s Scenario) Validate() error {
	var errs []error
	if s.Service == "" {
		errs = append(errs, fmt.Errorf("%w: service is required", ErrInvalidScenario))
	}
	if !s.Mode.Valid() {
		errs = append(errs, fmt.Errorf("%w: unknown failure mode %q", ErrInvalidScenario, s.Mode))
	}
	if !s.Severity.Valid() {
		errs = append(errs, fmt.Errorf("%w: unknown severity %q", ErrInvalidScenario, s.Severity))
	}
	if s.Intensity < 0 || s.Intensity > 1 {
		errs = append(errs, fmt.Errorf("%w: intensity %.2f outside [0, 1]", ErrInvalidScenario, s.Intensity))
	}
	if s.Duration < minDuration || s.Duration > maxDuration {
		errs = append(errs, fmt.Errorf("%w: duration %s outside [%s, %s]", ErrInvalidScenario, s.Duration, minDuration, maxDuration))
	}
	return errors.Join(errs...)
}

// Simulation is a Scenario that has been started.
type Simulation struct {
	ID        SimulationID `json:"id"`
	Scenario  Scenario     `json:"scenario"`
	State     State        `json:"state"`
	StartedAt time.Time    `json:"started_at"`
	EndsAt    time.Time    `json:"ends_at"`
	EndedAt   *time.Time   `json:"ended_at,omitempty"` // empty until the simulation ends
}

// EXTENSION POINTS

// Generator makes the pretend numbers for one kind of break, and it must be quick.
type Generator interface {
	Sample(sim Simulation, elapsed time.Duration) MetricSample
}

// Other packages write their own small interfaces for the engine, so there is no big one here.

// ENGINE

// Config holds the engine's settings, and empty ones get good default values.
type Config struct {
	TickInterval     time.Duration // how often simulations take a step
	MaxActive        int           // how many simulations can run at the same time
	SubscriberBuffer int           // how many samples can wait in line for each listener
}

func (c Config) withDefaults() Config {
	if c.TickInterval <= 0 {
		c.TickInterval = time.Second
	}
	if c.MaxActive <= 0 {
		c.MaxActive = 10
	}
	if c.SubscriberBuffer <= 0 {
		c.SubscriberBuffer = 64
	}
	return c
}

// Engine runs the simulations and shares their numbers with everyone who is listening.
type Engine struct {
	cfg        Config
	log        *slog.Logger
	now        func() time.Time
	generators map[FailureMode]Generator

	mu      sync.RWMutex
	sims    map[SimulationID]*Simulation
	subs    map[uint64]chan MetricSample
	nextSub uint64

	running atomic.Bool
	dropped atomic.Uint64 // samples thrown away because a listener was too slow
}

// Option is a little helper that changes one setting when the engine is built.
type Option func(*Engine)

// WithGenerator tells the engine who makes the numbers for one kind of break.
func WithGenerator(mode FailureMode, g Generator) Option {
	return func(e *Engine) { e.generators[mode] = g }
}

// WithClock lets tests give the engine a pretend clock.
func WithClock(now func() time.Time) Option {
	return func(e *Engine) { e.now = now }
}

// NewEngine builds an engine that waits quietly until Run is called.
func NewEngine(cfg Config, logger *slog.Logger, opts ...Option) (*Engine, error) {
	if logger == nil {
		return nil, errors.New("chaos: logger is required")
	}
	e := &Engine{
		cfg:        cfg.withDefaults(),
		log:        logger.With(slog.String("component", "chaos_engine")),
		now:        time.Now,
		generators: make(map[FailureMode]Generator),
		sims:       make(map[SimulationID]*Simulation),
		subs:       make(map[uint64]chan MetricSample),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e, nil
}

// Ready tells us if the engine is awake and ticking.
func (e *Engine) Ready() bool { return e.running.Load() }

// Run keeps the engine ticking until someone tells it to stop.
func (e *Engine) Run(ctx context.Context) error {
	if !e.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer e.running.Store(false)

	ticker := time.NewTicker(e.cfg.TickInterval)
	defer ticker.Stop()

	e.log.InfoContext(ctx, "engine started",
		slog.Duration("tick_interval", e.cfg.TickInterval),
		slog.Int("max_active", e.cfg.MaxActive),
	)

	for {
		select {
		case <-ctx.Done():
			e.log.InfoContext(ctx, "engine stopped", slog.Uint64("dropped_samples", e.dropped.Load()))
			return nil
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

// tick moves every running simulation forward by one step.
func (e *Engine) tick(ctx context.Context) {
	now := e.now()

	// First we lock the door, update each simulation, and remember which ones are still running.
	e.mu.Lock()
	active := make([]Simulation, 0, len(e.sims))
	for _, sim := range e.sims {
		if sim.State != StateRunning {
			continue
		}
		if !now.Before(sim.EndsAt) {
			sim.State, sim.EndedAt = StateResolved, &now
			e.log.InfoContext(ctx, "simulation resolved", slog.String("simulation_id", string(sim.ID)))
			continue
		}
		active = append(active, *sim)
	}
	e.mu.Unlock()

	// Then we make the numbers with the door unlocked so nobody has to wait.
	for _, sim := range active {
		gen, ok := e.generators[sim.Scenario.Mode]
		if !ok {
			e.log.DebugContext(ctx, "no generator registered", slog.String("mode", string(sim.Scenario.Mode)))
			continue
		}
		e.broadcast(gen.Sample(sim, now.Sub(sim.StartedAt)))
	}
}

// broadcast sends a sample to every listener and skips anyone who is too slow.
func (e *Engine) broadcast(sample MetricSample) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, ch := range e.subs {
		select {
		case ch <- sample:
		default:
			e.dropped.Add(1)
		}
	}
}

// CONTROL API

// Inject checks a scenario and starts it right away.
func (e *Engine) Inject(ctx context.Context, sc Scenario) (Simulation, error) {
	if err := ctx.Err(); err != nil {
		return Simulation{}, fmt.Errorf("inject: %w", err)
	}
	if err := sc.Validate(); err != nil {
		return Simulation{}, fmt.Errorf("inject: %w", err)
	}

	id, err := newSimulationID()
	if err != nil {
		return Simulation{}, fmt.Errorf("inject: %w", err)
	}

	now := e.now()
	sim := &Simulation{
		ID:        id,
		Scenario:  sc,
		State:     StateRunning,
		StartedAt: now,
		EndsAt:    now.Add(sc.Duration),
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	activeCount := 0
	for _, s := range e.sims {
		if s.State == StateRunning {
			activeCount++
		}
	}
	if activeCount >= e.cfg.MaxActive {
		return Simulation{}, fmt.Errorf("inject: %w (%d)", ErrCapacity, e.cfg.MaxActive)
	}
	e.sims[id] = sim

	e.log.InfoContext(ctx, "simulation injected",
		slog.String("simulation_id", string(id)),
		slog.String("target_service", sc.Service),
		slog.String("mode", string(sc.Mode)),
		slog.String("severity", string(sc.Severity)),
	)
	return *sim, nil
}

// Halt stops a running simulation early, like someone fixing the problem.
func (e *Engine) Halt(ctx context.Context, id SimulationID) (Simulation, error) {
	if err := ctx.Err(); err != nil {
		return Simulation{}, fmt.Errorf("halt %s: %w", id, err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	sim, ok := e.sims[id]
	if !ok {
		return Simulation{}, fmt.Errorf("halt %s: %w", id, ErrNotFound)
	}
	if sim.State.Terminal() {
		return Simulation{}, fmt.Errorf("halt %s (state %s): %w", id, sim.State, ErrNotActive)
	}
	ended := e.now()
	sim.State, sim.EndedAt = StateHalted, &ended

	e.log.InfoContext(ctx, "simulation halted", slog.String("simulation_id", string(id)))
	return *sim, nil
}

// Get gives back a copy of one simulation.
func (e *Engine) Get(ctx context.Context, id SimulationID) (Simulation, error) {
	if err := ctx.Err(); err != nil {
		return Simulation{}, fmt.Errorf("get %s: %w", id, err)
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	sim, ok := e.sims[id]
	if !ok {
		return Simulation{}, fmt.Errorf("get %s: %w", id, ErrNotFound)
	}
	return *sim, nil
}

// List gives back copies of all simulations, newest first.
func (e *Engine) List(ctx context.Context) ([]Simulation, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}

	e.mu.RLock()
	out := make([]Simulation, 0, len(e.sims))
	for _, sim := range e.sims {
		out = append(out, *sim)
	}
	e.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

// Subscribe gives back a channel of live numbers that closes when ctx is done.
func (e *Engine) Subscribe(ctx context.Context) (<-chan MetricSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}

	ch := make(chan MetricSample, e.cfg.SubscriberBuffer)

	e.mu.Lock()
	id := e.nextSub
	e.nextSub++
	e.subs[id] = ch
	e.mu.Unlock()

	go func() {
		<-ctx.Done()
		// We lock the door before closing the channel so nobody is sending on it at the same time.
		e.mu.Lock()
		delete(e.subs, id)
		e.mu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// HELPERS

func newSimulationID() (SimulationID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate simulation id: %w", err)
	}
	return SimulationID("sim_" + hex.EncodeToString(b[:])), nil
}
