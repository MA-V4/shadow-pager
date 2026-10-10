import type { MetricSample, Simulation } from '../api/types'
import type { SampleHistory } from './samples'

// GROUPING

export interface SimulationView {
  simulation: Simulation
  samples: readonly MetricSample[]
  // True when the newest sample of this simulation is over at least one limit.
  breaching: boolean
}

export interface ServiceGroup {
  service: string
  simulations: SimulationView[]
  // True when at least one simulation on this service is breaching.
  breaching: boolean
}

// groupByService puts running simulations under their service, with the services that are breaching first.
export function groupByService(simulations: readonly Simulation[], history: SampleHistory): ServiceGroup[] {
  const groups = new Map<string, ServiceGroup>()
  for (const simulation of simulations) {
    const samples = history[simulation.id] ?? []
    const latest = samples.at(-1)
    const view: SimulationView = { simulation, samples, breaching: latest !== undefined && !latest.healthy }

    const group = groups.get(simulation.service) ?? { service: simulation.service, simulations: [], breaching: false }
    group.simulations.push(view)
    group.breaching = group.breaching || view.breaching
    groups.set(simulation.service, group)
  }

  const sorted = [...groups.values()]
  for (const group of sorted) {
    // Inside a service the breaching simulations come first, then the ones that started most recently.
    group.simulations.sort(
      (a, b) => Number(b.breaching) - Number(a.breaching) || b.simulation.started_at.localeCompare(a.simulation.started_at),
    )
  }
  return sorted.sort((a, b) => Number(b.breaching) - Number(a.breaching) || a.service.localeCompare(b.service))
}

// PROGRESS

export interface SimulationProgress {
  // How much of the planned time is over, from 0 to 1.
  fraction: number
  // How many milliseconds are left, never below zero.
  remainingMs: number
}

// progressOf works out how far along a simulation is at the given moment.
export function progressOf(simulation: Pick<Simulation, 'started_at' | 'ends_at'>, nowMs: number): SimulationProgress {
  const start = new Date(simulation.started_at).getTime()
  const end = new Date(simulation.ends_at).getTime()
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
    return { fraction: 1, remainingMs: 0 }
  }
  const fraction = Math.min(1, Math.max(0, (nowMs - start) / (end - start)))
  return { fraction, remainingMs: Math.max(0, end - nowMs) }
}

// formatRemaining turns milliseconds into a short countdown such as 1m 05s or 42s.
export function formatRemaining(ms: number): string {
  const totalSeconds = Math.max(0, Math.ceil(ms / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return minutes > 0 ? `${minutes}m ${String(seconds).padStart(2, '0')}s` : `${seconds}s`
}
