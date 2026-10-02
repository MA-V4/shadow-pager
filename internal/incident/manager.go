package incident

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// ERRORS

// ErrAlreadyRunning means someone tried to start the manager twice.
var ErrAlreadyRunning = errors.New("incident manager already running")

// INTERFACES

// engine lists the only things the manager needs the chaos engine to do.
type engine interface {
	Subscribe(ctx context.Context) (<-chan chaos.MetricSample, error)
	SubscribeEvents(ctx context.Context) (<-chan chaos.Event, error)
	Halt(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error)
}

// Notifier is how the manager tells the outside world what happened to an incident.
type Notifier interface {
	// IncidentDeclared announces a new incident and gives back the channel made for it, if any.
	IncidentDeclared(ctx context.Context, inc Incident) (channelID string, err error)
	// StatusChanged announces that an incident moved to a new status.
	StatusChanged(ctx context.Context, inc Incident) error
	// TimelineEntryAdded announces a new line in the story that did not change the status.
	TimelineEntryAdded(ctx context.Context, inc Incident, entry TimelineEntry) error
}

// NopNotifier is a Notifier that stays silent, for when Slack is switched off.
type NopNotifier struct{}

func (NopNotifier) IncidentDeclared(context.Context, Incident) (string, error) { return "", nil }
func (NopNotifier) StatusChanged(context.Context, Incident) error              { return nil }
func (NopNotifier) TimelineEntryAdded(context.Context, Incident, TimelineEntry) error {
	return nil
}

// MANAGER

const (
	notifyQueueSize      = 256              // how many messages can wait for the notifier
	defaultNotifyTimeout = 15 * time.Second // how long one message to the notifier may take
)

// notificationKind says which Notifier method a queued message is for.
type notificationKind int

const (
	notifyDeclared notificationKind = iota
	notifyStatusChanged
	notifyTimelineEntry
)

// notification is one message waiting to be handed to the Notifier.
type notification struct {
	kind  notificationKind
	inc   Incident
	entry TimelineEntry
}

// Manager watches the engine, opens incidents, and lets people move them along.
type Manager struct {
	engine        engine
	notifier      Notifier
	log           *slog.Logger
	now           func() time.Time
	notifyTimeout time.Duration

	mu        sync.Mutex
	incidents map[IncidentID]*Incident
	bySim     map[chaos.SimulationID]IncidentID       // every simulation that ever got an incident
	sims      map[chaos.SimulationID]chaos.Simulation // simulations that are running right now
	known     map[IncidentID]map[string]bool          // broken limits we already told people about
	halting   map[chaos.SimulationID]bool             // true when the simulation ended while a person was halting it

	jobs    chan notification
	running atomic.Bool
	dropped atomic.Uint64 // messages thrown away because the queue was full
}

// Option is a little helper that changes one setting when the manager is built.
type Option func(*Manager)

// WithClock lets tests give the manager a pretend clock.
func WithClock(now func() time.Time) Option {
	return func(m *Manager) { m.now = now }
}

// WithNotifyTimeout changes how long one message to the notifier may take.
func WithNotifyTimeout(d time.Duration) Option {
	return func(m *Manager) { m.notifyTimeout = d }
}

// NewManager builds a manager that waits quietly until Run is called.
func NewManager(eng engine, notifier Notifier, logger *slog.Logger, opts ...Option) (*Manager, error) {
	if eng == nil {
		return nil, errors.New("incident: engine is required")
	}
	if notifier == nil {
		return nil, errors.New("incident: notifier is required")
	}
	if logger == nil {
		return nil, errors.New("incident: logger is required")
	}
	m := &Manager{
		engine:        eng,
		notifier:      notifier,
		log:           logger.With(slog.String("component", "incident_manager")),
		now:           time.Now,
		notifyTimeout: defaultNotifyTimeout,
		incidents:     make(map[IncidentID]*Incident),
		bySim:         make(map[chaos.SimulationID]IncidentID),
		sims:          make(map[chaos.SimulationID]chaos.Simulation),
		known:         make(map[IncidentID]map[string]bool),
		halting:       make(map[chaos.SimulationID]bool),
		jobs:          make(chan notification, notifyQueueSize),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m, nil
}

// Run listens to the engine and opens or updates incidents until ctx is done.
func (m *Manager) Run(ctx context.Context) error {
	if !m.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer m.running.Store(false)

	samples, err := m.engine.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("subscribe to samples: %w", err)
	}
	events, err := m.engine.SubscribeEvents(ctx)
	if err != nil {
		return fmt.Errorf("subscribe to events: %w", err)
	}

	// The notifier gets its own goroutine so a slow Slack never slows down the samples.
	notifyDone := make(chan struct{})
	go func() {
		defer close(notifyDone)
		m.notifyLoop(ctx)
	}()

	m.log.InfoContext(ctx, "incident manager started")
	for {
		select {
		case <-ctx.Done():
			<-notifyDone
			m.log.InfoContext(ctx, "incident manager stopped", slog.Uint64("dropped_notifications", m.dropped.Load()))
			return nil
		case sample, ok := <-samples:
			if !ok {
				// A nil channel is never ready, so this case goes quiet.
				samples = nil
				continue
			}
			m.handleSample(ctx, sample)
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			m.handleEvent(ctx, ev)
		}
	}
}

// handleSample opens an incident on the first bad sample and notes any newly broken limits.
func (m *Manager) handleSample(ctx context.Context, sample chaos.MetricSample) {
	if sample.Healthy || len(sample.Breaches) == 0 {
		return
	}

	m.mu.Lock()
	if id, ok := m.bySim[sample.SimulationID]; ok {
		job, changed := m.noteNewBreachesLocked(id, sample.Breaches)
		m.mu.Unlock()
		if changed {
			m.enqueue(ctx, job)
		}
		return
	}

	id, err := newIncidentID()
	if err != nil {
		m.mu.Unlock()
		m.log.ErrorContext(ctx, "incident not declared", slog.Any("error", err))
		return
	}
	sim, ok := m.sims[sample.SimulationID]
	if !ok {
		// We never saw this simulation start, so we build what we can from the sample.
		sim = chaos.Simulation{
			ID:       sample.SimulationID,
			Scenario: chaos.Scenario{Service: sample.Service, Mode: sample.Mode, Severity: chaos.SeverityMajor},
		}
	}
	inc := New(id, sim, sample.Breaches, m.now())
	m.incidents[id] = &inc
	m.bySim[sample.SimulationID] = id
	m.known[id] = make(map[string]bool, len(sample.Breaches))
	for _, b := range sample.Breaches {
		m.known[id][b] = true
	}
	snap := inc.Clone()
	m.mu.Unlock()

	m.log.InfoContext(ctx, "incident declared",
		slog.String("incident_id", string(id)),
		slog.String("simulation_id", string(sample.SimulationID)),
		slog.String("target_service", snap.Service),
		slog.String("severity", string(snap.Severity)),
		slog.Any("breaches", snap.Breaches),
	)
	m.enqueue(ctx, notification{kind: notifyDeclared, inc: snap})
}

// noteNewBreachesLocked adds a story line when a limit breaks that we have not seen before.
func (m *Manager) noteNewBreachesLocked(id IncidentID, breaches []string) (notification, bool) {
	inc := m.incidents[id]
	if inc.Status != StatusDeclared && inc.Status != StatusAcknowledged {
		return notification{}, false
	}
	var fresh []string
	for _, b := range breaches {
		if !m.known[id][b] {
			m.known[id][b] = true
			fresh = append(fresh, b)
		}
	}
	if len(fresh) == 0 {
		return notification{}, false
	}
	entry := inc.AddEntry(m.now(), ActorSystem, "New SLO breach: "+strings.Join(fresh, ", "))
	return notification{kind: notifyTimelineEntry, inc: inc.Clone(), entry: entry}, true
}

// handleEvent remembers running simulations and marks an incident mitigated when its simulation ends.
func (m *Manager) handleEvent(ctx context.Context, ev chaos.Event) {
	simID := ev.Simulation.ID

	m.mu.Lock()
	if ev.Type == chaos.EventSimulationStarted {
		m.sims[simID] = ev.Simulation
		m.mu.Unlock()
		return
	}
	delete(m.sims, simID)

	if _, busy := m.halting[simID]; busy {
		// A person is halting this simulation right now, so Mitigate will finish the job.
		m.halting[simID] = true
		m.mu.Unlock()
		return
	}
	job, changed := m.autoMitigateLocked(simID, ev.Type)
	m.mu.Unlock()

	if changed {
		m.log.InfoContext(ctx, "incident mitigated because the simulation ended",
			slog.String("incident_id", string(job.inc.ID)),
			slog.String("simulation_id", string(simID)),
		)
		m.enqueue(ctx, job)
	}
}

// autoMitigateLocked marks the incident of an ended simulation as mitigated, if it is still open.
func (m *Manager) autoMitigateLocked(simID chaos.SimulationID, why chaos.EventType) (notification, bool) {
	id, ok := m.bySim[simID]
	if !ok {
		return notification{}, false
	}
	inc := m.incidents[id]
	text := "Simulation ended on its own, marked as mitigated"
	if why == chaos.EventSimulationHalted {
		text = "Simulation was halted, marked as mitigated"
	}
	if err := inc.Transition(StatusMitigated, m.now(), ActorSystem, text); err != nil {
		return notification{}, false
	}
	return notification{kind: notifyStatusChanged, inc: inc.Clone()}, true
}

// RESPONDER ACTIONS

// Acknowledge records that a person has seen the incident and is working on it.
func (m *Manager) Acknowledge(ctx context.Context, id IncidentID, actor string) (Incident, error) {
	return m.transition(ctx, id, StatusAcknowledged, actor, "Acknowledged")
}

// Resolve records that a person has declared the incident over.
func (m *Manager) Resolve(ctx context.Context, id IncidentID, actor string) (Incident, error) {
	return m.transition(ctx, id, StatusResolved, actor, "Resolved")
}

// Mitigate halts the simulation behind the incident and marks the incident as mitigated.
func (m *Manager) Mitigate(ctx context.Context, id IncidentID, actor string) (Incident, error) {
	if err := ctx.Err(); err != nil {
		return Incident{}, fmt.Errorf("mitigate incident %s: %w", id, err)
	}

	m.mu.Lock()
	inc, ok := m.incidents[id]
	if !ok {
		m.mu.Unlock()
		return Incident{}, fmt.Errorf("mitigate incident %s: %w", id, ErrNotFound)
	}
	if !inc.Status.CanTransitionTo(StatusMitigated) {
		status := inc.Status
		m.mu.Unlock()
		return Incident{}, fmt.Errorf("mitigate incident %s from %s: %w", id, status, ErrInvalidTransition)
	}
	simID := inc.SimulationID
	m.halting[simID] = false
	m.mu.Unlock()

	// The engine is called with no lock held, like every other slow or outside call.
	_, haltErr := m.engine.Halt(ctx, simID)

	m.mu.Lock()
	endedMeanwhile := m.halting[simID]
	delete(m.halting, simID)

	// A simulation that already stopped is fine, because stopping it is all we wanted.
	if haltErr != nil && !errors.Is(haltErr, chaos.ErrNotActive) {
		var job notification
		changed := false
		if endedMeanwhile {
			job, changed = m.autoMitigateLocked(simID, chaos.EventSimulationResolved)
		}
		m.mu.Unlock()
		if changed {
			m.enqueue(ctx, job)
		}
		return Incident{}, fmt.Errorf("mitigate incident %s: halt simulation: %w", id, haltErr)
	}

	if err := inc.Transition(StatusMitigated, m.now(), actor, "Mitigated: simulation halted"); err != nil {
		m.mu.Unlock()
		return Incident{}, fmt.Errorf("mitigate: %w", err)
	}
	snap := inc.Clone()
	m.mu.Unlock()

	m.logStatus(ctx, snap, actor)
	m.enqueue(ctx, notification{kind: notifyStatusChanged, inc: snap})
	return snap, nil
}

// transition moves an incident to a new status on behalf of a person.
func (m *Manager) transition(ctx context.Context, id IncidentID, next Status, actor, text string) (Incident, error) {
	if err := ctx.Err(); err != nil {
		return Incident{}, fmt.Errorf("move incident %s to %s: %w", id, next, err)
	}

	m.mu.Lock()
	inc, ok := m.incidents[id]
	if !ok {
		m.mu.Unlock()
		return Incident{}, fmt.Errorf("move incident %s to %s: %w", id, next, ErrNotFound)
	}
	if err := inc.Transition(next, m.now(), actor, text); err != nil {
		m.mu.Unlock()
		return Incident{}, fmt.Errorf("move to %s: %w", next, err)
	}
	snap := inc.Clone()
	m.mu.Unlock()

	m.logStatus(ctx, snap, actor)
	m.enqueue(ctx, notification{kind: notifyStatusChanged, inc: snap})
	return snap, nil
}

// logStatus writes down that a person moved an incident.
func (m *Manager) logStatus(ctx context.Context, inc Incident, actor string) {
	m.log.InfoContext(ctx, "incident status changed",
		slog.String("incident_id", string(inc.ID)),
		slog.String("status", string(inc.Status)),
		slog.String("actor", actor),
	)
}

// QUERIES

// Get gives back a copy of one incident.
func (m *Manager) Get(ctx context.Context, id IncidentID) (Incident, error) {
	if err := ctx.Err(); err != nil {
		return Incident{}, fmt.Errorf("get incident %s: %w", id, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.incidents[id]
	if !ok {
		return Incident{}, fmt.Errorf("get incident %s: %w", id, ErrNotFound)
	}
	return inc.Clone(), nil
}

// List gives back copies of all incidents, newest first.
func (m *Manager) List(ctx context.Context) ([]Incident, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}

	m.mu.Lock()
	out := make([]Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		out = append(out, inc.Clone())
	}
	m.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].DeclaredAt.Equal(out[j].DeclaredAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].DeclaredAt.After(out[j].DeclaredAt)
	})
	return out, nil
}

// NOTIFICATIONS

// enqueue puts a message in line for the notifier and never waits.
func (m *Manager) enqueue(ctx context.Context, job notification) {
	select {
	case m.jobs <- job:
	default:
		m.dropped.Add(1)
		m.log.ErrorContext(ctx, "notification dropped because the queue is full",
			slog.String("incident_id", string(job.inc.ID)),
		)
	}
}

// notifyLoop hands queued messages to the notifier one at a time, in order.
func (m *Manager) notifyLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-m.jobs:
			if err := m.deliver(ctx, job); err != nil {
				m.log.ErrorContext(ctx, "notifier failed",
					slog.String("incident_id", string(job.inc.ID)),
					slog.Any("error", err),
				)
			}
		}
	}
}

// deliver calls the right Notifier method for one message, with a time limit.
func (m *Manager) deliver(ctx context.Context, job notification) error {
	ctx, cancel := context.WithTimeout(ctx, m.notifyTimeout)
	defer cancel()

	// The channel may have been made after this copy was taken, so we look it up again.
	m.mu.Lock()
	if inc, ok := m.incidents[job.inc.ID]; ok {
		job.inc.ChannelID = inc.ChannelID
	}
	m.mu.Unlock()

	switch job.kind {
	case notifyDeclared:
		channelID, err := m.notifier.IncidentDeclared(ctx, job.inc)
		if channelID != "" {
			m.mu.Lock()
			if inc, ok := m.incidents[job.inc.ID]; ok {
				inc.ChannelID = channelID
			}
			m.mu.Unlock()
		}
		if err != nil {
			return fmt.Errorf("notify incident declared: %w", err)
		}
	case notifyStatusChanged:
		if err := m.notifier.StatusChanged(ctx, job.inc); err != nil {
			return fmt.Errorf("notify status changed to %s: %w", job.inc.Status, err)
		}
	case notifyTimelineEntry:
		if err := m.notifier.TimelineEntryAdded(ctx, job.inc, job.entry); err != nil {
			return fmt.Errorf("notify timeline entry: %w", err)
		}
	}
	return nil
}

// HELPERS

// newIncidentID makes a random name for a new incident.
func newIncidentID() (IncidentID, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate incident id: %w", err)
	}
	return IncidentID("inc_" + hex.EncodeToString(b[:])), nil
}
