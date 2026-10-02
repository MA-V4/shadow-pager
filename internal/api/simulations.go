package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// WIRE TYPES

// createSimulationRequest is what a caller sends to start a simulation.
type createSimulationRequest struct {
	Service   string  `json:"service"`
	Mode      string  `json:"mode"`
	Severity  string  `json:"severity"`
	Intensity float64 `json:"intensity"`
	Duration  string  `json:"duration"` // written like "2m" or "90s"
}

// toScenario turns the caller's words into the shape the engine understands.
func (req createSimulationRequest) toScenario() (chaos.Scenario, error) {
	d, err := time.ParseDuration(req.Duration)
	if err != nil {
		return chaos.Scenario{}, fmt.Errorf("%w: parse duration: %w", errBadRequest, err)
	}
	return chaos.Scenario{
		Service:   req.Service,
		Mode:      chaos.FailureMode(req.Mode),
		Severity:  chaos.Severity(req.Severity),
		Intensity: req.Intensity,
		Duration:  d,
	}, nil
}

// simulationResponse is how one simulation looks when we send it back.
type simulationResponse struct {
	ID        string     `json:"id"`
	Service   string     `json:"service"`
	Mode      string     `json:"mode"`
	Severity  string     `json:"severity"`
	Intensity float64    `json:"intensity"`
	Duration  string     `json:"duration"`
	State     string     `json:"state"`
	StartedAt time.Time  `json:"started_at"`
	EndsAt    time.Time  `json:"ends_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"` // left out until the simulation ends
}

// newSimulationResponse copies an engine simulation into the shape we send back.
func newSimulationResponse(sim chaos.Simulation) simulationResponse {
	return simulationResponse{
		ID:        string(sim.ID),
		Service:   sim.Scenario.Service,
		Mode:      string(sim.Scenario.Mode),
		Severity:  string(sim.Scenario.Severity),
		Intensity: sim.Scenario.Intensity,
		Duration:  sim.Scenario.Duration.String(),
		State:     string(sim.State),
		StartedAt: sim.StartedAt,
		EndsAt:    sim.EndsAt,
		EndedAt:   sim.EndedAt,
	}
}

// listSimulationsResponse is the box that holds many simulations.
type listSimulationsResponse struct {
	Simulations []simulationResponse `json:"simulations"`
}

// DECODING

// maxBodyBytes is the biggest request body we are willing to read.
const maxBodyBytes = 1 << 20

// decodeJSON reads exactly one JSON object from the request and says no to anything strange.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: decode json body: %w", errBadRequest, err)
	}
	// A second read must find nothing, or the caller sent extra stuff.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: body must hold only one json object", errBadRequest)
	}
	return nil
}

// HANDLERS

// createSimulation starts a new simulation from the request body.
func (h *Handler) createSimulation(w http.ResponseWriter, r *http.Request) {
	var req createSimulationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	sc, err := req.toScenario()
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	sim, err := h.sim.Inject(r.Context(), sc)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	w.Header().Set("Location", r.URL.Path+"/"+string(sim.ID))
	h.writeJSON(w, r, http.StatusCreated, newSimulationResponse(sim))
}

// listSimulations sends back every simulation, newest first.
func (h *Handler) listSimulations(w http.ResponseWriter, r *http.Request) {
	sims, err := h.sim.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	// We start with an empty list so JSON shows [] and never null.
	out := make([]simulationResponse, 0, len(sims))
	for _, sim := range sims {
		out = append(out, newSimulationResponse(sim))
	}
	h.writeJSON(w, r, http.StatusOK, listSimulationsResponse{Simulations: out})
}

// getSimulation sends back the one simulation named in the URL.
func (h *Handler) getSimulation(w http.ResponseWriter, r *http.Request) {
	sim, err := h.sim.Get(r.Context(), chaos.SimulationID(chi.URLParam(r, "id")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, newSimulationResponse(sim))
}

// haltSimulation stops the simulation named in the URL before its time is up.
func (h *Handler) haltSimulation(w http.ResponseWriter, r *http.Request) {
	sim, err := h.sim.Halt(r.Context(), chaos.SimulationID(chi.URLParam(r, "id")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, newSimulationResponse(sim))
}
