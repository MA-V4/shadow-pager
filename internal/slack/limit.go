package slack

import (
	"fmt"
	"math"
	"time"
)

// WRITE LIMIT

// writeLimiter lists the only thing the listener needs a rate limiter to do.
type writeLimiter interface {
	Allow(key string) (ok bool, retryAfter time.Duration)
}

// ListenerOption is a little helper that changes one setting when the listener is built.
type ListenerOption func(*Listener)

// WithWriteLimit makes every Slack user wait their turn before they inject or halt again.
func WithWriteLimit(limiter writeLimiter) ListenerOption {
	return func(l *Listener) {
		if limiter != nil {
			l.writes = limiter
		}
	}
}

// overLimit gives back a polite reply when this Slack user has no turn left right now.
func (l *Listener) overLimit(userID string) (string, bool) {
	if l.writes == nil {
		return "", false
	}
	ok, retryAfter := l.writes.Allow("slack:" + userID)
	if ok {
		return "", false
	}
	seconds := max(1, int(math.Ceil(retryAfter.Seconds())))
	return fmt.Sprintf(":hourglass_flowing_sand: That was a lot of commands, please try again in %d seconds.", seconds), true
}
