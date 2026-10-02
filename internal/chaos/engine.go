// Package chaos simulates infrastructure failures. An Engine owns a set of
// running Simulations, advances them on a fixed tick, asks a Generator for
// telemetry at each step, and fans those samples out to subscribers over
// channels.
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

// FailureMode is the kind of fault being simulated.
type FailureMode string

const (
	FailureLatencySpike     FailureMode = "latency_spike"
	FailureErrorRateSurge   FailureMode = "error_rate_surge"
	FailureMemoryLeak       FailureMode = "memory_leak"
	FailureCPUSaturation    FailureMode = "cpu_saturation"
	FailureDBPoolExhaustion FailureMode = "db_pool_exhaustion"
)

// Valid reports whether m is a mode the engine knows how to simulate.
func (m FailureMode) Valid() bool {
	switch m {
	case FailureLatencySpike, FailureErrorRateSurge, FailureMemoryLeak,
		FailureCPUSaturation, FailureDBPoolExhaustion:
		return true
	}
	return false
}

// Severity mirrors how an incident would be triaged once declared.
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

// State is a simulation's position in its lifecycle:
//
//	running ──(duration elapses)──▶ resolved
//	   └──────(Halt called)──────▶ halted
type State string

const (
	StateRunning  State = "running"
	StateResolved State = "resolved"
	StateHalted   State = "halted"
)

// Terminal reports whether no further transitions are possible.
func (s State) Terminal() bool { return s == StateResolved || s == StateHalted }

// SimulationID uniquely identifies one simulation run.
type SimulationID string

// Scenario is the request to inject a failure: what breaks, how badly, and
// for how long. It is the dashboard's input and contains no runtime state.
type Scenario struct {
	Service   string        `json:"service"`
	Mode      FailureMode   `json:"mode"`
	Severity  Severity      `json:"severity"`
	Intensity float64       `json:"intensity"` // 0.0 (barely noticeable) to 1.0 (total outage)
	Duration  time.Duration `json:"duration"`
}

const (
	minDuration = 10 * time.Second
	maxDuration = 30 * time.Minute // keeps free-tier compute bounded
)

// Validate checks every field and reports all problems at once, each
// wrapping ErrInvalidScenario so callers can map it to a 400 with errors.Is.
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

// Simulation is a Scenario in flight. The engine hands out copies, never
// pointers, so callers can never mutate state behind the engine's lock.
type Simulation struct {
	ID        SimulationID `json:"id"`
	Scenario  Scenario     `json:"scenario"`
	State     State        `json:"state"`
	StartedAt time.Time    `json:"started_at"`
	EndsAt    time.Time    `json:"ends_at"`
	EndedAt   *time.Time   `json:"ended_at,omitempty"` // nil while running
}

// EXTENSION POINTS

// Generator produces telemetry for one failure mode. Implementations live in
// metrics.go (Phase 2). Sample must be safe for concurrent use and should be
// a cheap, pure computation: it runs on the engine's tick path, so anything
// that blocks here stalls every simulation.
type Generator interface {
	Sample(sim Simulation, elapsed time.Duration) MetricSample
}

// Note what is NOT here: a big "Simulator" interface describing the Engine.
// In Go, interfaces belong to the consumer. The HTTP and Slack packages will
// each declare the one or two methods they need, and *Engine will satisfy
// them implicitly. The engine only declares interfaces for things it consumes.

// ENGINE

// Config tunes the engine. Zero values are replaced with sane defaults.
type Config struct {
	TickInterval     time.Duration // how often simulations advance
	MaxActive        int           // cap on concurrent running simulations
	SubscriberBuffer int           // per-subscriber channel capacity
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

// Engine runs simulations and broadcasts their telemetry. Construct one with
// NewEngine, start it with Run, and stop it by cancelling Run's context.
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
	dropped atomic.Uint64 // samples discarded because a subscriber was slow
}

// Option customises an Engine at construction time.
type Option func(*Engine)

// WithGenerator registers the telemetry source for a failure mode.
func WithGenerator(mode FailureMode, g Generator) Option {
	return func(e *Engine) { e.generators[mode] = g }
}

// WithClock overrides time.Now, which makes lifecycle tests deterministic.
func WithClock(now func() time.Time) Option {
	return func(e *Engine) { e.now = now }
}

// NewEngine builds an idle engine. Nothing runs until Run is called.
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

// Ready reports whether the tick loop is live. Backs the /readyz probe.
func (e *Engine) Ready() bool { return e.running.Load() }

// Run drives the tick loop until ctx is cancelled. A clean, requested stop
// returns nil; anything else is a real failure.
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

// tick advances every running simulation by one step.
func (e *Engine) tick(ctx context.Context) {
	now := e.now()

	// Phase 1: transition state and collect work under the write lock.
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

	// Phase 2: generate samples with no lock held, so a slow Generator never
	// blocks Inject, Halt, or Subscribe.
	for _, sim := range active {
		gen, ok := e.generators[sim.Scenario.Mode]
		if !ok {
			e.log.DebugContext(ctx, "no generator registered", slog.String("mode", string(sim.Scenario.Mode)))
			continue
		}
		e.broadcast(gen.Sample(sim, now.Sub(sim.StartedAt)))
	}
}

// broadcast fans a sample out without ever blocking. A subscriber that
// cannot keep up loses samples rather than stalling the whole engine: for
// live telemetry, fresh data beats complete data.
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

// Inject validates a scenario and starts simulating it immediately.
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
		slog.String("service", sc.Service),
		slog.String("mode", string(sc.Mode)),
		slog.String("severity", string(sc.Severity)),
	)
	return *sim, nil
}

// Halt stops a running simulation early, as a responder "fixing" it would.
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

// Get returns a snapshot of one simulation.
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

// List returns snapshots of all simulations, newest first.
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

// Subscribe returns a channel of live telemetry. The subscription lives
// exactly as long as ctx: when ctx is cancelled (for example, a WebSocket
// client disconnects) the channel is unregistered and closed, so the reader's
// `for sample := range ch` loop ends naturally with no leaked goroutines.
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
		// Taking the write lock guarantees broadcast is not mid-send on ch,
		// so closing here can never panic with "send on closed channel".
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
