package incident

import (
	"context"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// publicEvent hands the manager a lifecycle event of a drill that a visitor started.
func (h *harness) publicEvent(typ chaos.EventType, simID chaos.SimulationID) {
	h.t.Helper()
	sim := testSimulation(simID)
	sim.Scenario.Public = true
	select {
	case h.engine.events <- chaos.Event{Type: typ, Simulation: sim}:
	case <-time.After(testWait):
		h.t.Fatal("manager did not take the event, so event handling is stuck")
	}
}

func TestNewIncidentCopiesThePublicMark(t *testing.T) {
	sim := testSimulation("sim_1")
	if inc := New("inc_1", sim, nil, testStart); inc.Public {
		t.Error("an incident of an admin drill is marked public")
	}
	sim.Scenario.Public = true
	if inc := New("inc_2", sim, nil, testStart); !inc.Public {
		t.Error("an incident of a public drill is not marked public")
	}
}

func TestPublicDrillNeverReachesTheNotifier(t *testing.T) {
	ctx := context.Background()
	notifier := newFakeNotifier()
	h := startManager(t, notifier)

	// A public drill goes through every step that would normally tell Slack something.
	h.publicEvent(chaos.EventSimulationStarted, "sim_public")
	h.sample("sim_public", "latency_p99")
	h.settle()

	incidents := h.list()
	if len(incidents) != 1 {
		t.Fatalf("incidents = %d, want the public drill to declare 1", len(incidents))
	}
	public := incidents[0]
	if !public.Public || public.Status != StatusDeclared || public.SimulationID != "sim_public" {
		t.Fatalf("incident = %+v, want a declared public incident for sim_public", public)
	}

	h.sample("sim_public", "latency_p99", "error_rate")
	if _, err := h.manager.Acknowledge(ctx, public.ID, "ada"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	h.publicEvent(chaos.EventSimulationResolved, "sim_public")
	h.settle()
	if _, err := h.manager.Resolve(ctx, public.ID, "ada"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// It is still a full incident for the API, the dashboard and the status page.
	got, err := h.manager.Get(ctx, public.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusResolved || !got.Public || len(got.Timeline) < 4 {
		t.Errorf("public incident = status %s, public %v, %d timeline entries, want resolved, public, and its whole story", got.Status, got.Public, len(got.Timeline))
	}
	if got.ChannelID != "" || got.CardTimestamp != "" {
		t.Errorf("public incident has channel %q and card %q, want none, because Slack was never told", got.ChannelID, got.CardTimestamp)
	}

	// An admin drill on the same manager does reach the notifier.
	admin := h.declare("sim_admin")
	if admin.SimulationID != "sim_admin" || admin.Public {
		t.Fatalf("first incident the notifier saw = %+v, want the admin drill and nothing of the public one", admin)
	}
	// Messages are delivered one at a time and in order, so anything of the public drill would have arrived before this.
	if n := len(notifier.changed) + len(notifier.entries) + len(notifier.declared); n != 0 {
		t.Errorf("the notifier holds %d more messages, want none from the public drill", n)
	}

	if _, err := h.manager.Acknowledge(ctx, admin.ID, "ada"); err != nil {
		t.Fatalf("Acknowledge admin incident: %v", err)
	}
	if changed := recv(t, notifier.changed, "status changed"); changed.ID != admin.ID {
		t.Errorf("status change came for %s, want the admin incident %s", changed.ID, admin.ID)
	}
}

func TestPublicIncidentHaltsItsDrillOnMitigate(t *testing.T) {
	notifier := newFakeNotifier()
	h := startManager(t, notifier)
	h.publicEvent(chaos.EventSimulationStarted, "sim_public")
	h.sample("sim_public", "latency_p99")
	h.settle()
	public := h.list()[0]

	// Staying out of Slack must not change what the incident itself can do.
	if _, err := h.manager.Mitigate(context.Background(), public.ID, "ada"); err != nil {
		t.Fatalf("Mitigate: %v", err)
	}
	if halted := h.engine.haltedIDs(); len(halted) != 1 || halted[0] != "sim_public" {
		t.Errorf("halted = %v, want sim_public", halted)
	}
	h.settle()
	if n := len(notifier.changed) + len(notifier.declared); n != 0 {
		t.Errorf("the notifier got %d messages about a public incident, want none", n)
	}
}
