package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// HELPERS

// logLine is the part of one JSON log line that the tests look at.
type logLine struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Addr  string `json:"addr"`
}

// logRecorder keeps every log line and also passes each one along on a channel.
type logRecorder struct {
	mu    sync.Mutex
	all   []logLine
	lines chan logLine
}

func newLogRecorder() *logRecorder {
	return &logRecorder{lines: make(chan logLine, 1024)}
}

func (r *logRecorder) Write(p []byte) (int, error) {
	var line logLine
	if err := json.Unmarshal(p, &line); err == nil {
		r.mu.Lock()
		r.all = append(r.all, line)
		r.mu.Unlock()
		select {
		case r.lines <- line:
		default:
		}
	}
	return len(p), nil
}

// waitFor reads log lines until one has the wanted message, and fails the test if none comes.
func (r *logRecorder) waitFor(t *testing.T, msg string) logLine {
	t.Helper()
	timeout := time.After(testWait)
	for {
		select {
		case line := <-r.lines:
			if line.Msg == msg {
				return line
			}
		case <-timeout:
			t.Fatalf("timed out waiting for the log line %q", msg)
		}
	}
}

// messages gives back every log message written so far, in order.
func (r *logRecorder) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.all))
	for _, line := range r.all {
		out = append(out, line.Msg)
	}
	return out
}

// indexOf finds the place of a message in a list, or gives back minus one.
func indexOf(messages []string, msg string) int {
	for i, m := range messages {
		if m == msg {
			return i
		}
	}
	return -1
}

// call sends one HTTP request and gives back the status and the body.
func call(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, url, err)
	}
	return res.StatusCode, string(raw)
}

// CONFIG TESTS

func TestLoadConfigSlack(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantErr     bool
		wantEnabled bool
		wantInvite  []string
		wantChannel string
	}{
		{name: "no tokens means slack is off", env: nil},
		{
			name:        "both tokens switch slack on",
			env:         map[string]string{"SLACK_APP_TOKEN": "xapp-1", "SLACK_BOT_TOKEN": "xoxb-1"},
			wantEnabled: true,
		},
		{
			name: "optional settings are trimmed and split",
			env: map[string]string{
				"SLACK_APP_TOKEN":           " xapp-1 ",
				"SLACK_BOT_TOKEN":           "xoxb-1",
				"SLACK_INVITE_USER_IDS":     "U1, U2,,U3 ",
				"SLACK_ANNOUNCE_CHANNEL_ID": " C9 ",
			},
			wantEnabled: true,
			wantInvite:  []string{"U1", "U2", "U3"},
			wantChannel: "C9",
		},
		{name: "app token alone is an error", env: map[string]string{"SLACK_APP_TOKEN": "xapp-1"}, wantErr: true},
		{name: "bot token alone is an error", env: map[string]string{"SLACK_BOT_TOKEN": "xoxb-1"}, wantErr: true},
		{name: "app token with the wrong start", env: map[string]string{"SLACK_APP_TOKEN": "xoxb-1", "SLACK_BOT_TOKEN": "xoxb-1"}, wantErr: true},
		{name: "bot token with the wrong start", env: map[string]string{"SLACK_APP_TOKEN": "xapp-1", "SLACK_BOT_TOKEN": "xapp-1"}, wantErr: true},
		{name: "optional settings without tokens are ignored", env: map[string]string{"SLACK_INVITE_USER_IDS": "U1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadConfig(mapEnv(tc.env))

			if (err != nil) != tc.wantErr {
				t.Fatalf("loadConfig error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				if !errors.Is(err, errSlackConfig) {
					t.Errorf("error %v does not wrap errSlackConfig", err)
				}
				// A config error must never repeat a token.
				for _, secret := range tc.env {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("error %q repeats the secret value %q", err, secret)
					}
				}
				return
			}
			if cfg.slackEnabled() != tc.wantEnabled {
				t.Errorf("slackEnabled = %v, want %v", cfg.slackEnabled(), tc.wantEnabled)
			}
			if strings.Join(cfg.SlackInviteUserIDs, ",") != strings.Join(tc.wantInvite, ",") {
				t.Errorf("SlackInviteUserIDs = %v, want %v", cfg.SlackInviteUserIDs, tc.wantInvite)
			}
			if cfg.SlackAnnounceChannelID != tc.wantChannel {
				t.Errorf("SlackAnnounceChannelID = %q, want %q", cfg.SlackAnnounceChannelID, tc.wantChannel)
			}
		})
	}
}

// END TO END TEST

func TestRunDeclaresIncidentsWithoutSlack(t *testing.T) {
	getenv := mapEnv(map[string]string{"HOST": "127.0.0.1", "PORT": "0", "CHAOS_TICK_INTERVAL": "20ms"})
	logs := newLogRecorder()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, getenv, logs) }()

	base := "http://" + logs.waitFor(t, "http server listening").Addr + "/api/v1"

	if status, body := call(t, http.MethodGet, base+"/incidents", ""); status != http.StatusOK || !strings.Contains(body, `"incidents":[]`) {
		t.Fatalf("GET /incidents before any failure = %d %s, want 200 with an empty list", status, body)
	}

	// A full strength latency spike breaks its limit within about half a second.
	scenario := `{"service":"payments-api","mode":"latency_spike","severity":"critical","intensity":1,"duration":"10s"}`
	status, body := call(t, http.MethodPost, base+"/simulations", scenario)
	if status != http.StatusCreated {
		t.Fatalf("POST /simulations = %d %s, want 201", status, body)
	}
	var sim struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &sim); err != nil {
		t.Fatalf("decode simulation: %v", err)
	}

	logs.waitFor(t, "incident declared")
	status, body = call(t, http.MethodGet, base+"/incidents", "")
	for _, want := range []string{`"status":"declared"`, `"simulation_id":"` + sim.ID + `"`, `"severity":"critical"`, `"service":"payments-api"`} {
		if status != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("GET /incidents after the breach = %d %s, want it to contain %s", status, body, want)
		}
	}

	if status, body := call(t, http.MethodPost, base+"/simulations/"+sim.ID+"/halt", ""); status != http.StatusOK {
		t.Fatalf("POST halt = %d %s, want 200", status, body)
	}
	logs.waitFor(t, "incident mitigated because the simulation ended")
	if _, body := call(t, http.MethodGet, base+"/incidents", ""); !strings.Contains(body, `"status":"mitigated"`) {
		t.Errorf("GET /incidents after the halt = %s, want a mitigated incident", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v, want nil", err)
		}
	case <-time.After(testWait):
		t.Fatal("timed out waiting for run to return")
	}

	messages := logs.messages()
	const disabled = "slack is disabled because SLACK_APP_TOKEN and SLACK_BOT_TOKEN are not set"
	count := 0
	for _, m := range messages {
		if m == disabled {
			count++
		}
	}
	if count != 1 {
		t.Errorf("slack disabled warning was logged %d times, want exactly 1", count)
	}

	// The shutdown order is HTTP drain, then engine, then incident manager.
	order := []string{"draining http server", "engine stopped", "incident manager stopped", "shutdown complete"}
	last := -1
	for _, msg := range order {
		i := indexOf(messages, msg)
		if i <= last {
			t.Errorf("log line %q is at position %d, want it after position %d (messages: %v)", msg, i, last, order)
		}
		last = i
	}
}

func TestRunRejectsHalfASlackConfig(t *testing.T) {
	getenv := mapEnv(map[string]string{"HOST": "127.0.0.1", "PORT": "0", "SLACK_BOT_TOKEN": "xoxb-only"})

	err := run(context.Background(), getenv, io.Discard)
	if !errors.Is(err, errSlackConfig) {
		t.Fatalf("run error = %v, want one that wraps errSlackConfig", err)
	}
}
