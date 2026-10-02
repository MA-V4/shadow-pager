package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// FAKES

// fakeIncidents pretends to be the incident manager and writes down every call.
type fakeIncidents struct {
	err   error
	calls []string
}

func (f *fakeIncidents) record(verb string, id incident.IncidentID, actor string) (incident.Incident, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s %s by %s", verb, id, actor))
	return incident.Incident{ID: id}, f.err
}

func (f *fakeIncidents) Acknowledge(_ context.Context, id incident.IncidentID, actor string) (incident.Incident, error) {
	return f.record("acknowledge", id, actor)
}

func (f *fakeIncidents) Mitigate(_ context.Context, id incident.IncidentID, actor string) (incident.Incident, error) {
	return f.record("mitigate", id, actor)
}

func (f *fakeIncidents) Resolve(_ context.Context, id incident.IncidentID, actor string) (incident.Incident, error) {
	return f.record("resolve", id, actor)
}

// fakeSimulations pretends to be the chaos engine for the slash command.
type fakeSimulations struct {
	injectErr error
	haltErr   error
	listErr   error
	sims      []chaos.Simulation

	injected []chaos.Scenario
	halted   []chaos.SimulationID
}

func (f *fakeSimulations) Inject(_ context.Context, sc chaos.Scenario) (chaos.Simulation, error) {
	f.injected = append(f.injected, sc)
	if f.injectErr != nil {
		return chaos.Simulation{}, f.injectErr
	}
	return chaos.Simulation{ID: "sim_new", Scenario: sc, State: chaos.StateRunning}, nil
}

func (f *fakeSimulations) List(context.Context) ([]chaos.Simulation, error) {
	return f.sims, f.listErr
}

func (f *fakeSimulations) Halt(_ context.Context, id chaos.SimulationID) (chaos.Simulation, error) {
	f.halted = append(f.halted, id)
	if f.haltErr != nil {
		return chaos.Simulation{}, f.haltErr
	}
	return chaos.Simulation{ID: id, Scenario: chaos.Scenario{Service: "payments-api"}, State: chaos.StateHalted}, nil
}

// ackRecord is one ack the fake was asked to send.
type ackRecord struct {
	envelopeID string
	payload    interface{}
}

// fakeAcker writes down every ack instead of sending it over a socket.
type fakeAcker struct {
	acks []ackRecord
}

func (f *fakeAcker) Ack(req socketmode.Request, payload ...interface{}) {
	rec := ackRecord{envelopeID: req.EnvelopeID}
	if len(payload) > 0 {
		rec.payload = payload[0]
	}
	f.acks = append(f.acks, rec)
}

// HARNESS

// testListener holds a Listener together with all of its fakes.
type testListener struct {
	*Listener
	incidents *fakeIncidents
	sims      *fakeSimulations
	acker     *fakeAcker
	messenger *fakeMessenger
}

func newTestListener() *testListener {
	tl := &testListener{
		incidents: &fakeIncidents{},
		sims:      &fakeSimulations{},
		acker:     &fakeAcker{},
		messenger: &fakeMessenger{},
	}
	tl.Listener = &Listener{
		ack:       tl.acker,
		api:       tl.messenger,
		incidents: tl.incidents,
		sims:      tl.sims,
		log:       discardLogger(),
	}
	return tl
}

// click builds the callback Slack sends when a user presses a card button.
func click(actionID, incidentID string) slackapi.InteractionCallback {
	cb := slackapi.InteractionCallback{Type: slackapi.InteractionTypeBlockActions}
	cb.User.ID = "U1"
	cb.Channel.ID = "C1"
	cb.ActionCallback.BlockActions = []*slackapi.BlockAction{{ActionID: actionID, BlockID: actionsBlockID, Value: incidentID}}
	return cb
}

func slash(text string) slackapi.SlashCommand {
	return slackapi.SlashCommand{Command: "/shadowpager", Text: text, UserID: "U1", ChannelID: "C1"}
}

// BUTTON TESTS

func TestRouteActionCallsTheManager(t *testing.T) {
	tests := []struct {
		actionID string
		want     string
	}{
		{actionID: actionAcknowledge, want: "acknowledge inc_1 by <@U1>"},
		{actionID: actionMitigate, want: "mitigate inc_1 by <@U1>"},
		{actionID: actionResolve, want: "resolve inc_1 by <@U1>"},
	}

	for _, tc := range tests {
		t.Run(tc.actionID, func(t *testing.T) {
			tl := newTestListener()

			reply := tl.routeAction(context.Background(), click(tc.actionID, "inc_1"))

			if reply != "" {
				t.Errorf("reply = %q, want none for a click that worked", reply)
			}
			if len(tl.incidents.calls) != 1 || tl.incidents.calls[0] != tc.want {
				t.Errorf("manager calls = %v, want [%s]", tl.incidents.calls, tc.want)
			}
		})
	}
}

func TestRouteActionExplainsFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "invalid transition",
			err:  fmt.Errorf("move: %w", incident.ErrInvalidTransition),
			want: ":warning: I could not acknowledge this incident because it has already moved past that step.",
		},
		{
			name: "unknown incident",
			err:  fmt.Errorf("move: %w", incident.ErrNotFound),
			want: ":warning: I do not know this incident any more, most likely because the server restarted.",
		},
		{
			name: "anything else hides the details",
			err:  errors.New("secret internal detail"),
			want: ":warning: Something went wrong and I could not acknowledge this incident.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestListener()
			tl.incidents.err = tc.err

			if got := tl.routeAction(context.Background(), click(actionAcknowledge, "inc_1")); got != tc.want {
				t.Errorf("reply = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRouteActionIgnoresWhatItDoesNotKnow(t *testing.T) {
	tl := newTestListener()
	ctx := context.Background()

	if got := tl.routeAction(ctx, click("some_other_button", "inc_1")); got != "" {
		t.Errorf("reply to an unknown button = %q, want none", got)
	}
	other := click(actionResolve, "inc_1")
	other.Type = slackapi.InteractionTypeViewSubmission
	if got := tl.routeAction(ctx, other); got != "" {
		t.Errorf("reply to another interaction type = %q, want none", got)
	}
	if len(tl.incidents.calls) != 0 {
		t.Errorf("manager calls = %v, want none", tl.incidents.calls)
	}
}

// SLASH COMMAND TESTS

func TestRouteCommandInject(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		injectErr error
		want      chaos.Scenario
		wantCall  bool
		wantReply string
	}{
		{
			name:      "defaults",
			text:      "inject latency_spike payments-api",
			want:      chaos.Scenario{Service: "payments-api", Mode: chaos.FailureLatencySpike, Severity: chaos.SeverityMajor, Intensity: 0.8, Duration: 2 * time.Minute},
			wantCall:  true,
			wantReply: "Injected `latency_spike` on `payments-api` with intensity 0.8 for 2m0s as simulation `sim_new`",
		},
		{
			name:      "intensity and duration",
			text:      "inject  CPU_SATURATION checkout 0.95 90s",
			want:      chaos.Scenario{Service: "checkout", Mode: chaos.FailureCPUSaturation, Severity: chaos.SeverityCritical, Intensity: 0.95, Duration: 90 * time.Second},
			wantCall:  true,
			wantReply: "intensity 0.95 for 1m30s",
		},
		{
			name:      "low intensity is minor",
			text:      "inject memory_leak search 0.2",
			want:      chaos.Scenario{Service: "search", Mode: chaos.FailureMemoryLeak, Severity: chaos.SeverityMinor, Intensity: 0.2, Duration: 2 * time.Minute},
			wantCall:  true,
			wantReply: "Injected `memory_leak`",
		},
		{
			name:      "engine validation error reaches the user",
			text:      "inject meteor_strike payments-api",
			injectErr: fmt.Errorf("inject: %w: unknown failure mode %q", chaos.ErrInvalidScenario, "meteor_strike"),
			want:      chaos.Scenario{Service: "payments-api", Mode: "meteor_strike", Severity: chaos.SeverityMajor, Intensity: 0.8, Duration: 2 * time.Minute},
			wantCall:  true,
			wantReply: `I could not inject that failure: inject: invalid scenario: unknown failure mode "meteor_strike"`,
		},
		{name: "missing service", text: "inject latency_spike", wantReply: "Usage: `/shadowpager inject"},
		{name: "too many words", text: "inject latency_spike a 0.5 2m extra", wantReply: "Usage: `/shadowpager inject"},
		{name: "bad intensity", text: "inject latency_spike payments-api lots", wantReply: "I could not read the intensity `lots`"},
		{name: "bad duration", text: "inject latency_spike payments-api 0.5 soon", wantReply: "I could not read the duration `soon`"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestListener()
			tl.sims.injectErr = tc.injectErr

			reply := tl.routeCommand(context.Background(), slash(tc.text))

			if !strings.Contains(reply, tc.wantReply) {
				t.Errorf("reply = %q, want it to contain %q", reply, tc.wantReply)
			}
			if !tc.wantCall {
				if len(tl.sims.injected) != 0 {
					t.Errorf("engine Inject was called with %+v, want no call", tl.sims.injected)
				}
				return
			}
			if len(tl.sims.injected) != 1 || tl.sims.injected[0] != tc.want {
				t.Errorf("scenario passed to Inject = %+v, want %+v", tl.sims.injected, tc.want)
			}
		})
	}
}

func TestRouteCommandListHaltAndHelp(t *testing.T) {
	started := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	running := chaos.Simulation{
		ID:        "sim_1",
		Scenario:  chaos.Scenario{Service: "payments-api", Mode: chaos.FailureLatencySpike},
		State:     chaos.StateRunning,
		StartedAt: started,
	}

	tests := []struct {
		name       string
		text       string
		sims       []chaos.Simulation
		haltErr    error
		listErr    error
		wantReply  string
		wantHalted string
	}{
		{name: "empty text shows help", text: "", wantReply: "*Shadow-Pager commands*"},
		{name: "help", text: "help", wantReply: "/shadowpager halt <simulation_id>"},
		{name: "unknown command", text: "dance", wantReply: "I do not know the command `dance`."},
		{name: "list with nothing", text: "list", wantReply: "There are no simulations yet."},
		{name: "list", text: "LIST", sims: []chaos.Simulation{running}, wantReply: "• `sim_1` running: `latency_spike` on `payments-api`, started <!date^1767366245^"},
		{name: "list error", text: "list", listErr: errors.New("engine exploded"), wantReply: "I could not list the simulations: engine exploded"},
		{name: "halt", text: "halt sim_1", wantReply: ":octagonal_sign: Halted simulation `sim_1` on `payments-api`.", wantHalted: "sim_1"},
		{name: "halt without id", text: "halt", wantReply: "Usage: `/shadowpager halt <simulation_id>`"},
		{name: "halt unknown", text: "halt sim_x", haltErr: fmt.Errorf("halt: %w", chaos.ErrNotFound), wantReply: "I could not find a simulation called `sim_x`.", wantHalted: "sim_x"},
		{name: "halt finished", text: "halt sim_1", haltErr: fmt.Errorf("halt: %w", chaos.ErrNotActive), wantReply: "Simulation `sim_1` has already finished.", wantHalted: "sim_1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestListener()
			tl.sims.sims, tl.sims.haltErr, tl.sims.listErr = tc.sims, tc.haltErr, tc.listErr

			reply := tl.routeCommand(context.Background(), slash(tc.text))

			if !strings.Contains(reply, tc.wantReply) {
				t.Errorf("reply = %q, want it to contain %q", reply, tc.wantReply)
			}
			var halted string
			if len(tl.sims.halted) > 0 {
				halted = string(tl.sims.halted[0])
			}
			if halted != tc.wantHalted {
				t.Errorf("engine Halt called with %q, want %q", halted, tc.wantHalted)
			}
		})
	}
}

func TestCommandListStopsAtTheLimit(t *testing.T) {
	tl := newTestListener()
	for i := 0; i < listLimit+3; i++ {
		tl.sims.sims = append(tl.sims.sims, chaos.Simulation{ID: chaos.SimulationID(fmt.Sprintf("sim_%02d", i))})
	}

	reply := tl.routeCommand(context.Background(), slash("list"))

	if got := strings.Count(reply, "• "); got != listLimit {
		t.Errorf("reply lists %d simulations, want %d", got, listLimit)
	}
	if !strings.HasSuffix(reply, "and 3 more") {
		t.Errorf("reply = %q, want it to end by counting the rest", reply)
	}
}

// EVENT TESTS

func TestHandleEventAcksEveryEnvelope(t *testing.T) {
	request := func(id string) *socketmode.Request { return &socketmode.Request{EnvelopeID: id} }

	tests := []struct {
		name        string
		evt         socketmode.Event
		wantAcks    int
		wantPayload bool
	}{
		{name: "button click", evt: socketmode.Event{Type: socketmode.EventTypeInteractive, Data: click(actionResolve, "inc_1"), Request: request("env_1")}, wantAcks: 1},
		{name: "slash command", evt: socketmode.Event{Type: socketmode.EventTypeSlashCommand, Data: slash("help"), Request: request("env_2")}, wantAcks: 1, wantPayload: true},
		{name: "events api", evt: socketmode.Event{Type: socketmode.EventTypeEventsAPI, Request: request("env_3")}, wantAcks: 1},
		{name: "broken button payload", evt: socketmode.Event{Type: socketmode.EventTypeInteractive, Data: "nonsense", Request: request("env_4")}, wantAcks: 1},
		{name: "broken slash payload", evt: socketmode.Event{Type: socketmode.EventTypeSlashCommand, Data: 42, Request: request("env_5")}, wantAcks: 1},
		{name: "hello has no envelope", evt: socketmode.Event{Type: socketmode.EventTypeHello, Request: &socketmode.Request{}}, wantAcks: 0},
		{name: "connected has no request", evt: socketmode.Event{Type: socketmode.EventTypeConnected}, wantAcks: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestListener()

			tl.handleEvent(context.Background(), tc.evt)

			if len(tl.acker.acks) != tc.wantAcks {
				t.Fatalf("acks = %d, want %d", len(tl.acker.acks), tc.wantAcks)
			}
			if tc.wantAcks == 0 {
				return
			}
			ack := tl.acker.acks[0]
			if ack.envelopeID != tc.evt.Request.EnvelopeID {
				t.Errorf("acked envelope %q, want %q", ack.envelopeID, tc.evt.Request.EnvelopeID)
			}
			if (ack.payload != nil) != tc.wantPayload {
				t.Errorf("ack payload = %v, want payload present %v", ack.payload, tc.wantPayload)
			}
		})
	}
}

func TestHandleEventSlashCommandRepliesEphemerally(t *testing.T) {
	tl := newTestListener()
	evt := socketmode.Event{
		Type:    socketmode.EventTypeSlashCommand,
		Data:    slash("halt sim_1"),
		Request: &socketmode.Request{EnvelopeID: "env_1"},
	}

	tl.handleEvent(context.Background(), evt)

	payload, ok := tl.acker.acks[0].payload.(map[string]interface{})
	if !ok {
		t.Fatalf("ack payload is %T, want a map", tl.acker.acks[0].payload)
	}
	if payload["response_type"] != "ephemeral" {
		t.Errorf("response_type = %v, want ephemeral", payload["response_type"])
	}
	if want := ":octagonal_sign: Halted simulation `sim_1` on `payments-api`."; payload["text"] != want {
		t.Errorf("text = %v, want %q", payload["text"], want)
	}
}

func TestHandleEventButtonFailureRepliesEphemerally(t *testing.T) {
	tl := newTestListener()
	tl.incidents.err = fmt.Errorf("move: %w", incident.ErrInvalidTransition)
	evt := socketmode.Event{
		Type:    socketmode.EventTypeInteractive,
		Data:    click(actionMitigate, "inc_1"),
		Request: &socketmode.Request{EnvelopeID: "env_1"},
	}

	tl.handleEvent(context.Background(), evt)

	if len(tl.messenger.ephemeral) != 1 {
		t.Fatalf("ephemeral replies = %d, want 1", len(tl.messenger.ephemeral))
	}
	got := tl.messenger.ephemeral[0]
	if got.channelID != "C1" || got.userID != "U1" {
		t.Errorf("reply went to user %s in %s, want U1 in C1", got.userID, got.channelID)
	}
	if !strings.Contains(got.text, "could not mitigate this incident") {
		t.Errorf("reply = %q, want it to explain the failed mitigate", got.text)
	}

	// A click that works must stay silent.
	ok := newTestListener()
	ok.handleEvent(context.Background(), evt)
	if len(ok.messenger.ephemeral) != 0 {
		t.Errorf("ephemeral replies after a good click = %d, want 0", len(ok.messenger.ephemeral))
	}
}

// RUN TESTS

func TestListenerRun(t *testing.T) {
	socketErr := errors.New("invalid_auth")

	t.Run("returns the error when the socket gives up by itself", func(t *testing.T) {
		tl := newTestListener()
		tl.events = make(chan socketmode.Event)
		tl.run = func(context.Context) error { return socketErr }

		done := make(chan error, 1)
		go func() { done <- tl.Run(context.Background()) }()

		select {
		case err := <-done:
			if !errors.Is(err, socketErr) {
				t.Errorf("Run error = %v, want one that wraps the socket error", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after the socket gave up")
		}
	})

	t.Run("handles events and returns nil when asked to stop", func(t *testing.T) {
		tl := newTestListener()
		events := make(chan socketmode.Event)
		tl.events = events
		tl.run = func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- tl.Run(ctx) }()

		evt := socketmode.Event{Type: socketmode.EventTypeEventsAPI, Request: &socketmode.Request{EnvelopeID: "env_1"}}
		// The channel has no buffer, so the second send only finishes after the first event was handled.
		for i := 0; i < 2; i++ {
			select {
			case events <- evt:
			case <-time.After(2 * time.Second):
				t.Fatal("Run did not take the event")
			}
		}

		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run error = %v, want nil for a requested stop", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
		if len(tl.acker.acks) == 0 {
			t.Error("Run handled no events, want at least the first one acked")
		}
	})
}
