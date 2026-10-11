// Package storage persists simulations and incident timelines to Postgres
// using a pgx connection pool sized for free-tier connection limits.
//
// Implemented in Phase 4.
package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SETTINGS

const (
	maxConns        = 5                // free tiers only allow a handful of connections
	connectTimeout  = 5 * time.Second  // how long one try to connect may take
	maxConnIdleTime = 5 * time.Minute  // idle connections are closed so a sleepy database can go back to sleep
	maxConnLifetime = 30 * time.Minute // old connections are swapped for fresh ones
)

// retryPolicy says how many times we knock on the database door at startup and how long we wait in between.
type retryPolicy struct {
	attempts  int
	baseDelay time.Duration
}

// defaultRetry waits half a second, then one, two, four and eight, which is enough for a sleeping Neon database to wake up.
var defaultRetry = retryPolicy{attempts: 6, baseDelay: 500 * time.Millisecond}

// STORE

// Store is the door to the database that the rest of the program uses.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// Open connects to Postgres and waits until the database answers.
func Open(ctx context.Context, url string, logger *slog.Logger) (*Store, error) {
	return open(ctx, url, logger, defaultRetry)
}

// open is Open with a retry policy that tests can make short.
func open(ctx context.Context, url string, logger *slog.Logger, retry retryPolicy) (*Store, error) {
	if logger == nil {
		return nil, errors.New("storage: logger is required")
	}
	log := logger.With(slog.String("component", "storage"))

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		// The parser's own message can repeat the address with its password, so it is dropped on purpose.
		return nil, fmt.Errorf("parse database url: %w", ErrInvalidURL)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = maxConnIdleTime
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	// This mode never makes named prepared statements, so it is safe behind poolers that share connections.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	s := &Store{pool: pool, log: log}
	if err := s.waitUntilReady(ctx, retry); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// waitUntilReady pings the database and waits longer after every failed try.
func (s *Store) waitUntilReady(ctx context.Context, retry retryPolicy) error {
	delay := retry.baseDelay
	var lastErr error
	for attempt := 1; attempt <= retry.attempts; attempt++ {
		if lastErr = s.Ping(ctx); lastErr == nil {
			return nil
		}
		if attempt == retry.attempts {
			break
		}
		s.log.WarnContext(ctx, "database not ready, trying again",
			slog.Int("attempt", attempt),
			slog.Duration("wait", delay),
			slog.Any("error", lastErr),
		)
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for database: %w", ctx.Err())
		case <-time.After(delay):
		}
		delay *= 2
	}
	return fmt.Errorf("database not ready after %d tries: %w", retry.attempts, lastErr)
}

// Ping asks the database a tiny question to see if it is there.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Migrate brings the tables up to date.
func (s *Store) Migrate(ctx context.Context) error {
	if err := migrate(ctx, s.pool, s.log); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	return nil
}

// Close gives every connection back to the database.
func (s *Store) Close() {
	s.pool.Close()
}
