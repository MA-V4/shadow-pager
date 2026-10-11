package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/ratelimit"
)

// HELPERS

// longAdminToken is long enough for the server to accept it as an admin token.
const longAdminToken = "test-admin-token-with-at-least-32-characters"

// publicProtection is the write protection of a server that lets visitors start drills.
func publicProtection() WriteProtection {
	return WriteProtection{AdminToken: longAdminToken, PublicDrills: true}
}

// publicSimulation builds a running simulation that a visitor or the admin started.
func publicSimulation(id chaos.SimulationID, public bool) chaos.Simulation {
	sim := testSimulation(id, chaos.StateRunning)
	sim.Scenario.Public = public
	return sim
}

// send runs one request against a handler and gives back the answer.
func send(h *Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:4711"
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

// quietHandler builds a Handler that talks to the fake and logs to nowhere.
func quietHandler(fake *fakeSimulator, opts ...Option) *Handler {
	return NewHandler(fake, slog.New(slog.NewJSONHandler(io.Discard, nil)), context.Background(), opts...)
}

// PUBLIC DRILL TESTS

func TestPublicModeMarksDrillsByWhoStartedThem(t *testing.T) {
	tests := []struct {
		name          string
		protection    WriteProtection
		authorization string
		wantStatus    int
		wantPublic    bool
	}{
		{name: "visitor starts a public drill", protection: publicProtection(), wantStatus: http.StatusCreated, wantPublic: true},
		{name: "admin starts a normal drill", protection: publicProtection(), authorization: "Bearer " + longAdminToken, wantStatus: http.StatusCreated},
		{name: "a wrong token is refused, not treated as a visitor", protection: publicProtection(), authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "public mode with no admin token at all", protection: WriteProtection{PublicDrills: true}, wantStatus: http.StatusCreated, wantPublic: true},
		{name: "public mode with no admin token refuses any token", protection: WriteProtection{PublicDrills: true}, authorization: "Bearer " + longAdminToken, wantStatus: http.StatusUnauthorized},
		{name: "public mode off still needs the token", protection: WriteProtection{AdminToken: longAdminToken}, wantStatus: http.StatusUnauthorized},
		{name: "public mode off with the token is a normal drill", protection: WriteProtection{AdminToken: longAdminToken}, authorization: "Bearer " + longAdminToken, wantStatus: http.StatusCreated},
		{name: "an open server without public mode starts normal drills", protection: WriteProtection{}, wantStatus: http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSimulator{sim: testSimulation("sim_abc", chaos.StateRunning)}
			h := quietHandler(fake, WithWriteProtection(tt.protection))

			rec := send(h, http.MethodPost, "/simulations", validBody, tt.authorization)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus != http.StatusCreated {
				if reachedEngine(fake) {
					t.Error("a refused request reached the engine")
				}
				if strings.Contains(rec.Body.String(), longAdminToken) || strings.Contains(rec.Body.String(), "wrong") {
					t.Errorf("the answer repeats a token: %s", rec.Body)
				}
				return
			}
			if fake.gotScenario.Public != tt.wantPublic {
				t.Errorf("scenario public = %v, want %v", fake.gotScenario.Public, tt.wantPublic)
			}
		})
	}
}

func TestPublicHaltOnlyStopsPublicDrills(t *testing.T) {
	tests := []struct {
		name          string
		drillPublic   bool
		authorization string
		wantStatus    int
		wantHalted    bool
	}{
		{name: "visitor halts a public drill", drillPublic: true, wantStatus: http.StatusOK, wantHalted: true},
		{name: "visitor cannot halt a drill of the admin", drillPublic: false, wantStatus: http.StatusUnauthorized},
		{name: "admin halts a public drill", drillPublic: true, authorization: "Bearer " + longAdminToken, wantStatus: http.StatusOK, wantHalted: true},
		{name: "admin halts a drill of the admin", drillPublic: false, authorization: "Bearer " + longAdminToken, wantStatus: http.StatusOK, wantHalted: true},
		{name: "a wrong token halts nothing", drillPublic: true, authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &haltRecorder{fakeSimulator: fakeSimulator{sim: publicSimulation("sim_abc", tt.drillPublic)}}
			h := NewHandler(fake, slog.New(slog.NewJSONHandler(io.Discard, nil)), context.Background(), WithWriteProtection(publicProtection()))

			rec := send(h, http.MethodPost, "/simulations/sim_abc/halt", "", tt.authorization)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if fake.halted != tt.wantHalted {
				t.Errorf("engine halt called = %v, want %v", fake.halted, tt.wantHalted)
			}
			if tt.wantStatus == http.StatusUnauthorized && !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want it to ask for a bearer token", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

// haltRecorder is a fake that remembers if Halt was really called, apart from the Get that comes before it.
type haltRecorder struct {
	fakeSimulator
	halted bool
}

func (f *haltRecorder) Halt(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	f.halted = true
	return f.fakeSimulator.Halt(ctx, id)
}

func TestPublicHaltOfAnUnknownDrillIsNotFound(t *testing.T) {
	fake := &haltRecorder{fakeSimulator: fakeSimulator{getErr: chaos.ErrNotFound}}
	h := NewHandler(fake, slog.New(slog.NewJSONHandler(io.Discard, nil)), context.Background(), WithWriteProtection(publicProtection()))

	rec := send(h, http.MethodPost, "/simulations/sim_missing/halt", "", "")

	if rec.Code != http.StatusNotFound || fake.halted {
		t.Fatalf("status = %d and halt called = %v, want 404 and no halt", rec.Code, fake.halted)
	}
}

func TestPublicCapacityIsATooManyRequestsAnswer(t *testing.T) {
	fake := &fakeSimulator{injectErr: chaos.ErrPublicCapacity}
	h := quietHandler(fake, WithWriteProtection(publicProtection()))

	rec := send(h, http.MethodPost, "/simulations", validBody, "")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "public drill limit reached") {
		t.Errorf("body = %s, want it to name the public drill limit", rec.Body)
	}
}

func TestRateLimitsApplyInBothModes(t *testing.T) {
	modes := map[string]WriteProtection{
		"public mode on":  {AdminToken: longAdminToken, PublicDrills: true},
		"public mode off": {AdminToken: longAdminToken},
	}

	for name, base := range modes {
		t.Run(name, func(t *testing.T) {
			p := base
			p.PerClient = ratelimit.New(2, time.Hour)
			p.Global = ratelimit.New(100, time.Hour)
			fake := &fakeSimulator{sim: testSimulation("sim_abc", chaos.StateRunning)}
			h := quietHandler(fake, WithWriteProtection(p))
			auth := "Bearer " + longAdminToken

			for i := 0; i < 2; i++ {
				if rec := send(h, http.MethodPost, "/simulations", validBody, auth); rec.Code != http.StatusCreated {
					t.Fatalf("write %d = %d, want 201 (body %s)", i+1, rec.Code, rec.Body)
				}
			}
			// The third write is over the caller's limit, with the admin token and without it.
			for _, authorization := range []string{auth, ""} {
				rec := send(h, http.MethodPost, "/simulations", validBody, authorization)
				if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
					t.Errorf("write over the limit with authorization %t = %d, want 429 with Retry-After", authorization != "", rec.Code)
				}
			}
		})
	}
}

func TestSharedLimitAppliesToVisitors(t *testing.T) {
	p := publicProtection()
	p.Global = ratelimit.New(1, time.Hour)
	fake := &fakeSimulator{sim: testSimulation("sim_abc", chaos.StateRunning)}
	h := quietHandler(fake, WithWriteProtection(p))

	if rec := send(h, http.MethodPost, "/simulations", validBody, ""); rec.Code != http.StatusCreated {
		t.Fatalf("first public write = %d, want 201", rec.Code)
	}
	if rec := send(h, http.MethodPost, "/simulations", validBody, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second public write = %d, want 429 from the shared limit", rec.Code)
	}
}

// DTO TESTS

func TestSimulationAndIncidentAnswersSayIfTheyArePublic(t *testing.T) {
	for _, public := range []bool{true, false} {
		fake := &fakeSimulator{sim: publicSimulation("sim_abc", public)}
		rec := send(quietHandler(fake), http.MethodGet, "/simulations/sim_abc", "", "")

		var got struct {
			Public *bool `json:"public"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode %s: %v", rec.Body, err)
		}
		// The field is always sent, so the web app never has to guess what a missing one means.
		if got.Public == nil || *got.Public != public {
			t.Errorf("simulation public = %v in %s, want %v", got.Public, rec.Body, public)
		}

		inc := testIncident(t, "inc_abc")
		inc.Public = public
		body, err := json.Marshal(newIncidentResponse(inc))
		if err != nil {
			t.Fatalf("encode incident: %v", err)
		}
		if want := `"public":` + map[bool]string{true: "true", false: "false"}[public]; !bytes.Contains(body, []byte(want)) {
			t.Errorf("incident answer %s does not hold %s", body, want)
		}
	}
}

func TestConfigSaysIfPublicDrillsAreOn(t *testing.T) {
	tests := map[string]struct {
		opts []Option
		want string
	}{
		"public mode on":      {opts: []Option{WithWriteProtection(publicProtection())}, want: `{"public_drills":true}`},
		"public mode off":     {opts: []Option{WithWriteProtection(WriteProtection{AdminToken: longAdminToken})}, want: `{"public_drills":false}`},
		"no write protection": {want: `{"public_drills":false}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rec := send(quietHandler(&fakeSimulator{}, tt.opts...), http.MethodGet, "/config", "", "")
			if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != tt.want {
				t.Errorf("GET /config = %d %s, want 200 %s", rec.Code, rec.Body, tt.want)
			}
			// The answer must never say anything about the token itself.
			if strings.Contains(rec.Body.String(), "token") {
				t.Errorf("GET /config mentions a token: %s", rec.Body)
			}
		})
	}
}

// READ LIMIT TESTS

func TestReadLimitIsSeparateFromTheWriteLimit(t *testing.T) {
	reads := ratelimit.New(3, time.Hour)
	writes := ratelimit.New(1, time.Hour)
	fake := &fakeSimulator{sim: testSimulation("sim_abc", chaos.StateRunning)}
	h := quietHandler(fake, WithWriteProtection(WriteProtection{PerClient: writes}), WithReadLimit(reads))

	// A write uses a write turn and no read turn.
	if rec := send(h, http.MethodPost, "/simulations", validBody, ""); rec.Code != http.StatusCreated {
		t.Fatalf("write = %d, want 201", rec.Code)
	}
	if rec := send(h, http.MethodPost, "/simulations", validBody, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second write = %d, want 429 from the write limit", rec.Code)
	}

	// All three read turns are still there, on every kind of read route.
	for _, path := range []string{"/simulations", "/simulations/sim_abc", "/config"} {
		if rec := send(h, http.MethodGet, path, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (body %s)", path, rec.Code, rec.Body)
		}
	}
	rec := send(h, http.MethodGet, "/simulations", "", "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("fourth read = %d, want 429 with Retry-After", rec.Code)
	}
	// A route that does not exist costs a read turn too, so guessing addresses is not free.
	if rec := send(h, http.MethodGet, "/nowhere", "", ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("GET /nowhere over the limit = %d, want 429", rec.Code)
	}
}

func TestReadLimitIsPerCaller(t *testing.T) {
	h := quietHandler(&fakeSimulator{}, WithReadLimit(ratelimit.New(1, time.Hour)))
	get := func(remoteAddr string) int {
		req := httptest.NewRequest(http.MethodGet, "/simulations", nil)
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, req)
		return rec.Code
	}

	if got := get("203.0.113.9:1"); got != http.StatusOK {
		t.Fatalf("first read = %d, want 200", got)
	}
	if got := get("203.0.113.9:2"); got != http.StatusTooManyRequests {
		t.Fatalf("second read of the same caller = %d, want 429", got)
	}
	if got := get("198.51.100.7:1"); got != http.StatusOK {
		t.Fatalf("read of another caller = %d, want 200", got)
	}
}

func TestReadsAreUnlimitedWithoutTheOption(t *testing.T) {
	h := quietHandler(&fakeSimulator{})
	for i := 0; i < 500; i++ {
		if rec := send(h, http.MethodGet, "/simulations", "", ""); rec.Code != http.StatusOK {
			t.Fatalf("read %d = %d, want 200", i+1, rec.Code)
		}
	}
}
