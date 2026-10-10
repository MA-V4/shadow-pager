package main

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// READINESS

const (
	pingMaxAge  = 60 * time.Second // how old the remembered database answer may get before we ask again
	pingTimeout = 2 * time.Second  // how long the database may take to answer one ping
)

// pingCache remembers the last database ping, so the readiness probe never has to wait for the database.
type pingCache struct {
	ping    func(context.Context) error
	now     func() time.Time
	log     *slog.Logger
	maxAge  time.Duration
	timeout time.Duration

	mu         sync.Mutex
	err        error
	checkedAt  time.Time
	refreshing bool

	refreshed chan struct{} // tests listen here to learn that a background ping has finished
}

// newPingCache starts out healthy, because the database was just reached when the program started.
func newPingCache(ping func(context.Context) error, now func() time.Time, logger *slog.Logger) *pingCache {
	return &pingCache{
		ping:      ping,
		now:       now,
		log:       logger,
		maxAge:    pingMaxAge,
		timeout:   pingTimeout,
		checkedAt: now(),
	}
}

// Err gives back the remembered answer right away, and asks the database again in the background when the answer is old.
func (c *pingCache) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.refreshing && c.now().Sub(c.checkedAt) >= c.maxAge {
		c.refreshing = true
		go c.refresh()
	}
	return c.err
}

// refresh pings the database once with a time limit and remembers what happened.
func (c *pingCache) refresh() {
	// The probe that asked for this has long been answered, so the ping gets its own context.
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	err := c.ping(ctx)
	cancel()

	c.mu.Lock()
	changed := (err == nil) != (c.err == nil)
	c.err = err
	c.checkedAt = c.now()
	c.refreshing = false
	c.mu.Unlock()

	if changed && err != nil {
		c.log.Warn("database stopped answering the readiness ping", slog.Any("error", err))
	} else if changed {
		c.log.Info("database answers the readiness ping again")
	}

	if c.refreshed != nil {
		select {
		case c.refreshed <- struct{}{}:
		default:
		}
	}
}
