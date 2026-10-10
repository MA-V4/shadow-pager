package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// FAKES

// testClock is a pretend clock that only moves when a test tells it to.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakePinger pretends to be the database and counts how often it was asked.
type fakePinger struct {
	mu      sync.Mutex
	calls   int
	err     error
	block   chan struct{} // when set, a ping waits here until the test closes it
	started chan struct{} // gets a value every time a ping begins
}

func (f *fakePinger) Ping(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	err := f.err
	block := f.block
	f.mu.Unlock()

	if f.started != nil {
		f.started <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (f *fakePinger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakePinger) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// newTestPingCache builds a cache on the fakes that tells the test when a background ping is done.
func newTestPingCache(pinger *fakePinger, clock *testClock) *pingCache {
	c := newPingCache(pinger.Ping, clock.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.refreshed = make(chan struct{}, 8)
	return c
}

func waitRefreshed(t *testing.T, c *pingCache) {
	t.Helper()
	select {
	case <-c.refreshed:
	case <-time.After(testWait):
		t.Fatal("timed out waiting for the background ping to finish")
	}
}

// TESTS

func TestPingCacheAsksAtMostOncePerMinute(t *testing.T) {
	pinger := &fakePinger{}
	clock := &testClock{now: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)}
	c := newTestPingCache(pinger, clock)

	// Probes arrive every few seconds, and none of them may reach the database yet.
	for i := 0; i < 20; i++ {
		if err := c.Err(); err != nil {
			t.Fatalf("Err = %v, want nil right after startup", err)
		}
		clock.Advance(2 * time.Second)
	}
	if got := pinger.count(); got != 0 {
		t.Fatalf("database was pinged %d times in the first 40 seconds, want 0", got)
	}

	// After a full minute one probe starts one ping, and the others in that moment start none.
	clock.Advance(20 * time.Second)
	for i := 0; i < 5; i++ {
		_ = c.Err()
	}
	waitRefreshed(t, c)
	if got := pinger.count(); got != 1 {
		t.Fatalf("database was pinged %d times after one minute, want exactly 1", got)
	}

	clock.Advance(59 * time.Second)
	_ = c.Err()
	if got := pinger.count(); got != 1 {
		t.Errorf("database was pinged %d times within the next minute, want still 1", got)
	}
}

func TestPingCacheReportsFailureAndRecovery(t *testing.T) {
	down := errors.New("connection refused")
	pinger := &fakePinger{err: down}
	clock := &testClock{now: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)}
	c := newTestPingCache(pinger, clock)

	clock.Advance(time.Minute)
	if err := c.Err(); err != nil {
		t.Errorf("Err = %v, want the old healthy answer while the new ping is still running", err)
	}
	waitRefreshed(t, c)
	if err := c.Err(); !errors.Is(err, down) {
		t.Fatalf("Err = %v, want the failed ping to be remembered", err)
	}

	pinger.setErr(nil)
	clock.Advance(time.Minute)
	_ = c.Err()
	waitRefreshed(t, c)
	if err := c.Err(); err != nil {
		t.Errorf("Err = %v, want nil after the database came back", err)
	}
}

func TestPingCacheNeverBlocksTheProbe(t *testing.T) {
	pinger := &fakePinger{block: make(chan struct{}), started: make(chan struct{}, 4)}
	clock := &testClock{now: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)}
	c := newTestPingCache(pinger, clock)
	clock.Advance(time.Minute)

	answered := make(chan error, 1)
	go func() { answered <- c.Err() }()
	select {
	case err := <-answered:
		if err != nil {
			t.Errorf("Err = %v, want the remembered healthy answer", err)
		}
	case <-time.After(testWait):
		t.Fatal("Err waited for a slow database ping")
	}

	// The ping is stuck now, and more probes must neither wait nor start a second ping.
	select {
	case <-pinger.started:
	case <-time.After(testWait):
		t.Fatal("the background ping never started")
	}
	for i := 0; i < 5; i++ {
		_ = c.Err()
	}
	if got := pinger.count(); got != 1 {
		t.Errorf("database was pinged %d times while one ping was stuck, want 1", got)
	}

	close(pinger.block)
	waitRefreshed(t, c)
}
