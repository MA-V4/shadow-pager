package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// FAKE

// fakeSimulator pretends to be the engine and answers with whatever the test put in its fields.
type fakeSimulator struct {
	sim    chaos.Simulation
	sims   []chaos.Simulation
	stream chan chaos.MetricSample

	injectErr    error
	haltErr      error
	getErr       error
	listErr      error
	subscribeErr error

	gotScenario chaos.Scenario
	gotID       chaos.SimulationID
}

func (f *fakeSimulator) Inject(_ context.Context, sc chaos.Scenario) (chaos.Simulation, error) {
	f.gotScenario = sc
	return f.sim, f.injectErr
}

func (f *fakeSimulator) Halt(_ context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	f.gotID = id
	return f.sim, f.haltErr
}

func (f *fakeSimulator) Get(_ context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	f.gotID = id
	return f.sim, f.getErr
}

func (f *fakeSimulator) List(_ context.Context) ([]chaos.Simulation, error) {
	return f.sims, f.listErr
}

func (f *fakeSimulator) Subscribe(_ context.Context) (<-chan chaos.MetricSample, error) {
	return f.stream, f.subscribeErr
}

// FIXTURES

const validBody = `{"service":"payments-api","mode":"latency_spike","severity":"major","intensity":0.9,"duration":"2m"}`

var validScenario = chaos.Scenario{
	Service:   "payments-api",
	Mode:      chaos.FailureLatencySpike,
	Severity:  chaos.SeverityMajor,
	Intensity: 0.9,
	Duration:  2 * time.Minute,
}

// testSimulation builds a simulation with a fixed start time so tests always see the same thing.
func testSimulation(id chaos.SimulationID, state chaos.State) chaos.Simulation {
	started := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	sim := chaos.Simulation{
		ID:        id,
		Scenario:  validScenario,
		State:     state,
		StartedAt: started,
		EndsAt:    started.Add(validScenario.Duration),
	}
	if state.Terminal() {
		ended := started.Add(30 * time.Second)
		sim.EndedAt = &ended
	}
	return sim
}

// TESTS

func TestSimulationHandlers(t *testing.T) {
	tests := []struct {
		name            string
		method          string
		path            string
		body            string
		fake            fakeSimulator
		wantStatus      int
		wantLocation    string
		wantBody        []string
		wantNotInBody   []string
		wantLogContains string
		check           func(t *testing.T, f *fakeSimulator)
	}{
		{
			name:         "create success",
			method:       http.MethodPost,
			path:         "/simulations",
			body:         validBody,
			fake:         fakeSimulator{sim: testSimulation("sim_abc", chaos.StateRunning)},
			wantStatus:   http.StatusCreated,
			wantLocation: "/simulations/sim_abc",
			wantBody: []string{
				`"id":"sim_abc"`,
				`"duration":"2m0s"`,
				`"state":"running"`,
				`"started_at":"2026-01-02T15:04:05Z"`,
				`"ends_at":"2026-01-02T15:06:05Z"`,
			},
			wantNotInBody: []string{"ended_at"},
			check: func(t *testing.T, f *fakeSimulator) {
				if f.gotScenario != validScenario {
					t.Errorf("scenario passed to Inject = %+v, want %+v", f.gotScenario, validScenario)
				}
			},
		},
		{
			name:       "create invalid json",
			method:     http.MethodPost,
			path:       "/simulations",
			body:       `{"service":"payments-api"`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create unknown field",
			method:     http.MethodPost,
			path:       "/simulations",
			body:       `{"service":"payments-api","colour":"red"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   []string{"colour"},
		},
		{
			name:       "create trailing data",
			method:     http.MethodPost,
			path:       "/simulations",
			body:       validBody + `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create bad duration",
			method:     http.MethodPost,
			path:       "/simulations",
			body:       `{"service":"payments-api","mode":"latency_spike","severity":"major","intensity":0.9,"duration":"soon"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   []string{"duration"},
		},
		{
			name:       "create invalid scenario",
			method:     http.MethodPost,
			path:       "/simulations",
			body:       validBody,
			fake:       fakeSimulator{injectErr: fmt.Errorf("inject: %w: service is required", chaos.ErrInvalidScenario)},
			wantStatus: http.StatusBadRequest,
			wantBody:   []string{"service is required"},
		},
		{
			name:       "create at capacity",
			method:     http.MethodPost,
			path:       "/simulations",
			body:       validBody,
			fake:       fakeSimulator{injectErr: fmt.Errorf("inject: %w (10)", chaos.ErrCapacity)},
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name:            "create unexpected error",
			method:          http.MethodPost,
			path:            "/simulations",
			body:            validBody,
			fake:            fakeSimulator{injectErr: errors.New("dial tcp 10.0.0.7:5432: connection refused")},
			wantStatus:      http.StatusInternalServerError,
			wantBody:        []string{"internal server error"},
			wantNotInBody:   []string{"10.0.0.7", "connection refused", "dial tcp"},
			wantLogContains: "dial tcp 10.0.0.7:5432: connection refused",
		},
		{
			name:   "list newest first",
			method: http.MethodGet,
			path:   "/simulations",
			fake: fakeSimulator{sims: []chaos.Simulation{
				testSimulation("sim_new", chaos.StateRunning),
				testSimulation("sim_old", chaos.StateResolved),
			}},
			wantStatus: http.StatusOK,
			wantBody:   []string{`"simulations":[{"id":"sim_new"`, `"id":"sim_old"`},
		},
		{
			name:       "list empty is an empty array",
			method:     http.MethodGet,
			path:       "/simulations",
			wantStatus: http.StatusOK,
			wantBody:   []string{`"simulations":[]`},
		},
		{
			name:       "get success",
			method:     http.MethodGet,
			path:       "/simulations/sim_abc",
			fake:       fakeSimulator{sim: testSimulation("sim_abc", chaos.StateRunning)},
			wantStatus: http.StatusOK,
			wantBody:   []string{`"id":"sim_abc"`},
			check: func(t *testing.T, f *fakeSimulator) {
				if f.gotID != "sim_abc" {
					t.Errorf("id passed to Get = %q, want %q", f.gotID, "sim_abc")
				}
			},
		},
		{
			name:       "get not found",
			method:     http.MethodGet,
			path:       "/simulations/sim_missing",
			fake:       fakeSimulator{getErr: fmt.Errorf("get sim_missing: %w", chaos.ErrNotFound)},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "halt success",
			method:     http.MethodPost,
			path:       "/simulations/sim_abc/halt",
			fake:       fakeSimulator{sim: testSimulation("sim_abc", chaos.StateHalted)},
			wantStatus: http.StatusOK,
			wantBody:   []string{`"state":"halted"`, `"ended_at":"2026-01-02T15:04:35Z"`},
			check: func(t *testing.T, f *fakeSimulator) {
				if f.gotID != "sim_abc" {
					t.Errorf("id passed to Halt = %q, want %q", f.gotID, "sim_abc")
				}
			},
		},
		{
			name:       "halt not found",
			method:     http.MethodPost,
			path:       "/simulations/sim_missing/halt",
			fake:       fakeSimulator{haltErr: fmt.Errorf("halt sim_missing: %w", chaos.ErrNotFound)},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "halt finished simulation",
			method:     http.MethodPost,
			path:       "/simulations/sim_abc/halt",
			fake:       fakeSimulator{haltErr: fmt.Errorf("halt sim_abc (state resolved): %w", chaos.ErrNotActive)},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "wrong method",
			method:     http.MethodDelete,
			path:       "/simulations/sim_abc",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unknown route",
			method:     http.MethodGet,
			path:       "/nowhere",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := tc.fake
			var logs bytes.Buffer
			h := NewHandler(&fake, slog.New(slog.NewJSONHandler(&logs, nil)), context.Background())

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.Routes().ServeHTTP(rec, req)

			body := rec.Body.String()
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, body)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Header().Get("Location"); got != tc.wantLocation {
				t.Errorf("Location = %q, want %q", got, tc.wantLocation)
			}
			for _, want := range tc.wantBody {
				if !strings.Contains(body, want) {
					t.Errorf("body %s does not contain %q", body, want)
				}
			}
			for _, banned := range tc.wantNotInBody {
				if strings.Contains(body, banned) {
					t.Errorf("body %s must not contain %q", body, banned)
				}
			}
			if !strings.Contains(logs.String(), tc.wantLogContains) {
				t.Errorf("logs %s do not contain %q", logs.String(), tc.wantLogContains)
			}

			// Every error answer must be a JSON object with one filled in error field.
			if tc.wantStatus >= 400 {
				var got map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatalf("error body %s is not a JSON object of strings: %v", body, err)
				}
				if len(got) != 1 || got["error"] == "" {
					t.Errorf("error body = %v, want only a non-empty error field", got)
				}
			}

			if tc.check != nil {
				tc.check(t, &fake)
			}
		})
	}
}
