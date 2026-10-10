package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// TESTS

func TestListRecentSimulations(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if got, err := store.ListRecentSimulations(ctx, 10); err != nil || len(got) != 0 {
		t.Fatalf("ListRecentSimulations on an empty table = %v, %v, want nothing and no error", got, err)
	}

	for _, sim := range []chaos.Simulation{
		testSimulation("sim_old", chaos.StateResolved, 0),
		testSimulation("sim_mid", chaos.StateHalted, time.Minute),
		testSimulation("sim_new", chaos.StateRunning, 2*time.Minute),
	} {
		if err := store.UpsertSimulation(ctx, sim); err != nil {
			t.Fatalf("UpsertSimulation %s: %v", sim.ID, err)
		}
	}

	tests := []struct {
		name  string
		limit int
		want  string
	}{
		{name: "all of them newest first", limit: 10, want: "sim_new,sim_mid,sim_old"},
		{name: "the limit cuts off the oldest", limit: 2, want: "sim_new,sim_mid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.ListRecentSimulations(ctx, tc.limit)
			if err != nil {
				t.Fatalf("ListRecentSimulations: %v", err)
			}
			var ids []string
			for _, sim := range got {
				ids = append(ids, string(sim.ID))
			}
			if strings.Join(ids, ",") != tc.want {
				t.Errorf("ids = %v, want %s", ids, tc.want)
			}
		})
	}

	// A finished simulation must come back complete, with the moment it ended.
	got, err := store.ListRecentSimulations(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentSimulations: %v", err)
	}
	mid := got[1]
	if mid.State != chaos.StateHalted || mid.EndedAt == nil || !mid.EndedAt.Equal(testStart.Add(90*time.Second)) {
		t.Errorf("sim_mid = state %s ended at %v, want halted 30 seconds after its start", mid.State, mid.EndedAt)
	}
	if mid.Scenario != testSimulation("sim_mid", chaos.StateHalted, time.Minute).Scenario {
		t.Errorf("sim_mid scenario = %+v, want the saved one", mid.Scenario)
	}
}

func TestGetSimulation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.UpsertSimulation(ctx, testSimulation("sim_1", chaos.StateHalted, 0)); err != nil {
		t.Fatalf("UpsertSimulation: %v", err)
	}

	got, err := store.GetSimulation(ctx, "sim_1")
	if err != nil {
		t.Fatalf("GetSimulation: %v", err)
	}
	if got.ID != "sim_1" || got.State != chaos.StateHalted || !got.StartedAt.Equal(testStart) {
		t.Errorf("GetSimulation = %+v, want the saved halted simulation", got)
	}

	if _, err := store.GetSimulation(ctx, "sim_missing"); !errors.Is(err, chaos.ErrNotFound) {
		t.Errorf("GetSimulation for an unknown id = %v, want one that wraps chaos.ErrNotFound", err)
	}
}

func TestListRecentIncidents(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if got, err := store.ListRecentIncidents(ctx, 10); err != nil || len(got) != 0 {
		t.Fatalf("ListRecentIncidents on an empty table = %v, %v, want nothing and no error", got, err)
	}

	old := testIncident(t, "inc_old", "sim_1", incident.StatusAcknowledged, incident.StatusResolved)
	mid := testIncident(t, "inc_mid", "sim_2")
	mid.DeclaredAt = testStart.Add(time.Hour)
	newest := testIncident(t, "inc_new", "sim_3", incident.StatusMitigated)
	newest.DeclaredAt = testStart.Add(2 * time.Hour)
	for _, inc := range []incident.Incident{mid, old, newest} {
		if err := store.SaveIncident(ctx, inc); err != nil {
			t.Fatalf("SaveIncident %s: %v", inc.ID, err)
		}
	}

	got, err := store.ListRecentIncidents(ctx, 2)
	if err != nil {
		t.Fatalf("ListRecentIncidents: %v", err)
	}
	if len(got) != 2 || got[0].ID != "inc_new" || got[1].ID != "inc_mid" {
		t.Fatalf("ListRecentIncidents with limit 2 = %+v, want inc_new then inc_mid", got)
	}

	all, err := store.ListRecentIncidents(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentIncidents: %v", err)
	}
	if len(all) != 3 || all[2].ID != "inc_old" {
		t.Fatalf("ListRecentIncidents = %d incidents, want 3 with the resolved one last", len(all))
	}
	resolved := all[2]
	if resolved.Status != incident.StatusResolved || resolved.ResolvedAt == nil {
		t.Errorf("inc_old = status %s resolved at %v, want a resolved incident with its time", resolved.Status, resolved.ResolvedAt)
	}
	if len(resolved.Timeline) != 3 || resolved.Timeline[2].Text != "Moved to resolved" {
		t.Errorf("inc_old timeline = %+v, want all 3 entries in order", resolved.Timeline)
	}
	if len(all[0].Timeline) != 2 || len(all[1].Timeline) != 1 {
		t.Errorf("timelines have %d and %d entries, want 2 and 1 so stories are not mixed up", len(all[0].Timeline), len(all[1].Timeline))
	}
}

func TestGetIncident(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	saved := testIncident(t, "inc_1", "sim_1", incident.StatusAcknowledged, incident.StatusResolved)
	saved.ChannelID = "C123"
	if err := store.SaveIncident(ctx, saved); err != nil {
		t.Fatalf("SaveIncident: %v", err)
	}

	got, err := store.GetIncident(ctx, "inc_1")
	if err != nil {
		t.Fatalf("GetIncident: %v", err)
	}
	if got.Status != incident.StatusResolved || got.ChannelID != "C123" || len(got.Timeline) != 3 {
		t.Errorf("GetIncident = status %s, channel %q, %d timeline entries, want resolved in C123 with 3", got.Status, got.ChannelID, len(got.Timeline))
	}
	if d, ok := got.TimeToResolve(); !ok || d != 2*time.Minute {
		t.Errorf("TimeToResolve = %s, %v, want 2m0s from the saved times", d, ok)
	}

	if _, err := store.GetIncident(ctx, "inc_missing"); !errors.Is(err, incident.ErrNotFound) {
		t.Errorf("GetIncident for an unknown id = %v, want one that wraps incident.ErrNotFound", err)
	}
}
