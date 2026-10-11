package incident

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// FAKE STORE

// appendCall is one story line the fake store was asked to append.
type appendCall struct {
	id    IncidentID
	seq   int
	entry TimelineEntry
}

// fakeStore pretends to be the database and writes down everything it is asked to save.
type fakeStore struct {
	saves   chan Incident   // every incident handed to SaveIncident, in order
	appends chan appendCall // every story line handed to AppendTimeline, in order
	block   chan struct{}   // when set, SaveIncident waits here until the test closes it

	mu      sync.Mutex
	open    []Incident // what LoadOpenIncidents gives back
	err     error      // when set, every call fails with it
	entered chan struct{}
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		saves:   make(chan Incident, 64),
		appends: make(chan appendCall, 64),
		entered: make(chan struct{}, 64),
	}
}

func (f *fakeStore) failure() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *fakeStore) SaveIncident(ctx context.Context, inc Incident) error {
	f.entered <- struct{}{}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := f.failure(); err != nil {
		return err
	}
	f.saves <- inc
	return nil
}

func (f *fakeStore) AppendTimeline(_ context.Context, id IncidentID, seq int, entry TimelineEntry) error {
	if err := f.failure(); err != nil {
		return err
	}
	f.appends <- appendCall{id: id, seq: seq, entry: entry}
	return nil
}

func (f *fakeStore) LoadOpenIncidents(context.Context) ([]Incident, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open, f.err
}

// waitForSave reads saves until one passes the check, and fails the test if none does.
func waitForSave(t *testing.T, store *fakeStore, what string, ok func(Incident) bool) Incident {
	t.Helper()
	timeout := time.After(testWait)
	for {
		select {
		case inc := <-store.saves:
			if ok(inc) {
				return inc
			}
		case <-timeout:
			t.Fatalf("timed out waiting for a save where %s", what)
		}
	}
}

// savedIncident builds an incident the way it would come back from the database.
func savedIncident(t *testing.T, id IncidentID, simID chaos.SimulationID, steps ...Status) Incident {
	t.Helper()
	inc := New(id, testSimulation(simID), []string{"latency_p99"}, testStart.Add(-10*time.Minute))
	inc.ChannelID = "C_" + string(id)
	inc.CardTimestamp = "1700000000.000200"
	for i, next := range steps {
		at := inc.DeclaredAt.Add(time.Duration(i+1) * time.Minute)
		if err := inc.Transition(next, at, "ada", "Moved to "+string(next)); err != nil {
			t.Fatalf("move saved incident to %s: %v", next, err)
		}
	}
	return inc
}

// SAVING TESTS

func TestManagerSavesOnEveryLifecycleChange(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, newFakeNotifier(), WithStore(store))
	h.start()
	ctx := context.Background()

	inc := h.declare("sim_1")
	waitForSave(t, store, "the incident is declared", func(s Incident) bool {
		return s.ID == inc.ID && s.Status == StatusDeclared
	})
	// The channel and the card only exist after Slack answered, and that must be saved too.
	withCard := waitForSave(t, store, "the channel and the card are known", func(s Incident) bool {
		return s.ChannelID == "C123" && s.CardTimestamp == "1700000000.000100"
	})
	if len(withCard.Timeline) != 1 {
		t.Errorf("saved timeline has %d entries, want 1", len(withCard.Timeline))
	}

	if _, err := h.manager.Acknowledge(ctx, inc.ID, "ada"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	acked := waitForSave(t, store, "the incident is acknowledged", func(s Incident) bool { return s.Status == StatusAcknowledged })
	if acked.AcknowledgedBy != "ada" || len(acked.Timeline) != 2 {
		t.Errorf("saved incident acknowledged by %q with %d timeline entries, want ada and 2", acked.AcknowledgedBy, len(acked.Timeline))
	}

	h.sample("sim_1", "latency_p99", "cpu")
	appended := recv(t, store.appends, "an appended timeline entry")
	if appended.id != inc.ID || appended.seq != 2 || appended.entry.Text != "New SLO breach: cpu" {
		t.Errorf("appended = %+v, want the cpu breach as entry number 2", appended)
	}

	h.event(chaos.EventSimulationHalted, "sim_1")
	waitForSave(t, store, "the incident is mitigated", func(s Incident) bool { return s.Status == StatusMitigated })

	if _, err := h.manager.Resolve(ctx, inc.ID, "ada"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resolved := waitForSave(t, store, "the incident is resolved", func(s Incident) bool { return s.Status == StatusResolved })
	if len(resolved.Timeline) != 5 || resolved.ResolvedAt == nil {
		t.Errorf("saved incident has %d timeline entries and resolved at %v, want 5 and a time", len(resolved.Timeline), resolved.ResolvedAt)
	}
}

func TestManagerKeepsWorkingWhenStoreFails(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("database is on fire")
	h := newHarness(t, newFakeNotifier(), WithStore(store))
	h.start()
	ctx := context.Background()

	inc := h.declare("sim_1")
	if _, err := h.manager.Acknowledge(ctx, inc.ID, "ada"); err != nil {
		t.Fatalf("Acknowledge with a failing store: %v", err)
	}
	h.sample("sim_1", "latency_p99", "cpu")
	h.event(chaos.EventSimulationHalted, "sim_1")
	// The halt must be fully handled first, or Resolve can win the race and the mitigated line is never written.
	h.settle()
	if _, err := h.manager.Resolve(ctx, inc.ID, "ada"); err != nil {
		t.Fatalf("Resolve with a failing store: %v", err)
	}
	h.settle()

	// Memory is the source of truth, so the incident is complete even though nothing was saved.
	got, err := h.manager.Get(ctx, inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusResolved || len(got.Timeline) != 5 {
		t.Errorf("incident in memory = status %s with %d timeline entries, want resolved with 5", got.Status, len(got.Timeline))
	}
	if len(store.saves) != 0 {
		t.Errorf("the failing store recorded %d saves, want 0", len(store.saves))
	}
}

func TestManagerSlowStoreDoesNotStallSamples(t *testing.T) {
	store := newFakeStore()
	store.block = make(chan struct{})
	defer close(store.block)
	h := newHarness(t, newFakeNotifier(), WithStore(store))
	h.start()

	h.declare("sim_1")
	recv(t, store.entered, "the first save to start")

	// The store is now stuck inside its first save, and samples and people must still get through.
	h.event(chaos.EventSimulationStarted, "sim_2")
	h.sample("sim_2", "cpu")
	h.event(chaos.EventSimulationHalted, "sim_1")
	h.settle()

	got := h.list()
	if len(got) != 2 {
		t.Fatalf("incidents = %d, want 2 while the store is stuck", len(got))
	}
	for _, inc := range got {
		if inc.SimulationID == "sim_1" && inc.Status != StatusMitigated {
			t.Errorf("sim_1 incident status = %s, want %s while the store is stuck", inc.Status, StatusMitigated)
		}
	}
}

func TestManagerWritesTheLastSavesBeforeItStops(t *testing.T) {
	store := newFakeStore()
	store.block = make(chan struct{})
	h := newHarness(t, newFakeNotifier(), WithStore(store))
	h.start()

	inc := h.declare("sim_1")
	recv(t, store.entered, "the first save to start")
	if _, err := h.manager.Acknowledge(context.Background(), inc.ID, "ada"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}

	// The manager is asked to stop while one save is stuck and more are waiting in line.
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		h.stop()
	}()
	close(store.block)
	recv(t, stopped, "Run to return")

	var last Incident
	for len(store.saves) > 0 {
		last = <-store.saves
	}
	if last.Status != StatusAcknowledged {
		t.Errorf("last saved status = %q, want %s because waiting saves are written before stopping", last.Status, StatusAcknowledged)
	}
}

// RESTORE TESTS

func TestManagerRestore(t *testing.T) {
	store := newFakeStore()
	store.open = []Incident{
		savedIncident(t, "inc_running", "sim_running"),
		savedIncident(t, "inc_orphan", "sim_gone", StatusAcknowledged),
		savedIncident(t, "inc_mitigated", "sim_gone_too", StatusMitigated),
		savedIncident(t, "inc_resolved", "sim_still_running", StatusResolved),
	}
	notifier := newFakeNotifier()
	h := newHarness(t, notifier, WithStore(store))
	ctx := context.Background()

	critical := testSimulation("sim_fresh")
	critical.Scenario.Severity = chaos.SeverityCritical
	ended := testSimulation("sim_ended")
	ended.State = chaos.StateResolved
	sims := []chaos.Simulation{testSimulation("sim_running"), testSimulation("sim_still_running"), critical, ended}

	if err := h.manager.Restore(ctx, sims); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	want := map[IncidentID]Status{
		"inc_running":   StatusDeclared,
		"inc_orphan":    StatusMitigated,
		"inc_mitigated": StatusMitigated,
		"inc_resolved":  StatusResolved,
	}
	got := h.list()
	if len(got) != len(want) {
		t.Fatalf("restored %d incidents, want %d", len(got), len(want))
	}
	for _, inc := range got {
		if inc.Status != want[inc.ID] {
			t.Errorf("incident %s has status %s, want %s", inc.ID, inc.Status, want[inc.ID])
		}
	}

	orphan, err := h.manager.Get(ctx, "inc_orphan")
	if err != nil {
		t.Fatalf("Get inc_orphan: %v", err)
	}
	last := orphan.Timeline[len(orphan.Timeline)-1]
	if last.Actor != ActorSystem || last.Text != "Simulation ended while the server was off, marked as mitigated" || !last.At.Equal(testStart) {
		t.Errorf("last entry of the orphan = %+v, want the mitigated while off line at the pretend clock time", last)
	}

	// The queued work only runs once the manager is started.
	h.start()
	changed := recv(t, notifier.changed, "status changed for the orphan")
	if changed.ID != "inc_orphan" || changed.Status != StatusMitigated {
		t.Errorf("notified %s as %s, want inc_orphan as mitigated", changed.ID, changed.Status)
	}
	if changed.ChannelID != "C_inc_orphan" || changed.CardTimestamp != "1700000000.000200" {
		t.Errorf("notified channel %q and card %q, want the saved ones so the old card is updated", changed.ChannelID, changed.CardTimestamp)
	}
	waitForSave(t, store, "the orphan is saved as mitigated", func(s Incident) bool {
		return s.ID == "inc_orphan" && s.Status == StatusMitigated
	})

	// A restored incident still answers to people, like a button on an old card.
	if _, err := h.manager.Acknowledge(ctx, "inc_running", "grace"); err != nil {
		t.Fatalf("Acknowledge a restored incident: %v", err)
	}
	if got := recv(t, notifier.changed, "status changed for the restored incident"); got.ChannelID != "C_inc_running" {
		t.Errorf("notified channel %q, want the saved channel C_inc_running", got.ChannelID)
	}

	// Bad samples for simulations that already had an incident must not open a second one.
	h.sample("sim_running", "latency_p99")
	h.sample("sim_still_running", "latency_p99")
	h.settle()
	if got := h.list(); len(got) != len(want) {
		t.Fatalf("incidents = %d after bad samples, want still %d", len(got), len(want))
	}

	// A restored simulation without an incident is known, so its incident gets the right severity.
	h.sample("sim_fresh", "cpu")
	fresh := recv(t, notifier.declared, "incident declared for the fresh simulation")
	if fresh.Severity != chaos.SeverityCritical {
		t.Errorf("severity = %s, want the restored simulation's severity %s", fresh.Severity, chaos.SeverityCritical)
	}
}

func TestManagerRestoreRemembersKnownBreaches(t *testing.T) {
	store := newFakeStore()
	saved := savedIncident(t, "inc_1", "sim_1")
	saved.AddEntry(saved.DeclaredAt.Add(time.Minute), ActorSystem, "New SLO breach: error_rate, cpu")
	store.open = []Incident{saved}
	notifier := newFakeNotifier()
	h := newHarness(t, notifier, WithStore(store))

	if err := h.manager.Restore(context.Background(), []chaos.Simulation{testSimulation("sim_1")}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	h.start()

	h.sample("sim_1", "latency_p99", "error_rate", "cpu")
	h.settle()
	if len(notifier.entries) != 0 || len(store.appends) != 0 {
		t.Errorf("old breaches made %d messages and %d saves, want 0 of each", len(notifier.entries), len(store.appends))
	}

	h.sample("sim_1", "memory")
	if entry := recv(t, notifier.entries, "timeline entry for the new breach"); entry.Text != "New SLO breach: memory" {
		t.Errorf("entry = %q, want only the memory breach", entry.Text)
	}
}

func TestManagerRestoreErrors(t *testing.T) {
	t.Run("store cannot load", func(t *testing.T) {
		loadErr := errors.New("connection refused")
		store := newFakeStore()
		store.err = loadErr
		h := newHarness(t, newFakeNotifier(), WithStore(store))

		if err := h.manager.Restore(context.Background(), nil); !errors.Is(err, loadErr) {
			t.Fatalf("Restore error = %v, want one that wraps the store error", err)
		}
	})

	t.Run("after Run has started", func(t *testing.T) {
		h := newHarness(t, newFakeNotifier(), WithStore(newFakeStore()))
		h.start()
		h.settle()

		if err := h.manager.Restore(context.Background(), nil); !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("Restore error = %v, want ErrAlreadyRunning", err)
		}
	})

	t.Run("without a store there is nothing to restore", func(t *testing.T) {
		h := newHarness(t, newFakeNotifier())

		if err := h.manager.Restore(context.Background(), nil); err != nil {
			t.Fatalf("Restore error = %v, want nil", err)
		}
		if got := h.list(); len(got) != 0 {
			t.Errorf("incidents = %d, want 0", len(got))
		}
	})
}
