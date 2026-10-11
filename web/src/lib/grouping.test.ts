import { describe, expect, it } from 'vitest'

import type { MetricSample, Simulation } from '../api/types'
import { formatRemaining, groupByService, progressOf } from './grouping'

// HELPERS

function simulation(id: string, service: string, startedAt = '2026-01-02T15:04:05Z'): Simulation {
  return {
    id,
    service,
    mode: 'latency_spike',
    severity: 'major',
    intensity: 0.9,
    duration: '2m0s',
    state: 'running',
    started_at: startedAt,
    ends_at: '2026-01-02T15:06:05Z',
    public: false,
  }
}

function sample(simulationId: string, healthy: boolean): MetricSample {
  return {
    simulation_id: simulationId,
    service: 'unused',
    mode: 'latency_spike',
    timestamp: '2026-01-02T15:04:10Z',
    latency_p99_ms: healthy ? 120 : 950,
    error_rate: 0.002,
    cpu_percent: 30,
    memory_mb: 512,
    healthy,
    breaches: healthy ? [] : ['latency_p99'],
  }
}

// TESTS

describe('groupByService', () => {
  it('puts simulations under their service, with breaching services first and the rest by name', () => {
    const groups = groupByService(
      [simulation('sim_a', 'search'), simulation('sim_b', 'payments-api'), simulation('sim_c', 'checkout'), simulation('sim_d', 'payments-api')],
      { sim_a: [sample('sim_a', true)], sim_b: [sample('sim_b', true)], sim_d: [sample('sim_d', true), sample('sim_d', false)] },
    )

    expect(groups.map((g) => g.service)).toEqual(['payments-api', 'checkout', 'search'])
    expect(groups.map((g) => g.breaching)).toEqual([true, false, false])
    expect(groups[0]?.simulations.map((s) => s.simulation.id)).toEqual(['sim_d', 'sim_b'])
    expect(groups[0]?.simulations.map((s) => s.breaching)).toEqual([true, false])
  })

  it('judges a simulation by its newest sample only', () => {
    const recovered = groupByService([simulation('sim_a', 'search')], { sim_a: [sample('sim_a', false), sample('sim_a', true)] })
    expect(recovered[0]?.breaching).toBe(false)
  })

  it('treats a simulation without samples as not breaching and keeps its empty history', () => {
    const groups = groupByService([simulation('sim_a', 'search')], {})
    expect(groups).toHaveLength(1)
    expect(groups[0]?.breaching).toBe(false)
    expect(groups[0]?.simulations[0]?.samples).toEqual([])
  })

  it('orders simulations of one service by newest start when none is breaching', () => {
    const groups = groupByService(
      [simulation('sim_old', 'search', '2026-01-02T15:00:00Z'), simulation('sim_new', 'search', '2026-01-02T15:05:00Z')],
      {},
    )
    expect(groups[0]?.simulations.map((s) => s.simulation.id)).toEqual(['sim_new', 'sim_old'])
  })

  it('gives nothing back for nothing', () => {
    expect(groupByService([], {})).toEqual([])
  })
})

describe('progressOf', () => {
  const sim = simulation('sim_a', 'search')
  const at = (iso: string) => new Date(iso).getTime()

  it.each([
    ['before the start', '2026-01-02T15:00:00Z', 0, 365_000],
    ['at the start', '2026-01-02T15:04:05Z', 0, 120_000],
    ['a quarter in', '2026-01-02T15:04:35Z', 0.25, 90_000],
    ['at the end', '2026-01-02T15:06:05Z', 1, 0],
    ['after the end', '2026-01-02T15:10:00Z', 1, 0],
  ])('works out the progress %s', (_name, now, fraction, remainingMs) => {
    expect(progressOf(sim, at(now))).toEqual({ fraction, remainingMs })
  })

  it('calls a simulation with broken times finished', () => {
    expect(progressOf({ started_at: 'garbage', ends_at: 'garbage' }, 0)).toEqual({ fraction: 1, remainingMs: 0 })
    expect(progressOf({ started_at: sim.ends_at, ends_at: sim.started_at }, 0)).toEqual({ fraction: 1, remainingMs: 0 })
  })
})

describe('formatRemaining', () => {
  it.each([
    [0, '0s'],
    [400, '1s'],
    [42_000, '42s'],
    [60_000, '1m 00s'],
    [65_000, '1m 05s'],
    [1_799_500, '30m 00s'],
    [-5_000, '0s'],
  ])('shows %d ms as %s', (ms, want) => {
    expect(formatRemaining(ms)).toBe(want)
  })
})
