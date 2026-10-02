package chaos

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestScenarioValidate(t *testing.T) {
	valid := Scenario{
		Service:   "payments-api",
		Mode:      FailureLatencySpike,
		Severity:  SeverityMajor,
		Intensity: 0.7,
		Duration:  2 * time.Minute,
	}

	tests := []struct {
		name    string
		mutate  func(*Scenario)
		wantErr bool
	}{
		{"valid", func(*Scenario) {}, false},
		{"missing service", func(s *Scenario) { s.Service = "" }, true},
		{"unknown mode", func(s *Scenario) { s.Mode = "meteor_strike" }, true},
		{"unknown severity", func(s *Scenario) { s.Severity = "spicy" }, true},
		{"intensity too high", func(s *Scenario) { s.Intensity = 1.5 }, true},
		{"duration too short", func(s *Scenario) { s.Duration = time.Second }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := valid
			tt.mutate(&sc)
			err := sc.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidScenario) {
				t.Fatalf("error %v does not wrap ErrInvalidScenario", err)
			}
		})
	}
}

func TestSubscribeClosesOnCancel(t *testing.T) {
	e, err := NewEngine(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := e.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel, got a sample")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription channel was not closed after cancel")
	}
}
