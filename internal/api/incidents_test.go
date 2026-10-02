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

	"github.com/MA-V4/shadow-pager/internal/incident"
)

// FAKE

// fakeIncidentSource pretends to be the incident manager and answers with whatever the test put in its fields.
type fakeIncidentSource struct {
	inc       incident.Incident
	incidents []incident.Incident
	getErr    error
	listErr   error
	gotID     incident.IncidentID
}

func (f *fakeIncidentSource) Get(_ context.Context, id incident.IncidentID) (incident.Incident, error) {
	f.gotID = id
	return f.inc, f.getErr
}

func (f *fakeIncidentSource) List(context.Context) ([]incident.Incident, error) {
	return f.incidents, f.listErr
}

// FIXTURES

// testIncident builds an incident and walks it through the given statuses, 90 seconds apart.
func testIncident(t *testing.T, id incident.IncidentID, steps ...incident.Status) incident.Incident {
	t.Helper()
	declaredAt := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	inc := incident.New(id, testSimulation("sim_abc", "running"), []string{"latency_p99"}, declaredAt)
	inc.ChannelID = "C123"
	for i, next := range steps {
		at := declaredAt.Add(time.Duration(i+1) * 90 * time.Second)
		if err := inc.Transition(next, at, "ada", "Moved to "+string(next)); err != nil {
			t.Fatalf("move test incident to %s: %v", next, err)
		}
	}
	return inc
}

// TESTS

func TestIncidentHandlers(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		fake            fakeIncidentSource
		wantStatus      int
		wantBody        []string
		wantNotInBody   []string
		wantLogContains string
		wantID          incident.IncidentID
	}{
		{
			name:       "get a fresh incident",
			path:       "/incidents/inc_1",
			fake:       fakeIncidentSource{inc: testIncident(t, "inc_1")},
			wantStatus: http.StatusOK,
			wantBody: []string{
				`"id":"inc_1"`,
				`"simulation_id":"sim_abc"`,
				`"service":"payments-api"`,
				`"mode":"latency_spike"`,
				`"severity":"major"`,
				`"status":"declared"`,
				`"breaches":["latency_p99"]`,
				`"declared_at":"2026-01-02T15:04:05Z"`,
				`"slack_channel_id":"C123"`,
				`"timeline":[{"at":"2026-01-02T15:04:05Z","actor":"shadow-pager","text":"Incident declared"}]`,
			},
			wantNotInBody: []string{"acknowledged_at", "acknowledged_by", "mitigated_at", "resolved_at", "time_to_acknowledge", "time_to_resolve"},
			wantID:        "inc_1",
		},
		{
			name:       "get a resolved incident",
			path:       "/incidents/inc_1",
			fake:       fakeIncidentSource{inc: testIncident(t, "inc_1", incident.StatusAcknowledged, incident.StatusMitigated, incident.StatusResolved)},
			wantStatus: http.StatusOK,
			wantBody: []string{
				`"status":"resolved"`,
				`"acknowledged_at":"2026-01-02T15:05:35Z"`,
				`"acknowledged_by":"ada"`,
				`"mitigated_at":"2026-01-02T15:07:05Z"`,
				`"resolved_at":"2026-01-02T15:08:35Z"`,
				`"time_to_acknowledge":"1m30s"`,
				`"time_to_resolve":"4m30s"`,
			},
			wantID: "inc_1",
		},
		{
			name:       "get not found",
			path:       "/incidents/inc_missing",
			fake:       fakeIncidentSource{getErr: fmt.Errorf("get incident inc_missing: %w", incident.ErrNotFound)},
			wantStatus: http.StatusNotFound,
			wantBody:   []string{"incident not found"},
			wantID:     "inc_missing",
		},
		{
			name: "list newest first",
			path: "/incidents",
			fake: fakeIncidentSource{incidents: []incident.Incident{
				testIncident(t, "inc_new"),
				testIncident(t, "inc_old", incident.StatusResolved),
			}},
			wantStatus: http.StatusOK,
			wantBody:   []string{`"incidents":[{"id":"inc_new"`, `"id":"inc_old"`},
		},
		{
			name:       "list empty is an empty array",
			path:       "/incidents",
			wantStatus: http.StatusOK,
			wantBody:   []string{`"incidents":[]`},
		},
		{
			name:            "unexpected error",
			path:            "/incidents",
			fake:            fakeIncidentSource{listErr: errors.New("mutex poisoned at 0xc000123456")},
			wantStatus:      http.StatusInternalServerError,
			wantBody:        []string{"internal server error"},
			wantNotInBody:   []string{"mutex", "0xc000123456"},
			wantLogContains: "mutex poisoned at 0xc000123456",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := tc.fake
			var logs bytes.Buffer
			h := NewHandler(&fakeSimulator{}, slog.New(slog.NewJSONHandler(&logs, nil)), context.Background(), WithIncidents(&fake))

			rec := httptest.NewRecorder()
			h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			body := rec.Body.String()
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, body)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
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
			if fake.gotID != tc.wantID {
				t.Errorf("id passed to Get = %q, want %q", fake.gotID, tc.wantID)
			}

			if tc.wantStatus >= 400 {
				var got map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatalf("error body %s is not a JSON object of strings: %v", body, err)
				}
				if len(got) != 1 || got["error"] == "" {
					t.Errorf("error body = %v, want only a non-empty error field", got)
				}
			}
		})
	}
}

func TestIncidentRoutesAreOffWithoutASource(t *testing.T) {
	h := NewHandler(&fakeSimulator{}, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), context.Background())

	for _, path := range []string{"/incidents", "/incidents/inc_1"} {
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d when incidents are switched off", path, rec.Code, http.StatusNotFound)
		}
	}
}
