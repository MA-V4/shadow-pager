import { describe, expect, it } from 'vitest'

import { failureModes } from '../api/types'
import { modeShapes, shapeCurves, shapePoints, type ShapeName } from './shapes'

const names: ShapeName[] = ['spike', 'gradual', 'leak', 'cliff']

describe('shape curves', () => {
  it('match the curves of the engine at the points that define them', () => {
    // A spike is up after a tenth of the time and holds until nine tenths.
    expect(shapeCurves.spike(0)).toBe(0)
    expect(shapeCurves.spike(0.1)).toBe(1)
    expect(shapeCurves.spike(0.5)).toBe(1)
    expect(shapeCurves.spike(1)).toBe(0)
    // A gradual failure is only half way up after a fifth of the time.
    expect(shapeCurves.gradual(0.2)).toBeCloseTo(0.5)
    expect(shapeCurves.gradual(0.4)).toBe(1)
    expect(shapeCurves.gradual(1)).toBe(0)
    // A leak grows in a straight line and never recovers.
    expect(shapeCurves.leak(0.25)).toBe(0.25)
    expect(shapeCurves.leak(1)).toBe(1)
    expect(shapeCurves.leak(2)).toBe(1)
    // A cliff looks fine for a while and then collapses.
    expect(shapeCurves.cliff(0.3)).toBe(0)
    expect(shapeCurves.cliff(0.4)).toBe(1)
    expect(shapeCurves.cliff(1)).toBe(0)
  })

  it('stay between zero and one', () => {
    for (const name of names) {
      for (let p = 0; p <= 1; p += 0.01) {
        expect(shapeCurves[name](p)).toBeGreaterThanOrEqual(0)
        expect(shapeCurves[name](p)).toBeLessThanOrEqual(1)
      }
    }
  })

  it('give every failure mode the curve the engine gives it', () => {
    expect(modeShapes).toEqual({
      latency_spike: 'spike',
      error_rate_surge: 'spike',
      memory_leak: 'leak',
      cpu_saturation: 'gradual',
      db_pool_exhaustion: 'cliff',
    })
    expect(Object.keys(modeShapes).sort()).toEqual([...failureModes].sort())
  })
})

describe('shapePoints', () => {
  it('fits every drawing inside its 40 by 16 box', () => {
    for (const name of names) {
      const points = shapePoints(name)
      expect(points).toHaveLength(41)
      for (const [x, y] of points) {
        expect(x).toBeGreaterThanOrEqual(1)
        expect(x).toBeLessThanOrEqual(39)
        expect(y).toBeGreaterThanOrEqual(1)
        expect(y).toBeLessThanOrEqual(15)
      }
    }
  })

  it('draws four different lines', () => {
    const drawings = names.map((name) => JSON.stringify(shapePoints(name)))
    expect(new Set(drawings).size).toBe(4)
  })
})
