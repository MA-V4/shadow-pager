import { describe, expect, it } from 'vitest'

import { formatClock, formatRelative, niceStep, relativeTicks, valueScale } from './chartScale'

describe('niceStep', () => {
  it.each([
    [0.9, 1],
    [1, 1],
    [1.3, 2],
    [2.2, 2.5],
    [3, 5],
    [7, 10],
    [103, 200],
    [240, 250],
    [600, 1000],
    [0.003, 0.005],
    [0.0012, 0.002],
  ])('rounds %d up to the natural step %d', (rough, want) => {
    expect(niceStep(rough)).toBe(want)
  })

  it('falls back to 1 for nonsense', () => {
    expect(niceStep(0)).toBe(1)
    expect(niceStep(-5)).toBe(1)
    expect(niceStep(Number.NaN)).toBe(1)
  })
})

describe('valueScale', () => {
  it.each([
    // The latency limit of 500 ms with healthy data still shows the limit with room above it.
    { name: 'healthy latency', dataMax: 130, threshold: 500, ticks: [0, 200, 400, 600] },
    { name: 'breaching latency', dataMax: 950, threshold: 500, ticks: [0, 500, 1000] },
    { name: 'awkward latency', dataMax: 103, threshold: undefined, ticks: [0, 50, 100, 150] },
    { name: 'memory near its limit', dataMax: 2400, threshold: 2048, ticks: [0, 1000, 2000, 3000] },
    { name: 'cpu', dataMax: 95, threshold: 90, ticks: [0, 50, 100, 150] },
    { name: 'error rate as a share', dataMax: 0.012, threshold: 0.01, ticks: [0, 0.005, 0.01, 0.015] },
    { name: 'tiny error rate', dataMax: 0.002, threshold: undefined, ticks: [0, 0.001, 0.002] },
  ])('gives round ticks for $name', ({ dataMax, threshold, ticks }) => {
    const scale = valueScale(dataMax, threshold)
    expect(scale.ticks).toEqual(ticks)
    expect(scale.max).toBe(ticks.at(-1))
  })

  it('never has more than three ticks above zero, whatever the data', () => {
    for (const dataMax of [0.0007, 0.3, 1, 7, 49, 51, 99, 101, 333, 999, 1001, 4321, 98765]) {
      for (const threshold of [undefined, 0.01, 90, 500, 2048]) {
        const scale = valueScale(dataMax, threshold)
        expect(scale.ticks[0]).toBe(0)
        expect(scale.ticks.length).toBeLessThanOrEqual(4)
        expect(scale.max).toBeGreaterThanOrEqual(dataMax)
      }
    }
  })

  it('always covers the data and the limit', () => {
    for (const [dataMax, threshold] of [
      [1, 500],
      [499, 500],
      [501, 500],
      [12345, 500],
      [0.5, 0.01],
    ] as const) {
      const scale = valueScale(dataMax, threshold)
      expect(scale.max).toBeGreaterThanOrEqual(dataMax)
      expect(scale.max).toBeGreaterThan(threshold)
      expect(scale.ticks[0]).toBe(0)
      expect(scale.ticks.length).toBeLessThanOrEqual(4)
    }
  })

  it('has no rounding dust in its ticks', () => {
    const scale = valueScale(0.031, 0.01)
    for (const tick of scale.ticks) {
      expect(String(tick).length).toBeLessThanOrEqual(6)
    }
  })

  it('still gives a usable axis with no data at all', () => {
    expect(valueScale(0)).toEqual({ max: 1, ticks: [0, 1] })
    expect(valueScale(Number.NaN)).toEqual({ max: 1, ticks: [0, 1] })
  })
})

describe('relativeTicks', () => {
  const at = (iso: string) => new Date(iso).getTime()
  const labels = (from: string, to: string) => relativeTicks(at(from), at(to)).map((t) => formatRelative(t, at(to)))

  it('counts back from now in round steps', () => {
    expect(labels('2026-01-02T15:04:00Z', '2026-01-02T15:06:00Z')).toEqual(['-120s', '-60s', 'now'])
    expect(labels('2026-01-02T15:04:07Z', '2026-01-02T15:05:02Z')).toEqual(['-30s', 'now'])
    expect(labels('2026-01-02T15:04:01Z', '2026-01-02T15:04:12Z')).toEqual(['-10s', '-5s', 'now'])
  })

  it('always ends on now and never has more than three ticks', () => {
    for (const seconds of [1, 4, 9, 14, 29, 44, 59, 61, 90, 119, 120, 600, 1800]) {
      const to = at('2026-01-02T15:30:00Z')
      const ticks = relativeTicks(to - seconds * 1000, to)
      expect(ticks.at(-1)).toBe(to)
      expect(ticks.length).toBeLessThanOrEqual(3)
      expect(ticks.every((t) => t >= to - seconds * 1000)).toBe(true)
    }
  })

  it('gives no ticks for an empty or backwards range', () => {
    expect(relativeTicks(5, 5)).toEqual([])
    expect(relativeTicks(10, 5)).toEqual([])
    expect(relativeTicks(Number.NaN, 5)).toEqual([])
  })
})

describe('formatRelative', () => {
  it.each([
    [0, 'now'],
    [-3, 'now'],
    [5, '-5s'],
    [60, '-60s'],
    [90, '-90s'],
    [300, '-5m'],
    [600, '-10m'],
  ])('shows %d seconds ago as %s', (seconds, want) => {
    expect(formatRelative(1_000_000 - seconds * 1000, 1_000_000)).toBe(want)
  })
})

describe('formatClock', () => {
  it('uses a 24 hour clock with seconds', () => {
    expect(formatClock(new Date('2026-01-02T00:04:05Z').getTime(), 'UTC')).toBe('00:04:05')
    expect(formatClock(new Date('2026-01-02T23:59:59Z').getTime(), 'UTC')).toBe('23:59:59')
  })
})
