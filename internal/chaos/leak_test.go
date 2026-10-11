package chaos

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"
)

// LEAK CHECKS

// leakWait is the longest the test waits for goroutines to start or to go away.
const leakWait = 2 * time.Second

// waitForGoroutines polls the goroutine count until done says it is right, and reports the last count it saw.
func waitForGoroutines(done func(count int) bool) (int, bool) {
	deadline := time.After(leakWait)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		count := runtime.NumGoroutine()
		if done(count) {
			return count, true
		}
		select {
		case <-deadline:
			return count, false
		case <-tick.C:
		}
	}
}

// TestSubscriptionsLeaveNoGoroutines proves that every listener cleans up after itself when its context is cancelled.
func TestSubscriptionsLeaveNoGoroutines(t *testing.T) {
	const listeners = 50

	e, err := NewEngine(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	samples := make([]<-chan MetricSample, 0, listeners)
	events := make([]<-chan Event, 0, listeners)
	for i := 0; i < listeners; i++ {
		s, err := e.Subscribe(ctx)
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		ev, err := e.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents: %v", err)
		}
		samples, events = append(samples, s), append(events, ev)
	}

	// Each listener owns one goroutine, and seeing them first proves the count can catch a leak.
	if count, ok := waitForGoroutines(func(n int) bool { return n >= before+2*listeners }); !ok {
		t.Fatalf("goroutines while subscribed = %d, want at least %d", count, before+2*listeners)
	}

	cancel()

	// Every channel must close, which is the promise that readers rely on.
	timeout := time.After(leakWait)
	for i := 0; i < listeners; i++ {
		select {
		case _, open := <-samples[i]:
			if open {
				t.Fatalf("sample channel %d delivered a value instead of closing", i)
			}
		case <-timeout:
			t.Fatalf("sample channel %d was not closed after cancel", i)
		}
		select {
		case _, open := <-events[i]:
			if open {
				t.Fatalf("event channel %d delivered a value instead of closing", i)
			}
		case <-timeout:
			t.Fatalf("event channel %d was not closed after cancel", i)
		}
	}

	if count, ok := waitForGoroutines(func(n int) bool { return n <= before }); !ok {
		t.Fatalf("goroutines after cancel = %d, want at most %d, so %d leaked", count, before, count-before)
	}

	e.mu.RLock()
	left := len(e.subs) + len(e.eventSubs)
	e.mu.RUnlock()
	if left != 0 {
		t.Fatalf("engine still remembers %d listeners after cancel", left)
	}
}
