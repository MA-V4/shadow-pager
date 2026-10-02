package incident

import (
	"errors"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// FIXTURES

var testStart = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

func testSimulation(id chaos.SimulationID) chaos.Simulation {
	return chaos.Simulation{
		ID: id,
		Scenario: chaos.Scenario{
			Service:   "payments-api",
			Mode:      chaos.FailureLatencySpike,
			Severity:  chaos.SeverityMajor,
			Intensity: 0.9,
			Duration:  2 * time.Minute,
		},
		State:     chaos.StateRunning,
		StartedAt: testStart,
		EndsAt:    testStart.Add(2 * time.Minute),
	}
}

// TESTS

func TestStatusCanTransitionTo(t *testing.T) {
	all := []Status{StatusDeclared, StatusAcknowledged, StatusMitigated, StatusResolved}
	allowed := map[Status][]Status{
		StatusDeclared:     {StatusAcknowledged, StatusMitigated, StatusResolved},
		StatusAcknowledged: {StatusMitigated, StatusResolved},
		StatusMitigated:    {StatusResolved},
		StatusResolved:     {},
	}

	for _, from := range all {
		for _, to := range all {
			want := false
			for _, ok := range allowed[from] {
				if ok == to {
					want = true
				}
			}
			t.Run(string(from)+" to "+string(to), func(t *testing.T) {
				if got := from.CanTransitionTo(to); got != want {
					t.Errorf("CanTransitionTo = %v, want %v", got, want)
				}
			})
		}
	}

	if StatusDeclared.CanTransitionTo("on_fire") {
		t.Error("a made up status must never be allowed")
	}
	if Status("on_fire").CanTransitionTo(StatusResolved) {
		t.Error("a made up status must never be allowed to move")
	}
}

func TestIncidentTransition(t *testing.T) {
	tests := []struct {
		name       string
		steps      []Status
		wantErr    bool
		wantStatus Status
	}{
		{name: "full path", steps: []Status{StatusAcknowledged, StatusMitigated, StatusResolved}, wantStatus: StatusResolved},
		{name: "mitigated without acknowledge", steps: []Status{StatusMitigated}, wantStatus: StatusMitigated},
		{name: "resolved straight away", steps: []Status{StatusResolved}, wantStatus: StatusResolved},
		{name: "acknowledge twice", steps: []Status{StatusAcknowledged, StatusAcknowledged}, wantErr: true, wantStatus: StatusAcknowledged},
		{name: "acknowledge after mitigated", steps: []Status{StatusMitigated, StatusAcknowledged}, wantErr: true, wantStatus: StatusMitigated},
		{name: "anything after resolved", steps: []Status{StatusResolved, StatusMitigated}, wantErr: true, wantStatus: StatusResolved},
		{name: "back to declared", steps: []Status{StatusAcknowledged, StatusDeclared}, wantErr: true, wantStatus: StatusAcknowledged},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inc := New("inc_1", testSimulation("sim_1"), []string{"latency_p99"}, testStart)

			var err error
			for i, next := range tc.steps {
				at := testStart.Add(time.Duration(i+1) * time.Minute)
				err = inc.Transition(next, at, "ada", "moved to "+string(next))
			}

			if (err != nil) != tc.wantErr {
				t.Fatalf("last Transition error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("error %v does not wrap ErrInvalidTransition", err)
			}
			if inc.Status != tc.wantStatus {
				t.Errorf("Status = %s, want %s", inc.Status, tc.wantStatus)
			}

			// A refused step must not write anything into the story.
			wantEntries := 1 + len(tc.steps)
			if tc.wantErr {
				wantEntries--
			}
			if len(inc.Timeline) != wantEntries {
				t.Errorf("timeline has %d entries, want %d", len(inc.Timeline), wantEntries)
			}
		})
	}
}

func TestIncidentTimings(t *testing.T) {
	inc := New("inc_1", testSimulation("sim_1"), []string{"latency_p99"}, testStart)

	if _, ok := inc.TimeToAcknowledge(); ok {
		t.Error("TimeToAcknowledge is known before anyone acknowledged")
	}
	if _, ok := inc.TimeToResolve(); ok {
		t.Error("TimeToResolve is known before the incident is resolved")
	}

	if err := inc.Transition(StatusAcknowledged, testStart.Add(90*time.Second), "ada", "on it"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := inc.Transition(StatusResolved, testStart.Add(5*time.Minute), "ada", "fixed"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got, ok := inc.TimeToAcknowledge(); !ok || got != 90*time.Second {
		t.Errorf("TimeToAcknowledge = %s, %v, want 1m30s, true", got, ok)
	}
	if got, ok := inc.TimeToResolve(); !ok || got != 5*time.Minute {
		t.Errorf("TimeToResolve = %s, %v, want 5m0s, true", got, ok)
	}
	if inc.AcknowledgedBy != "ada" {
		t.Errorf("AcknowledgedBy = %q, want ada", inc.AcknowledgedBy)
	}
}

func TestIncidentCloneIsIndependent(t *testing.T) {
	inc := New("inc_1", testSimulation("sim_1"), []string{"latency_p99"}, testStart)
	if err := inc.Transition(StatusAcknowledged, testStart.Add(time.Minute), "ada", "on it"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	clone := inc.Clone()
	clone.Breaches[0] = "changed"
	clone.Timeline[0].Text = "changed"
	*clone.AcknowledgedAt = testStart.Add(time.Hour)

	if inc.Breaches[0] != "latency_p99" {
		t.Errorf("changing the clone changed Breaches to %q", inc.Breaches[0])
	}
	if inc.Timeline[0].Text != "Incident declared" {
		t.Errorf("changing the clone changed the timeline to %q", inc.Timeline[0].Text)
	}
	if !inc.AcknowledgedAt.Equal(testStart.Add(time.Minute)) {
		t.Errorf("changing the clone changed AcknowledgedAt to %s", inc.AcknowledgedAt)
	}
}
