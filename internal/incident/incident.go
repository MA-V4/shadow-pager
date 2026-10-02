// Package incident keeps track of incidents from the moment they start until a person closes them.
package incident

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// ERRORS

var (
	ErrNotFound          = errors.New("incident not found")
	ErrInvalidTransition = errors.New("invalid incident status change")
)

// STATUS

// Status is the step an incident has reached.
type Status string

const (
	StatusDeclared     Status = "declared"
	StatusAcknowledged Status = "acknowledged"
	StatusMitigated    Status = "mitigated"
	StatusResolved     Status = "resolved"
)

// statusOrder gives every status a place in line, and an incident only ever moves forward.
var statusOrder = map[Status]int{
	StatusDeclared:     1,
	StatusAcknowledged: 2,
	StatusMitigated:    3,
	StatusResolved:     4,
}

// Valid tells us if this is a status we know.
func (s Status) Valid() bool {
	_, ok := statusOrder[s]
	return ok
}

// Open tells us if people still have work to do on the incident.
func (s Status) Open() bool { return s != StatusResolved }

// CanTransitionTo tells us if an incident may move from this status to the next one.
func (s Status) CanTransitionTo(next Status) bool {
	return s.Valid() && next.Valid() && statusOrder[next] > statusOrder[s]
}

// INCIDENT

// IncidentID is the special name of one incident.
type IncidentID string

// ActorSystem is the name we write down when the program acts by itself.
const ActorSystem = "shadow-pager"

// TimelineEntry is one line in the story of an incident.
type TimelineEntry struct {
	At    time.Time
	Actor string
	Text  string
}

// Incident is one problem that people are working on.
type Incident struct {
	ID             IncidentID
	SimulationID   chaos.SimulationID
	Service        string
	Mode           chaos.FailureMode
	Severity       chaos.Severity
	Status         Status
	Breaches       []string // the limits that were broken when the incident started
	DeclaredAt     time.Time
	AcknowledgedAt *time.Time
	AcknowledgedBy string
	MitigatedAt    *time.Time
	ResolvedAt     *time.Time
	Timeline       []TimelineEntry
	ChannelID      string // the Slack channel for this incident, empty when there is none
}

// New starts an incident for a simulation that broke its limits.
func New(id IncidentID, sim chaos.Simulation, breaches []string, at time.Time) Incident {
	inc := Incident{
		ID:           id,
		SimulationID: sim.ID,
		Service:      sim.Scenario.Service,
		Mode:         sim.Scenario.Mode,
		Severity:     sim.Scenario.Severity,
		Status:       StatusDeclared,
		Breaches:     slices.Clone(breaches),
		DeclaredAt:   at,
	}
	inc.AddEntry(at, ActorSystem, "Incident declared")
	return inc
}

// AddEntry writes one more line into the story of the incident.
func (inc *Incident) AddEntry(at time.Time, actor, text string) TimelineEntry {
	entry := TimelineEntry{At: at, Actor: actor, Text: text}
	inc.Timeline = append(inc.Timeline, entry)
	return entry
}

// Transition moves the incident to the next status and writes it into the story.
func (inc *Incident) Transition(next Status, at time.Time, actor, text string) error {
	if !inc.Status.CanTransitionTo(next) {
		return fmt.Errorf("incident %s from %s to %s: %w", inc.ID, inc.Status, next, ErrInvalidTransition)
	}

	inc.Status = next
	switch next {
	case StatusAcknowledged:
		inc.AcknowledgedAt, inc.AcknowledgedBy = &at, actor
	case StatusMitigated:
		inc.MitigatedAt = &at
	case StatusResolved:
		inc.ResolvedAt = &at
	}
	inc.AddEntry(at, actor, text)
	return nil
}

// TimeToAcknowledge tells us how long it took for a person to say they were on it.
func (inc Incident) TimeToAcknowledge() (time.Duration, bool) {
	if inc.AcknowledgedAt == nil {
		return 0, false
	}
	return inc.AcknowledgedAt.Sub(inc.DeclaredAt), true
}

// TimeToResolve tells us how long the incident lasted from start to finish.
func (inc Incident) TimeToResolve() (time.Duration, bool) {
	if inc.ResolvedAt == nil {
		return 0, false
	}
	return inc.ResolvedAt.Sub(inc.DeclaredAt), true
}

// Clone makes a full copy so nobody can change the original by accident.
func (inc Incident) Clone() Incident {
	out := inc
	out.Breaches = slices.Clone(inc.Breaches)
	out.Timeline = slices.Clone(inc.Timeline)
	out.AcknowledgedAt = cloneTime(inc.AcknowledgedAt)
	out.MitigatedAt = cloneTime(inc.MitigatedAt)
	out.ResolvedAt = cloneTime(inc.ResolvedAt)
	return out
}

// cloneTime copies a time that might be missing.
func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}
