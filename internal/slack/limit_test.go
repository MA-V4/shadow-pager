package slack

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/ratelimit"
)

// TESTS

func TestSlashCommandWriteLimit(t *testing.T) {
	clock := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	tl := newTestListener()
	WithWriteLimit(ratelimit.New(2, 30*time.Second, ratelimit.WithClock(func() time.Time { return clock })))(tl.Listener)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if reply := tl.routeCommand(ctx, slash("inject latency_spike payments-api")); !strings.Contains(reply, "Injected") {
			t.Fatalf("command %d = %q, want it to go through", i+1, reply)
		}
	}

	limited := tl.routeCommand(ctx, slash("inject latency_spike payments-api"))
	if want := ":hourglass_flowing_sand: That was a lot of commands, please try again in 30 seconds."; limited != want {
		t.Errorf("third command = %q, want %q", limited, want)
	}
	if reply := tl.routeCommand(ctx, slash("halt sim_1")); !strings.Contains(reply, "try again in") {
		t.Errorf("halt after the limit = %q, want it limited too because it shares the bucket", reply)
	}
	if len(tl.sims.injected) != 2 || len(tl.sims.halted) != 0 {
		t.Errorf("engine saw %d injects and %d halts, want 2 and 0", len(tl.sims.injected), len(tl.sims.halted))
	}

	// Looking around costs nothing, and another person has their own turns.
	for _, text := range []string{"list", "help", ""} {
		if reply := tl.routeCommand(ctx, slash(text)); strings.Contains(reply, "try again in") {
			t.Errorf("command %q was limited, want reading to stay free", text)
		}
	}
	other := slash("inject latency_spike payments-api")
	other.UserID = "U2"
	if reply := tl.routeCommand(ctx, other); !strings.Contains(reply, "Injected") {
		t.Errorf("another user = %q, want it to go through", reply)
	}
}

func TestSlashCommandsAreOpenWithoutALimiter(t *testing.T) {
	tl := newTestListener()
	WithWriteLimit(nil)(tl.Listener)

	for i := 0; i < 20; i++ {
		if reply := tl.routeCommand(context.Background(), slash("inject latency_spike payments-api")); !strings.Contains(reply, "Injected") {
			t.Fatalf("command %d = %q, want no limit when none is configured", i+1, reply)
		}
	}
}
