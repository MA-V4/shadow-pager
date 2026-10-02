package slack

// Interactive button and slash command handlers land here in Phase 3.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// INTERFACES

// incidents lists the only things the buttons need the incident manager to do.
type incidents interface {
	Acknowledge(ctx context.Context, id incident.IncidentID, actor string) (incident.Incident, error)
	Mitigate(ctx context.Context, id incident.IncidentID, actor string) (incident.Incident, error)
	Resolve(ctx context.Context, id incident.IncidentID, actor string) (incident.Incident, error)
}

// simulations lists the only things the slash command needs the engine to do.
type simulations interface {
	Inject(ctx context.Context, sc chaos.Scenario) (chaos.Simulation, error)
	List(ctx context.Context) ([]chaos.Simulation, error)
	Halt(ctx context.Context, id chaos.SimulationID) (chaos.Simulation, error)
}

// acker is how we tell Slack that we received a request.
type acker interface {
	Ack(req socketmode.Request, payload ...interface{})
}

// LISTENER

// Listener receives button clicks and slash commands from Slack over Socket Mode.
type Listener struct {
	run       func(ctx context.Context) error // keeps the socket open, swapped for a fake in tests
	events    <-chan socketmode.Event
	ack       acker
	api       messenger
	incidents incidents
	sims      simulations
	log       *slog.Logger
}

// NewListener builds a Listener that uses the same Slack connection settings as the Client.
func NewListener(client *Client, inc incidents, sims simulations, logger *slog.Logger) *Listener {
	logger = logger.With(slog.String("component", "slack"))
	// The Slack library writes its own log lines, and this sends them into our structured log.
	socket := socketmode.New(client.raw,
		socketmode.OptionLog(slog.NewLogLogger(logger.Handler(), slog.LevelDebug)),
	)
	return &Listener{
		run:       socket.RunContext,
		events:    socket.Events,
		ack:       socket,
		api:       client.api,
		incidents: inc,
		sims:      sims,
		log:       logger,
	}
}

// Run keeps the Socket Mode connection open and handles requests until ctx is done.
func (l *Listener) Run(ctx context.Context) error {
	// The event loop gets its own context so it also stops when the socket gives up by itself.
	loopCtx, stopLoop := context.WithCancel(ctx)
	defer stopLoop()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		for {
			select {
			case <-loopCtx.Done():
				return
			case evt := <-l.events:
				l.handleEvent(loopCtx, evt)
			}
		}
	}()

	err := l.run(ctx)
	stopLoop()
	<-loopDone
	// Stopping because we were asked to is not a failure.
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("run socket mode: %w", err)
	}
	return nil
}

// handleEvent acks every request right away and then does what it asks.
func (l *Listener) handleEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		l.log.InfoContext(ctx, "connecting to slack")
	case socketmode.EventTypeConnected:
		l.log.InfoContext(ctx, "connected to slack")
	case socketmode.EventTypeConnectionError, socketmode.EventTypeInvalidAuth:
		l.log.ErrorContext(ctx, "slack connection problem", slog.String("event", string(evt.Type)))

	case socketmode.EventTypeInteractive:
		// Slack only waits three seconds, so we say thanks before doing the work.
		l.ackRequest(evt)
		cb, ok := evt.Data.(slackapi.InteractionCallback)
		if !ok {
			l.log.ErrorContext(ctx, "unexpected interactive payload", slog.String("type", fmt.Sprintf("%T", evt.Data)))
			return
		}
		reply := l.routeAction(ctx, cb)
		if reply == "" {
			return
		}
		if err := l.api.PostEphemeral(ctx, cb.Channel.ID, cb.User.ID, reply); err != nil {
			l.log.ErrorContext(ctx, "reply to button click", slog.Any("error", fmt.Errorf("post ephemeral: %w", err)))
		}

	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slackapi.SlashCommand)
		if !ok {
			l.ackRequest(evt)
			l.log.ErrorContext(ctx, "unexpected slash command payload", slog.String("type", fmt.Sprintf("%T", evt.Data)))
			return
		}
		// The engine answers from memory in no time, so the reply can ride along with the ack.
		reply := l.routeCommand(ctx, cmd)
		if evt.Request != nil {
			l.ack.Ack(*evt.Request, map[string]interface{}{"response_type": "ephemeral", "text": reply})
		}

	default:
		l.ackRequest(evt)
	}
}

// ackRequest tells Slack we got the request, when there is one to answer.
func (l *Listener) ackRequest(evt socketmode.Event) {
	if evt.Request != nil && evt.Request.EnvelopeID != "" {
		l.ack.Ack(*evt.Request)
	}
}

// BUTTONS

// routeAction runs the button that was clicked and gives back a private reply when it did not work.
func (l *Listener) routeAction(ctx context.Context, cb slackapi.InteractionCallback) string {
	if cb.Type != slackapi.InteractionTypeBlockActions {
		return ""
	}
	actor := mention(cb.User.ID)

	var replies []string
	for _, action := range cb.ActionCallback.BlockActions {
		id := incident.IncidentID(action.Value)

		var err error
		var verb string
		switch action.ActionID {
		case actionAcknowledge:
			verb = "acknowledge"
			_, err = l.incidents.Acknowledge(ctx, id, actor)
		case actionMitigate:
			verb = "mitigate"
			_, err = l.incidents.Mitigate(ctx, id, actor)
		case actionResolve:
			verb = "resolve"
			_, err = l.incidents.Resolve(ctx, id, actor)
		default:
			continue
		}
		if err != nil {
			l.log.WarnContext(ctx, "button action failed",
				slog.String("action", action.ActionID),
				slog.String("incident_id", string(id)),
				slog.Any("error", err),
			)
			replies = append(replies, actionErrorText(verb, err))
		}
	}
	return strings.Join(replies, "\n")
}

// actionErrorText explains a failed button click in words a responder can act on.
func actionErrorText(verb string, err error) string {
	switch {
	case errors.Is(err, incident.ErrInvalidTransition):
		return fmt.Sprintf(":warning: I could not %s this incident because it has already moved past that step.", verb)
	case errors.Is(err, incident.ErrNotFound):
		return ":warning: I do not know this incident any more, most likely because the server restarted."
	default:
		return fmt.Sprintf(":warning: Something went wrong and I could not %s this incident.", verb)
	}
}

// mention writes a user ID the way Slack turns into a clickable name.
func mention(userID string) string {
	return "<@" + userID + ">"
}

// SLASH COMMAND

const (
	defaultIntensity = 0.8
	defaultDuration  = 2 * time.Minute
	listLimit        = 15
)

const helpText = "*Shadow-Pager commands*\n" +
	"• `/shadowpager inject <mode> <service> [intensity] [duration]` starts a pretend failure, for example `/shadowpager inject latency_spike payments-api 0.9 2m`\n" +
	"• `/shadowpager list` shows the simulations\n" +
	"• `/shadowpager halt <simulation_id>` stops one early\n" +
	"• `/shadowpager help` shows this message\n" +
	"Modes: `latency_spike`, `error_rate_surge`, `memory_leak`, `cpu_saturation`, `db_pool_exhaustion`. " +
	"Intensity goes from 0 to 1 and defaults to 0.8. Duration goes from 10s to 30m and defaults to 2m."

// routeCommand runs one /shadowpager command and gives back the private reply.
func (l *Listener) routeCommand(ctx context.Context, cmd slackapi.SlashCommand) string {
	args := strings.Fields(cmd.Text)
	if len(args) == 0 {
		return helpText
	}

	switch strings.ToLower(args[0]) {
	case "help":
		return helpText
	case "inject":
		return l.commandInject(ctx, args[1:])
	case "list":
		return l.commandList(ctx)
	case "halt":
		return l.commandHalt(ctx, args[1:])
	default:
		return fmt.Sprintf("I do not know the command `%s`.\n%s", args[0], helpText)
	}
}

// commandInject starts a simulation from the words typed after inject.
func (l *Listener) commandInject(ctx context.Context, args []string) string {
	if len(args) < 2 || len(args) > 4 {
		return "Usage: `/shadowpager inject <mode> <service> [intensity] [duration]`"
	}

	sc := chaos.Scenario{
		Mode:      chaos.FailureMode(strings.ToLower(args[0])),
		Service:   args[1],
		Intensity: defaultIntensity,
		Duration:  defaultDuration,
	}
	if len(args) >= 3 {
		v, err := strconv.ParseFloat(args[2], 64)
		if err != nil {
			return fmt.Sprintf("I could not read the intensity `%s`, it must be a number from 0 to 1.", args[2])
		}
		sc.Intensity = v
	}
	if len(args) == 4 {
		d, err := time.ParseDuration(args[3])
		if err != nil {
			return fmt.Sprintf("I could not read the duration `%s`, write it like `90s` or `2m`.", args[3])
		}
		sc.Duration = d
	}
	sc.Severity = severityFor(sc.Intensity)

	sim, err := l.sims.Inject(ctx, sc)
	if err != nil {
		return fmt.Sprintf(":warning: I could not inject that failure: %v", err)
	}
	return fmt.Sprintf(":boom: Injected `%s` on `%s` with intensity %g for %s as simulation `%s`. An incident is declared if it breaks its SLO.",
		sim.Scenario.Mode, sim.Scenario.Service, sim.Scenario.Intensity, sim.Scenario.Duration, sim.ID)
}

// severityFor picks a severity from the intensity, because the command has no word for it.
func severityFor(intensity float64) chaos.Severity {
	switch {
	case intensity >= 0.9:
		return chaos.SeverityCritical
	case intensity >= 0.5:
		return chaos.SeverityMajor
	default:
		return chaos.SeverityMinor
	}
}

// commandList shows the newest simulations.
func (l *Listener) commandList(ctx context.Context) string {
	sims, err := l.sims.List(ctx)
	if err != nil {
		return fmt.Sprintf(":warning: I could not list the simulations: %v", err)
	}
	if len(sims) == 0 {
		return "There are no simulations yet. Start one with `/shadowpager inject <mode> <service>`."
	}

	var b strings.Builder
	b.WriteString("*Simulations, newest first*\n")
	for i, sim := range sims {
		if i == listLimit {
			fmt.Fprintf(&b, "and %d more", len(sims)-listLimit)
			break
		}
		fmt.Fprintf(&b, "• `%s` %s: `%s` on `%s`, started %s\n",
			sim.ID, sim.State, sim.Scenario.Mode, sim.Scenario.Service, slackDate(sim.StartedAt))
	}
	return strings.TrimRight(b.String(), "\n")
}

// commandHalt stops one simulation early.
func (l *Listener) commandHalt(ctx context.Context, args []string) string {
	if len(args) != 1 {
		return "Usage: `/shadowpager halt <simulation_id>`"
	}
	id := chaos.SimulationID(args[0])

	sim, err := l.sims.Halt(ctx, id)
	switch {
	case errors.Is(err, chaos.ErrNotFound):
		return fmt.Sprintf(":warning: I could not find a simulation called `%s`. Try `/shadowpager list`.", id)
	case errors.Is(err, chaos.ErrNotActive):
		return fmt.Sprintf(":warning: Simulation `%s` has already finished.", id)
	case err != nil:
		return fmt.Sprintf(":warning: I could not halt `%s`: %v", id, err)
	}
	return fmt.Sprintf(":octagonal_sign: Halted simulation `%s` on `%s`.", sim.ID, sim.Scenario.Service)
}
