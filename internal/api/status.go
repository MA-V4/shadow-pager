package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
	"github.com/MA-V4/shadow-pager/internal/status"
)

// STATUS SOURCE

// statusSource lists the only thing this package needs the status aggregator to do.
type statusSource interface {
	Snapshot(ctx context.Context) (status.Snapshot, error)
}

// WithStatus switches on the public status route and says where the picture comes from.
func WithStatus(src statusSource) Option {
	return func(h *Handler) {
		h.extraRoutes = append(h.extraRoutes, func(r chi.Router) {
			r.Get("/status", func(w http.ResponseWriter, r *http.Request) { h.getStatus(w, r, src) })
		})
	}
}

// WithSLO switches on the route that tells clients where the limits are, so charts can draw them.
func WithSLO(slo chaos.SLO) Option {
	return func(h *Handler) {
		h.extraRoutes = append(h.extraRoutes, func(r chi.Router) {
			r.Get("/slo", func(w http.ResponseWriter, r *http.Request) {
				h.writeJSON(w, r, http.StatusOK, sloResponse{
					MaxLatencyP99Ms: slo.MaxLatencyP99Ms,
					MaxErrorRate:    slo.MaxErrorRate,
					MaxCPUPercent:   slo.MaxCPUPercent,
					MaxMemoryMB:     slo.MaxMemoryMB,
				})
			})
		})
	}
}

// WIRE TYPES

// sloResponse is how the limits look when we send them back.
type sloResponse struct {
	MaxLatencyP99Ms float64 `json:"max_latency_p99_ms"`
	MaxErrorRate    float64 `json:"max_error_rate"`
	MaxCPUPercent   float64 `json:"max_cpu_percent"`
	MaxMemoryMB     float64 `json:"max_memory_mb"`
}

// statusMetricsResponse holds the newest numbers of one service.
type statusMetricsResponse struct {
	LatencyP99Ms float64   `json:"latency_p99_ms"`
	ErrorRate    float64   `json:"error_rate"`
	CPUPercent   float64   `json:"cpu_percent"`
	MemoryMB     float64   `json:"memory_mb"`
	Breaches     []string  `json:"breaches"`
	SampledAt    time.Time `json:"sampled_at"`
}

// statusServiceResponse is how one service looks on the public status page.
type statusServiceResponse struct {
	Name    string                 `json:"name"`
	Status  string                 `json:"status"`
	Metrics *statusMetricsResponse `json:"metrics"` // null when nothing was measured lately
}

// statusUpdateResponse is one public line of an incident's story, without the name of who did it.
type statusUpdateResponse struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// statusIncidentResponse is the part of an incident that anyone may see.
type statusIncidentResponse struct {
	ID             string                 `json:"id"`
	Service        string                 `json:"service"`
	Mode           string                 `json:"mode"`
	Severity       string                 `json:"severity"`
	Status         string                 `json:"status"`
	Breaches       []string               `json:"breaches"`
	DeclaredAt     time.Time              `json:"declared_at"`
	AcknowledgedAt *time.Time             `json:"acknowledged_at,omitempty"`
	MitigatedAt    *time.Time             `json:"mitigated_at,omitempty"`
	Updates        []statusUpdateResponse `json:"updates"`
}

// statusResponse is the whole public status page as data.
type statusResponse struct {
	Status    string                   `json:"status"`
	UpdatedAt time.Time                `json:"updated_at"`
	Services  []statusServiceResponse  `json:"services"`
	Incidents []statusIncidentResponse `json:"incidents"`
}

// newStatusResponse copies a snapshot into the shape we send back, and leaves every private field behind.
func newStatusResponse(snap status.Snapshot) statusResponse {
	// We start with empty lists so JSON shows [] and never null.
	out := statusResponse{
		Status:    string(snap.Level),
		UpdatedAt: snap.At,
		Services:  make([]statusServiceResponse, 0, len(snap.Services)),
		Incidents: make([]statusIncidentResponse, 0, len(snap.Incidents)),
	}
	for _, svc := range snap.Services {
		item := statusServiceResponse{Name: svc.Name, Status: string(svc.Level)}
		if s := svc.Sample; s != nil {
			item.Metrics = &statusMetricsResponse{
				LatencyP99Ms: roundTo(s.LatencyP99Ms, 2),
				ErrorRate:    roundTo(s.ErrorRate, 4),
				CPUPercent:   roundTo(s.CPUPercent, 2),
				MemoryMB:     roundTo(s.MemoryMB, 2),
				Breaches:     append([]string{}, s.Breaches...),
				SampledAt:    s.Timestamp,
			}
		}
		out.Services = append(out.Services, item)
	}
	for _, inc := range snap.Incidents {
		out.Incidents = append(out.Incidents, newStatusIncidentResponse(inc))
	}
	return out
}

// newStatusIncidentResponse keeps only the public fields of an incident.
func newStatusIncidentResponse(inc incident.Incident) statusIncidentResponse {
	updates := make([]statusUpdateResponse, 0, len(inc.Timeline))
	for _, entry := range inc.Timeline {
		updates = append(updates, statusUpdateResponse{At: entry.At, Text: entry.Text})
	}
	return statusIncidentResponse{
		ID:             string(inc.ID),
		Service:        inc.Service,
		Mode:           string(inc.Mode),
		Severity:       string(inc.Severity),
		Status:         string(inc.Status),
		Breaches:       append([]string{}, inc.Breaches...),
		DeclaredAt:     inc.DeclaredAt,
		AcknowledgedAt: inc.AcknowledgedAt,
		MitigatedAt:    inc.MitigatedAt,
		Updates:        updates,
	}
}

// HANDLER

// getStatus sends back how every service is doing right now.
func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request, src statusSource) {
	snap, err := src.Snapshot(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, newStatusResponse(snap))
}
