package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// HELPERS

// server is one running copy of the program that a test can talk to and stop.
type server struct {
	t      *testing.T
	base   string
	root   string
	logs   *logRecorder
	cancel context.CancelFunc
	done   chan error
}

// startServer runs the program with the given settings and waits until it listens.
func startServer(t *testing.T, env map[string]string) *server {
	t.Helper()
	full := map[string]string{"HOST": "127.0.0.1", "PORT": "0", "CHAOS_TICK_INTERVAL": "20ms"}
	for k, v := range env {
		full[k] = v
	}
	logs := newLogRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	s := &server{t: t, logs: logs, cancel: cancel, done: make(chan error, 1)}
	go func() { s.done <- run(ctx, mapEnv(full), logs) }()
	t.Cleanup(cancel)

	select {
	case err := <-s.done:
		t.Fatalf("run returned before the server was listening: %v", err)
	case line := <-waitForLine(logs, "http server listening"):
		s.root = "http://" + line.Addr
		s.base = s.root + "/api/v1"
	case <-time.After(4 * testWait):
		t.Fatal("timed out waiting for the server to listen")
	}
	return s
}

// waitForLine gives back a channel that gets the first log line with the wanted message.
func waitForLine(logs *logRecorder, msg string) <-chan logLine {
	out := make(chan logLine, 1)
	go func() {
		for line := range logs.lines {
			if line.Msg == msg {
				out <- line
				return
			}
		}
	}()
	return out
}

// stop asks the program to shut down and waits until run has returned.
func (s *server) stop() {
	s.t.Helper()
	s.cancel()
	select {
	case err := <-s.done:
		if err != nil {
			s.t.Errorf("run returned %v, want nil", err)
		}
	case <-time.After(4 * testWait):
		s.t.Fatal("timed out waiting for run to return")
	}
}

// testDatabaseURL makes an empty schema for this test and gives back a URL that points into it.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set, so the Postgres integration tests are skipped")
	}
	ctx := context.Background()

	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("make schema name: %v", err)
	}
	schema := "test_" + hex.EncodeToString(b[:])

	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to TEST_DATABASE_URL: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
		conn.Close(ctx)
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// simulationJSON is the part of a simulation answer that the tests look at.
type simulationJSON struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"started_at"`
}

// incidentJSON is the part of an incident answer that the tests look at.
type incidentJSON struct {
	ID           string `json:"id"`
	SimulationID string `json:"simulation_id"`
	Status       string `json:"status"`
	Timeline     []struct {
		Text string `json:"text"`
	} `json:"timeline"`
}

func (s *server) simulations() []simulationJSON {
	s.t.Helper()
	status, body := call(s.t, http.MethodGet, s.base+"/simulations", "")
	if status != http.StatusOK {
		s.t.Fatalf("GET /simulations = %d %s, want 200", status, body)
	}
	var out struct {
		Simulations []simulationJSON `json:"simulations"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		s.t.Fatalf("decode simulations: %v", err)
	}
	return out.Simulations
}

func (s *server) incidents() []incidentJSON {
	s.t.Helper()
	status, body := call(s.t, http.MethodGet, s.base+"/incidents", "")
	if status != http.StatusOK {
		s.t.Fatalf("GET /incidents = %d %s, want 200", status, body)
	}
	var out struct {
		Incidents []incidentJSON `json:"incidents"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		s.t.Fatalf("decode incidents: %v", err)
	}
	return out.Incidents
}

// TESTS

func TestLoadConfigDatabaseURL(t *testing.T) {
	cfg, err := loadConfig(mapEnv(nil))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty when DATABASE_URL is not set", cfg.DatabaseURL)
	}

	cfg, err = loadConfig(mapEnv(map[string]string{"DATABASE_URL": " postgres://u@h/db "}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DatabaseURL != "postgres://u@h/db" {
		t.Errorf("DatabaseURL = %q, want the trimmed value", cfg.DatabaseURL)
	}
}

func TestRunWithoutDatabase(t *testing.T) {
	s := startServer(t, nil)

	status, body := call(t, http.MethodGet, s.root+"/readyz", "")
	if status != http.StatusOK || strings.Contains(body, "database") {
		t.Errorf("GET /readyz = %d %s, want 200 with no database check", status, body)
	}
	s.stop()

	const disabled = "storage is disabled because DATABASE_URL is not set, so nothing survives a restart"
	count := 0
	for _, m := range s.logs.messages() {
		if m == disabled {
			count++
		}
	}
	if count != 1 {
		t.Errorf("storage disabled warning was logged %d times, want exactly 1", count)
	}
}

func TestRunSurvivesRestartsWithDatabase(t *testing.T) {
	env := map[string]string{"DATABASE_URL": testDatabaseURL(t)}

	// FIRST RUN: start a simulation and let it open an incident.
	first := startServer(t, env)

	if status, body := call(t, http.MethodGet, first.root+"/readyz", ""); status != http.StatusOK || !strings.Contains(body, `"database":"ok"`) {
		t.Fatalf("GET /readyz = %d %s, want 200 with the database check ok", status, body)
	}
	if status, body := call(t, http.MethodGet, first.root+"/healthz", ""); status != http.StatusOK || strings.Contains(body, "database") {
		t.Errorf("GET /healthz = %d %s, want 200 and no word about the database", status, body)
	}

	scenario := `{"service":"payments-api","mode":"latency_spike","severity":"critical","intensity":1,"duration":"1m"}`
	status, body := call(t, http.MethodPost, first.base+"/simulations", scenario)
	if status != http.StatusCreated {
		t.Fatalf("POST /simulations = %d %s, want 201", status, body)
	}
	var sim simulationJSON
	if err := json.Unmarshal([]byte(body), &sim); err != nil {
		t.Fatalf("decode simulation: %v", err)
	}
	first.logs.waitFor(t, "incident declared")
	before := first.incidents()
	if len(before) != 1 || before[0].SimulationID != sim.ID {
		t.Fatalf("incidents before the restart = %+v, want one for %s", before, sim.ID)
	}
	first.stop()

	// SECOND RUN: the simulation keeps running from its old start, and the incident is still open.
	second := startServer(t, env)

	sims := second.simulations()
	if len(sims) != 1 || sims[0].ID != sim.ID || sims[0].State != "running" {
		t.Fatalf("simulations after the restart = %+v, want %s still running", sims, sim.ID)
	}
	// The database keeps microseconds, so that is how closely the start times must match.
	if got, want := sims[0].StartedAt.Truncate(time.Microsecond), sim.StartedAt.Truncate(time.Microsecond); !got.Equal(want) {
		t.Errorf("start time after the restart = %s, want the original %s", got, want)
	}
	after := second.incidents()
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].Status != "declared" {
		t.Fatalf("incidents after the restart = %+v, want %s still declared", after, before[0].ID)
	}

	if status, body := call(t, http.MethodPost, second.base+"/simulations/"+sim.ID+"/halt", ""); status != http.StatusOK {
		t.Fatalf("POST halt after the restart = %d %s, want 200", status, body)
	}
	second.logs.waitFor(t, "incident mitigated because the simulation ended")
	mitigated := second.incidents()
	if len(mitigated) != 1 || mitigated[0].Status != "mitigated" {
		t.Fatalf("incidents after the halt = %+v, want one mitigated", mitigated)
	}
	second.stop()

	// THIRD RUN: the halt and the mitigation were saved, so nothing is running and nothing is mitigated twice.
	third := startServer(t, env)

	if sims := third.simulations(); len(sims) != 1 || sims[0].ID != sim.ID || sims[0].State != "halted" {
		t.Errorf("simulations after the second restart = %+v, want %s listed from history as halted", sims, sim.ID)
	}
	if status, body := call(t, http.MethodGet, third.base+"/simulations/"+sim.ID, ""); status != http.StatusOK || !strings.Contains(body, `"state":"halted"`) {
		t.Errorf("GET the finished simulation after the second restart = %d %s, want it found in history", status, body)
	}
	final := third.incidents()
	if len(final) != 1 || final[0].Status != "mitigated" {
		t.Fatalf("incidents after the second restart = %+v, want the same one still mitigated", final)
	}
	if len(final[0].Timeline) != len(mitigated[0].Timeline) {
		t.Errorf("timeline has %d entries after the second restart, want the same %d as before", len(final[0].Timeline), len(mitigated[0].Timeline))
	}
	third.stop()

	// The pool must be the last thing to go.
	messages := third.logs.messages()
	order := []string{"draining http server", "engine stopped", "incident manager stopped", "recorder stopped", "shutdown complete"}
	last := -1
	for _, msg := range order {
		i := indexOf(messages, msg)
		if i <= last {
			t.Errorf("log line %q is at position %d, want it after position %d (wanted order: %v)", msg, i, last, order)
		}
		last = i
	}
}
