package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"
)

// HELPERS

// testWait is the longest a test waits before it gives up.
const testWait = 5 * time.Second

// mapEnv gives back a getenv that reads from a map instead of the real environment.
func mapEnv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

// listenWatcher reads the log lines and tells the test which address the server opened.
type listenWatcher struct {
	addr chan string
}

func (w *listenWatcher) Write(p []byte) (int, error) {
	var line struct {
		Msg  string `json:"msg"`
		Addr string `json:"addr"`
	}
	if err := json.Unmarshal(p, &line); err == nil && line.Msg == "http server listening" {
		select {
		case w.addr <- line.Addr:
		default:
		}
	}
	return len(p), nil
}

// TESTS

func TestLoadConfigHostAndPort(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantHost string
		wantPort string
	}{
		{name: "defaults listen on every network", env: nil, wantHost: "", wantPort: "8080"},
		{name: "host and port come from the environment", env: map[string]string{"HOST": "127.0.0.1", "PORT": "0"}, wantHost: "127.0.0.1", wantPort: "0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadConfig(mapEnv(tc.env))
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.Host != tc.wantHost {
				t.Errorf("Host = %q, want %q", cfg.Host, tc.wantHost)
			}
			if cfg.Port != tc.wantPort {
				t.Errorf("Port = %q, want %q", cfg.Port, tc.wantPort)
			}
		})
	}
}

func TestRunServesOnLocalhostAndShutsDown(t *testing.T) {
	// HOST keeps the server on this machine only and PORT 0 asks for any free port.
	getenv := mapEnv(map[string]string{"HOST": "127.0.0.1", "PORT": "0"})
	logs := &listenWatcher{addr: make(chan string, 1)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, getenv, logs) }()

	var addr string
	select {
	case addr = <-logs.addr:
	case err := <-done:
		t.Fatalf("run returned before the server was listening: %v", err)
	case <-time.After(testWait):
		t.Fatal("timed out waiting for the server to listen")
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split listen address %q: %v", addr, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("listen host = %q, want 127.0.0.1", host)
	}

	for _, path := range []string{"/healthz", "/api/v1/simulations"} {
		res, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", path, res.StatusCode, http.StatusOK)
		}
	}

	// Cancelling the context is the same as the server getting a stop signal.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v, want nil", err)
		}
	case <-time.After(testWait):
		t.Fatal("timed out waiting for run to return")
	}
}
