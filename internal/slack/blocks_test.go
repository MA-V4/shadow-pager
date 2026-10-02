package slack

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	slackapi "github.com/slack-go/slack"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// FIXTURES

var testDeclaredAt = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

// testIncident builds an incident and walks it through the given statuses, one minute apart.
func testIncident(t *testing.T, steps ...incident.Status) incident.Incident {
	t.Helper()
	sim := chaos.Simulation{
		ID: "sim_1",
		Scenario: chaos.Scenario{
			Service:  "payments-api",
			Mode:     chaos.FailureLatencySpike,
			Severity: chaos.SeverityMajor,
		},
	}
	inc := incident.New("inc_1", sim, []string{"latency_p99", "error_rate"}, testDeclaredAt)
	inc.ChannelID = "C1"
	for i, next := range steps {
		at := testDeclaredAt.Add(time.Duration(i+1) * time.Minute)
		if err := inc.Transition(next, at, "<@U1>", "Moved to "+string(next)); err != nil {
			t.Fatalf("move test incident to %s: %v", next, err)
		}
	}
	return inc
}

// buttonsOf pulls the buttons out of a list of block elements.
func buttonsOf(t *testing.T, elements []slackapi.BlockElement) []*slackapi.ButtonBlockElement {
	t.Helper()
	var out []*slackapi.ButtonBlockElement
	for _, el := range elements {
		b, ok := el.(*slackapi.ButtonBlockElement)
		if !ok {
			t.Fatalf("element %T is not a button", el)
		}
		out = append(out, b)
	}
	return out
}

// TESTS

func TestCardButtonsByStatus(t *testing.T) {
	tests := []struct {
		name  string
		steps []incident.Status
		want  []string
	}{
		{name: "declared shows all three", want: []string{actionAcknowledge, actionMitigate, actionResolve}},
		{name: "acknowledged hides acknowledge", steps: []incident.Status{incident.StatusAcknowledged}, want: []string{actionMitigate, actionResolve}},
		{name: "mitigated only shows resolve", steps: []incident.Status{incident.StatusMitigated}, want: []string{actionResolve}},
		{name: "resolved shows none", steps: []incident.Status{incident.StatusResolved}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buttons := buttonsOf(t, cardButtons(testIncident(t, tc.steps...)))

			var got []string
			for _, b := range buttons {
				got = append(got, b.ActionID)
				if b.Value != "inc_1" {
					t.Errorf("button %s value = %q, want the incident id inc_1", b.ActionID, b.Value)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("buttons = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCardBlocksForDeclaredIncident(t *testing.T) {
	blocks := cardBlocks(testIncident(t))

	if len(blocks) != 4 {
		t.Fatalf("card has %d blocks, want header, fields, buttons and footer", len(blocks))
	}

	header, ok := blocks[0].(*slackapi.HeaderBlock)
	if !ok {
		t.Fatalf("block 0 is %T, want a header", blocks[0])
	}
	if want := ":red_circle: Latency spike on payments-api"; header.Text.Text != want {
		t.Errorf("header = %q, want %q", header.Text.Text, want)
	}

	section, ok := blocks[1].(*slackapi.SectionBlock)
	if !ok {
		t.Fatalf("block 1 is %T, want a section", blocks[1])
	}
	wantFields := []string{
		"*Severity*\nMajor",
		"*Status*\n:red_circle: Declared",
		"*Service*\n`payments-api`",
		"*Failure mode*\nLatency spike",
		"*SLO breaches*\n`latency_p99`, `error_rate`",
		"*Declared*\n<!date^1767366245^{date_short_pretty} at {time_secs}|2026-01-02T15:04:05Z>",
	}
	if len(section.Fields) != len(wantFields) {
		t.Fatalf("section has %d fields, want %d", len(section.Fields), len(wantFields))
	}
	for i, want := range wantFields {
		if got := section.Fields[i].Text; got != want {
			t.Errorf("field %d = %q, want %q", i, got, want)
		}
	}

	actions, ok := blocks[2].(*slackapi.ActionBlock)
	if !ok {
		t.Fatalf("block 2 is %T, want the buttons", blocks[2])
	}
	if actions.BlockID != actionsBlockID {
		t.Errorf("actions block id = %q, want %q", actions.BlockID, actionsBlockID)
	}
	buttons := buttonsOf(t, actions.Elements.ElementSet)
	if len(buttons) != 3 {
		t.Fatalf("card has %d buttons, want 3", len(buttons))
	}
	if buttons[0].Style != slackapi.StylePrimary || buttons[1].Style != slackapi.StyleDanger {
		t.Errorf("button styles = %q and %q, want primary and danger", buttons[0].Style, buttons[1].Style)
	}
	if want := "Mitigate (halt simulation)"; buttons[1].Text.Text != want {
		t.Errorf("mitigate label = %q, want %q", buttons[1].Text.Text, want)
	}

	if _, ok := blocks[3].(*slackapi.ContextBlock); !ok {
		t.Fatalf("block 3 is %T, want the footer", blocks[3])
	}

	// Slack only accepts blocks that turn into JSON, so we make sure they do.
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal card blocks: %v", err)
	}
	for _, want := range []string{`"type":"header"`, `"type":"actions"`, `"action_id":"incident_mitigate"`, "inc_1", "sim_1"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("card JSON does not contain %s", want)
		}
	}
}

func TestCardBlocksForResolvedIncident(t *testing.T) {
	blocks := cardBlocks(testIncident(t, incident.StatusAcknowledged, incident.StatusResolved))

	if len(blocks) != 3 {
		t.Fatalf("resolved card has %d blocks, want header, fields and footer", len(blocks))
	}
	for i, b := range blocks {
		if _, ok := b.(*slackapi.ActionBlock); ok {
			t.Errorf("block %d is a button row, but a resolved incident has nothing left to click", i)
		}
	}
	header := blocks[0].(*slackapi.HeaderBlock)
	if !strings.HasPrefix(header.Text.Text, ":large_green_circle:") {
		t.Errorf("header = %q, want it to start with the green dot", header.Text.Text)
	}
}

func TestChannelName(t *testing.T) {
	long := strings.Repeat("very-long-service-name-", 6)

	tests := []struct {
		name    string
		service string
		want    string
	}{
		{name: "simple", service: "payments-api", want: "inc-20260102-payments-api-1a2b"},
		{name: "upper case and spaces", service: "Payments API", want: "inc-20260102-payments-api-1a2b"},
		{name: "strange characters", service: "pay.ments/api!!", want: "inc-20260102-pay-ments-api-1a2b"},
		{name: "underscore is kept", service: "db_pool", want: "inc-20260102-db_pool-1a2b"},
		{name: "nothing usable", service: "!!!", want: "inc-20260102-service-1a2b"},
		{name: "empty", service: "", want: "inc-20260102-service-1a2b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := channelName(testDeclaredAt, tc.service, "1a2b"); got != tc.want {
				t.Errorf("channelName = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("too long is cut to the limit", func(t *testing.T) {
		got := channelName(testDeclaredAt, long, "1a2b")
		if len(got) > channelNameLimit {
			t.Errorf("name is %d characters, want at most %d", len(got), channelNameLimit)
		}
		if !strings.HasPrefix(got, "inc-20260102-very-long") || !strings.HasSuffix(got, "-1a2b") {
			t.Errorf("name = %q, want it to keep the date in front and the ending at the back", got)
		}
		if strings.Contains(got, "--") {
			t.Errorf("name = %q, want no doubled dashes", got)
		}
	})
}

func TestSummaryText(t *testing.T) {
	tests := []struct {
		name  string
		steps []incident.Status
		want  string
	}{
		{
			name:  "acknowledged then resolved",
			steps: []incident.Status{incident.StatusAcknowledged, incident.StatusMitigated, incident.StatusResolved},
			want:  ":white_check_mark: *Incident resolved.* Time to acknowledge: 1m0s. Time to resolve: 3m0s.",
		},
		{
			name:  "resolved without acknowledge",
			steps: []incident.Status{incident.StatusResolved},
			want:  ":white_check_mark: *Incident resolved.* Time to acknowledge: never acknowledged. Time to resolve: 1m0s.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := summaryText(testIncident(t, tc.steps...)); got != tc.want {
				t.Errorf("summaryText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTimelineText(t *testing.T) {
	tests := []struct {
		name  string
		entry incident.TimelineEntry
		want  string
	}{
		{name: "a person", entry: incident.TimelineEntry{Actor: "<@U1>", Text: "Acknowledged"}, want: "<@U1>: Acknowledged"},
		{name: "the program", entry: incident.TimelineEntry{Actor: incident.ActorSystem, Text: "Incident declared"}, want: ":robot_face: Shadow-Pager: Incident declared"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := timelineText(tc.entry); got != tc.want {
				t.Errorf("timelineText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAnnounceText(t *testing.T) {
	want := ":rotating_light: *Major* incident declared for `payments-api` (latency spike): <#C1>"
	if got := announceText(testIncident(t)); got != want {
		t.Errorf("announceText = %q, want %q", got, want)
	}
}
