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
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// FAKE

// fakeArchive pretends to be the database and answers with whatever the test put in its fields.
type fakeArchive struct {
	sims      []chaos.Simulation
	incidents []incident.Incident
	err       error

	gotLimit int
}

func (f *fakeArchive) ListRecentSimulations(_ context.Context, limit int) ([]chaos.Simulation, error) {
	f.gotLimit = limit
	return f.sims, f.err
}

func (f *fakeArchive) GetSimulation(_ context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	if f.err != nil {
		return chaos.Simulation{}, f.err
	}
	for _, sim := range f.sims {
		if sim.ID == id {
			return sim, nil
		}
	}
	return chaos.Simulation{}, fmt.Errorf("get saved simulation %s: %w", id, chaos.ErrNotFound)
}

func (f *fakeArchive) ListRecentIncidents(_ context.Context, limit int) ([]incident.Incident, error) {
	f.gotLimit = limit
	return f.incidents, f.err
}

func (f *fakeArchive) GetIncident(_ context.Context, id incident.IncidentID) (incident.Incident, error) {
	if f.err != nil {
		return incident.Incident{}, f.err
	}
	for _, inc := range f.incidents {
		if inc.ID == id {
			return inc, nil
		}
	}
	return incident.Incident{}, fmt.Errorf("get saved incident %s: %w", id, incident.ErrNotFound)
}

// HELPERS

// simulationAt builds a simulation that started some minutes after the usual test start.
func simulationAt(id chaos.SimulationID, state chaos.State, minutes int) chaos.Simulation {
	sim := testSimulation(id, state)
	sim.StartedAt = sim.StartedAt.Add(time.Duration(minutes) * time.Minute)
	return sim
}

// incidentAt builds an incident that was declared some minutes after the usual test start.
func incidentAt(t *testing.T, id incident.IncidentID, minutes int, steps ...incident.Status) incident.Incident {
	t.Helper()
	inc := testIncident(t, id, steps...)
	inc.DeclaredAt = inc.DeclaredAt.Add(time.Duration(minutes) * time.Minute)
	return inc
}

// get sends one GET request to a handler built with history switched on.
func getWithHistory(live *fakeSimulator, incidents *fakeIncidentSource, arch *fakeArchive, logs *bytes.Buffer, path string) *httptest.ResponseRecorder {
	h := NewHandler(live, slog.New(slog.NewJSONHandler(logs, nil)), context.Background(), WithIncidents(incidents), WithHistory(arch))
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// idsIn pulls the ids out of a list answer, in the order they were sent.
func idsIn(t *testing.T, body []byte, key string) string {
	t.Helper()
	var decoded map[string][]struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode list answer %s: %v", body, err)
	}
	var ids []string
	for _, item := range decoded[key] {
		ids = append(ids, item.ID)
	}
	return strings.Join(ids, ",")
}

// TESTS

func TestParseLimit(t *testing.T) {
	tests := []struct {
		query   string
		want    int
		wantErr bool
	}{
		{query: "", want: 50},
		{query: "?limit=1", want: 1},
		{query: "?limit=200", want: 200},
		{query: "?limit=5000", want: 200},
		{query: "?limit=0", wantErr: true},
		{query: "?limit=-3", wantErr: true},
		{query: "?limit=many", wantErr: true},
		{query: "?limit=1.5", wantErr: true},
	}

	for _, tc := range tests {
		t.Run("query "+tc.query, func(t *testing.T) {
			got, err := parseLimit(httptest.NewRequest(http.MethodGet, "/simulations"+tc.query, nil))
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseLimit error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, errBadRequest) {
				t.Errorf("error %v does not wrap errBadRequest", err)
			}
			if got != tc.want {
				t.Errorf("parseLimit = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestListSimulationsWithHistory(t *testing.T) {
	live := &fakeSimulator{sims: []chaos.Simulation{
		simulationAt("sim_running", chaos.StateRunning, 30),
		simulationAt("sim_both", chaos.StateHalted, 20),
	}}
	// The database still says sim_both is running, and the live copy must win.
	arch := &fakeArchive{sims: []chaos.Simulation{
		simulationAt("sim_both", chaos.StateRunning, 20),
		simulationAt("sim_yesterday", chaos.StateResolved, 10),
		simulationAt("sim_last_week", chaos.StateResolved, 0),
	}}
	var logs bytes.Buffer

	rec := getWithHistory(live, &fakeIncidentSource{}, arch, &logs, "/simulations")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if got, want := idsIn(t, rec.Body.Bytes(), "simulations"), "sim_running,sim_both,sim_yesterday,sim_last_week"; got != want {
		t.Errorf("ids = %s, want %s", got, want)
	}
	if strings.Count(rec.Body.String(), `"id":"sim_both"`) != 1 || !strings.Contains(rec.Body.String(), `"id":"sim_both","service":"payments-api","mode":"latency_spike","severity":"major","intensity":0.9,"duration":"2m0s","state":"halted"`) {
		t.Errorf("body %s must hold sim_both once, in its live halted state", rec.Body)
	}
	if arch.gotLimit != maxListLimit {
		t.Errorf("asked the database for %d simulations, want %d", arch.gotLimit, maxListLimit)
	}

	limited := getWithHistory(live, &fakeIncidentSource{}, arch, &logs, "/simulations?limit=2")
	if got, want := idsIn(t, limited.Body.Bytes(), "simulations"), "sim_running,sim_both"; got != want {
		t.Errorf("ids with limit 2 = %s, want %s", got, want)
	}

	bad := getWithHistory(live, &fakeIncidentSource{}, arch, &logs, "/simulations?limit=lots")
	if bad.Code != http.StatusBadRequest {
		t.Errorf("status for a bad limit = %d, want 400", bad.Code)
	}
}

func TestListSimulationsDefaultLimit(t *testing.T) {
	live := &fakeSimulator{}
	for i := 0; i < 60; i++ {
		live.sims = append(live.sims, simulationAt(chaos.SimulationID(fmt.Sprintf("sim_%02d", i)), chaos.StateResolved, -i))
	}
	h := NewHandler(live, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), context.Background())

	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/simulations", nil))

	if got := strings.Count(rec.Body.String(), `"id":`); got != defaultListLimit {
		t.Errorf("list holds %d simulations, want the default limit of %d", got, defaultListLimit)
	}
}

func TestListWithHistorySurvivesASickDatabase(t *testing.T) {
	live := &fakeSimulator{sims: []chaos.Simulation{simulationAt("sim_running", chaos.StateRunning, 0)}}
	incidents := &fakeIncidentSource{incidents: []incident.Incident{incidentAt(t, "inc_open", 0)}}
	arch := &fakeArchive{err: errors.New("connection refused to 10.0.0.7")}
	var logs bytes.Buffer

	sims := getWithHistory(live, incidents, arch, &logs, "/simulations")
	if sims.Code != http.StatusOK || idsIn(t, sims.Body.Bytes(), "simulations") != "sim_running" {
		t.Errorf("GET /simulations = %d %s, want 200 with the live simulation only", sims.Code, sims.Body)
	}
	incs := getWithHistory(live, incidents, arch, &logs, "/incidents")
	if incs.Code != http.StatusOK || idsIn(t, incs.Body.Bytes(), "incidents") != "inc_open" {
		t.Errorf("GET /incidents = %d %s, want 200 with the live incident only", incs.Code, incs.Body)
	}
	if !strings.Contains(logs.String(), "simulation history is missing") || !strings.Contains(logs.String(), "incident history is missing") {
		t.Errorf("logs %s do not mention the missing history", logs.String())
	}
	if strings.Contains(sims.Body.String()+incs.Body.String(), "10.0.0.7") {
		t.Error("an answer repeats the internal database error")
	}
}

func TestListIncidentsWithHistory(t *testing.T) {
	incidents := &fakeIncidentSource{incidents: []incident.Incident{
		incidentAt(t, "inc_open", 30, incident.StatusAcknowledged),
	}}
	arch := &fakeArchive{incidents: []incident.Incident{
		incidentAt(t, "inc_open", 30),
		incidentAt(t, "inc_closed", 10, incident.StatusResolved),
	}}
	var logs bytes.Buffer

	rec := getWithHistory(&fakeSimulator{}, incidents, arch, &logs, "/incidents")

	if got, want := idsIn(t, rec.Body.Bytes(), "incidents"), "inc_open,inc_closed"; got != want {
		t.Errorf("ids = %s, want %s", got, want)
	}
	if !strings.Contains(rec.Body.String(), `"id":"inc_open","simulation_id":"sim_abc","service":"payments-api","mode":"latency_spike","severity":"major","status":"acknowledged"`) {
		t.Errorf("body %s must hold inc_open in its live acknowledged state", rec.Body)
	}

	limited := getWithHistory(&fakeSimulator{}, incidents, arch, &logs, "/incidents?limit=1")
	if got := idsIn(t, limited.Body.Bytes(), "incidents"); got != "inc_open" {
		t.Errorf("ids with limit 1 = %s, want inc_open", got)
	}
}

func TestGetFallsBackToHistory(t *testing.T) {
	notInEngine := fmt.Errorf("get: %w", chaos.ErrNotFound)
	notInManager := fmt.Errorf("get: %w", incident.ErrNotFound)
	arch := &fakeArchive{
		sims:      []chaos.Simulation{simulationAt("sim_old", chaos.StateResolved, 0)},
		incidents: []incident.Incident{incidentAt(t, "inc_old", 0, incident.StatusResolved)},
	}

	tests := []struct {
		name       string
		path       string
		archiveErr error
		wantStatus int
		wantBody   string
	}{
		{name: "finished simulation comes from the database", path: "/simulations/sim_old", wantStatus: http.StatusOK, wantBody: `"state":"resolved"`},
		{name: "finished incident comes from the database", path: "/incidents/inc_old", wantStatus: http.StatusOK, wantBody: `"status":"resolved"`},
		{name: "simulation unknown everywhere", path: "/simulations/sim_never", wantStatus: http.StatusNotFound, wantBody: "simulation not found"},
		{name: "incident unknown everywhere", path: "/incidents/inc_never", wantStatus: http.StatusNotFound, wantBody: "incident not found"},
		{name: "database failure is a generic 500", path: "/incidents/inc_old", archiveErr: errors.New("connection refused to 10.0.0.7"), wantStatus: http.StatusInternalServerError, wantBody: "internal server error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := *arch
			a.err = tc.archiveErr
			var logs bytes.Buffer

			rec := getWithHistory(&fakeSimulator{getErr: notInEngine}, &fakeIncidentSource{getErr: notInManager}, &a, &logs, tc.path)

			if rec.Code != tc.wantStatus || !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("GET %s = %d %s, want %d with %q", tc.path, rec.Code, rec.Body, tc.wantStatus, tc.wantBody)
			}
			if strings.Contains(rec.Body.String(), "10.0.0.7") {
				t.Errorf("body %s repeats the internal database error", rec.Body)
			}
		})
	}

	t.Run("a live item never touches the database", func(t *testing.T) {
		a := &fakeArchive{err: errors.New("must not be called")}
		var logs bytes.Buffer
		live := &fakeSimulator{sim: simulationAt("sim_live", chaos.StateRunning, 0)}

		rec := getWithHistory(live, &fakeIncidentSource{}, a, &logs, "/simulations/sim_live")
		if rec.Code != http.StatusOK {
			t.Errorf("GET a live simulation = %d %s, want 200", rec.Code, rec.Body)
		}
	})
}
