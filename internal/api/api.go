// Package api lets people start, watch, and stop pretend failures over HTTP.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// ERRORS

// errBadRequest means the caller sent something we could not read.
var errBadRequest = errors.New("bad request")

// SIMULATOR

// simulator lists the only things this package needs the engine to do.
type simulator interface {
	Inject(ctx context.Context, sc chaos.Scenario) (chaos.Simulation, error)
	Halt(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error)
	Get(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error)
	List(ctx context.Context) ([]chaos.Simulation, error)
	Subscribe(ctx context.Context) (<-chan chaos.MetricSample, error)
}

// HANDLER

// Handler answers the HTTP requests about simulations.
type Handler struct {
	sim       simulator
	log       *slog.Logger
	shutdown  context.Context // done when the server starts to turn off
	heartbeat time.Duration   // how often a quiet stream says hello
	incidents incidentSource
}

// NewHandler builds a Handler from the things it needs to do its job.
func NewHandler(sim simulator, logger *slog.Logger, shutdownCtx context.Context, opts ...Option) *Handler {
	h := &Handler{
		sim:       sim,
		log:       logger.With(slog.String("component", "api")),
		shutdown:  shutdownCtx,
		heartbeat: heartbeatInterval,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Routes gives back a router that knows which function answers which URL.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		h.writeJSON(w, r, http.StatusNotFound, errorResponse{Error: "route not found"})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		h.writeJSON(w, r, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
	})

	r.Post("/simulations", h.createSimulation)
	r.Get("/simulations", h.listSimulations)
	r.Get("/simulations/{id}", h.getSimulation)
	r.Post("/simulations/{id}/halt", h.haltSimulation)
	r.Get("/stream", h.streamMetrics)

	// The incident routes only exist when someone gave us a place to read incidents from.
	if h.incidents != nil {
		r.Get("/incidents", h.listIncidents)
		r.Get("/incidents/{id}", h.getIncident)
	}

	return r
}

// RESPONSES

// errorResponse is the shape of every error we send back.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON sends a value back to the caller as JSON.
func (h *Handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.log.ErrorContext(r.Context(), "encode response",
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.Any("error", err),
		)
	}
}

// writeError tells the caller what went wrong and keeps our secrets in the log.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := errorStatus(err)
	msg := err.Error()
	if status == http.StatusInternalServerError {
		h.log.ErrorContext(r.Context(), "request failed",
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.Any("error", err),
		)
		msg = "internal server error"
	}
	h.writeJSON(w, r, status, errorResponse{Error: msg})
}

// errorStatus picks the HTTP status number that matches an error.
func errorStatus(err error) int {
	switch {
	case errors.Is(err, errBadRequest), errors.Is(err, chaos.ErrInvalidScenario):
		return http.StatusBadRequest
	case errors.Is(err, chaos.ErrNotFound), errors.Is(err, incident.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, chaos.ErrNotActive):
		return http.StatusConflict
	case errors.Is(err, chaos.ErrCapacity):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
