package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// FIXTURES

// testIncident builds an incident for a simulation and walks it through the given statuses, one minute apart.
func testIncident(t *testing.T, id incident.IncidentID, simID chaos.SimulationID, steps ...incident.Status) incident.Incident {
	t.Helper()
	inc := incident.New(id, testSimulation(simID, chaos.StateRunning, 0), []string{"latency_p99", "error_rate"}, testStart)
	for i, next := range steps {
		at := testStart.Add(time.Duration(i+1) * time.Minute)
		if err := inc.Transition(next, at, "<@U1>", "Moved to "+string(next)); err != nil {
			t.Fatalf("move test incident to %s: %v", next, err)
		}
	}
	return inc
}

// loadOne loads the open incidents and gives back the one with the wanted id.
func loadOne(t *testing.T, s *Store, id incident.IncidentID) incident.Incident {
	t.Helper()
	open, err := s.LoadOpenIncidents(context.Background())
	if err != nil {
		t.Fatalf("LoadOpenIncidents: %v", err)
	}
	for _, inc := range open {
		if inc.ID == id {
			return inc
		}
	}
	t.Fatalf("incident %s is not among the %d open incidents", id, len(open))
	return incident.Incident{}
}

// sameTime compares two times that might be missing.
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// TESTS

func TestSaveAndLoadIncident(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	want := testIncident(t, "inc_1", "sim_1", incident.StatusAcknowledged, incident.StatusMitigated)
	want.ChannelID = "C123"
	want.CardTimestamp = "1700000000.000100"
	if err := store.SaveIncident(ctx, want); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}

	got := loadOne(t, store, "inc_1")

	if got.SimulationID != want.SimulationID || got.Service != want.Service || got.Mode != want.Mode || got.Severity != want.Severity {
		t.Errorf("loaded %+v, want the same simulation, service, mode and severity as %+v", got, want)
	}
	if got.Status != incident.StatusMitigated || got.AcknowledgedBy != "<@U1>" {
		t.Errorf("status = %s acknowledged by %q, want mitigated by <@U1>", got.Status, got.AcknowledgedBy)
	}
	if strings.Join(got.Breaches, ",") != "latency_p99,error_rate" {
		t.Errorf("Breaches = %v, want [latency_p99 error_rate]", got.Breaches)
	}
	if got.ChannelID != "C123" || got.CardTimestamp != "1700000000.000100" {
		t.Errorf("channel = %q, card = %q, want the saved Slack channel and card", got.ChannelID, got.CardTimestamp)
	}
	if !got.DeclaredAt.Equal(want.DeclaredAt) || got.DeclaredAt.Location() != time.UTC {
		t.Errorf("DeclaredAt = %s, want %s in UTC", got.DeclaredAt, want.DeclaredAt)
	}
	if !sameTime(got.AcknowledgedAt, want.AcknowledgedAt) || !sameTime(got.MitigatedAt, want.MitigatedAt) || got.ResolvedAt != nil {
		t.Errorf("times = ack %v, mitigated %v, resolved %v, want the saved ones and no resolve", got.AcknowledgedAt, got.MitigatedAt, got.ResolvedAt)
	}

	if len(got.Timeline) != len(want.Timeline) {
		t.Fatalf("timeline has %d entries, want %d", len(got.Timeline), len(want.Timeline))
	}
	for i, w := range want.Timeline {
		g := got.Timeline[i]
		if g.Actor != w.Actor || g.Text != w.Text || !g.At.Equal(w.At) {
			t.Errorf("timeline entry %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestSaveIncidentIsAnUpsert(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.SaveIncident(ctx, testIncident(t, "inc_1", "sim_1")); err != nil {
		t.Fatalf("first SaveIncident: %v", err)
	}
	later := testIncident(t, "inc_1", "sim_1", incident.StatusAcknowledged)
	later.ChannelID = "C123"
	// Saving the same incident twice must be harmless, because the manager may ask more than once.
	for i := 0; i < 2; i++ {
		if err := store.SaveIncident(ctx, later); err != nil {
			t.Fatalf("SaveIncident run %d: %v", i+1, err)
		}
	}

	if n := countRows(t, store, "incidents"); n != 1 {
		t.Errorf("incidents table has %d rows, want 1", n)
	}
	if n := countRows(t, store, "incident_timeline"); n != 2 {
		t.Errorf("incident_timeline has %d rows, want 2 with no repeats", n)
	}
	got := loadOne(t, store, "inc_1")
	if got.Status != incident.StatusAcknowledged || got.ChannelID != "C123" {
		t.Errorf("loaded status %s in channel %q, want acknowledged in C123", got.Status, got.ChannelID)
	}
}

func TestSaveIncidentAllOrNothing(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.SaveIncident(ctx, testIncident(t, "inc_1", "sim_1")); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}

	// A second incident for the same simulation breaks the unique rule, so nothing of it may be kept.
	clash := testIncident(t, "inc_2", "sim_1", incident.StatusAcknowledged)
	if err := store.SaveIncident(ctx, clash); err == nil {
		t.Fatal("SaveIncident for a simulation that already has an incident returned no error")
	}

	if n := countRows(t, store, "incidents"); n != 1 {
		t.Errorf("incidents table has %d rows, want 1", n)
	}
	if n := countRows(t, store, "incident_timeline"); n != 1 {
		t.Errorf("incident_timeline has %d rows, want 1 because the failed save is rolled back", n)
	}
}

func TestAppendTimeline(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	inc := testIncident(t, "inc_1", "sim_1")
	if err := store.SaveIncident(ctx, inc); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}
	entry := incident.TimelineEntry{At: testStart.Add(time.Minute), Actor: incident.ActorSystem, Text: "New SLO breach: cpu"}
	// Appending the same line twice must keep it once.
	for i := 0; i < 2; i++ {
		if err := store.AppendTimeline(ctx, "inc_1", 1, entry); err != nil {
			t.Fatalf("AppendTimeline run %d: %v", i+1, err)
		}
	}

	got := loadOne(t, store, "inc_1")
	if len(got.Timeline) != 2 {
		t.Fatalf("timeline has %d entries, want 2", len(got.Timeline))
	}
	if last := got.Timeline[1]; last.Text != entry.Text || last.Actor != entry.Actor || !last.At.Equal(entry.At) {
		t.Errorf("appended entry = %+v, want %+v", last, entry)
	}

	if err := store.AppendTimeline(ctx, "inc_missing", 0, entry); err == nil {
		t.Error("AppendTimeline for an unknown incident returned no error")
	}
}

func TestLoadOpenIncidents(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if got, err := store.LoadOpenIncidents(ctx); err != nil || len(got) != 0 {
		t.Fatalf("LoadOpenIncidents on empty tables = %v, %v, want nothing and no error", got, err)
	}

	// Only sim_live is still running, so its resolved incident must be loaded to block a second one.
	if err := store.UpsertSimulation(ctx, testSimulation("sim_live", chaos.StateRunning, 0)); err != nil {
		t.Fatalf("UpsertSimulation: %v", err)
	}
	if err := store.UpsertSimulation(ctx, testSimulation("sim_over", chaos.StateHalted, 0)); err != nil {
		t.Fatalf("UpsertSimulation: %v", err)
	}

	newer := testIncident(t, "inc_newer", "sim_3", incident.StatusAcknowledged)
	newer.DeclaredAt = testStart.Add(time.Hour)
	for _, inc := range []incident.Incident{
		newer,
		testIncident(t, "inc_declared", "sim_1"),
		testIncident(t, "inc_mitigated", "sim_2", incident.StatusMitigated),
		testIncident(t, "inc_closed", "sim_over", incident.StatusResolved),
		testIncident(t, "inc_closed_live", "sim_live", incident.StatusResolved),
	} {
		if err := store.SaveIncident(ctx, inc); err != nil {
			t.Fatalf("SaveIncident %s: %v", inc.ID, err)
		}
	}

	got, err := store.LoadOpenIncidents(ctx)
	if err != nil {
		t.Fatalf("LoadOpenIncidents: %v", err)
	}

	var ids []string
	for _, inc := range got {
		ids = append(ids, string(inc.ID))
	}
	// Oldest first, with the id as the tie breaker.
	want := "inc_closed_live,inc_declared,inc_mitigated,inc_newer"
	if strings.Join(ids, ",") != want {
		t.Fatalf("loaded %v, want %s", ids, want)
	}
	wantEntries := map[incident.IncidentID]int{"inc_closed_live": 2, "inc_declared": 1, "inc_mitigated": 2, "inc_newer": 2}
	for _, inc := range got {
		if len(inc.Timeline) != wantEntries[inc.ID] {
			t.Errorf("incident %s has %d timeline entries, want %d", inc.ID, len(inc.Timeline), wantEntries[inc.ID])
		}
		if inc.Timeline[0].Text != "Incident declared" {
			t.Errorf("incident %s starts with %q, want the declared line first", inc.ID, inc.Timeline[0].Text)
		}
	}
}
