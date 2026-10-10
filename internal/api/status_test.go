package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
	"github.com/MA-V4/shadow-pager/internal/status"
)

// FAKE

// fakeStatusSource pretends to be the status aggregator.
type fakeStatusSource struct {
	snap status.Snapshot
	err  error
}

func (f fakeStatusSource) Snapshot(context.Context) (status.Snapshot, error) {
	return f.snap, f.err
}

// HELPERS

func getStatusRoute(src statusSource, logs *bytes.Buffer, path string) *httptest.ResponseRecorder {
	h := NewHandler(&fakeSimulator{}, slog.New(slog.NewJSONHandler(logs, nil)), context.Background(),
		WithStatus(src), WithSLO(chaos.DefaultSLO))
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TESTS

func TestGetStatus(t *testing.T) {
	at := time.Date(2026, 1, 2, 15, 10, 0, 0, time.UTC)
	sample := testSample("sim_abc", 950.12345, "latency_p99")
	sample.ErrorRate = 0.0123456

	// This incident carries everything that must stay private: a Slack channel, a card and the names of people.
	inc := testIncident(t, "inc_1", incident.StatusAcknowledged)
	inc.CardTimestamp = "1700000000.000100"
	inc.AcknowledgedBy = "<@U1SECRET>"

	src := fakeStatusSource{snap: status.Snapshot{
		Level: status.MajorOutage,
		At:    at,
		Services: []status.Service{
			{Name: "checkout", Level: status.Operational},
			{Name: "payments-api", Level: status.MajorOutage, Sample: &sample},
		},
		Incidents: []incident.Incident{inc},
	}}
	var logs bytes.Buffer

	rec := getStatusRoute(src, &logs, "/status")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	want := `{"status":"major_outage","updated_at":"2026-01-02T15:10:00Z",` +
		`"services":[` +
		`{"name":"checkout","status":"operational","metrics":null},` +
		`{"name":"payments-api","status":"major_outage","metrics":{"latency_p99_ms":950.12,"error_rate":0.0123,"cpu_percent":40,"memory_mb":512,"breaches":["latency_p99"],"sampled_at":"2026-01-02T15:04:05Z"}}` +
		`],` +
		`"incidents":[{"id":"inc_1","service":"payments-api","mode":"latency_spike","severity":"major","status":"acknowledged","breaches":["latency_p99"],` +
		`"declared_at":"2026-01-02T15:04:05Z","acknowledged_at":"2026-01-02T15:05:35Z",` +
		`"updates":[{"at":"2026-01-02T15:04:05Z","text":"Incident declared"},{"at":"2026-01-02T15:05:35Z","text":"Moved to acknowledged"}]}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body =\n%s\nwant\n%s", got, want)
	}

	for _, private := range []string{"C123", "1700000000.000100", "U1SECRET", "ada", "shadow-pager", "actor", "slack", "acknowledged_by", "simulation_id"} {
		if strings.Contains(rec.Body.String(), private) {
			t.Errorf("public status body contains %q, which must stay private", private)
		}
	}
}

func TestGetStatusWhenAllIsQuiet(t *testing.T) {
	src := fakeStatusSource{snap: status.Snapshot{Level: status.Operational, At: time.Date(2026, 1, 2, 15, 10, 0, 0, time.UTC)}}

	rec := getStatusRoute(src, &bytes.Buffer{}, "/status")

	want := `{"status":"operational","updated_at":"2026-01-02T15:10:00Z","services":[],"incidents":[]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestGetStatusError(t *testing.T) {
	var logs bytes.Buffer

	rec := getStatusRoute(fakeStatusSource{err: errors.New("mutex poisoned at 0xc000123456")}, &logs, "/status")

	if rec.Code != http.StatusInternalServerError || strings.TrimSpace(rec.Body.String()) != `{"error":"internal server error"}` {
		t.Errorf("status = %d, body = %s, want a generic 500", rec.Code, rec.Body)
	}
	if !strings.Contains(logs.String(), "mutex poisoned") {
		t.Errorf("logs %s do not hold the real error", logs.String())
	}
}

func TestGetSLO(t *testing.T) {
	rec := getStatusRoute(fakeStatusSource{}, &bytes.Buffer{}, "/slo")

	want := `{"max_latency_p99_ms":500,"max_error_rate":0.01,"max_cpu_percent":90,"max_memory_mb":2048}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("GET /slo = %d %s, want 200 %s", rec.Code, rec.Body, want)
	}
}

func TestStatusRoutesAreOffWithoutTheOptions(t *testing.T) {
	h := NewHandler(&fakeSimulator{}, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), context.Background())

	for _, path := range []string{"/status", "/slo"} {
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 when the option is not used", path, rec.Code)
		}
	}
}
