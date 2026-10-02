package slack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	slackapi "github.com/slack-go/slack"

	"github.com/MA-V4/shadow-pager/internal/incident"
)

// FAKE

// sentMessage is one message the fake was asked to post or update.
type sentMessage struct {
	channelID string
	timestamp string
	text      string
	blocks    []slackapi.Block
}

// ephemeralMessage is one private reply the fake was asked to send.
type ephemeralMessage struct {
	channelID string
	userID    string
	text      string
}

// fakeMessenger pretends to be Slack and writes down everything it is asked to do.
type fakeMessenger struct {
	createErrs []error // one per CreateChannel call, nil or missing means success
	inviteErr  error
	joinErr    error
	postErr    error
	updateErr  error

	created   []string
	invited   map[string][]string
	joined    []string
	posts     []sentMessage
	updates   []sentMessage
	ephemeral []ephemeralMessage
}

func (f *fakeMessenger) CreateChannel(_ context.Context, name string) (string, error) {
	f.created = append(f.created, name)
	if n := len(f.created); n <= len(f.createErrs) && f.createErrs[n-1] != nil {
		return "", f.createErrs[n-1]
	}
	return fmt.Sprintf("C%d", len(f.created)), nil
}

func (f *fakeMessenger) InviteUsers(_ context.Context, channelID string, userIDs []string) error {
	if f.invited == nil {
		f.invited = make(map[string][]string)
	}
	f.invited[channelID] = userIDs
	return f.inviteErr
}

func (f *fakeMessenger) JoinChannel(_ context.Context, channelID string) error {
	f.joined = append(f.joined, channelID)
	return f.joinErr
}

func (f *fakeMessenger) PostMessage(_ context.Context, channelID, text string, blocks []slackapi.Block) (string, error) {
	if f.postErr != nil {
		return "", f.postErr
	}
	f.posts = append(f.posts, sentMessage{channelID: channelID, text: text, blocks: blocks})
	return fmt.Sprintf("1700000000.%06d", len(f.posts)), nil
}

func (f *fakeMessenger) UpdateMessage(_ context.Context, channelID, timestamp, text string, blocks []slackapi.Block) error {
	f.updates = append(f.updates, sentMessage{channelID: channelID, timestamp: timestamp, text: text, blocks: blocks})
	return f.updateErr
}

func (f *fakeMessenger) PostEphemeral(_ context.Context, channelID, userID, text string) error {
	f.ephemeral = append(f.ephemeral, ephemeralMessage{channelID: channelID, userID: userID, text: text})
	return nil
}

// HELPERS

var errSlackNameTaken = slackapi.SlackErrorResponse{Err: "name_taken"}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestClient builds a Client on the fake with channel name endings that count up.
func newTestClient(fake *fakeMessenger, cfg Config) *Client {
	c := newClient(fake, cfg, discardLogger())
	n := 0
	c.suffix = func() (string, error) {
		n++
		return fmt.Sprintf("%04x", n), nil
	}
	return c
}

// declared builds a fresh incident with no channel, as the manager would hand it over.
func declared(t *testing.T) incident.Incident {
	t.Helper()
	inc := testIncident(t)
	inc.ChannelID = ""
	return inc
}

// TESTS

func TestIncidentDeclaredCreatesChannelAndCard(t *testing.T) {
	fake := &fakeMessenger{}
	c := newTestClient(fake, Config{InviteUserIDs: []string{"U1", "U2"}, AnnounceChannelID: "CANNOUNCE"})

	channelID, err := c.IncidentDeclared(context.Background(), declared(t))
	if err != nil {
		t.Fatalf("IncidentDeclared: %v", err)
	}

	if channelID != "C1" {
		t.Errorf("channel id = %q, want C1", channelID)
	}
	if want := []string{"inc-20260102-payments-api-0001"}; strings.Join(fake.created, ",") != strings.Join(want, ",") {
		t.Errorf("created channels = %v, want %v", fake.created, want)
	}
	if got := fake.invited["C1"]; strings.Join(got, ",") != "U1,U2" {
		t.Errorf("invited to C1 = %v, want [U1 U2]", got)
	}

	if len(fake.posts) != 2 {
		t.Fatalf("posted %d messages, want the card and the announcement", len(fake.posts))
	}
	card := fake.posts[0]
	if card.channelID != "C1" || len(card.blocks) != 4 {
		t.Errorf("card went to %s with %d blocks, want C1 with 4 blocks", card.channelID, len(card.blocks))
	}
	if want := ":red_circle: Latency spike on payments-api"; card.text != want {
		t.Errorf("card fallback text = %q, want %q", card.text, want)
	}

	announce := fake.posts[1]
	if announce.channelID != "CANNOUNCE" || !strings.Contains(announce.text, "<#C1>") {
		t.Errorf("announcement = %+v, want a link to C1 in CANNOUNCE", announce)
	}
	if len(fake.joined) != 1 || fake.joined[0] != "CANNOUNCE" {
		t.Errorf("joined = %v, want [CANNOUNCE]", fake.joined)
	}
	if len(fake.updates) != 0 {
		t.Errorf("declaring updated %d messages, want 0", len(fake.updates))
	}
}

func TestIncidentDeclaredWithoutOptionalSettings(t *testing.T) {
	fake := &fakeMessenger{}
	c := newTestClient(fake, Config{})

	if _, err := c.IncidentDeclared(context.Background(), declared(t)); err != nil {
		t.Fatalf("IncidentDeclared: %v", err)
	}

	if fake.invited != nil {
		t.Errorf("invited = %v, want no invites when no users are set", fake.invited)
	}
	if len(fake.joined) != 0 || len(fake.posts) != 1 {
		t.Errorf("joined %d channels and posted %d messages, want 0 and only the card", len(fake.joined), len(fake.posts))
	}
}

func TestIncidentDeclaredJoinsAnnounceChannelOnce(t *testing.T) {
	fake := &fakeMessenger{}
	c := newTestClient(fake, Config{AnnounceChannelID: "CANNOUNCE"})

	for i := 0; i < 2; i++ {
		if _, err := c.IncidentDeclared(context.Background(), declared(t)); err != nil {
			t.Fatalf("IncidentDeclared %d: %v", i, err)
		}
	}
	if len(fake.joined) != 1 {
		t.Errorf("joined the announce channel %d times, want 1", len(fake.joined))
	}
}

func TestCreateChannelRetriesWhenNameIsTaken(t *testing.T) {
	fake := &fakeMessenger{createErrs: []error{errSlackNameTaken, errSlackNameTaken}}
	c := newTestClient(fake, Config{})

	channelID, err := c.IncidentDeclared(context.Background(), declared(t))
	if err != nil {
		t.Fatalf("IncidentDeclared: %v", err)
	}

	want := []string{
		"inc-20260102-payments-api-0001",
		"inc-20260102-payments-api-0002",
		"inc-20260102-payments-api-0003",
	}
	if strings.Join(fake.created, ",") != strings.Join(want, ",") {
		t.Errorf("tried names %v, want %v", fake.created, want)
	}
	if channelID != "C3" {
		t.Errorf("channel id = %q, want the third try C3", channelID)
	}
}

func TestCreateChannelGivesUp(t *testing.T) {
	boom := errors.New("missing_scope")

	tests := []struct {
		name       string
		createErrs []error
		wantErr    error
		wantTries  int
	}{
		{
			name:       "every name is taken",
			createErrs: []error{errSlackNameTaken, errSlackNameTaken, errSlackNameTaken, errSlackNameTaken, errSlackNameTaken},
			wantErr:    errNameTaken,
			wantTries:  maxNameAttempts,
		},
		{
			name:       "another error stops right away",
			createErrs: []error{boom},
			wantErr:    boom,
			wantTries:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeMessenger{createErrs: tc.createErrs}
			c := newTestClient(fake, Config{InviteUserIDs: []string{"U1"}, AnnounceChannelID: "CANNOUNCE"})

			channelID, err := c.IncidentDeclared(context.Background(), declared(t))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want one that wraps %v", err, tc.wantErr)
			}
			if channelID != "" {
				t.Errorf("channel id = %q, want empty when no channel was made", channelID)
			}
			if len(fake.created) != tc.wantTries {
				t.Errorf("tried %d names, want %d", len(fake.created), tc.wantTries)
			}
			if len(fake.posts) != 0 || fake.invited != nil {
				t.Errorf("posted %d messages and invited %v, want nothing without a channel", len(fake.posts), fake.invited)
			}
		})
	}
}

func TestIncidentDeclaredKeepsGoingWhenInviteFails(t *testing.T) {
	inviteErr := errors.New("user_not_found")
	fake := &fakeMessenger{inviteErr: inviteErr}
	c := newTestClient(fake, Config{InviteUserIDs: []string{"U404"}})

	channelID, err := c.IncidentDeclared(context.Background(), declared(t))
	if !errors.Is(err, inviteErr) {
		t.Fatalf("error = %v, want one that wraps the invite error", err)
	}
	if channelID != "C1" {
		t.Errorf("channel id = %q, want C1 so the manager can still remember the channel", channelID)
	}
	if len(fake.posts) != 1 {
		t.Errorf("posted %d messages, want the card even though the invite failed", len(fake.posts))
	}
}

func TestStatusChangedUpdatesCardAndPostsTimeline(t *testing.T) {
	fake := &fakeMessenger{}
	c := newTestClient(fake, Config{})
	ctx := context.Background()
	if _, err := c.IncidentDeclared(ctx, declared(t)); err != nil {
		t.Fatalf("IncidentDeclared: %v", err)
	}
	cardTS := "1700000000.000001"

	if err := c.StatusChanged(ctx, testIncident(t, incident.StatusAcknowledged)); err != nil {
		t.Fatalf("StatusChanged to acknowledged: %v", err)
	}

	if len(fake.updates) != 1 {
		t.Fatalf("updated %d messages, want 1", len(fake.updates))
	}
	update := fake.updates[0]
	if update.channelID != "C1" || update.timestamp != cardTS {
		t.Errorf("update went to %s at %s, want the card in C1 at %s", update.channelID, update.timestamp, cardTS)
	}
	actions, ok := update.blocks[2].(*slackapi.ActionBlock)
	if !ok {
		t.Fatalf("updated card block 2 is %T, want the buttons", update.blocks[2])
	}
	if got := len(actions.Elements.ElementSet); got != 2 {
		t.Errorf("updated card has %d buttons, want 2 after acknowledge", got)
	}
	if got := fake.posts[len(fake.posts)-1]; got.channelID != "C1" || got.text != "<@U1>: Moved to acknowledged" {
		t.Errorf("timeline message = %+v, want the acknowledge line in C1", got)
	}

	postsBefore := len(fake.posts)
	resolved := testIncident(t, incident.StatusAcknowledged, incident.StatusResolved)
	if err := c.StatusChanged(ctx, resolved); err != nil {
		t.Fatalf("StatusChanged to resolved: %v", err)
	}
	if len(fake.updates) != 2 {
		t.Fatalf("updated %d messages, want 2", len(fake.updates))
	}
	newPosts := fake.posts[postsBefore:]
	if len(newPosts) != 2 {
		t.Fatalf("resolve posted %d messages, want the timeline line and the summary", len(newPosts))
	}
	if want := ":white_check_mark: *Incident resolved.* Time to acknowledge: 1m0s. Time to resolve: 2m0s."; newPosts[1].text != want {
		t.Errorf("summary = %q, want %q", newPosts[1].text, want)
	}
}

func TestStatusChangedErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("no channel", func(t *testing.T) {
		fake := &fakeMessenger{}
		c := newTestClient(fake, Config{})
		inc := testIncident(t, incident.StatusAcknowledged)
		inc.ChannelID = ""

		if err := c.StatusChanged(ctx, inc); !errors.Is(err, errNoChannel) {
			t.Fatalf("error = %v, want errNoChannel", err)
		}
		if len(fake.posts)+len(fake.updates) != 0 {
			t.Errorf("made %d Slack calls, want 0 without a channel", len(fake.posts)+len(fake.updates))
		}
	})

	t.Run("unknown card still posts the timeline", func(t *testing.T) {
		fake := &fakeMessenger{}
		c := newTestClient(fake, Config{})

		err := c.StatusChanged(ctx, testIncident(t, incident.StatusAcknowledged))
		if !errors.Is(err, errNoCard) {
			t.Fatalf("error = %v, want errNoCard", err)
		}
		if len(fake.updates) != 0 || len(fake.posts) != 1 {
			t.Errorf("updates = %d, posts = %d, want 0 and 1", len(fake.updates), len(fake.posts))
		}
	})

	t.Run("slack update failure is wrapped", func(t *testing.T) {
		updateErr := errors.New("ratelimited")
		fake := &fakeMessenger{updateErr: updateErr}
		c := newTestClient(fake, Config{})
		if _, err := c.IncidentDeclared(ctx, declared(t)); err != nil {
			t.Fatalf("IncidentDeclared: %v", err)
		}

		if err := c.StatusChanged(ctx, testIncident(t, incident.StatusAcknowledged)); !errors.Is(err, updateErr) {
			t.Fatalf("error = %v, want one that wraps the update error", err)
		}
	})
}

func TestTimelineEntryAddedPostsWithoutTouchingTheCard(t *testing.T) {
	fake := &fakeMessenger{}
	c := newTestClient(fake, Config{})
	ctx := context.Background()
	entry := incident.TimelineEntry{Actor: incident.ActorSystem, Text: "New SLO breach: cpu"}

	if err := c.TimelineEntryAdded(ctx, testIncident(t), entry); err != nil {
		t.Fatalf("TimelineEntryAdded: %v", err)
	}
	if len(fake.posts) != 1 || fake.posts[0].text != ":robot_face: Shadow-Pager: New SLO breach: cpu" {
		t.Errorf("posts = %+v, want one timeline message", fake.posts)
	}
	if len(fake.updates) != 0 {
		t.Errorf("updated %d messages, want 0 because the card only changes on lifecycle changes", len(fake.updates))
	}

	noChannel := testIncident(t)
	noChannel.ChannelID = ""
	if err := c.TimelineEntryAdded(ctx, noChannel, entry); !errors.Is(err, errNoChannel) {
		t.Fatalf("error = %v, want errNoChannel", err)
	}
}

func TestIsNameTaken(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "name taken", err: errSlackNameTaken, want: true},
		{name: "wrapped name taken", err: fmt.Errorf("create: %w", errSlackNameTaken), want: true},
		{name: "another slack error", err: slackapi.SlackErrorResponse{Err: "invalid_name"}, want: false},
		{name: "plain error", err: errors.New("name_taken"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNameTaken(tc.err); got != tc.want {
				t.Errorf("isNameTaken = %v, want %v", got, tc.want)
			}
		})
	}
}
