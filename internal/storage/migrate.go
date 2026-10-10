package storage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MIGRATIONS

// migrationFiles holds the SQL files, baked into the program when it is built.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockKey is the number every instance uses to take turns at migrating.
const migrationLockKey int64 = 742319055101

// createMigrationsTable makes the list where we write down which files were already run.
const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
)`

// migration is one SQL file and the name we remember it by.
type migration struct {
	version string
	sql     string
}

// loadMigrations reads the baked in SQL files and puts them in name order.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations folder: %w", err)
	}

	var out []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		raw, err := fs.ReadFile(migrationFiles, "migrations/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		out = append(out, migration{version: strings.TrimSuffix(entry.Name(), ".sql"), sql: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// migrate runs every SQL file that has not been run yet, one transaction per file.
func migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	err = inLockedTx(ctx, pool, log, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, createMigrationsTable)
		return err
	})
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	for _, m := range migrations {
		applied := false
		err := inLockedTx(ctx, pool, log, func(tx pgx.Tx) error {
			var done bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.version).Scan(&done); err != nil {
				return fmt.Errorf("check if already applied: %w", err)
			}
			if done {
				return nil
			}
			// A file holds many statements, and only the simple protocol can send those in one go.
			if _, err := tx.Exec(ctx, m.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
				return fmt.Errorf("run sql: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version); err != nil {
				return fmt.Errorf("record as applied: %w", err)
			}
			applied = true
			return nil
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", m.version, err)
		}
		if applied {
			log.InfoContext(ctx, "migration applied", slog.String("version", m.version))
		}
	}
	return nil
}

// inLockedTx runs fn inside a transaction that holds the migration lock until it ends.
func inLockedTx(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rolling back after a commit does nothing, so this is safe on every path.
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.ErrorContext(ctx, "rollback migration transaction", slog.Any("error", err))
		}
	}()

	// This lock lives inside the transaction, so it also works through poolers that share connections.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
