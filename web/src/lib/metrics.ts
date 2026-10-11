import type { MetricSample, Slo } from '../api/types'

// METRICS

// The four numbers every simulation reports, named the way the API names them in its list of breaches.
export type MetricKey = 'latency_p99' | 'error_rate' | 'cpu' | 'memory'

export const metricKeys: readonly MetricKey[] = ['latency_p99', 'error_rate', 'cpu', 'memory']

const precise = new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 })

export interface MetricDef {
  key: MetricKey
  label: string
  // pick reads this metric out of one sample.
  pick: (sample: MetricSample) => number
  // threshold reads the limit for this metric out of the SLO.
  threshold: (slo: Slo) => number
  // format shows a value with its unit, such as 950 ms.
  format: (value: number) => string
}

// Units are joined to their number with a space that never breaks, so a value is never split over two lines.
const nbsp = ' '

export const metricDefs: Record<MetricKey, MetricDef> = {
  latency_p99: {
    key: 'latency_p99',
    label: 'Latency p99',
    pick: (s) => s.latency_p99_ms,
    threshold: (slo) => slo.max_latency_p99_ms,
    format: (v) => `${precise.format(v)}${nbsp}ms`,
  },
  error_rate: {
    key: 'error_rate',
    label: 'Error rate',
    pick: (s) => s.error_rate,
    threshold: (slo) => slo.max_error_rate,
    format: (v) => `${precise.format(v * 100)}%`,
  },
  cpu: {
    key: 'cpu',
    label: 'CPU',
    pick: (s) => s.cpu_percent,
    threshold: (slo) => slo.max_cpu_percent,
    format: (v) => `${precise.format(v)}%`,
  },
  memory: {
    key: 'memory',
    label: 'Memory',
    pick: (s) => s.memory_mb,
    threshold: (slo) => slo.max_memory_mb,
    format: (v) => `${precise.format(v)}${nbsp}MB`,
  },
}

// isMetricKey tells us if a breach name from the API is one of the four metrics we can draw.
export function isMetricKey(value: string): value is MetricKey {
  return (metricKeys as readonly string[]).includes(value)
}

// breachingMetrics gives back the metrics that the newest sample says are over their limit, in a fixed order.
export function breachingMetrics(latest: MetricSample | undefined): MetricKey[] {
  if (latest === undefined) {
    return []
  }
  return metricKeys.filter((key) => latest.breaches.includes(key))
}

// UNITS

// splitUnit cuts a formatted value such as "950 ms" into its number and its unit, so the unit can be drawn smaller.
export function splitUnit(formatted: string): [string, string] {
  const match = /^(.*\d)(\s*\D+)$/u.exec(formatted)
  return match ? [match[1] ?? formatted, match[2] ?? ''] : [formatted, '']
}

// tickLabel shows an axis value as a plain number, and only the top one carries the unit.
export function tickLabel(def: MetricDef, value: number, isTop: boolean): string {
  const formatted = def.format(value)
  return isTop ? formatted : splitUnit(formatted)[0]
}
