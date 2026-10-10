package chaos

import (
	"context"
	"fmt"
	"log/slog"
)

// RESTORE

// Restore puts saved simulations back into the engine before Run, and gives back what each one became.
func (e *Engine) Restore(ctx context.Context, sims []Simulation) ([]Simulation, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("restore: %w", err)
	}
	if e.running.Load() {
		return nil, fmt.Errorf("restore: %w", ErrAlreadyRunning)
	}
	now := e.now()

	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]Simulation, 0, len(sims))
	resumed, expired := 0, 0
	for _, saved := range sims {
		if saved.ID == "" {
			continue
		}
		if _, exists := e.sims[saved.ID]; exists {
			continue
		}

		// A copy keeps the engine's state apart from the slice the caller gave us.
		sim := saved
		if sim.State == StateRunning {
			if now.Before(sim.EndsAt) {
				resumed++
			} else {
				// The simulation ran out while the program was off, so it ended at its planned end.
				endedAt := sim.EndsAt
				sim.State, sim.EndedAt = StateResolved, &endedAt
				expired++
			}
		}
		e.sims[sim.ID] = &sim
		out = append(out, sim)
	}

	e.log.InfoContext(ctx, "simulations restored",
		slog.Int("resumed", resumed),
		slog.Int("expired_while_down", expired),
	)
	return out, nil
}
