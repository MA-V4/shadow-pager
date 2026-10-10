import { describe, expect, it } from 'vitest'

import { formatClock, niceStep, timeTicks, valueScale } from './chartScale'

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
    { name: 'breaching latency', dataMax: 950, threshold: 500, ticks: [0, 250, 500, 750, 1000] },
    { name: 'awkward latency', dataMax: 103, threshold: undefined, ticks: [0, 50, 100, 150] },
    { name: 'memory near its limit', dataMax: 2400, threshold: 2048, ticks: [0, 1000, 2000, 3000] },
    { name: 'cpu', dataMax: 95, threshold: 90, ticks: [0, 50, 100, 150] },
    { name: 'error rate as a share', dataMax: 0.012, threshold: 0.01, ticks: [0, 0.005, 0.01, 0.015] },
    { name: 'tiny error rate', dataMax: 0.002, threshold: undefined, ticks: [0, 0.0005, 0.001, 0.0015, 0.002] },
  ])('gives round ticks for $name', ({ dataMax, threshold, ticks }) => {
    const scale = valueScale(dataMax, threshold)
    expect(scale.ticks).toEqual(ticks)
    expect(scale.max).toBe(ticks.at(-1))
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
      expect(scale.ticks.length).toBeLessThanOrEqual(6)
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

describe('timeTicks', () => {
  const at = (iso: string) => new Date(iso).getTime()

  it('lands on round clock times inside the range', () => {
    const ticks = timeTicks(at('2026-01-02T15:04:07Z'), at('2026-01-02T15:05:02Z'))
    expect(ticks.map((t) => formatClock(t, 'UTC'))).toEqual(['15:04:15', '15:04:30', '15:04:45', '15:05:00'])
  })

  it('uses wider gaps for longer ranges and never more than five ticks', () => {
    const twoMinutes = timeTicks(at('2026-01-02T15:04:07Z'), at('2026-01-02T15:06:07Z'))
    expect(twoMinutes.map((t) => formatClock(t, 'UTC'))).toEqual(['15:04:30', '15:05:00', '15:05:30', '15:06:00'])

    const halfHour = timeTicks(at('2026-01-02T15:00:10Z'), at('2026-01-02T15:30:10Z'))
    expect(halfHour.length).toBeLessThanOrEqual(5)
    expect(halfHour.map((t) => formatClock(t, 'UTC'))).toEqual(['15:10:00', '15:20:00', '15:30:00'])
  })

  it('uses five second gaps for the first few samples', () => {
    const ticks = timeTicks(at('2026-01-02T15:04:01Z'), at('2026-01-02T15:04:12Z'))
    expect(ticks.map((t) => formatClock(t, 'UTC'))).toEqual(['15:04:05', '15:04:10'])
  })

  it('gives no ticks for an empty or backwards range', () => {
    expect(timeTicks(5, 5)).toEqual([])
    expect(timeTicks(10, 5)).toEqual([])
    expect(timeTicks(Number.NaN, 5)).toEqual([])
  })
})

describe('formatClock', () => {
  it('uses a 24 hour clock with seconds', () => {
    expect(formatClock(new Date('2026-01-02T00:04:05Z').getTime(), 'UTC')).toBe('00:04:05')
    expect(formatClock(new Date('2026-01-02T23:59:59Z').getTime(), 'UTC')).toBe('23:59:59')
  })
})
