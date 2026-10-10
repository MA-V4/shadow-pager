// Package status keeps a small, always current picture of how every service is doing.
package status

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
	"github.com/MA-V4/shadow-pager/internal/incident"
)

// LEVELS

// Level says how well a service is doing right now.
type Level string

const (
	Operational Level = "operational"
	Degraded    Level = "degraded"
	MajorOutage Level = "major_outage"
)

// levelRank gives every level a place in line, where a bigger number is worse.
var levelRank = map[Level]int{Operational: 0, Degraded: 1, MajorOutage: 2}

// worse gives back the worse one of two levels.
func worse(a, b Level) Level {
	if levelRank[b] > levelRank[a] {
		return b
	}
	return a
}

// INTERFACES

// sampleSource lists the only thing the aggregator needs the engine to do.
type sampleSource interface {
	Subscribe(ctx context.Context) (<-chan chaos.MetricSample, error)
}

// incidentLister lists the only thing the aggregator needs the incident manager to do.
type incidentLister interface {
	List(ctx context.Context) ([]incident.Incident, error)
}

// SNAPSHOT

// Service is how one service is doing at the moment of a snapshot.
type Service struct {
	Name   string
	Level  Level
	Sample *chaos.MetricSample // the newest numbers, or nil when nothing was measured lately
}

// Snapshot is the whole picture at one moment.
type Snapshot struct {
	Level     Level
	At        time.Time
	Services  []Service           // sorted by name
	Incidents []incident.Incident // only the ones people are still working on, newest first
}

// AGGREGATOR

const (
	defaultStaleAfter  = 15 * time.Second // numbers older than this no longer say anything about right now
	defaultForgetAfter = time.Hour        // a service that was quiet this long drops off the list
	defaultMaxServices = 200              // how many services the list holds at most
)

// measurement is the newest sample of one simulation and the moment it arrived.
type measurement struct {
	sample chaos.MetricSample
	seenAt time.Time
}

// Aggregator listens to the engine and remembers the newest numbers of every service.
type Aggregator struct {
	samples   <-chan chaos.MetricSample
	incidents incidentLister
	log       *slog.Logger
	now       func() time.Time

	staleAfter  time.Duration
	forgetAfter time.Duration
	maxServices int

	mu       sync.Mutex
	latest   map[chaos.SimulationID]measurement // one entry per simulation that sent numbers lately
	services map[string]time.Time               // every service we know, with the last time it sent numbers
}

// Option is a little helper that changes one setting when the aggregator is built.
type Option func(*Aggregator)

// WithClock lets tests give the aggregator a pretend clock.
func WithClock(now func() time.Time) Option {
	return func(a *Aggregator) { a.now = now }
}

// WithMaxServices changes how many services the list holds at most.
func WithMaxServices(n int) Option {
	return func(a *Aggregator) {
		if n > 0 {
			a.maxServices = n
		}
	}
}

// NewAggregator starts listening to the engine right away, so no sample can slip past before Run.
func NewAggregator(ctx context.Context, samples sampleSource, incidents incidentLister, logger *slog.Logger, opts ...Option) (*Aggregator, error) {
	if samples == nil || incidents == nil || logger == nil {
		return nil, errors.New("status: aggregator needs a sample source, an incident lister and a logger")
	}
	ch, err := samples.Subscribe(ctx)
	if err != nil {
		return nil, fmt.Errorf("subscribe status aggregator to samples: %w", err)
	}
	a := &Aggregator{
		samples:     ch,
		incidents:   incidents,
		log:         logger.With(slog.String("component", "status")),
		now:         time.Now,
		staleAfter:  defaultStaleAfter,
		forgetAfter: defaultForgetAfter,
		maxServices: defaultMaxServices,
		latest:      make(map[chaos.SimulationID]measurement),
		services:    make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// Run remembers samples until the context given to NewAggregator is done.
func (a *Aggregator) Run() {
	a.log.Info("status aggregator started")
	// Each sample only takes a moment to file away, so the engine never has to wait for us.
	for sample := range a.samples {
		a.record(sample)
	}
	a.log.Info("status aggregator stopped")
}

// record files one sample away as the newest numbers of its simulation and its service.
func (a *Aggregator) record(sample chaos.MetricSample) {
	now := a.now()

	a.mu.Lock()
	defer a.mu.Unlock()

	a.pruneLocked(now)
	if _, known := a.services[sample.Service]; !known && len(a.services) >= a.maxServices {
		a.forgetQuietestLocked()
	}
	a.services[sample.Service] = now
	a.latest[sample.SimulationID] = measurement{sample: sample, seenAt: now}
}

// pruneLocked throws away numbers that are too old to matter, and the caller must hold the lock.
func (a *Aggregator) pruneLocked(now time.Time) {
	for id, m := range a.latest {
		if now.Sub(m.seenAt) > a.staleAfter {
			delete(a.latest, id)
		}
	}
	for name, seenAt := range a.services {
		if now.Sub(seenAt) > a.forgetAfter {
			delete(a.services, name)
		}
	}
}

// forgetQuietestLocked drops the service that has been quiet the longest, and the caller must hold the lock.
func (a *Aggregator) forgetQuietestLocked() {
	var quietest string
	var oldest time.Time
	for name, seenAt := range a.services {
		if quietest == "" || seenAt.Before(oldest) {
			quietest, oldest = name, seenAt
		}
	}
	delete(a.services, quietest)
}

// Snapshot gives back how every known service is doing right now, together with the open incidents.
func (a *Aggregator) Snapshot(ctx context.Context) (Snapshot, error) {
	all, err := a.incidents.List(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("status snapshot: list incidents: %w", err)
	}
	var open []incident.Incident
	serious := make(map[string]bool) // services with an open incident that is critical or major
	hasIncident := make(map[string]bool)
	for _, inc := range all {
		if !inc.Status.Open() {
			continue
		}
		open = append(open, inc)
		hasIncident[inc.Service] = true
		if inc.Severity == chaos.SeverityCritical || inc.Severity == chaos.SeverityMajor {
			serious[inc.Service] = true
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].DeclaredAt.After(open[j].DeclaredAt) })

	now := a.now()
	a.mu.Lock()
	a.pruneLocked(now)
	current := make(map[string]chaos.MetricSample, len(a.latest))
	for _, m := range a.latest {
		if best, ok := current[m.sample.Service]; !ok || worseSample(m.sample, best) {
			current[m.sample.Service] = m.sample
		}
	}
	names := make(map[string]bool, len(a.services)+len(hasIncident))
	for name := range a.services {
		names[name] = true
	}
	a.mu.Unlock()
	// A service with an open incident stays on the list even when its numbers stopped coming.
	for name := range hasIncident {
		names[name] = true
	}

	snap := Snapshot{Level: Operational, At: now, Incidents: open}
	for name := range names {
		svc := Service{Name: name, Level: Operational}
		if sample, ok := current[name]; ok {
			svc.Sample = &sample
			if !sample.Healthy {
				svc.Level = Degraded
				if serious[name] {
					svc.Level = MajorOutage
				}
			}
		}
		snap.Level = worse(snap.Level, svc.Level)
		snap.Services = append(snap.Services, svc)
	}
	sort.Slice(snap.Services, func(i, j int) bool { return snap.Services[i].Name < snap.Services[j].Name })
	return snap, nil
}

// worseSample tells us if a describes a sicker service than b, so the worst simulation speaks for the service.
func worseSample(a, b chaos.MetricSample) bool {
	if a.Healthy != b.Healthy {
		return !a.Healthy
	}
	if len(a.Breaches) != len(b.Breaches) {
		return len(a.Breaches) > len(b.Breaches)
	}
	return a.Timestamp.After(b.Timestamp)
}
