package chaos

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestScenarioValidateServiceName(t *testing.T) {
	tests := []struct {
		name    string
		service string
		wantErr bool
	}{
		{name: "plain name", service: "payments-api"},
		{name: "dots, underscores and capitals", service: "Checkout_v2.eu-west"},
		{name: "longest allowed", service: strings.Repeat("a", 63)},
		{name: "one too long", service: strings.Repeat("a", 64), wantErr: true},
		{name: "a megabyte of text", service: strings.Repeat("a", 1<<20), wantErr: true},
		{name: "space", service: "payments api", wantErr: true},
		{name: "backtick that would break out of code text in Slack", service: "pay`ments", wantErr: true},
		{name: "Slack mention", service: "<!channel>", wantErr: true},
		{name: "script tag", service: "<script>", wantErr: true},
		{name: "newline", service: "payments\napi", wantErr: true},
		{name: "letters outside plain ASCII", service: "zahlungen-ä", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := Scenario{
				Service:   tc.service,
				Mode:      FailureLatencySpike,
				Severity:  SeverityMajor,
				Intensity: 0.5,
				Duration:  time.Minute,
			}
			err := sc.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidScenario) {
				t.Errorf("error %v does not wrap ErrInvalidScenario", err)
			}
			// An error must never echo a huge or strange name back to the caller.
			if err != nil && len(err.Error()) > 200 {
				t.Errorf("error message is %d characters long, want it short", len(err.Error()))
			}
		})
	}
}
