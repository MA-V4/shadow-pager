package chaos

import (
	"math"
	"math/rand/v2"
	"time"
)

// TELEMETRY
type MetricSample struct {
	SimulationID SimulationID `json:"simulation_id"`
	Service      string       `json:"service"`
	Mode         FailureMode  `json:"mode"`
	Timestamp    time.Time    `json:"ts"`
	LatencyP99Ms float64      `json:"latency_p99_ms"`
	ErrorRate    float64      `json:"error_rate"` // fraction of failed requests, 0.0 to 1.0
	CPUPercent   float64      `json:"cpu_percent"`
	MemoryMB     float64      `json:"memory_mb"`
	Healthy      bool         `json:"healthy"`
	Breaches     []string     `json:"breaches,omitempty"` // which SLOs this sample violates
}

type Baseline struct {
	LatencyP99Ms float64
	ErrorRate    float64
	CPUPercent   float64
	MemoryMB     float64
}

var DefaultBaseline = Baseline{
	LatencyP99Ms: 120,
	ErrorRate:    0.002,
	CPUPercent:   30,
	MemoryMB:     512,
}

type SLO struct {
	MaxLatencyP99Ms float64
	MaxErrorRate    float64
	MaxCPUPercent   float64
	MaxMemoryMB     float64
}

var DefaultSLO = SLO{
	MaxLatencyP99Ms: 500,
	MaxErrorRate:    0.01,
	MaxCPUPercent:   90,
	MaxMemoryMB:     2048,
}

// Breaches lists the signals in m that violate the SLO. Nil means healthy.
func (s SLO) Breaches(m MetricSample) []string {
	var out []string
	if m.LatencyP99Ms > s.MaxLatencyP99Ms {
		out = append(out, "latency_p99")
	}
	if m.ErrorRate > s.MaxErrorRate {
		out = append(out, "error_rate")
	}
	if m.CPUPercent > s.MaxCPUPercent {
		out = append(out, "cpu")
	}
	if m.MemoryMB > s.MaxMemoryMB {
		out = append(out, "memory")
	}
	return out
}

// SHAPES

// Shape maps simulation progress (0 at injection, 1 at the scheduled end) to
// how strongly the failure is expressed (0 healthy, 1 full impact). Shapes
type Shape func(progress float64) float64

// ShapeSpike: sharp onset, holds, recovers quickly at the end.
func ShapeSpike(p float64) float64 {
	return smoothstep(0, 0.1, p) * (1 - smoothstep(0.9, 1, p))
}

// ShapeGradual: slow build over the first 40%, slow recovery at the end.
func ShapeGradual(p float64) float64 {
	return smoothstep(0, 0.4, p) * (1 - smoothstep(0.85, 1, p))
}

// ShapeLeak: grows steadily and never recovers on its own, like a real leak.
// It only clears when the simulation is resolved or halted.
func ShapeLeak(p float64) float64 { return clamp(p, 0, 1) }

// ShapeCliff: looks fine for a while, then collapses suddenly once a hidden
// limit is hit. Connection pools fail exactly like this.
func ShapeCliff(p float64) float64 {
	return smoothstep(0.3, 0.4, p) * (1 - smoothstep(0.9, 1, p))
}

func smoothstep(edge0, edge1, x float64) float64 {
	t := clamp((x-edge0)/(edge1-edge0), 0, 1)
	return t * t * (3 - 2*t)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// GENERATOR

// Impact is how far each signal moves at full intensity and full shape.
type Impact struct {
	LatencyMul float64
	ErrorAdd   float64
	CPUAdd     float64
	MemoryAdd  float64
}

// ProfileGenerator turns a Baseline, an Impact and a Shape into telemetry.

type ProfileGenerator struct {
	Base   Baseline
	Impact Impact
	Shape  Shape
	SLO    SLO
	Jitter float64        // relative noise, 0.05 means about plus or minus 5%
	Noise  func() float64 // standard normal source; tests swap in a constant
}

// Compile-time proof that ProfileGenerator satisfies Generator.
var _ Generator = ProfileGenerator{}

func (g ProfileGenerator) Sample(sim Simulation, elapsed time.Duration) MetricSample {
	progress := 0.0
	if d := sim.Scenario.Duration; d > 0 {
		progress = clamp(float64(elapsed)/float64(d), 0, 1)
	}
	s := g.Shape(progress) * sim.Scenario.Intensity

	m := MetricSample{
		SimulationID: sim.ID,
		Service:      sim.Scenario.Service,
		Mode:         sim.Scenario.Mode,
		Timestamp:    sim.StartedAt.Add(elapsed),
		LatencyP99Ms: math.Max(0, g.jitter(g.Base.LatencyP99Ms*(1+(g.Impact.LatencyMul-1)*s))),
		ErrorRate:    clamp(g.jitter(g.Base.ErrorRate+g.Impact.ErrorAdd*s), 0, 1),
		CPUPercent:   clamp(g.jitter(g.Base.CPUPercent+g.Impact.CPUAdd*s), 0, 100),
		MemoryMB:     math.Max(0, g.jitter(g.Base.MemoryMB+g.Impact.MemoryAdd*s)),
	}
	m.Breaches = g.SLO.Breaches(m)
	m.Healthy = len(m.Breaches) == 0
	return m
}

func (g ProfileGenerator) jitter(v float64) float64 {
	if g.Noise == nil || g.Jitter == 0 {
		return v
	}
	return v * (1 + g.Jitter*g.Noise())
}

// DEFAULT PROFILES

var impacts = map[FailureMode]struct {
	impact Impact
	shape  Shape
}{
	FailureLatencySpike:     {Impact{LatencyMul: 8, ErrorAdd: 0.02, CPUAdd: 10}, ShapeSpike},
	FailureErrorRateSurge:   {Impact{LatencyMul: 1.5, ErrorAdd: 0.35, CPUAdd: 5}, ShapeSpike},
	FailureMemoryLeak:       {Impact{LatencyMul: 2, ErrorAdd: 0.01, CPUAdd: 20, MemoryAdd: 2560}, ShapeLeak},
	FailureCPUSaturation:    {Impact{LatencyMul: 4, ErrorAdd: 0.03, CPUAdd: 65}, ShapeGradual},
	FailureDBPoolExhaustion: {Impact{LatencyMul: 20, ErrorAdd: 0.25}, ShapeCliff},
}

// DefaultGenerators builds a generator for every known failure mode.
func DefaultGenerators() map[FailureMode]Generator {
	out := make(map[FailureMode]Generator, len(impacts))
	for mode, p := range impacts {
		out[mode] = ProfileGenerator{
			Base:   DefaultBaseline,
			Impact: p.impact,
			Shape:  p.shape,
			SLO:    DefaultSLO,
			Jitter: 0.05,
			Noise:  rand.NormFloat64,
		}
	}
	return out
}

// WithDefaultGenerators registers DefaultGenerators on the engine.
func WithDefaultGenerators() Option {
	return func(e *Engine) {
		for mode, g := range DefaultGenerators() {
			e.generators[mode] = g
		}
	}
}
