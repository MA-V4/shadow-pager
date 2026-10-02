// Package slack drives the incident lifecycle in Slack over Socket Mode:
// channel creation, Block Kit incident cards, and interactive actions.
//
// Implemented in Phase 3.
package slack

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	slackapi "github.com/slack-go/slack"

	"github.com/MA-V4/shadow-pager/internal/incident"
)

// ERRORS

var (
	errNoChannel = errors.New("incident has no slack channel")
	errNoCard    = errors.New("incident card message is unknown")
	errNameTaken = errors.New("no free channel name found")
)

// MESSENGER

// messenger lists the only things this package needs Slack to do.
type messenger interface {
	CreateChannel(ctx context.Context, name string) (channelID string, err error)
	InviteUsers(ctx context.Context, channelID string, userIDs []string) error
	JoinChannel(ctx context.Context, channelID string) error
	PostMessage(ctx context.Context, channelID, text string, blocks []slackapi.Block) (timestamp string, err error)
	UpdateMessage(ctx context.Context, channelID, timestamp, text string, blocks []slackapi.Block) error
	PostEphemeral(ctx context.Context, channelID, userID, text string) error
}

// webAPI turns our small messenger calls into real Slack web API calls.
type webAPI struct {
	api *slackapi.Client
}

func (w webAPI) CreateChannel(ctx context.Context, name string) (string, error) {
	ch, err := w.api.CreateConversationContext(ctx, slackapi.CreateConversationParams{ChannelName: name})
	if err != nil {
		return "", err
	}
	return ch.ID, nil
}

func (w webAPI) InviteUsers(ctx context.Context, channelID string, userIDs []string) error {
	_, err := w.api.InviteUsersToConversationContext(ctx, channelID, userIDs...)
	return err
}

func (w webAPI) JoinChannel(ctx context.Context, channelID string) error {
	_, _, _, err := w.api.JoinConversationContext(ctx, channelID)
	return err
}

func (w webAPI) PostMessage(ctx context.Context, channelID, text string, blocks []slackapi.Block) (string, error) {
	_, ts, err := w.api.PostMessageContext(ctx, channelID, messageOptions(text, blocks)...)
	return ts, err
}

func (w webAPI) UpdateMessage(ctx context.Context, channelID, timestamp, text string, blocks []slackapi.Block) error {
	_, _, _, err := w.api.UpdateMessageContext(ctx, channelID, timestamp, messageOptions(text, blocks)...)
	return err
}

func (w webAPI) PostEphemeral(ctx context.Context, channelID, userID, text string) error {
	_, err := w.api.PostEphemeralContext(ctx, channelID, userID, slackapi.MsgOptionText(text, false))
	return err
}

// messageOptions packs the text and the optional blocks the way the Slack library wants them.
func messageOptions(text string, blocks []slackapi.Block) []slackapi.MsgOption {
	opts := []slackapi.MsgOption{slackapi.MsgOptionText(text, false)}
	if len(blocks) > 0 {
		opts = append(opts, slackapi.MsgOptionBlocks(blocks...))
	}
	return opts
}

// isNameTaken tells us if Slack refused a channel name because it already exists.
func isNameTaken(err error) bool {
	var slackErr slackapi.SlackErrorResponse
	return errors.As(err, &slackErr) && slackErr.Err == "name_taken"
}

// CLIENT

// Config holds the Slack settings that come from the environment.
type Config struct {
	AppToken          string   // starts with xapp- and opens the Socket Mode connection
	BotToken          string   // starts with xoxb- and is used for every web API call
	InviteUserIDs     []string // people to invite into every incident channel
	AnnounceChannelID string   // where to announce new incidents, empty for nowhere
}

// maxNameAttempts is how many channel names we try before giving up.
const maxNameAttempts = 5

// Client tells Slack about incidents by making channels and posting messages.
type Client struct {
	raw    *slackapi.Client // nil in tests, used only to open Socket Mode
	api    messenger
	cfg    Config
	log    *slog.Logger
	suffix func() (string, error)

	mu             sync.Mutex
	cards          map[incident.IncidentID]string // the message timestamp of each incident card
	joinedAnnounce bool
}

// This line makes the compiler check that Client can be used as an incident.Notifier.
var _ incident.Notifier = (*Client)(nil)

// NewClient builds a Client that talks to the real Slack.
func NewClient(cfg Config, logger *slog.Logger) *Client {
	raw := slackapi.New(cfg.BotToken, slackapi.OptionAppLevelToken(cfg.AppToken))
	c := newClient(webAPI{api: raw}, cfg, logger)
	c.raw = raw
	return c
}

// newClient builds a Client on top of any messenger, so tests can pass a fake.
func newClient(api messenger, cfg Config, logger *slog.Logger) *Client {
	return &Client{
		api:    api,
		cfg:    cfg,
		log:    logger.With(slog.String("component", "slack")),
		suffix: randomSuffix,
		cards:  make(map[incident.IncidentID]string),
	}
}

// NOTIFIER

// IncidentDeclared makes the incident channel, invites people, and posts the incident card.
func (c *Client) IncidentDeclared(ctx context.Context, inc incident.Incident) (string, error) {
	channelID, err := c.createChannel(ctx, inc)
	if err != nil {
		return "", err
	}
	inc.ChannelID = channelID

	// The channel exists now, so later problems are collected instead of stopping the rest.
	var errs []error
	if len(c.cfg.InviteUserIDs) > 0 {
		if err := c.api.InviteUsers(ctx, channelID, c.cfg.InviteUserIDs); err != nil {
			errs = append(errs, fmt.Errorf("invite responders to %s: %w", channelID, err))
		}
	}

	ts, err := c.api.PostMessage(ctx, channelID, cardTitle(inc), cardBlocks(inc))
	if err != nil {
		errs = append(errs, fmt.Errorf("post incident card to %s: %w", channelID, err))
	} else {
		c.mu.Lock()
		c.cards[inc.ID] = ts
		c.mu.Unlock()
	}

	if err := c.announce(ctx, inc); err != nil {
		errs = append(errs, err)
	}
	return channelID, errors.Join(errs...)
}

// createChannel makes a public channel and tries a new ending when the name is already used.
func (c *Client) createChannel(ctx context.Context, inc incident.Incident) (string, error) {
	for attempt := 1; attempt <= maxNameAttempts; attempt++ {
		suffix, err := c.suffix()
		if err != nil {
			return "", fmt.Errorf("pick channel name ending: %w", err)
		}
		name := channelName(inc.DeclaredAt, inc.Service, suffix)

		channelID, err := c.api.CreateChannel(ctx, name)
		if err == nil {
			return channelID, nil
		}
		if !isNameTaken(err) {
			return "", fmt.Errorf("create channel %s: %w", name, err)
		}
		c.log.DebugContext(ctx, "channel name taken, trying another", slog.String("name", name), slog.Int("attempt", attempt))
	}
	return "", fmt.Errorf("create channel for incident %s after %d tries: %w", inc.ID, maxNameAttempts, errNameTaken)
}

// announce posts one line with a link to the incident channel, if an announce channel is set.
func (c *Client) announce(ctx context.Context, inc incident.Incident) error {
	announceID := c.cfg.AnnounceChannelID
	if announceID == "" {
		return nil
	}

	c.mu.Lock()
	joined := c.joinedAnnounce
	c.mu.Unlock()

	var joinErr error
	if !joined {
		if joinErr = c.api.JoinChannel(ctx, announceID); joinErr == nil {
			c.mu.Lock()
			c.joinedAnnounce = true
			c.mu.Unlock()
		}
	}

	// We still try to post when joining failed, because someone may have invited the bot by hand.
	if _, err := c.api.PostMessage(ctx, announceID, announceText(inc), nil); err != nil {
		postErr := fmt.Errorf("announce incident in %s: %w", announceID, err)
		if joinErr != nil {
			return errors.Join(fmt.Errorf("join announce channel %s: %w", announceID, joinErr), postErr)
		}
		return postErr
	}
	return nil
}

// StatusChanged redraws the incident card and posts what happened in the incident channel.
func (c *Client) StatusChanged(ctx context.Context, inc incident.Incident) error {
	if inc.ChannelID == "" {
		return fmt.Errorf("update slack for incident %s: %w", inc.ID, errNoChannel)
	}

	var errs []error

	c.mu.Lock()
	ts, ok := c.cards[inc.ID]
	if inc.Status == incident.StatusResolved {
		// A resolved incident never changes again, so we can forget its card.
		delete(c.cards, inc.ID)
	}
	c.mu.Unlock()

	if !ok {
		errs = append(errs, fmt.Errorf("update card for incident %s: %w", inc.ID, errNoCard))
	} else if err := c.api.UpdateMessage(ctx, inc.ChannelID, ts, cardTitle(inc), cardBlocks(inc)); err != nil {
		errs = append(errs, fmt.Errorf("update card for incident %s: %w", inc.ID, err))
	}

	if n := len(inc.Timeline); n > 0 {
		if _, err := c.api.PostMessage(ctx, inc.ChannelID, timelineText(inc.Timeline[n-1]), nil); err != nil {
			errs = append(errs, fmt.Errorf("post timeline message for incident %s: %w", inc.ID, err))
		}
	}

	if inc.Status == incident.StatusResolved {
		if _, err := c.api.PostMessage(ctx, inc.ChannelID, summaryText(inc), nil); err != nil {
			errs = append(errs, fmt.Errorf("post summary for incident %s: %w", inc.ID, err))
		}
	}
	return errors.Join(errs...)
}

// TimelineEntryAdded posts one new story line in the incident channel and leaves the card alone.
func (c *Client) TimelineEntryAdded(ctx context.Context, inc incident.Incident, entry incident.TimelineEntry) error {
	if inc.ChannelID == "" {
		return fmt.Errorf("post timeline message for incident %s: %w", inc.ID, errNoChannel)
	}
	if _, err := c.api.PostMessage(ctx, inc.ChannelID, timelineText(entry), nil); err != nil {
		return fmt.Errorf("post timeline message for incident %s: %w", inc.ID, err)
	}
	return nil
}

// HELPERS

// randomSuffix makes four random letters and digits for the end of a channel name.
func randomSuffix() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
