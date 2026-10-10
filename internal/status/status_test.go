package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// FAKES

// statusWait is the longest a test waits before it gives up.
const statusWait = 2 * time.Second

var testStart = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

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

// fakeSamples pretends to be the engine and hands out a channel the test controls.
type fakeSamples struct {
	// The channel has no buffer, so a send only finishes when the aggregator takes the sample.
	ch  chan chaos.MetricSample
	err error
}

func (f *fakeSamples) Subscribe(context.Context) (<-chan chaos.MetricSample, error) {
	return f.ch, f.err
}

// fakeIncidents pretends to be the incident manager.
type fakeIncidents struct {
	mu        sync.Mutex
	incidents []incident.Incident
	err       error
}

func (f *fakeIncidents) List(context.Context) ([]incident.Incident, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.incidents, f.err
}

func (f *fakeIncidents) set(incidents ...incident.Incident) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.incidents = incidents
}

// HARNESS

// harness holds a running aggregator together with its fakes.
type harness struct {
	t         *testing.T
	agg       *Aggregator
	samples   *fakeSamples
	incidents *fakeIncidents
	clock     *fakeClock
}

func startAggregator(t *testing.T, opts ...Option) *harness {
	t.Helper()
	h := &harness{
		t:         t,
		samples:   &fakeSamples{ch: make(chan chaos.MetricSample)},
		incidents: &fakeIncidents{},
		clock:     &fakeClock{now: testStart},
	}
	opts = append([]Option{WithClock(h.clock.Now)}, opts...)
	agg, err := NewAggregator(context.Background(), h.samples, h.incidents, slog.New(slog.NewTextHandler(io.Discard, nil)), opts...)
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	h.agg = agg

	done := make(chan struct{})
	go func() {
		defer close(done)
		agg.Run()
	}()
	t.Cleanup(func() {
		close(h.samples.ch)
		select {
		case <-done:
		case <-time.After(statusWait):
			t.Error("Run did not return after the sample channel closed")
		}
	})
	return h
}

// send hands one sample to the aggregator and waits until it is taken.
func (h *harness) send(simID chaos.SimulationID, service string, breaches ...string) {
	h.t.Helper()
	s := chaos.MetricSample{
		SimulationID: simID,
		Service:      service,
		Mode:         chaos.FailureLatencySpike,
		Timestamp:    h.clock.Now(),
		LatencyP99Ms: 120,
		Healthy:      len(breaches) == 0,
		Breaches:     breaches,
	}
	if !s.Healthy {
		s.LatencyP99Ms = 950
	}
	select {
	case h.samples.ch <- s:
	case <-time.After(statusWait):
		h.t.Fatal("aggregator did not take the sample")
	}
}

// snapshot sends one more sample first, because taking it proves the earlier ones are filed away.
func (h *harness) snapshot() Snapshot {
	h.t.Helper()
	h.send("sim_settle", "zz-settle")
	snap, err := h.agg.Snapshot(context.Background())
	if err != nil {
		h.t.Fatalf("Snapshot: %v", err)
	}
	// The helper service is taken out again so tests only see their own services.
	kept := snap.Services[:0]
	for _, svc := range snap.Services {
		if svc.Name != "zz-settle" {
			kept = append(kept, svc)
		}
	}
	snap.Services = kept
	return snap
}

// openIncident builds an incident for a service with the given severity and status.
func openIncident(t *testing.T, id incident.IncidentID, service string, severity chaos.Severity, steps ...incident.Status) incident.Incident {
	t.Helper()
	sim := chaos.Simulation{ID: chaos.SimulationID("sim_" + string(id)), Scenario: chaos.Scenario{Service: service, Mode: chaos.FailureLatencySpike, Severity: severity}}
	inc := incident.New(id, sim, []string{"latency_p99"}, testStart)
	for i, next := range steps {
		if err := inc.Transition(next, testStart.Add(time.Duration(i+1)*time.Minute), "ada", "Moved to "+string(next)); err != nil {
			t.Fatalf("move incident to %s: %v", next, err)
		}
	}
	return inc
}

func levels(snap Snapshot) map[string]Level {
	out := make(map[string]Level, len(snap.Services))
	for _, svc := range snap.Services {
		out[svc.Name] = svc.Level
	}
	return out
}

// TESTS

func TestServiceLevels(t *testing.T) {
	tests := []struct {
		name      string
		breaches  []string
		incidents func(t *testing.T) []incident.Incident
		want      Level
	}{
		{name: "healthy numbers are operational", want: Operational},
		{name: "a breach without an incident is degraded", breaches: []string{"latency_p99"}, want: Degraded},
		{
			name:     "a breach with an open critical incident is a major outage",
			breaches: []string{"latency_p99"},
			incidents: func(t *testing.T) []incident.Incident {
				return []incident.Incident{openIncident(t, "inc_1", "payments-api", chaos.SeverityCritical)}
			},
			want: MajorOutage,
		},
		{
			name:     "a breach with an acknowledged major incident is a major outage",
			breaches: []string{"latency_p99"},
			incidents: func(t *testing.T) []incident.Incident {
				return []incident.Incident{openIncident(t, "inc_1", "payments-api", chaos.SeverityMajor, incident.StatusAcknowledged)}
			},
			want: MajorOutage,
		},
		{
			name:     "a breach with only a minor incident stays degraded",
			breaches: []string{"latency_p99"},
			incidents: func(t *testing.T) []incident.Incident {
				return []incident.Incident{openIncident(t, "inc_1", "payments-api", chaos.SeverityMinor)}
			},
			want: Degraded,
		},
		{
			name:     "a breach with a resolved critical incident is only degraded",
			breaches: []string{"latency_p99"},
			incidents: func(t *testing.T) []incident.Incident {
				return []incident.Incident{openIncident(t, "inc_1", "payments-api", chaos.SeverityCritical, incident.StatusResolved)}
			},
			want: Degraded,
		},
		{
			name:     "an incident on another service does not count",
			breaches: []string{"latency_p99"},
			incidents: func(t *testing.T) []incident.Incident {
				return []incident.Incident{openIncident(t, "inc_1", "checkout", chaos.SeverityCritical)}
			},
			want: Degraded,
		},
		{
			name: "an open critical incident without a breach is operational",
			incidents: func(t *testing.T) []incident.Incident {
				return []incident.Incident{openIncident(t, "inc_1", "payments-api", chaos.SeverityCritical, incident.StatusMitigated)}
			},
			want: Operational,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := startAggregator(t)
			if tc.incidents != nil {
				h.incidents.set(tc.incidents(t)...)
			}

			h.send("sim_1", "payments-api", tc.breaches...)
			snap := h.snapshot()

			if got := levels(snap)["payments-api"]; got != tc.want {
				t.Errorf("payments-api level = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSnapshotOverallAndOrder(t *testing.T) {
	h := startAggregator(t)
	h.incidents.set(
		openIncident(t, "inc_old", "payments-api", chaos.SeverityCritical),
		openIncident(t, "inc_closed", "search", chaos.SeverityMajor, incident.StatusResolved),
	)

	h.send("sim_1", "search")
	h.send("sim_2", "payments-api", "latency_p99", "error_rate")
	h.send("sim_3", "checkout", "cpu")
	snap := h.snapshot()

	if snap.Level != MajorOutage {
		t.Errorf("overall level = %q, want the worst one, %q", snap.Level, MajorOutage)
	}
	if !snap.At.Equal(testStart) {
		t.Errorf("At = %s, want the pretend clock time", snap.At)
	}
	var names []string
	for _, svc := range snap.Services {
		names = append(names, svc.Name)
	}
	if fmt.Sprint(names) != "[checkout payments-api search]" {
		t.Errorf("services = %v, want them sorted by name", names)
	}
	want := map[string]Level{"checkout": Degraded, "payments-api": MajorOutage, "search": Operational}
	for name, level := range want {
		if got := levels(snap)[name]; got != level {
			t.Errorf("%s level = %q, want %q", name, got, level)
		}
	}
	if len(snap.Incidents) != 1 || snap.Incidents[0].ID != "inc_old" {
		t.Errorf("incidents = %+v, want only the open one", snap.Incidents)
	}
	if svc := snap.Services[1]; svc.Sample == nil || svc.Sample.LatencyP99Ms != 950 || len(svc.Sample.Breaches) != 2 {
		t.Errorf("payments-api sample = %+v, want the newest numbers with both breaches", svc.Sample)
	}
}

func TestEmptySnapshotIsOperational(t *testing.T) {
	h := startAggregator(t)

	snap, err := h.agg.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Level != Operational || len(snap.Services) != 0 || len(snap.Incidents) != 0 {
		t.Errorf("snapshot = %+v, want operational with nothing on the lists", snap)
	}
}

func TestOldNumbersStopCounting(t *testing.T) {
	h := startAggregator(t)

	h.send("sim_1", "payments-api", "latency_p99")
	if got := levels(h.snapshot())["payments-api"]; got != Degraded {
		t.Fatalf("level = %q, want %q while the breach is fresh", got, Degraded)
	}

	// The simulation ended, so no more numbers arrive and the old breach must fade out.
	h.clock.Advance(16 * time.Second)
	snap := h.snapshot()
	if got := levels(snap)["payments-api"]; got != Operational {
		t.Errorf("level = %q, want %q once the numbers are stale", got, Operational)
	}
	if svc := snap.Services[0]; svc.Name != "payments-api" || svc.Sample != nil {
		t.Errorf("service = %+v, want payments-api still listed but with no current numbers", svc)
	}

	h.clock.Advance(2 * time.Hour)
	if snap := h.snapshot(); len(snap.Services) != 0 {
		t.Errorf("services = %+v, want a service that was quiet for hours to drop off the list", snap.Services)
	}
}

func TestWorstSimulationSpeaksForTheService(t *testing.T) {
	h := startAggregator(t)

	// Two simulations hit the same service, and the healthy one must not hide the sick one.
	h.send("sim_sick", "payments-api", "latency_p99")
	h.send("sim_fine", "payments-api")
	if got := levels(h.snapshot())["payments-api"]; got != Degraded {
		t.Errorf("level = %q, want %q because one simulation still breaches", got, Degraded)
	}

	h.send("sim_sick", "payments-api")
	if got := levels(h.snapshot())["payments-api"]; got != Operational {
		t.Errorf("level = %q, want %q after the sick simulation recovered", got, Operational)
	}
}

func TestServiceWithOpenIncidentStaysListed(t *testing.T) {
	h := startAggregator(t)
	h.incidents.set(openIncident(t, "inc_1", "payments-api", chaos.SeverityCritical, incident.StatusMitigated))

	snap := h.snapshot()

	if len(snap.Services) != 1 || snap.Services[0].Name != "payments-api" || snap.Services[0].Level != Operational {
		t.Errorf("services = %+v, want payments-api listed as operational because its incident is still open", snap.Services)
	}
}

func TestServiceListStaysBounded(t *testing.T) {
	h := startAggregator(t, WithMaxServices(3))

	for i := 0; i < 10; i++ {
		h.send(chaos.SimulationID(fmt.Sprintf("sim_%d", i)), fmt.Sprintf("service-%d", i))
		h.clock.Advance(time.Second)
	}
	h.send("sim_settle", "zz-settle")

	h.agg.mu.Lock()
	count := len(h.agg.services)
	_, newestKept := h.agg.services["service-9"]
	_, oldestKept := h.agg.services["service-0"]
	h.agg.mu.Unlock()

	if count != 3 {
		t.Errorf("aggregator remembers %d services, want at most 3", count)
	}
	if !newestKept || oldestKept {
		t.Errorf("newest kept = %v, oldest kept = %v, want the quietest services dropped first", newestKept, oldestKept)
	}
}

func TestSnapshotAndNewAggregatorErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("incident list fails", func(t *testing.T) {
		h := startAggregator(t)
		listErr := errors.New("manager is gone")
		h.incidents.mu.Lock()
		h.incidents.err = listErr
		h.incidents.mu.Unlock()

		if _, err := h.agg.Snapshot(context.Background()); !errors.Is(err, listErr) {
			t.Errorf("Snapshot error = %v, want one that wraps the list error", err)
		}
	})

	t.Run("subscribe fails", func(t *testing.T) {
		subscribeErr := errors.New("engine is gone")
		_, err := NewAggregator(context.Background(), &fakeSamples{err: subscribeErr}, &fakeIncidents{}, logger)
		if !errors.Is(err, subscribeErr) {
			t.Errorf("NewAggregator error = %v, want one that wraps the subscribe error", err)
		}
	})

	t.Run("missing parts", func(t *testing.T) {
		if _, err := NewAggregator(context.Background(), nil, &fakeIncidents{}, logger); err == nil {
			t.Error("NewAggregator without a sample source returned no error")
		}
	})
}
