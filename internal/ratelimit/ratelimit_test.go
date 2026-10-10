package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// HELPERS

// fakeClock is a pretend clock that only moves when a test tells it to.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)}
}

// TESTS

func TestAllowBurstThenRefill(t *testing.T) {
	clock := newClock()
	l := New(3, 10*time.Second, WithClock(clock.Now))

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("1.2.3.4"); !ok {
			t.Fatalf("call %d was refused, want the first 3 allowed", i+1)
		}
	}

	ok, retry := l.Allow("1.2.3.4")
	if ok {
		t.Fatal("call 4 was allowed, want it refused after the burst")
	}
	if retry != 10*time.Second {
		t.Errorf("retry after = %s, want 10s for one full turn", retry)
	}

	// Part of the wait has passed, so the remaining wait is shorter.
	clock.Advance(4 * time.Second)
	if ok, retry := l.Allow("1.2.3.4"); ok || retry != 6*time.Second {
		t.Errorf("after 4s: allowed = %v, retry after = %s, want refused with 6s left", ok, retry)
	}

	clock.Advance(6 * time.Second)
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Error("call after a full interval was refused, want one turn earned back")
	}
	if ok, _ := l.Allow("1.2.3.4"); ok {
		t.Error("second call right after was allowed, want only one turn earned back")
	}
}

func TestAllowNeverSavesMoreThanTheBurst(t *testing.T) {
	clock := newClock()
	l := New(2, time.Second, WithClock(clock.Now))
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first call was refused")
	}

	clock.Advance(time.Hour)
	allowed := 0
	for i := 0; i < 10; i++ {
		if ok, _ := l.Allow("a"); ok {
			allowed++
		}
	}
	if allowed != 2 {
		t.Errorf("after a long rest %d calls were allowed, want the burst of 2", allowed)
	}
}

func TestCallersDoNotShareABucket(t *testing.T) {
	clock := newClock()
	l := New(1, time.Minute, WithClock(clock.Now))

	if ok, _ := l.Allow("alice"); !ok {
		t.Fatal("alice's first call was refused")
	}
	if ok, _ := l.Allow("alice"); ok {
		t.Error("alice's second call was allowed")
	}
	if ok, _ := l.Allow("bob"); !ok {
		t.Error("bob was refused because alice used her turn")
	}
}

func TestStaleCallersAreForgotten(t *testing.T) {
	clock := newClock()
	l := New(2, time.Second, WithClock(clock.Now))

	for i := 0; i < 100; i++ {
		l.Allow(fmt.Sprintf("10.0.0.%d", i))
	}
	if got := l.Len(); got != 100 {
		t.Fatalf("limiter remembers %d callers, want 100", got)
	}

	// Two seconds refill a whole bucket, so nobody from before is worth remembering.
	clock.Advance(3 * time.Second)
	l.Allow("fresh")
	if got := l.Len(); got != 1 {
		t.Errorf("limiter remembers %d callers after they went quiet, want only the fresh one", got)
	}
}

func TestActiveCallersSurviveTheSweep(t *testing.T) {
	clock := newClock()
	l := New(2, 10*time.Second, WithClock(clock.Now))

	l.Allow("busy")
	l.Allow("busy")
	clock.Advance(15 * time.Second)
	l.Allow("busy")
	clock.Advance(6 * time.Second)

	// The sweep runs now, and busy was seen six seconds ago with a nearly empty bucket.
	if ok, _ := l.Allow("busy"); !ok {
		t.Fatal("busy caller was refused, want the one turn it earned back")
	}
	if ok, retry := l.Allow("busy"); ok || retry <= 0 {
		t.Errorf("busy caller: allowed = %v, retry after = %s, want it limited because the sweep must not hand it a fresh bucket", ok, retry)
	}
}

func TestFullTableRefusesStrangers(t *testing.T) {
	clock := newClock()
	l := New(5, time.Minute, WithClock(clock.Now), WithMaxKeys(3))

	for _, key := range []string{"a", "b", "c"} {
		if ok, _ := l.Allow(key); !ok {
			t.Fatalf("caller %s was refused while there was room", key)
		}
	}
	if ok, retry := l.Allow("d"); ok || retry <= 0 {
		t.Errorf("a fourth caller: allowed = %v, retry after = %s, want it refused so memory stays bounded", ok, retry)
	}
	if ok, _ := l.Allow("a"); !ok {
		t.Error("a known caller was refused because the table is full")
	}
	if got := l.Len(); got != 3 {
		t.Errorf("limiter remembers %d callers, want at most 3", got)
	}
}

func TestNewFixesSillySettings(t *testing.T) {
	clock := newClock()
	l := New(0, 0, WithClock(clock.Now), WithMaxKeys(-5))

	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first call was refused, want a burst of at least 1")
	}
	if ok, retry := l.Allow("a"); ok || retry != time.Second {
		t.Errorf("second call: allowed = %v, retry after = %s, want refused with the 1s fallback", ok, retry)
	}
}

func TestAllowIsSafeForManyGoroutines(t *testing.T) {
	l := New(50, time.Hour)

	var wg sync.WaitGroup
	results := make(chan bool, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _ := l.Allow("shared")
			results <- ok
		}()
	}
	wg.Wait()
	close(results)

	allowed := 0
	for ok := range results {
		if ok {
			allowed++
		}
	}
	if allowed != 50 {
		t.Errorf("%d of 200 calls at once were allowed, want exactly the burst of 50", allowed)
	}
}
