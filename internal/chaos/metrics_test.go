package chaos

import (
	"testing"
	"time"
)

// quietGenerator returns the default profile for mode with noise disabled,
// so every assertion below is exact and repeatable.
func quietGenerator(t *testing.T, mode FailureMode) ProfileGenerator {
	t.Helper()
	g, ok := DefaultGenerators()[mode].(ProfileGenerator)
	if !ok {
		t.Fatalf("no ProfileGenerator for mode %q", mode)
	}
	g.Noise = nil
	return g
}

func testSim(mode FailureMode, intensity float64) Simulation {
	return Simulation{
		ID:        "sim_test",
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Scenario: Scenario{
			Service: "payments-api", Mode: mode, Severity: SeverityMajor,
			Intensity: intensity, Duration: 10 * time.Minute,
		},
	}
}

func TestEveryModeHasAGenerator(t *testing.T) {
	gens := DefaultGenerators()
	for _, mode := range []FailureMode{
		FailureLatencySpike, FailureErrorRateSurge, FailureMemoryLeak,
		FailureCPUSaturation, FailureDBPoolExhaustion,
	} {
		if _, ok := gens[mode]; !ok {
			t.Errorf("missing generator for %q", mode)
		}
	}
}

func TestSampleHealthyAtInjection(t *testing.T) {
	for mode := range DefaultGenerators() {
		t.Run(string(mode), func(t *testing.T) {
			m := quietGenerator(t, mode).Sample(testSim(mode, 1), 0)
			if !m.Healthy {
				t.Fatalf("expected healthy at t=0, breaches: %v", m.Breaches)
			}
		})
	}
}

func TestSampleBreachesAtPeak(t *testing.T) {
	for mode := range DefaultGenerators() {
		t.Run(string(mode), func(t *testing.T) {
			sim := testSim(mode, 1)
			// 80% through: past every onset, before every recovery.
			m := quietGenerator(t, mode).Sample(sim, 8*time.Minute)
			if m.Healthy {
				t.Fatalf("expected SLO breach at peak, got %+v", m)
			}
		})
	}
}

func TestZeroIntensityNeverBreaches(t *testing.T) {
	for mode := range DefaultGenerators() {
		g := quietGenerator(t, mode)
		sim := testSim(mode, 0)
		for elapsed := time.Duration(0); elapsed <= sim.Scenario.Duration; elapsed += 30 * time.Second {
			if m := g.Sample(sim, elapsed); !m.Healthy {
				t.Fatalf("%s at %s: zero intensity breached %v", mode, elapsed, m.Breaches)
			}
		}
	}
}

func TestMemoryLeakIsMonotonic(t *testing.T) {
	g := quietGenerator(t, FailureMemoryLeak)
	sim := testSim(FailureMemoryLeak, 1)
	prev := -1.0
	for elapsed := time.Duration(0); elapsed <= sim.Scenario.Duration; elapsed += 15 * time.Second {
		m := g.Sample(sim, elapsed)
		if m.MemoryMB < prev {
			t.Fatalf("memory dropped from %.0f to %.0f at %s", prev, m.MemoryMB, elapsed)
		}
		prev = m.MemoryMB
	}
}

func TestSampleTimestampIsDeterministic(t *testing.T) {
	sim := testSim(FailureLatencySpike, 1)
	m := quietGenerator(t, FailureLatencySpike).Sample(sim, 90*time.Second)
	if want := sim.StartedAt.Add(90 * time.Second); !m.Timestamp.Equal(want) {
		t.Fatalf("timestamp = %s, want %s", m.Timestamp, want)
	}
}
