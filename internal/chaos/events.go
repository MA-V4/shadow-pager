package chaos

import (
	"context"
	"fmt"
	"log/slog"
)

// EVENTS

// EventType says what just happened to a simulation.
type EventType string

const (
	EventSimulationStarted  EventType = "simulation_started"
	EventSimulationResolved EventType = "simulation_resolved"
	EventSimulationHalted   EventType = "simulation_halted"
)

// Event is a note that a simulation started, finished by itself, or was stopped early.
type Event struct {
	Type       EventType
	Simulation Simulation
}

// eventBuffer is how many events can wait in line for each listener.
const eventBuffer = 256

// SubscribeEvents gives back a channel of lifecycle events that closes when ctx is done.
func (e *Engine) SubscribeEvents(ctx context.Context) (<-chan Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("subscribe events: %w", err)
	}

	ch := make(chan Event, eventBuffer)

	e.mu.Lock()
	id := e.nextEventSub
	e.nextEventSub++
	e.eventSubs[id] = ch
	e.mu.Unlock()

	go func() {
		<-ctx.Done()
		// We take the listener off the list first so nobody can send on a closed channel.
		e.mu.Lock()
		delete(e.eventSubs, id)
		e.mu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// emitLocked hands an event to every listener without waiting, and the caller must hold the lock.
func (e *Engine) emitLocked(ctx context.Context, ev Event) {
	for _, ch := range e.eventSubs {
		select {
		case ch <- ev:
		default:
			e.eventsDropped.Add(1)
			e.log.ErrorContext(ctx, "event dropped because a listener is full",
				slog.String("event", string(ev.Type)),
				slog.String("simulation_id", string(ev.Simulation.ID)),
			)
		}
	}
}
