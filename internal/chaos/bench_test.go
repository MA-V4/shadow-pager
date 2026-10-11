package chaos

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

// BENCHMARKS

// benchBuffer is how many samples each listener can hold before the benchmark empties it.
const benchBuffer = 1024

// benchEngine builds a quiet engine with the given number of listeners and hands back their channels.
func benchEngine(b *testing.B, subscribers, buffer int) (*Engine, []<-chan MetricSample) {
	b.Helper()
	e, err := NewEngine(Config{SubscriberBuffer: buffer}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		b.Fatalf("NewEngine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)

	chans := make([]<-chan MetricSample, 0, subscribers)
	for i := 0; i < subscribers; i++ {
		ch, err := e.Subscribe(ctx)
		if err != nil {
			b.Fatalf("Subscribe: %v", err)
		}
		chans = append(chans, ch)
	}
	return e, chans
}

// benchSimulation is one running simulation for the benchmarks to sample.
func benchSimulation() Simulation {
	start := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	return Simulation{
		ID: "sim_bench",
		Scenario: Scenario{
			Service:   "payments-api",
			Mode:      FailureLatencySpike,
			Severity:  SeverityMajor,
			Intensity: 0.8,
			Duration:  2 * time.Minute,
		},
		State:     StateRunning,
		StartedAt: start,
		EndsAt:    start.Add(2 * time.Minute),
	}
}

// BenchmarkBroadcast measures handing one sample to every listener when all of them have room.
func BenchmarkBroadcast(b *testing.B) {
	for _, subscribers := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("subscribers=%d", subscribers), func(b *testing.B) {
			e, chans := benchEngine(b, subscribers, benchBuffer)
			sample := MetricSample{SimulationID: "sim_bench", Service: "payments-api", Healthy: true}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// The listeners are emptied with the clock stopped, so only the sending is measured.
				if i > 0 && i%benchBuffer == 0 {
					b.StopTimer()
					for _, ch := range chans {
						for len(ch) > 0 {
							<-ch
						}
					}
					b.StartTimer()
				}
				e.broadcast(sample)
			}
			b.StopTimer()

			if dropped := e.dropped.Load(); dropped != 0 {
				b.Fatalf("dropped %d samples, want none because every listener had room", dropped)
			}
		})
	}
}

// BenchmarkBroadcastFull measures the path where every listener is full and every sample is dropped.
func BenchmarkBroadcastFull(b *testing.B) {
	const subscribers = 100
	e, _ := benchEngine(b, subscribers, 1)
	sample := MetricSample{SimulationID: "sim_bench", Service: "payments-api", Healthy: true}
	// One sample fills every listener, and nobody ever reads it.
	e.broadcast(sample)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.broadcast(sample)
	}
	b.StopTimer()

	if dropped, want := e.dropped.Load(), uint64(b.N)*subscribers; dropped != want {
		b.Fatalf("dropped %d samples, want %d", dropped, want)
	}
}

// BenchmarkProfileGeneratorSample measures making one sample with the real noise source.
func BenchmarkProfileGeneratorSample(b *testing.B) {
	gen, ok := DefaultGenerators()[FailureLatencySpike]
	if !ok {
		b.Fatal("no default generator for a latency spike")
	}
	sim := benchSimulation()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// The elapsed time walks through the whole simulation, so every part of the shape is used.
		_ = gen.Sample(sim, time.Duration(i%120)*time.Second)
	}
}
