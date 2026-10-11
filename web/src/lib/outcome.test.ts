import { describe, expect, it } from 'vitest'

import type { Incident, MetricSample } from '../api/types'
import { addPeak, formatPeak, keepPeaks, outcomeLead, outcomeOf, outcomeText, primaryMetric } from './outcome'

// HELPERS

function incident(id: string, simulationId: string): Incident {
  return {
    id,
    simulation_id: simulationId,
    service: 'payments-api',
    mode: 'latency_spike',
    severity: 'major',
    status: 'resolved',
    breaches: ['latency_p99'],
    declared_at: '2026-01-02T15:04:05Z',
    timeline: [],
  }
}

function sample(simulationId: string, mode: MetricSample['mode'], latency: number, cpu: number): MetricSample {
  return {
    simulation_id: simulationId,
    service: 'payments-api',
    mode,
    timestamp: '2026-01-02T15:04:10Z',
    latency_p99_ms: latency,
    error_rate: 0.002,
    cpu_percent: cpu,
    memory_mb: 512,
    healthy: true,
    breaches: [],
  }
}

// TESTS

describe('outcomeOf', () => {
  const incidents = [incident('inc_12', 'sim_a')]

  it.each([
    { name: 'a breach that declared an incident', id: 'sim_a', state: 'resolved', text: 'Breached SLO, incident inc_12' },
    { name: 'a breach that was halted', id: 'sim_a', state: 'halted', text: 'Breached SLO, incident inc_12, halted early' },
    { name: 'a quiet run', id: 'sim_b', state: 'resolved', text: 'Stayed healthy' },
    { name: 'a quiet run that was halted', id: 'sim_b', state: 'halted', text: 'Stayed healthy, halted early' },
  ] as const)('describes $name', ({ id, state, text }) => {
    expect(outcomeText(outcomeOf({ id, state }, incidents))).toBe(text)
  })

  it('links the incident that points at the simulation', () => {
    expect(outcomeOf({ id: 'sim_a', state: 'resolved' }, incidents)).toEqual({ breached: true, incidentId: 'inc_12', halted: false })
    expect(outcomeOf({ id: 'sim_b', state: 'resolved' }, incidents)).toEqual({ breached: false, halted: false })
  })

  it('does not guess when the incidents could not be loaded', () => {
    const outcome = outcomeOf({ id: 'sim_a', state: 'resolved' }, undefined)
    expect(outcome.breached).toBeNull()
    expect(outcomeLead(outcome)).toBe('Outcome unknown')
    expect(outcomeText(outcomeOf({ id: 'sim_a', state: 'halted' }, undefined))).toBe('Outcome unknown, halted early')
  })
})

describe('peaks', () => {
  it('keeps the highest value of the metric that matters for the failure mode', () => {
    let peaks = {}
    peaks = addPeak(peaks, sample('sim_a', 'latency_spike', 300, 20))
    peaks = addPeak(peaks, sample('sim_a', 'latency_spike', 950, 10))
    peaks = addPeak(peaks, sample('sim_a', 'latency_spike', 400, 99))
    peaks = addPeak(peaks, sample('sim_b', 'cpu_saturation', 120, 87))
    expect(peaks).toEqual({ sim_a: 950, sim_b: 87 })
  })

  it('gives back the same object when a sample is not a new peak', () => {
    const peaks = addPeak({}, sample('sim_a', 'latency_spike', 950, 10))
    expect(addPeak(peaks, sample('sim_a', 'latency_spike', 100, 10))).toBe(peaks)
  })

  it('shows a peak in the unit of its metric, and a dash when nobody watched', () => {
    const peaks = { sim_a: 950, sim_b: 87 }
    expect(formatPeak({ id: 'sim_a', mode: 'latency_spike' }, peaks)).toBe(`950${String.fromCharCode(160)}ms`)
    expect(formatPeak({ id: 'sim_b', mode: 'cpu_saturation' }, peaks)).toBe('87%')
    expect(formatPeak({ id: 'sim_c', mode: 'memory_leak' }, peaks)).toBe('-')
  })

  it('forgets simulations that are no longer listed', () => {
    const peaks = { sim_a: 950, sim_b: 87 }
    expect(keepPeaks(peaks, ['sim_b'])).toEqual({ sim_b: 87 })
    expect(keepPeaks(peaks, ['sim_a', 'sim_b', 'sim_c'])).toBe(peaks)
  })

  it('has a main metric for every failure mode', () => {
    expect(primaryMetric).toEqual({
      latency_spike: 'latency_p99',
      error_rate_surge: 'error_rate',
      memory_leak: 'memory',
      cpu_saturation: 'cpu',
      db_pool_exhaustion: 'latency_p99',
    })
  })
})
