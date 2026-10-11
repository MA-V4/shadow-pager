package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// HELPERS

// strongToken is an admin token that is long enough for the server to accept.
const strongToken = "test-admin-token-with-at-least-32-characters"

// secretDBPassword is a made up database password that must never show up in an error or a log line.
const secretDBPassword = "db-P4ssw0rd-do-not-print"

// simulationAnswer is the part of a simulation answer these tests read.
type simulationAnswer struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Public bool   `json:"public"`
}

// startDrill sends one inject request and gives back the status and the simulation.
func startDrill(t *testing.T, s *server, headers map[string]string) (int, simulationAnswer, *http.Response) {
	t.Helper()
	all := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		all[k] = v
	}
	res, body := send(t, http.MethodPost, s.base+"/simulations", injectBody, all)
	var sim simulationAnswer
	if res.StatusCode == http.StatusCreated {
		if err := json.Unmarshal([]byte(body), &sim); err != nil {
			t.Fatalf("decode simulation %q: %v", body, err)
		}
	}
	return res.StatusCode, sim, res
}

// CONFIG TESTS

func TestLoadConfigAdminTokenStrength(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{name: "no token", token: ""},
		{name: "only spaces counts as no token", token: "     "},
		{name: "exactly 32 characters", token: strings.Repeat("a", 32)},
		{name: "a long token", token: strongToken},
		{name: "31 characters", token: strings.Repeat("a", 31), wantErr: true},
		{name: "a short word", token: "hunter2", wantErr: true},
		{name: "spaces around a short token do not make it longer", token: "   " + strings.Repeat("a", 20) + "            ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(mapEnv(map[string]string{"ADMIN_TOKEN": tt.token}))
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadConfig error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				return
			}
			if !errors.Is(err, errProtectionConfig) {
				t.Errorf("error %v does not wrap errProtectionConfig", err)
			}
			if !strings.Contains(err.Error(), "at least 32 characters") {
				t.Errorf("error %q does not say how long the token must be", err)
			}
			// Not even the start of the token may be repeated.
			if token := strings.TrimSpace(tt.token); strings.Contains(err.Error(), token) || strings.Contains(err.Error(), token[:4]) {
				t.Errorf("error %q repeats the token or a part of it", err)
			}
		})
	}
}

func TestRunRefusesAWeakAdminTokenWithoutPrintingIt(t *testing.T) {
	const weak = "weak-token-zz9"
	logs := newLogRecorder()

	err := run(context.Background(), mapEnv(map[string]string{"HOST": "127.0.0.1", "PORT": "0", "ADMIN_TOKEN": weak}), logs)

	if !errors.Is(err, errProtectionConfig) {
		t.Fatalf("run error = %v, want errProtectionConfig", err)
	}
	if strings.Contains(err.Error(), weak) || strings.Contains(err.Error(), "zz9") {
		t.Errorf("the startup error repeats the token: %q", err)
	}
}

func TestLoadConfigDatabaseTransport(t *testing.T) {
	const base = "postgres://app:" + secretDBPassword + "@db.example.com/shadow"

	tests := []struct {
		name    string
		env     string
		url     string
		wantErr string
	}{
		{name: "production with require", env: "production", url: base + "?sslmode=require"},
		{name: "production with verify-ca", env: "production", url: base + "?sslmode=verify-ca"},
		{name: "production with verify-full", env: "production", url: base + "?sslmode=verify-full"},
		{name: "production keyword form with require", env: "production", url: "host=db user=app password=" + secretDBPassword + " dbname=shadow sslmode=require"},
		{name: "production with no database at all", env: "production", url: ""},
		{name: "production with disable", env: "production", url: base + "?sslmode=disable", wantErr: "it is disable"},
		{name: "production with allow", env: "production", url: base + "?sslmode=allow", wantErr: "it is allow"},
		{name: "production with prefer", env: "production", url: base + "?sslmode=prefer", wantErr: "it is prefer"},
		{name: "production with no sslmode", env: "production", url: base, wantErr: "it is not set"},
		{name: "production keyword form with no sslmode", env: "production", url: "host=db user=app password=" + secretDBPassword + " dbname=shadow", wantErr: "it is not set"},
		{name: "development with disable", env: "development", url: base + "?sslmode=disable"},
		{name: "development with no sslmode", env: "development", url: base},
		{name: "no APP_ENV means development", env: "", url: base + "?sslmode=disable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(mapEnv(map[string]string{"APP_ENV": tt.env, "DATABASE_URL": tt.url}))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("loadConfig error = %v, want none", err)
				}
				return
			}
			if !errors.Is(err, errDatabaseConfig) {
				t.Fatalf("loadConfig error = %v, want errDatabaseConfig", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not say that %s", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), secretDBPassword) || strings.Contains(err.Error(), "db.example.com") {
				t.Errorf("error %q repeats a part of the database address", err)
			}
		})
	}
}

func TestLoadConfigLimits(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, err := loadConfig(mapEnv(nil))
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if cfg.PublicDrills || cfg.PublicMaxActive != 5 || cfg.StreamMaxTotal != 100 || cfg.StreamMaxPerIP != 5 || cfg.TrustedProxyHops != 0 {
			t.Errorf("defaults = public %v/%d, streams %d/%d, hops %d, want false/5, 100/5, 0",
				cfg.PublicDrills, cfg.PublicMaxActive, cfg.StreamMaxTotal, cfg.StreamMaxPerIP, cfg.TrustedProxyHops)
		}
	})

	t.Run("everything set", func(t *testing.T) {
		cfg, err := loadConfig(mapEnv(map[string]string{
			"PUBLIC_DRILLS": "true", "PUBLIC_MAX_ACTIVE": " 3 ", "STREAM_MAX_TOTAL": "40", "STREAM_MAX_PER_IP": "2", "TRUSTED_PROXY_HOPS": "1",
		}))
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if !cfg.PublicDrills || cfg.PublicMaxActive != 3 || cfg.StreamMaxTotal != 40 || cfg.StreamMaxPerIP != 2 || cfg.TrustedProxyHops != 1 {
			t.Errorf("config = %+v, want every limit as it was set", cfg)
		}
	})

	bad := map[string]map[string]string{
		"PUBLIC_DRILLS that is not a yes or no":          {"PUBLIC_DRILLS": "maybe"},
		"PUBLIC_MAX_ACTIVE of zero":                      {"PUBLIC_MAX_ACTIVE": "0"},
		"PUBLIC_MAX_ACTIVE that is not a number":         {"PUBLIC_MAX_ACTIVE": "many"},
		"STREAM_MAX_TOTAL of zero":                       {"STREAM_MAX_TOTAL": "0"},
		"STREAM_MAX_PER_IP below one":                    {"STREAM_MAX_PER_IP": "-1"},
		"TRUSTED_PROXY_HOPS below zero":                  {"TRUSTED_PROXY_HOPS": "-1"},
		"more streams per caller than in total":          {"STREAM_MAX_TOTAL": "3", "STREAM_MAX_PER_IP": "4"},
		"public drills that could take every slot":       {"PUBLIC_DRILLS": "true", "PUBLIC_MAX_ACTIVE": "10", "CHAOS_MAX_ACTIVE": "10"},
		"public drills that could take more than exists": {"PUBLIC_DRILLS": "true", "PUBLIC_MAX_ACTIVE": "5", "CHAOS_MAX_ACTIVE": "2"},
	}
	for name, env := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig(mapEnv(env)); !errors.Is(err, errLimitsConfig) {
				t.Errorf("loadConfig error = %v, want errLimitsConfig", err)
			}
		})
	}

	t.Run("a large public limit is fine while public drills are off", func(t *testing.T) {
		if _, err := loadConfig(mapEnv(map[string]string{"PUBLIC_MAX_ACTIVE": "50"})); err != nil {
			t.Errorf("loadConfig error = %v, want none", err)
		}
	})
}

// PUBLIC DRILL TESTS

func TestRunPublicDrills(t *testing.T) {
	s := startServer(t, map[string]string{
		"PUBLIC_DRILLS":     "true",
		"PUBLIC_MAX_ACTIVE": "1",
		"ADMIN_TOKEN":       strongToken,
		"RATE_LIMIT_BURST":  "50",
	})
	admin := map[string]string{"Authorization": "Bearer " + strongToken}

	res, body := send(t, http.MethodGet, s.base+"/config", "", nil)
	if res.StatusCode != http.StatusOK || strings.TrimSpace(body) != `{"public_drills":true}` {
		t.Fatalf("GET /config = %d %s, want public drills on", res.StatusCode, body)
	}

	// A visitor starts a public drill without any token.
	status, public, _ := startDrill(t, s, nil)
	if status != http.StatusCreated || !public.Public {
		t.Fatalf("visitor drill = %d, public %v, want 201 and a public drill", status, public.Public)
	}
	// The public limit is one, so the next visitor is turned away while the admin still gets in.
	if status, _, res := startDrill(t, s, nil); status != http.StatusTooManyRequests {
		t.Fatalf("second visitor drill = %d, want 429 from the public limit", status)
	} else if got := res.Header.Get("WWW-Authenticate"); got != "" {
		t.Errorf("a full public limit asks for a token (%q), want a plain 429", got)
	}
	status, adminDrill, _ := startDrill(t, s, admin)
	if status != http.StatusCreated || adminDrill.Public {
		t.Fatalf("admin drill = %d, public %v, want 201 and a normal drill", status, adminDrill.Public)
	}
	if status, _, _ := startDrill(t, s, map[string]string{"Authorization": "Bearer wrong-token"}); status != http.StatusUnauthorized {
		t.Errorf("drill with a wrong token = %d, want 401", status)
	}

	// The list shows which is which.
	_, list := send(t, http.MethodGet, s.base+"/simulations", "", nil)
	var listed struct {
		Simulations []simulationAnswer `json:"simulations"`
	}
	if err := json.Unmarshal([]byte(list), &listed); err != nil {
		t.Fatalf("decode list %q: %v", list, err)
	}
	marks := map[string]bool{}
	for _, sim := range listed.Simulations {
		marks[sim.ID] = sim.Public
	}
	if !marks[public.ID] || marks[adminDrill.ID] || len(marks) != 2 {
		t.Errorf("listed public marks = %v, want %s public and %s not", marks, public.ID, adminDrill.ID)
	}

	// A visitor cannot halt the drill of the admin, but can halt the public one.
	if res, body := send(t, http.MethodPost, s.base+"/simulations/"+adminDrill.ID+"/halt", "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("visitor halting an admin drill = %d %s, want 401", res.StatusCode, body)
	}
	if res, body := send(t, http.MethodGet, s.base+"/simulations/"+adminDrill.ID, "", nil); !strings.Contains(body, `"state":"running"`) {
		t.Errorf("the admin drill is no longer running after the refused halt (%d %s)", res.StatusCode, body)
	}
	if res, body := send(t, http.MethodPost, s.base+"/simulations/"+public.ID+"/halt", "", nil); res.StatusCode != http.StatusOK {
		t.Errorf("visitor halting a public drill = %d %s, want 200", res.StatusCode, body)
	}
	if res, body := send(t, http.MethodPost, s.base+"/simulations/"+adminDrill.ID+"/halt", "", admin); res.StatusCode != http.StatusOK {
		t.Errorf("admin halting an admin drill = %d %s, want 200", res.StatusCode, body)
	}

	// The slot of the halted public drill is free again.
	if status, again, _ := startDrill(t, s, nil); status != http.StatusCreated || !again.Public {
		t.Errorf("visitor drill after the first was halted = %d, want 201", status)
	}

	for _, m := range s.logs.messages() {
		if strings.Contains(m, strongToken) {
			t.Errorf("log line %q repeats the admin token", m)
		}
	}
}

func TestRunPublicDrillDeclaresAPublicIncident(t *testing.T) {
	s := startServer(t, map[string]string{"PUBLIC_DRILLS": "true", "ADMIN_TOKEN": strongToken})
	const loud = `{"service":"payments-api","mode":"latency_spike","severity":"critical","intensity":1,"duration":"10s"}`

	res, body := send(t, http.MethodPost, s.base+"/simulations", loud, map[string]string{"Content-Type": "application/json"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("visitor drill = %d %s, want 201", res.StatusCode, body)
	}

	// The incident shows up in the API like any other, and says that it is public.
	deadline := time.After(testWait)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		_, list := send(t, http.MethodGet, s.base+"/incidents", "", nil)
		if strings.Contains(list, `"public":true`) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("no public incident was declared, the list is %s", list)
		case <-tick.C:
		}
	}
	if res, status := send(t, http.MethodGet, s.base+"/status", "", nil); res.StatusCode != http.StatusOK || !strings.Contains(status, "payments-api") {
		t.Errorf("GET /status = %d %s, want the service of the public drill on the status page", res.StatusCode, status)
	}
}

func TestRunWithoutPublicDrillsIsUnchanged(t *testing.T) {
	s := startServer(t, map[string]string{"ADMIN_TOKEN": strongToken})

	if res, body := send(t, http.MethodGet, s.base+"/config", "", nil); strings.TrimSpace(body) != `{"public_drills":false}` {
		t.Errorf("GET /config = %d %s, want public drills off", res.StatusCode, body)
	}
	if status, _, _ := startDrill(t, s, nil); status != http.StatusUnauthorized {
		t.Fatalf("drill without the token = %d, want 401", status)
	}
	status, sim, _ := startDrill(t, s, map[string]string{"Authorization": "Bearer " + strongToken})
	if status != http.StatusCreated || sim.Public {
		t.Fatalf("drill with the token = %d, public %v, want 201 and a normal drill", status, sim.Public)
	}
	if res, _ := send(t, http.MethodPost, s.base+"/simulations/"+sim.ID+"/halt", "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("halt without the token = %d, want 401", res.StatusCode)
	}
}

// LIMIT TESTS

func TestRunTrustsForwardedAddressesOnlyAsFarAsTold(t *testing.T) {
	forwarded := func(addr string) map[string]string { return map[string]string{"X-Forwarded-For": addr} }

	t.Run("one trusted proxy", func(t *testing.T) {
		s := startServer(t, map[string]string{"TRUSTED_PROXY_HOPS": "1", "RATE_LIMIT_BURST": "1", "RATE_LIMIT_REFILL": "1h"})
		if status, _, _ := startDrill(t, s, forwarded("203.0.113.9")); status != http.StatusCreated {
			t.Fatalf("first drill of 203.0.113.9 = %d, want 201", status)
		}
		// A forged address in front does not buy a new turn, because only the last one is believed.
		if status, _, _ := startDrill(t, s, forwarded("1.2.3.4, 203.0.113.9")); status != http.StatusTooManyRequests {
			t.Errorf("second drill of 203.0.113.9 behind a forged address = %d, want 429", status)
		}
		if status, _, _ := startDrill(t, s, forwarded("198.51.100.7")); status != http.StatusCreated {
			t.Errorf("first drill of another caller = %d, want 201", status)
		}
	})

	t.Run("no trusted proxy", func(t *testing.T) {
		s := startServer(t, map[string]string{"RATE_LIMIT_BURST": "1", "RATE_LIMIT_REFILL": "1h"})
		if status, _, _ := startDrill(t, s, forwarded("203.0.113.9")); status != http.StatusCreated {
			t.Fatalf("first drill = %d, want 201", status)
		}
		// The header is only something the caller typed, so a new value does not make a new caller.
		if status, _, _ := startDrill(t, s, forwarded("198.51.100.7")); status != http.StatusTooManyRequests {
			t.Errorf("drill with another forwarded address = %d, want 429 for the same real caller", status)
		}
	})
}

func TestRunLimitsReadsButNeverTheHealthProbes(t *testing.T) {
	s := startServer(t, nil)

	// The read limit is 120 in one go, so reading far more than that must hit it.
	limited := 0
	for i := 0; i < readRateLimitBurst+30; i++ {
		res, _ := send(t, http.MethodGet, s.base+"/slo", "", nil)
		if res.StatusCode == http.StatusTooManyRequests {
			if res.Header.Get("Retry-After") == "" {
				t.Fatal("a limited read has no Retry-After")
			}
			limited++
		}
	}
	if limited == 0 {
		t.Fatalf("%d reads in a row were never limited", readRateLimitBurst+30)
	}

	// The caller is over the limit now, and the probes still answer every time.
	for _, path := range []string{"/healthz", "/readyz"} {
		for i := 0; i < 50; i++ {
			if res, body := send(t, http.MethodGet, s.root+path, "", nil); res.StatusCode != http.StatusOK {
				t.Fatalf("GET %s number %d = %d %s, want 200, because probes are never limited", path, i+1, res.StatusCode, body)
			}
		}
	}
	// A write has its own bucket, so a caller that read too much can still start a drill.
	if status, _, _ := startDrill(t, s, nil); status != http.StatusCreated {
		t.Errorf("drill while over the read limit = %d, want 201", status)
	}
}

func TestRunLimitsOpenStreams(t *testing.T) {
	s := startServer(t, map[string]string{"STREAM_MAX_PER_IP": "1", "STREAM_MAX_TOTAL": "5"})

	ctx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/stream", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	first, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open first stream: %v", err)
	}
	defer first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first stream = %d, want 200", first.StatusCode)
	}

	res, body := send(t, http.MethodGet, s.base+"/stream", "", nil)
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") != "10" {
		t.Fatalf("second stream = %d with Retry-After %q (%s), want 429 and 10", res.StatusCode, res.Header.Get("Retry-After"), body)
	}

	// After the first caller hangs up the slot comes back, which the server notices a moment later.
	hangUp()
	deadline := time.After(testWait)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		again, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(again, http.MethodGet, s.base+"/stream", nil)
		if err != nil {
			cancel()
			t.Fatalf("build stream request: %v", err)
		}
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
		}
		cancel()
		if err == nil && res.StatusCode == http.StatusOK {
			return
		}
		select {
		case <-deadline:
			t.Fatal("the stream slot was never handed back after the caller hung up")
		case <-tick.C:
		}
	}
}

// TRANSPORT TESTS

func TestRunSendsStrictTransportSecurityOnlyInProduction(t *testing.T) {
	dev := startServer(t, nil)
	if res, _ := send(t, http.MethodGet, dev.root+"/healthz", "", nil); res.Header.Get("Strict-Transport-Security") != "" {
		t.Errorf("development sends Strict-Transport-Security %q, want none", res.Header.Get("Strict-Transport-Security"))
	}

	prod := startServer(t, map[string]string{"APP_ENV": "production"})
	for _, url := range []string{prod.root + "/healthz", prod.base + "/status"} {
		res, _ := send(t, http.MethodGet, url, "", nil)
		if got, want := res.Header.Get("Strict-Transport-Security"), "max-age=31536000; includeSubDomains"; got != want {
			t.Errorf("GET %s Strict-Transport-Security = %q, want %q", url, got, want)
		}
	}
}

func TestRunLogsTheGoVersionAndNoSecrets(t *testing.T) {
	s := startServer(t, map[string]string{"ADMIN_TOKEN": strongToken, "PUBLIC_DRILLS": "true"})
	startDrill(t, s, map[string]string{"Authorization": "Bearer " + strongToken})
	startDrill(t, s, map[string]string{"Authorization": "Bearer some-wrong-token-value"})
	s.stop()

	raw := s.logs.raw()
	if !regexp.MustCompile(`"msg":"starting"[^\n]*"go_version":"go1\.\d+`).MatchString(raw) {
		t.Error("the startup line with the Go version is missing from the log")
	}
	for _, secret := range []string{strongToken, "some-wrong-token-value", "Bearer "} {
		if strings.Contains(raw, secret) {
			t.Errorf("the log holds %q, and tokens and the Authorization header must never be logged", secret)
		}
	}
}

// DEPLOY CONFIG TESTS

func TestRenderBuildsWithTheToolchainOfGoMod(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(raw)
	}
	goMod, render := read("../../go.mod"), read("../../render.yaml")

	toolchain := regexp.MustCompile(`(?m)^toolchain (go\d+\.\d+\.\d+)$`).FindStringSubmatch(goMod)
	if toolchain == nil {
		t.Fatal("go.mod has no toolchain line with a full version")
	}
	// Render cannot pin Go by itself, so render.yaml forces the toolchain, and the two must never drift apart.
	want := "value: " + toolchain[1] + "+auto"
	block := regexp.MustCompile(`(?s)key: GOTOOLCHAIN\s+value: \S+`).FindString(render)
	if !strings.Contains(block, want) {
		t.Errorf("render.yaml sets GOTOOLCHAIN to %q, want %q to match go.mod", block, want)
	}
}
