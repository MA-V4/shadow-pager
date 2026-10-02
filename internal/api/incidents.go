package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MA-V4/shadow-pager/internal/incident"
)

// INCIDENT SOURCE

// incidentSource lists the only things this package needs the incident manager to do.
type incidentSource interface {
	Get(ctx context.Context, id incident.IncidentID) (incident.Incident, error)
	List(ctx context.Context) ([]incident.Incident, error)
}

// Option is a little helper that switches on an extra part of the Handler.
type Option func(*Handler)

// WithIncidents switches on the incident routes and says where to read incidents from.
func WithIncidents(src incidentSource) Option {
	return func(h *Handler) { h.incidents = src }
}

// WIRE TYPES

// timelineEntryResponse is how one story line looks when we send it back.
type timelineEntryResponse struct {
	At    time.Time `json:"at"`
	Actor string    `json:"actor"`
	Text  string    `json:"text"`
}

// incidentResponse is how one incident looks when we send it back.
type incidentResponse struct {
	ID                string                  `json:"id"`
	SimulationID      string                  `json:"simulation_id"`
	Service           string                  `json:"service"`
	Mode              string                  `json:"mode"`
	Severity          string                  `json:"severity"`
	Status            string                  `json:"status"`
	Breaches          []string                `json:"breaches"`
	DeclaredAt        time.Time               `json:"declared_at"`
	AcknowledgedAt    *time.Time              `json:"acknowledged_at,omitempty"`
	AcknowledgedBy    string                  `json:"acknowledged_by,omitempty"`
	MitigatedAt       *time.Time              `json:"mitigated_at,omitempty"`
	ResolvedAt        *time.Time              `json:"resolved_at,omitempty"`
	TimeToAcknowledge string                  `json:"time_to_acknowledge,omitempty"` // written like "1m30s"
	TimeToResolve     string                  `json:"time_to_resolve,omitempty"`     // written like "5m0s"
	SlackChannelID    string                  `json:"slack_channel_id,omitempty"`
	Timeline          []timelineEntryResponse `json:"timeline"`
}

// newIncidentResponse copies an incident into the shape we send back.
func newIncidentResponse(inc incident.Incident) incidentResponse {
	// We start with empty lists so JSON shows [] and never null.
	breaches := make([]string, 0, len(inc.Breaches))
	breaches = append(breaches, inc.Breaches...)
	timeline := make([]timelineEntryResponse, 0, len(inc.Timeline))
	for _, entry := range inc.Timeline {
		timeline = append(timeline, timelineEntryResponse{At: entry.At, Actor: entry.Actor, Text: entry.Text})
	}

	out := incidentResponse{
		ID:             string(inc.ID),
		SimulationID:   string(inc.SimulationID),
		Service:        inc.Service,
		Mode:           string(inc.Mode),
		Severity:       string(inc.Severity),
		Status:         string(inc.Status),
		Breaches:       breaches,
		DeclaredAt:     inc.DeclaredAt,
		AcknowledgedAt: inc.AcknowledgedAt,
		AcknowledgedBy: inc.AcknowledgedBy,
		MitigatedAt:    inc.MitigatedAt,
		ResolvedAt:     inc.ResolvedAt,
		SlackChannelID: inc.ChannelID,
		Timeline:       timeline,
	}
	if d, ok := inc.TimeToAcknowledge(); ok {
		out.TimeToAcknowledge = d.String()
	}
	if d, ok := inc.TimeToResolve(); ok {
		out.TimeToResolve = d.String()
	}
	return out
}

// listIncidentsResponse is the box that holds many incidents.
type listIncidentsResponse struct {
	Incidents []incidentResponse `json:"incidents"`
}

// HANDLERS

// listIncidents sends back every incident, newest first.
func (h *Handler) listIncidents(w http.ResponseWriter, r *http.Request) {
	incidents, err := h.incidents.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	out := make([]incidentResponse, 0, len(incidents))
	for _, inc := range incidents {
		out = append(out, newIncidentResponse(inc))
	}
	h.writeJSON(w, r, http.StatusOK, listIncidentsResponse{Incidents: out})
}

// getIncident sends back the one incident named in the URL.
func (h *Handler) getIncident(w http.ResponseWriter, r *http.Request) {
	inc, err := h.incidents.Get(r.Context(), incident.IncidentID(chi.URLParam(r, "id")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, newIncidentResponse(inc))
}
