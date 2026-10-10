import type { MetricSample } from '../api/types'

// SAMPLE HISTORY

// How many samples we keep per simulation, which is two minutes at one sample per second.
export const maxSamplesPerSimulation = 120

// SampleHistory holds the newest samples of every simulation, keyed by simulation id.
export type SampleHistory = Readonly<Record<string, readonly MetricSample[]>>

// addSample gives back a new history with one more sample, and drops the oldest when the list is full.
export function addSample(history: SampleHistory, sample: MetricSample): SampleHistory {
  const before = history[sample.simulation_id] ?? []
  return { ...history, [sample.simulation_id]: [...before, sample].slice(-maxSamplesPerSimulation) }
}

// keepOnly forgets the samples of simulations that are no longer wanted, so memory does not grow forever.
export function keepOnly(history: SampleHistory, ids: readonly string[]): SampleHistory {
  const wanted = new Set(ids)
  const kept = Object.entries(history).filter(([id]) => wanted.has(id))
  return kept.length === Object.keys(history).length ? history : Object.fromEntries(kept)
}
