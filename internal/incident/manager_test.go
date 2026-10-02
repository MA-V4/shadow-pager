package incident

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

// testWait is the longest a test waits before it gives up.
const testWait = 2 * time.Second

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

// fakeEngine pretends to be the chaos engine and lets the test push samples and events by hand.
type fakeEngine struct {
	// These channels have no buffer, so a send only finishes when the manager takes the item.
	samples chan chaos.MetricSample
	events  chan chaos.Event

	mu      sync.Mutex
	haltErr error
	halted  []chaos.SimulationID
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		samples: make(chan chaos.MetricSample),
		events:  make(chan chaos.Event),
	}
}

func (f *fakeEngine) Subscribe(context.Context) (<-chan chaos.MetricSample, error) {
	return f.samples, nil
}

func (f *fakeEngine) SubscribeEvents(context.Context) (<-chan chaos.Event, error) {
	return f.events, nil
}

func (f *fakeEngine) Halt(_ context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.halted = append(f.halted, id)
	return chaos.Simulation{ID: id, State: chaos.StateHalted}, f.haltErr
}

func (f *fakeEngine) haltedIDs() []chaos.SimulationID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chaos.SimulationID(nil), f.halted...)
}

// fakeNotifier writes down every call on a channel so tests can wait for it.
type fakeNotifier struct {
	declared chan Incident
	changed  chan Incident
	entries  chan TimelineEntry

	channelID  string
	declareErr error
	block      chan struct{} // when set, IncidentDeclared waits here until the test closes it
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{
		declared:  make(chan Incident, 16),
		changed:   make(chan Incident, 16),
		entries:   make(chan TimelineEntry, 16),
		channelID: "C123",
	}
}

func (f *fakeNotifier) IncidentDeclared(ctx context.Context, inc Incident) (string, error) {
	f.declared <- inc
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.declareErr != nil {
		return "", f.declareErr
	}
	return f.channelID, nil
}

func (f *fakeNotifier) StatusChanged(_ context.Context, inc Incident) error {
	f.changed <- inc
	return nil
}

func (f *fakeNotifier) TimelineEntryAdded(_ context.Context, _ Incident, entry TimelineEntry) error {
	f.entries <- entry
	return nil
}

// HARNESS

// harness holds a running manager together with its fakes.
type harness struct {
	t        *testing.T
	manager  *Manager
	engine   *fakeEngine
	notifier *fakeNotifier
	clock    *fakeClock
}

// startManager builds a manager on top of fakes and runs it until the test ends.
func startManager(t *testing.T, notifier *fakeNotifier) *harness {
	t.Helper()
	eng := newFakeEngine()
	clock := &fakeClock{now: testStart}
	m, err := NewManager(eng, notifier, slog.New(slog.NewTextHandler(io.Discard, nil)), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v, want nil", err)
			}
		case <-time.After(testWait):
			t.Error("Run did not return after cancel")
		}
	})

	return &harness{t: t, manager: m, engine: eng, notifier: notifier, clock: clock}
}

// sample hands one sample to the manager and waits until it is taken.
func (h *harness) sample(simID chaos.SimulationID, breaches ...string) {
	h.t.Helper()
	s := chaos.MetricSample{
		SimulationID: simID,
		Service:      "payments-api",
		Mode:         chaos.FailureLatencySpike,
		Timestamp:    h.clock.Now(),
		Healthy:      len(breaches) == 0,
		Breaches:     breaches,
	}
	select {
	case h.engine.samples <- s:
	case <-time.After(testWait):
		h.t.Fatal("manager did not take the sample, so sample handling is stuck")
	}
}

// event hands one lifecycle event to the manager and waits until it is taken.
func (h *harness) event(typ chaos.EventType, simID chaos.SimulationID) {
	h.t.Helper()
	select {
	case h.engine.events <- chaos.Event{Type: typ, Simulation: testSimulation(simID)}:
	case <-time.After(testWait):
		h.t.Fatal("manager did not take the event, so event handling is stuck")
	}
}

// settle returns once the manager has fully finished everything sent before it.
func (h *harness) settle() {
	h.t.Helper()
	// The manager works on one item at a time, so taking this healthy sample means the earlier ones are done.
	h.sample("sim_settle")
}

// declare starts a simulation, breaks its limit, and gives back the incident that was opened.
func (h *harness) declare(simID chaos.SimulationID) Incident {
	h.t.Helper()
	h.event(chaos.EventSimulationStarted, simID)
	h.sample(simID, "latency_p99")
	return recv(h.t, h.notifier.declared, "incident declared")
}

// recv waits for one value on a channel and fails the test if none comes.
func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(testWait):
		t.Fatalf("timed out waiting for %s", what)
	}
	var zero T
	return zero
}

func (h *harness) list() []Incident {
	h.t.Helper()
	out, err := h.manager.List(context.Background())
	if err != nil {
		h.t.Fatalf("List: %v", err)
	}
	return out
}

// TESTS

func TestManagerDeclaresOnFirstBreach(t *testing.T) {
	h := startManager(t, newFakeNotifier())

	h.event(chaos.EventSimulationStarted, "sim_1")
	h.sample("sim_1")
	h.settle()
	if got := h.list(); len(got) != 0 {
		t.Fatalf("a healthy sample opened %d incidents, want 0", len(got))
	}

	h.sample("sim_1", "latency_p99", "error_rate")
	inc := recv(t, h.notifier.declared, "incident declared")

	if inc.Status != StatusDeclared {
		t.Errorf("Status = %s, want %s", inc.Status, StatusDeclared)
	}
	if inc.SimulationID != "sim_1" || inc.Service != "payments-api" || inc.Mode != chaos.FailureLatencySpike {
		t.Errorf("incident = %+v, want it to describe sim_1 on payments-api", inc)
	}
	if inc.Severity != chaos.SeverityMajor {
		t.Errorf("Severity = %s, want the simulation's severity %s", inc.Severity, chaos.SeverityMajor)
	}
	if len(inc.Breaches) != 2 || inc.Breaches[0] != "latency_p99" || inc.Breaches[1] != "error_rate" {
		t.Errorf("Breaches = %v, want [latency_p99 error_rate]", inc.Breaches)
	}
	if !inc.DeclaredAt.Equal(testStart) {
		t.Errorf("DeclaredAt = %s, want the pretend clock time %s", inc.DeclaredAt, testStart)
	}

	// The next message is sent after the channel was saved, so its copy must carry the channel.
	if _, err := h.manager.Acknowledge(context.Background(), inc.ID, "ada"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	changed := recv(t, h.notifier.changed, "status changed")
	if changed.ChannelID != "C123" {
		t.Errorf("ChannelID = %q, want the one the notifier gave back", changed.ChannelID)
	}
	got, err := h.manager.Get(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ChannelID != "C123" {
		t.Errorf("stored ChannelID = %q, want C123", got.ChannelID)
	}
}

func TestManagerNeverDeclaresTwiceForOneSimulation(t *testing.T) {
	h := startManager(t, newFakeNotifier())
	inc := h.declare("sim_1")

	h.sample("sim_1", "latency_p99")
	h.sample("sim_1", "latency_p99")
	h.settle()
	if got := h.list(); len(got) != 1 {
		t.Fatalf("incidents = %d, want 1 after repeated bad samples", len(got))
	}

	// Even after the incident is closed, the same simulation must not open a new one.
	if _, err := h.manager.Resolve(context.Background(), inc.ID, "ada"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	h.sample("sim_1", "latency_p99")
	h.settle()
	if got := h.list(); len(got) != 1 {
		t.Fatalf("incidents = %d, want 1 after the incident was resolved", len(got))
	}
}

func TestManagerMitigatesWhenSimulationEnds(t *testing.T) {
	tests := []struct {
		name     string
		ending   chaos.EventType
		wantText string
	}{
		{name: "halted", ending: chaos.EventSimulationHalted, wantText: "Simulation was halted, marked as mitigated"},
		{name: "resolved by itself", ending: chaos.EventSimulationResolved, wantText: "Simulation ended on its own, marked as mitigated"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := startManager(t, newFakeNotifier())
			h.declare("sim_1")

			h.clock.Advance(time.Minute)
			h.event(tc.ending, "sim_1")
			inc := recv(t, h.notifier.changed, "status changed")

			if inc.Status != StatusMitigated {
				t.Fatalf("Status = %s, want %s, because only a person may resolve", inc.Status, StatusMitigated)
			}
			if inc.MitigatedAt == nil || !inc.MitigatedAt.Equal(testStart.Add(time.Minute)) {
				t.Errorf("MitigatedAt = %v, want one minute after the start", inc.MitigatedAt)
			}
			last := inc.Timeline[len(inc.Timeline)-1]
			if last.Actor != ActorSystem || last.Text != tc.wantText {
				t.Errorf("last timeline entry = %+v, want %q by %s", last, tc.wantText, ActorSystem)
			}
		})
	}
}

func TestManagerIgnoresEndOfSimulationWithoutOpenIncident(t *testing.T) {
	h := startManager(t, newFakeNotifier())

	// A simulation that never broke its limit has no incident.
	h.event(chaos.EventSimulationStarted, "sim_quiet")
	h.event(chaos.EventSimulationHalted, "sim_quiet")

	// A resolved incident must stay resolved when its simulation ends later.
	inc := h.declare("sim_1")
	if _, err := h.manager.Resolve(context.Background(), inc.ID, "ada"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	recv(t, h.notifier.changed, "status changed to resolved")
	h.event(chaos.EventSimulationResolved, "sim_1")
	h.settle()

	got, err := h.manager.Get(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusResolved || got.MitigatedAt != nil {
		t.Errorf("incident = status %s, mitigated at %v, want it to stay resolved", got.Status, got.MitigatedAt)
	}
	if len(h.notifier.changed) != 0 {
		t.Errorf("got %d extra status messages, want 0", len(h.notifier.changed))
	}
}

func TestManagerAcknowledgeAndResolve(t *testing.T) {
	h := startManager(t, newFakeNotifier())
	ctx := context.Background()
	inc := h.declare("sim_1")

	h.clock.Advance(90 * time.Second)
	acked, err := h.manager.Acknowledge(ctx, inc.ID, "ada")
	if err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if acked.Status != StatusAcknowledged || acked.AcknowledgedBy != "ada" {
		t.Errorf("after Acknowledge = status %s by %q, want acknowledged by ada", acked.Status, acked.AcknowledgedBy)
	}
	if got := recv(t, h.notifier.changed, "status changed"); got.Status != StatusAcknowledged {
		t.Errorf("notified status = %s, want %s", got.Status, StatusAcknowledged)
	}

	h.clock.Advance(210 * time.Second)
	resolved, err := h.manager.Resolve(ctx, inc.ID, "grace")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := recv(t, h.notifier.changed, "status changed"); got.Status != StatusResolved {
		t.Errorf("notified status = %s, want %s", got.Status, StatusResolved)
	}
	if tta, ok := resolved.TimeToAcknowledge(); !ok || tta != 90*time.Second {
		t.Errorf("TimeToAcknowledge = %s, %v, want 1m30s, true", tta, ok)
	}
	if ttr, ok := resolved.TimeToResolve(); !ok || ttr != 5*time.Minute {
		t.Errorf("TimeToResolve = %s, %v, want 5m0s, true", ttr, ok)
	}
	if len(h.engine.haltedIDs()) != 0 {
		t.Errorf("engine Halt was called %v, want no calls for acknowledge or resolve", h.engine.haltedIDs())
	}
}

func TestManagerMitigateHaltsTheSimulation(t *testing.T) {
	h := startManager(t, newFakeNotifier())
	ctx := context.Background()
	inc := h.declare("sim_1")

	mitigated, err := h.manager.Mitigate(ctx, inc.ID, "ada")
	if err != nil {
		t.Fatalf("Mitigate: %v", err)
	}
	if got := h.engine.haltedIDs(); len(got) != 1 || got[0] != "sim_1" {
		t.Errorf("engine Halt calls = %v, want [sim_1]", got)
	}
	last := mitigated.Timeline[len(mitigated.Timeline)-1]
	if mitigated.Status != StatusMitigated || last.Actor != "ada" {
		t.Errorf("after Mitigate = status %s, last entry by %q, want mitigated by ada", mitigated.Status, last.Actor)
	}
	recv(t, h.notifier.changed, "status changed to mitigated")

	// The real engine sends a halted event after Halt, and that must not mitigate a second time.
	h.event(chaos.EventSimulationHalted, "sim_1")
	h.settle()
	got, err := h.manager.Get(ctx, inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Timeline) != len(mitigated.Timeline) {
		t.Errorf("timeline grew from %d to %d entries after the halted event", len(mitigated.Timeline), len(got.Timeline))
	}
	if len(h.notifier.changed) != 0 {
		t.Errorf("got %d extra status messages, want 0", len(h.notifier.changed))
	}
}

func TestManagerMitigateHaltErrors(t *testing.T) {
	tests := []struct {
		name       string
		haltErr    error
		wantErr    bool
		wantStatus Status
	}{
		{name: "simulation already stopped is fine", haltErr: chaos.ErrNotActive, wantStatus: StatusMitigated},
		{name: "any other engine error is passed on", haltErr: errors.New("engine exploded"), wantErr: true, wantStatus: StatusDeclared},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := startManager(t, newFakeNotifier())
			ctx := context.Background()
			inc := h.declare("sim_1")
			h.engine.mu.Lock()
			h.engine.haltErr = tc.haltErr
			h.engine.mu.Unlock()

			_, err := h.manager.Mitigate(ctx, inc.ID, "ada")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Mitigate error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, tc.haltErr) {
				t.Errorf("error %v does not wrap the engine error", err)
			}
			got, err := h.manager.Get(ctx, inc.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("Status = %s, want %s", got.Status, tc.wantStatus)
			}
		})
	}
}

func TestManagerInvalidTransitions(t *testing.T) {
	ctx := context.Background()
	type action func(m *Manager, id IncidentID) (Incident, error)
	acknowledge := func(m *Manager, id IncidentID) (Incident, error) { return m.Acknowledge(ctx, id, "ada") }
	mitigate := func(m *Manager, id IncidentID) (Incident, error) { return m.Mitigate(ctx, id, "ada") }
	resolve := func(m *Manager, id IncidentID) (Incident, error) { return m.Resolve(ctx, id, "ada") }

	tests := []struct {
		name    string
		setup   []action
		act     action
		unknown bool
		wantErr error
	}{
		{name: "acknowledge twice", setup: []action{acknowledge}, act: acknowledge, wantErr: ErrInvalidTransition},
		{name: "acknowledge after mitigate", setup: []action{mitigate}, act: acknowledge, wantErr: ErrInvalidTransition},
		{name: "mitigate twice", setup: []action{mitigate}, act: mitigate, wantErr: ErrInvalidTransition},
		{name: "mitigate after resolve", setup: []action{resolve}, act: mitigate, wantErr: ErrInvalidTransition},
		{name: "resolve twice", setup: []action{resolve}, act: resolve, wantErr: ErrInvalidTransition},
		{name: "acknowledge unknown incident", act: acknowledge, unknown: true, wantErr: ErrNotFound},
		{name: "mitigate unknown incident", act: mitigate, unknown: true, wantErr: ErrNotFound},
		{name: "resolve unknown incident", act: resolve, unknown: true, wantErr: ErrNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := startManager(t, newFakeNotifier())
			id := h.declare("sim_1").ID
			for i, step := range tc.setup {
				if _, err := step(h.manager, id); err != nil {
					t.Fatalf("setup step %d: %v", i, err)
				}
			}
			haltsBefore := len(h.engine.haltedIDs())
			if tc.unknown {
				id = "inc_missing"
			}

			_, err := tc.act(h.manager, id)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want one that wraps %v", err, tc.wantErr)
			}
			if got := len(h.engine.haltedIDs()); got != haltsBefore {
				t.Errorf("a refused action called engine Halt %d more times, want 0", got-haltsBefore)
			}
		})
	}

	t.Run("get unknown incident", func(t *testing.T) {
		h := startManager(t, newFakeNotifier())
		if _, err := h.manager.Get(ctx, "inc_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get error = %v, want one that wraps ErrNotFound", err)
		}
	})
}

func TestManagerNotesNewBreaches(t *testing.T) {
	h := startManager(t, newFakeNotifier())
	inc := h.declare("sim_1")

	h.sample("sim_1", "latency_p99", "error_rate")
	entry := recv(t, h.notifier.entries, "timeline entry")
	if entry.Text != "New SLO breach: error_rate" || entry.Actor != ActorSystem {
		t.Errorf("entry = %+v, want a new breach note for error_rate", entry)
	}

	// The same limits again must not add another line.
	h.sample("sim_1", "latency_p99", "error_rate")
	h.settle()
	if len(h.notifier.entries) != 0 {
		t.Errorf("got %d extra timeline messages, want 0", len(h.notifier.entries))
	}
	got, err := h.manager.Get(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Timeline) != 2 {
		t.Errorf("timeline has %d entries, want 2", len(got.Timeline))
	}
	if len(got.Breaches) != 1 {
		t.Errorf("Breaches = %v, want only the ones from the declaration", got.Breaches)
	}
}

func TestManagerKeepsRunningWhenNotifierFails(t *testing.T) {
	notifier := newFakeNotifier()
	notifier.declareErr = errors.New("slack is down")
	h := startManager(t, notifier)

	first := h.declare("sim_1")
	second := h.declare("sim_2")

	if first.ID == second.ID {
		t.Fatalf("both incidents share the id %s", first.ID)
	}
	if got := h.list(); len(got) != 2 {
		t.Fatalf("incidents = %d, want 2 even though every notification failed", len(got))
	}
	if _, err := h.manager.Acknowledge(context.Background(), first.ID, "ada"); err != nil {
		t.Fatalf("Acknowledge after a failed notification: %v", err)
	}
}

func TestManagerSlowNotifierDoesNotStallSamples(t *testing.T) {
	notifier := newFakeNotifier()
	notifier.block = make(chan struct{})
	defer close(notifier.block)
	h := startManager(t, notifier)

	h.declare("sim_1")

	// The notifier is now stuck inside its first call, and samples must still flow.
	h.event(chaos.EventSimulationStarted, "sim_2")
	h.sample("sim_2", "cpu")
	h.event(chaos.EventSimulationHalted, "sim_1")
	h.settle()

	got := h.list()
	if len(got) != 2 {
		t.Fatalf("incidents = %d, want 2 while the notifier is stuck", len(got))
	}
	for _, inc := range got {
		if inc.SimulationID == "sim_1" && inc.Status != StatusMitigated {
			t.Errorf("sim_1 incident status = %s, want %s while the notifier is stuck", inc.Status, StatusMitigated)
		}
	}
}

func TestManagerListNewestFirst(t *testing.T) {
	h := startManager(t, newFakeNotifier())

	older := h.declare("sim_1")
	h.clock.Advance(time.Minute)
	newer := h.declare("sim_2")

	got := h.list()
	if len(got) != 2 || got[0].ID != newer.ID || got[1].ID != older.ID {
		t.Fatalf("List order = %v, want newest first", []IncidentID{got[0].ID, got[1].ID})
	}

	// Changing what List gave back must not change what the manager holds.
	got[0].Timeline[0].Text = "changed"
	again, err := h.manager.Get(context.Background(), newer.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if again.Timeline[0].Text != "Incident declared" {
		t.Errorf("changing a listed incident changed the stored one to %q", again.Timeline[0].Text)
	}
}

func TestManagerRunTwice(t *testing.T) {
	h := startManager(t, newFakeNotifier())
	// A settle proves the first Run is up before we try the second one.
	h.settle()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.manager.Run(ctx); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run error = %v, want ErrAlreadyRunning", err)
	}
}

func TestNopNotifier(t *testing.T) {
	var n Notifier = NopNotifier{}
	ctx := context.Background()

	channelID, err := n.IncidentDeclared(ctx, Incident{})
	if channelID != "" || err != nil {
		t.Errorf("IncidentDeclared = %q, %v, want empty and nil", channelID, err)
	}
	if err := n.StatusChanged(ctx, Incident{}); err != nil {
		t.Errorf("StatusChanged = %v, want nil", err)
	}
	if err := n.TimelineEntryAdded(ctx, Incident{}, TimelineEntry{}); err != nil {
		t.Errorf("TimelineEntryAdded = %v, want nil", err)
	}
}
