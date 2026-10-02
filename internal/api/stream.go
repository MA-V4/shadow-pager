package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// WIRE TYPES

// metricResponse is how one sample looks when we send it down the stream.
type metricResponse struct {
	SimulationID string    `json:"simulation_id"`
	Service      string    `json:"service"`
	Mode         string    `json:"mode"`
	Timestamp    time.Time `json:"timestamp"`
	LatencyP99Ms float64   `json:"latency_p99_ms"`
	ErrorRate    float64   `json:"error_rate"`
	CPUPercent   float64   `json:"cpu_percent"`
	MemoryMB     float64   `json:"memory_mb"`
	Healthy      bool      `json:"healthy"`
	Breaches     []string  `json:"breaches"`
}

// newMetricResponse copies an engine sample into the shape we send down the stream.
func newMetricResponse(m chaos.MetricSample) metricResponse {
	breaches := m.Breaches
	if breaches == nil {
		breaches = []string{}
	}
	return metricResponse{
		SimulationID: string(m.SimulationID),
		Service:      m.Service,
		Mode:         string(m.Mode),
		Timestamp:    m.Timestamp,
		LatencyP99Ms: m.LatencyP99Ms,
		ErrorRate:    m.ErrorRate,
		CPUPercent:   m.CPUPercent,
		MemoryMB:     m.MemoryMB,
		Healthy:      m.Healthy,
		Breaches:     breaches,
	}
}

// HANDLER

// heartbeatInterval is how often we say hello on a quiet stream.
const heartbeatInterval = 15 * time.Second

// streamMetrics keeps the connection open and sends every new sample as it happens.
func (h *Handler) streamMetrics(w http.ResponseWriter, r *http.Request) {
	// This context ends when the caller leaves or when the server starts to turn off.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(h.shutdown, cancel)
	defer stop()

	// The server normally hangs up on slow answers, so we switch that off for this one.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		h.writeError(w, r, fmt.Errorf("clear stream write deadline: %w", err))
		return
	}

	samples, err := h.sim.Subscribe(ctx)
	if err != nil {
		h.writeError(w, r, fmt.Errorf("subscribe to metrics: %w", err))
		return
	}

	only := chaos.SimulationID(r.URL.Query().Get("simulation_id"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		h.logStreamEnd(r, fmt.Errorf("flush stream headers: %w", err))
		return
	}

	heartbeat := time.NewTicker(h.heartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case sample, ok := <-samples:
			if !ok {
				return
			}
			if only != "" && sample.SimulationID != only {
				continue
			}
			if err := writeEvent(w, rc, "metric", newMetricResponse(sample)); err != nil {
				h.logStreamEnd(r, err)
				return
			}
		case <-heartbeat.C:
			if err := writeHeartbeat(w, rc); err != nil {
				h.logStreamEnd(r, err)
				return
			}
		}
	}
}

// logStreamEnd writes a quiet note when a stream stops because we could not write to it.
func (h *Handler) logStreamEnd(r *http.Request, err error) {
	h.log.DebugContext(r.Context(), "stream ended",
		slog.String("request_id", middleware.GetReqID(r.Context())),
		slog.Any("error", err),
	)
}

// WRITING

// writeEvent sends one named event and pushes it out right away.
func writeEvent(w io.Writer, rc *http.ResponseController, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", event, err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return fmt.Errorf("write %s event: %w", event, err)
	}
	if err := rc.Flush(); err != nil {
		return fmt.Errorf("flush %s event: %w", event, err)
	}
	return nil
}

// writeHeartbeat sends a tiny note that browsers ignore, so the line never looks dead.
func writeHeartbeat(w io.Writer, rc *http.ResponseController) error {
	if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
		return fmt.Errorf("write heartbeat: %w", err)
	}
	if err := rc.Flush(); err != nil {
		return fmt.Errorf("flush heartbeat: %w", err)
	}
	return nil
}
