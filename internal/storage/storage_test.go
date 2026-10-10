package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// HELPERS

// fastRetry keeps tests quick when the database is not there.
var fastRetry = retryPolicy{attempts: 2, baseDelay: time.Millisecond}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newEmptyTestStore gives the test its own empty schema, so tests never see each other's rows.
func newEmptyTestStore(t *testing.T) *Store {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set, so the Postgres integration tests are skipped")
	}
	ctx := context.Background()

	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("make schema name: %v", err)
	}
	schema := "test_" + hex.EncodeToString(b[:])

	admin, err := open(ctx, base, testLogger(), defaultRetry)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	if _, err := admin.pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}

	// The search_path setting makes every table name in this store point into the test schema.
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	store, err := open(ctx, u.String(), testLogger(), defaultRetry)
	if err != nil {
		admin.Close()
		t.Fatalf("open test store: %v", err)
	}

	t.Cleanup(func() {
		store.Close()
		if _, err := admin.pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
		admin.Close()
	})
	return store
}

// newTestStore gives the test its own schema with all tables already made.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store := newEmptyTestStore(t)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

// countRows counts the rows of one table.
func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return n
}

// MIGRATION TESTS

func TestLoadMigrationsAreSorted(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations found, want at least the first one")
	}
	if migrations[0].version != "0001_init" {
		t.Errorf("first migration = %q, want 0001_init", migrations[0].version)
	}
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].version >= migrations[i].version {
			t.Errorf("migration %q comes before %q, want name order", migrations[i-1].version, migrations[i].version)
		}
	}
	for _, m := range migrations {
		if strings.TrimSpace(m.sql) == "" {
			t.Errorf("migration %s is empty", m.version)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	store := newEmptyTestStore(t)
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := store.Migrate(ctx); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}

	if got := countRows(t, store, "schema_migrations"); got != len(migrations) {
		t.Errorf("schema_migrations has %d rows, want one per migration file (%d)", got, len(migrations))
	}
	for _, table := range []string{"simulations", "incidents", "incident_timeline"} {
		if got := countRows(t, store, table); got != 0 {
			t.Errorf("table %s has %d rows after migrating, want it to exist and be empty", table, got)
		}
	}
}

func TestMigrateFromManyInstancesAtOnce(t *testing.T) {
	store := newEmptyTestStore(t)
	ctx := context.Background()

	const instances = 4
	errs := make(chan error, instances)
	var wg sync.WaitGroup
	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Migrate(ctx)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("Migrate while others migrate: %v", err)
		}
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if got := countRows(t, store, "schema_migrations"); got != len(migrations) {
		t.Errorf("schema_migrations has %d rows, want each migration recorded exactly once", got)
	}
}

// POOL TESTS

func TestOpenRejectsBadURL(t *testing.T) {
	_, err := open(context.Background(), "this is not a database url", testLogger(), fastRetry)
	if err == nil {
		t.Fatal("open with a bad url returned no error")
	}
	if !strings.Contains(err.Error(), "parse database url") {
		t.Errorf("error = %v, want it to say the url could not be parsed", err)
	}
}

func TestOpenGivesUpAfterRetries(t *testing.T) {
	// Nothing listens on port 1, so every try is refused.
	_, err := open(context.Background(), "postgres://nobody@127.0.0.1:1/nothing?sslmode=disable", testLogger(), fastRetry)
	if err == nil {
		t.Fatal("open against a closed port returned no error")
	}
	if !strings.Contains(err.Error(), "database not ready after 2 tries") {
		t.Errorf("error = %v, want it to say how many tries were made", err)
	}
}

func TestOpenErrorsNeverRepeatThePassword(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "database is unreachable", url: "postgres://nobody:hunter2secret@127.0.0.1:1/nothing?sslmode=disable"},
		{name: "url cannot be parsed", url: "postgres://nobody:hunter2secret@127.0.0.1:notaport/nothing"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := open(context.Background(), tc.url, testLogger(), fastRetry)
			if err == nil {
				t.Fatal("open returned no error")
			}
			if strings.Contains(err.Error(), "hunter2secret") {
				t.Errorf("error %q repeats the database password", err)
			}
		})
	}
}

func TestOpenStopsWaitingWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	slow := retryPolicy{attempts: 5, baseDelay: time.Hour}
	done := make(chan error, 1)
	go func() {
		_, err := open(ctx, "postgres://nobody@127.0.0.1:1/nothing?sslmode=disable", testLogger(), slow)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("open with a finished context returned no error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("open kept waiting after its context ended")
	}
}

func TestPingAndClose(t *testing.T) {
	store := newTestStore(t)

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestQueriesLeaveNoPreparedStatements(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// One connection is held for the whole test, because prepared statements belong to a connection.
	conn, err := store.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	var answer int
	if err := conn.QueryRow(ctx, `SELECT $1::int + $2::int`, 40, 2).Scan(&answer); err != nil || answer != 42 {
		t.Fatalf("query with parameters = %d, %v, want 42", answer, err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, "0001_init"); err != nil {
		t.Fatalf("exec with parameters: %v", err)
	}

	// A pooler that shares connections breaks named prepared statements, so we must never leave one behind.
	var prepared int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_prepared_statements`).Scan(&prepared); err != nil {
		t.Fatalf("count prepared statements: %v", err)
	}
	if prepared != 0 {
		t.Errorf("the connection holds %d prepared statements, want 0 so it is safe behind a transaction pooler", prepared)
	}
}
