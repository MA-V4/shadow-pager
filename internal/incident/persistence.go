package incident

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// STORE

// Store is where the manager keeps incidents so they are still there after a restart.
type Store interface {
	// SaveIncident writes the whole incident, with every story line that is not saved yet.
	SaveIncident(ctx context.Context, inc Incident) error
	// AppendTimeline writes one story line, where seq is its place in the story starting at zero.
	AppendTimeline(ctx context.Context, id IncidentID, seq int, entry TimelineEntry) error
	// LoadOpenIncidents gives back the incidents the manager must know about after a restart.
	LoadOpenIncidents(ctx context.Context) ([]Incident, error)
}

// nopStore is a Store that remembers nothing, for when there is no database.
type nopStore struct{}

func (nopStore) SaveIncident(context.Context, Incident) error { return nil }
func (nopStore) AppendTimeline(context.Context, IncidentID, int, TimelineEntry) error {
	return nil
}
func (nopStore) LoadOpenIncidents(context.Context) ([]Incident, error) { return nil, nil }

// WithStore tells the manager where to save incidents.
func WithStore(store Store) Option {
	return func(m *Manager) {
		if store != nil {
			m.store = store
		}
	}
}

// WithPersistTimeout changes how long one save may take.
func WithPersistTimeout(d time.Duration) Option {
	return func(m *Manager) { m.persistTime = d }
}

// SAVING

const (
	persistQueueSize      = 256             // how many saves can wait for the store
	defaultPersistTimeout = 5 * time.Second // how long one save may take
)

// newBreachPrefix starts the story line that names newly broken limits.
const newBreachPrefix = "New SLO breach: "

// persistJob is one save waiting to be handed to the Store.
type persistJob struct {
	id          IncidentID
	appendEntry bool // false saves the whole incident, true only appends one story line
	seq         int
	entry       TimelineEntry
}

// persist puts a save in line for the store and never waits.
func (m *Manager) persist(ctx context.Context, job persistJob) {
	select {
	case m.persistJobs <- job:
	default:
		m.droppedWrites.Add(1)
		m.log.ErrorContext(ctx, "save dropped because the queue is full", slog.String("incident_id", string(job.id)))
	}
}

// persistLoop hands queued saves to the store one at a time, and writes the leftovers when told to stop.
func (m *Manager) persistLoop(ctx context.Context, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			for {
				select {
				case job := <-m.persistJobs:
					m.write(ctx, job)
				default:
					return
				}
			}
		case job := <-m.persistJobs:
			m.write(ctx, job)
		}
	}
}

// write does one save with a time limit, and only writes a log line when it fails.
func (m *Manager) write(ctx context.Context, job persistJob) {
	// The limit comes from a fresh clock, so the last saves still go through while the program shuts down.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.persistTime)
	defer cancel()

	var err error
	if job.appendEntry {
		if err = m.store.AppendTimeline(ctx, job.id, job.seq, job.entry); err != nil {
			err = fmt.Errorf("append timeline entry %d: %w", job.seq, err)
		}
	} else {
		// We save what the incident looks like right now, which is never older than what was asked for.
		m.mu.Lock()
		inc, ok := m.incidents[job.id]
		var snap Incident
		if ok {
			snap = inc.Clone()
		}
		m.mu.Unlock()
		if !ok {
			return
		}
		if err = m.store.SaveIncident(ctx, snap); err != nil {
			err = fmt.Errorf("save incident: %w", err)
		}
	}
	if err != nil {
		m.log.ErrorContext(ctx, "store failed, the incident lives on in memory",
			slog.String("incident_id", string(job.id)),
			slog.Any("error", err),
		)
	}
}

// RESTORE

// Restore loads saved incidents before Run, and closes the gap for simulations that ended while the program was off.
func (m *Manager) Restore(ctx context.Context, sims []chaos.Simulation) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("restore incidents: %w", err)
	}
	if m.running.Load() {
		return fmt.Errorf("restore incidents: %w", ErrAlreadyRunning)
	}

	saved, err := m.store.LoadOpenIncidents(ctx)
	if err != nil {
		return fmt.Errorf("restore incidents: load open incidents: %w", err)
	}
	now := m.now()

	m.mu.Lock()
	for _, sim := range sims {
		if sim.State == chaos.StateRunning {
			m.sims[sim.ID] = sim
		}
	}

	var mitigated []notification
	restored := 0
	for _, s := range saved {
		if _, exists := m.incidents[s.ID]; exists {
			continue
		}
		inc := s.Clone()
		m.incidents[inc.ID] = &inc
		m.bySim[inc.SimulationID] = inc.ID
		m.known[inc.ID] = knownBreaches(inc)
		restored++

		if _, running := m.sims[inc.SimulationID]; running {
			continue
		}
		// The simulation is over, so an incident that nobody mitigated yet is mitigated now.
		const text = "Simulation ended while the server was off, marked as mitigated"
		if err := inc.Transition(StatusMitigated, now, ActorSystem, text); err == nil {
			mitigated = append(mitigated, notification{kind: notifyStatusChanged, inc: inc.Clone()})
		}
	}
	m.mu.Unlock()

	// These wait in their queues until Run starts the workers.
	for _, job := range mitigated {
		m.persist(ctx, persistJob{id: job.inc.ID})
		m.enqueue(ctx, job)
	}
	m.log.InfoContext(ctx, "incidents restored",
		slog.Int("restored", restored),
		slog.Int("mitigated_while_down", len(mitigated)),
	)
	return nil
}

// knownBreaches rebuilds the list of broken limits we already told people about.
func knownBreaches(inc Incident) map[string]bool {
	known := make(map[string]bool, len(inc.Breaches))
	for _, b := range inc.Breaches {
		known[b] = true
	}
	for _, entry := range inc.Timeline {
		rest, ok := strings.CutPrefix(entry.Text, newBreachPrefix)
		if !ok {
			continue
		}
		for _, b := range strings.Split(rest, ", ") {
			known[b] = true
		}
	}
	return known
}
