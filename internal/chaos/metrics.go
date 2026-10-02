package chaos

import "time"

// MetricSample is one point of simulated telemetry, shaped for direct JSON
// streaming to the dashboard and status page.
//
// Phase 2 adds the Generator implementations (one per FailureMode) here.
type MetricSample struct {
	SimulationID SimulationID `json:"simulation_id"`
	Service      string       `json:"service"`
	Timestamp    time.Time    `json:"ts"`
	LatencyP99Ms float64      `json:"latency_p99_ms"`
	ErrorRate    float64      `json:"error_rate"` // fraction of failed requests, 0.0 to 1.0
	CPUPercent   float64      `json:"cpu_percent"`
	MemoryMB     float64      `json:"memory_mb"`
	Healthy      bool         `json:"healthy"`
}
