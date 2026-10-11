import type { FailureMode, Incident, MetricSample, Simulation } from '../api/types'
import { metricDefs, type MetricKey } from './metrics'

// PEAKS

// The one number that tells the story of each failure mode best.
export const primaryMetric: Record<FailureMode, MetricKey> = {
  latency_spike: 'latency_p99',
  error_rate_surge: 'error_rate',
  memory_leak: 'memory',
  cpu_saturation: 'cpu',
  db_pool_exhaustion: 'latency_p99',
}

// Peaks holds the highest value this browser has seen of each simulation's main metric, keyed by simulation id.
export type Peaks = Readonly<Record<string, number>>

// addPeak gives back the peaks with one more sample taken into account.
export function addPeak(peaks: Peaks, sample: MetricSample): Peaks {
  const key = primaryMetric[sample.mode] ?? 'latency_p99'
  const value = metricDefs[key].pick(sample)
  const before = peaks[sample.simulation_id]
  return before !== undefined && before >= value ? peaks : { ...peaks, [sample.simulation_id]: value }
}

// keepPeaks forgets the peaks of simulations that are no longer listed, so memory does not grow forever.
export function keepPeaks(peaks: Peaks, ids: readonly string[]): Peaks {
  const wanted = new Set(ids)
  const kept = Object.entries(peaks).filter(([id]) => wanted.has(id))
  return kept.length === Object.keys(peaks).length ? peaks : Object.fromEntries(kept)
}

// formatPeak shows the peak of a simulation in the unit of its main metric, or a dash when nobody watched it.
export function formatPeak(simulation: Pick<Simulation, 'id' | 'mode'>, peaks: Peaks): string {
  const peak = peaks[simulation.id]
  const key = primaryMetric[simulation.mode] ?? 'latency_p99'
  return peak === undefined ? '-' : metricDefs[key].format(peak)
}

// OUTCOME

export interface Outcome {
  // True when the simulation broke its SLO, false when it did not, and null when we cannot tell.
  breached: boolean | null
  // The incident that the breach declared.
  incidentId?: string
  // True when a person stopped the simulation before its time was up.
  halted: boolean
}

// outcomeOf works out how a finished simulation ended, from the incident that points at it.
export function outcomeOf(simulation: Pick<Simulation, 'id' | 'state'>, incidents: readonly Incident[] | undefined): Outcome {
  const halted = simulation.state === 'halted'
  if (incidents === undefined) {
    return { breached: null, halted }
  }
  const incident = incidents.find((candidate) => candidate.simulation_id === simulation.id)
  return incident === undefined ? { breached: false, halted } : { breached: true, incidentId: incident.id, halted }
}

// outcomeLead is the first half of the outcome line, before the incident id.
export function outcomeLead(outcome: Outcome): string {
  if (outcome.breached === null) {
    return 'Outcome unknown'
  }
  return outcome.breached ? 'Breached SLO' : 'Stayed healthy'
}

// outcomeText puts the whole outcome into one line, such as "Breached SLO, incident inc_12".
export function outcomeText(outcome: Outcome): string {
  const incident = outcome.incidentId === undefined ? '' : `, incident ${outcome.incidentId}`
  return `${outcomeLead(outcome)}${incident}${outcome.halted ? ', halted early' : ''}`
}
