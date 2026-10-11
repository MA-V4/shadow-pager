import type { FailureMode } from '../api/types'

// CURVES

// The four curves the engine uses to shape a failure over its lifetime, copied from internal/chaos/metrics.go.
export type ShapeName = 'spike' | 'gradual' | 'leak' | 'cliff'

function clamp(x: number, low: number, high: number): number {
  return Math.min(high, Math.max(low, x))
}

function smoothstep(edge0: number, edge1: number, x: number): number {
  const t = clamp((x - edge0) / (edge1 - edge0), 0, 1)
  return t * t * (3 - 2 * t)
}

// Each curve takes the progress of a simulation from 0 to 1 and gives back how strong the failure is, from 0 to 1.
export const shapeCurves: Record<ShapeName, (progress: number) => number> = {
  spike: (p) => smoothstep(0, 0.1, p) * (1 - smoothstep(0.9, 1, p)),
  gradual: (p) => smoothstep(0, 0.4, p) * (1 - smoothstep(0.85, 1, p)),
  leak: (p) => clamp(p, 0, 1),
  cliff: (p) => smoothstep(0.3, 0.4, p) * (1 - smoothstep(0.9, 1, p)),
}

// Which curve each failure mode follows in the engine, where two modes share the spike.
export const modeShapes: Record<FailureMode, ShapeName> = {
  latency_spike: 'spike',
  error_rate_surge: 'spike',
  memory_leak: 'leak',
  cpu_saturation: 'gradual',
  db_pool_exhaustion: 'cliff',
}

// DRAWING

// shapePoints samples a curve into the points of a tiny line drawing that fits a box of the given size.
export function shapePoints(shape: ShapeName, width = 40, height = 16, steps = 40): [number, number][] {
  // The line is kept one pixel away from every edge, so its round ends are never cut off.
  const pad = 1
  const curve = shapeCurves[shape]
  return Array.from({ length: steps + 1 }, (_, i) => {
    const p = i / steps
    const x = pad + p * (width - 2 * pad)
    const y = height - pad - curve(p) * (height - 2 * pad)
    return [Number(x.toFixed(2)), Number(y.toFixed(2))]
  })
}
