package slack

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	slackapi "github.com/slack-go/slack"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// BUTTONS

const (
	actionAcknowledge = "incident_acknowledge"
	actionMitigate    = "incident_mitigate"
	actionResolve     = "incident_resolve"
	actionsBlockID    = "incident_actions"
)

// cardButtons gives back the buttons that still make sense for the incident's status.
func cardButtons(inc incident.Incident) []slackapi.BlockElement {
	id := string(inc.ID)
	acknowledge := button(actionAcknowledge, id, "Acknowledge").WithStyle(slackapi.StylePrimary)
	mitigate := button(actionMitigate, id, "Mitigate (halt simulation)").WithStyle(slackapi.StyleDanger)
	resolve := button(actionResolve, id, "Resolve")

	switch inc.Status {
	case incident.StatusDeclared:
		return []slackapi.BlockElement{acknowledge, mitigate, resolve}
	case incident.StatusAcknowledged:
		return []slackapi.BlockElement{mitigate, resolve}
	case incident.StatusMitigated:
		return []slackapi.BlockElement{resolve.WithStyle(slackapi.StylePrimary)}
	default:
		return nil
	}
}

// button builds one button that carries the incident ID as its value.
func button(actionID, value, label string) *slackapi.ButtonBlockElement {
	return slackapi.NewButtonBlockElement(actionID, value, plain(label))
}

// CARD

// cardBlocks builds the incident card that sits at the top of the incident channel.
func cardBlocks(inc incident.Incident) []slackapi.Block {
	fields := []*slackapi.TextBlockObject{
		markdown("*Severity*\n" + capitalize(string(inc.Severity))),
		markdown("*Status*\n" + statusEmoji(inc.Status) + " " + capitalize(string(inc.Status))),
		markdown("*Service*\n`" + inc.Service + "`"),
		markdown("*Failure mode*\n" + modeLabel(inc.Mode)),
		markdown("*SLO breaches*\n" + breachesLabel(inc.Breaches)),
		markdown("*Declared*\n" + slackDate(inc.DeclaredAt)),
	}

	blocks := []slackapi.Block{
		slackapi.NewHeaderBlock(plain(cardTitle(inc))),
		slackapi.NewSectionBlock(nil, fields, nil),
	}
	if buttons := cardButtons(inc); len(buttons) > 0 {
		blocks = append(blocks, slackapi.NewActionBlock(actionsBlockID, buttons...))
	}
	footer := fmt.Sprintf("Incident `%s` for simulation `%s`", inc.ID, inc.SimulationID)
	return append(blocks, slackapi.NewContextBlock("", markdown(footer)))
}

// headerLimit is the longest title Slack allows in a header block.
const headerLimit = 150

// cardTitle is the big line at the top of the card and the text shown in notifications.
func cardTitle(inc incident.Incident) string {
	title := fmt.Sprintf("%s %s on %s", statusEmoji(inc.Status), modeLabel(inc.Mode), inc.Service)
	if r := []rune(title); len(r) > headerLimit {
		title = string(r[:headerLimit])
	}
	return title
}

// statusEmoji picks a coloured dot for each status.
func statusEmoji(s incident.Status) string {
	switch s {
	case incident.StatusDeclared:
		return ":red_circle:"
	case incident.StatusAcknowledged:
		return ":large_yellow_circle:"
	case incident.StatusMitigated:
		return ":large_blue_circle:"
	case incident.StatusResolved:
		return ":large_green_circle:"
	default:
		return ":white_circle:"
	}
}

// MESSAGES

// timelineText turns one story line into a short Slack message.
func timelineText(entry incident.TimelineEntry) string {
	actor := entry.Actor
	if actor == incident.ActorSystem {
		actor = ":robot_face: Shadow-Pager"
	}
	return actor + ": " + entry.Text
}

// summaryText is the closing message with how long things took.
func summaryText(inc incident.Incident) string {
	acknowledged := "never acknowledged"
	if d, ok := inc.TimeToAcknowledge(); ok {
		acknowledged = d.Round(time.Second).String()
	}
	resolved := "unknown"
	if d, ok := inc.TimeToResolve(); ok {
		resolved = d.Round(time.Second).String()
	}
	return fmt.Sprintf(":white_check_mark: *Incident resolved.* Time to acknowledge: %s. Time to resolve: %s.", acknowledged, resolved)
}

// announceText is the one line posted in the announce channel with a link to the incident channel.
func announceText(inc incident.Incident) string {
	return fmt.Sprintf(":rotating_light: *%s* incident declared for `%s` (%s): <#%s>",
		capitalize(string(inc.Severity)), inc.Service, strings.ToLower(modeLabel(inc.Mode)), inc.ChannelID)
}

// CHANNEL NAME

// channelNameLimit is the longest channel name Slack allows.
const channelNameLimit = 80

// channelName builds a name like inc-20260102-payments-api-1a2b that Slack will accept.
func channelName(at time.Time, service, suffix string) string {
	prefix := "inc-" + at.UTC().Format("20060102") + "-"
	tail := "-" + suffix

	slug := slugify(service)
	if room := channelNameLimit - len(prefix) - len(tail); len(slug) > room {
		slug = strings.Trim(slug[:max(room, 0)], "-")
	}
	if slug == "" {
		slug = "service"
	}
	return prefix + slug + tail
}

// slugify keeps only the letters Slack allows in a channel name and turns the rest into dashes.
func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// SMALL HELPERS

// plain wraps text that Slack shows exactly as written, with emoji names turned into pictures.
func plain(text string) *slackapi.TextBlockObject {
	return slackapi.NewTextBlockObject(slackapi.PlainTextType, text, true, false)
}

// markdown wraps text that may use Slack's bold and code marks.
func markdown(text string) *slackapi.TextBlockObject {
	return slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false)
}

// modeLabel turns latency_spike into Latency spike.
func modeLabel(mode chaos.FailureMode) string {
	return capitalize(strings.ReplaceAll(string(mode), "_", " "))
}

// capitalize makes the first letter big.
func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// breachesLabel lists the broken limits, or says there are none.
func breachesLabel(breaches []string) string {
	if len(breaches) == 0 {
		return "none"
	}
	return "`" + strings.Join(breaches, "`, `") + "`"
}

// slackDate asks Slack to show a time in each reader's own time zone.
func slackDate(t time.Time) string {
	return fmt.Sprintf("<!date^%d^{date_short_pretty} at {time_secs}|%s>", t.Unix(), t.UTC().Format(time.RFC3339))
}
