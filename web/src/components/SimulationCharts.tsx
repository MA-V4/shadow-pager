import type { MetricSample, Slo } from '../api/types'
import { breachingMetrics, metricDefs, metricKeys } from '../lib/metrics'
import { cn } from '../lib/utils'
import { MetricChart, MetricRow } from './MetricChart'

// SimulationCharts gives every breaching metric a big chart and folds the healthy ones into compact rows below.
export function SimulationCharts({ samples, slo }: { samples: readonly MetricSample[]; slo: Slo | undefined }) {
  const breaching = breachingMetrics(samples.at(-1))
  const healthy = metricKeys.filter((key) => !breaching.includes(key))

  return (
    <div className="flex flex-col gap-4">
      {breaching.length > 0 ? (
        <div className={cn('grid gap-x-8 gap-y-4', breaching.length > 1 && 'lg:grid-cols-2')} aria-label="Metrics over their limit">
          {breaching.map((key) => (
            <MetricChart key={key} def={metricDefs[key]} samples={samples} threshold={slo ? metricDefs[key].threshold(slo) : undefined} />
          ))}
        </div>
      ) : null}
      {healthy.length > 0 ? (
        // On a phone the compact rows sit two to a line, and on wider screens all of them share one line.
        <div className="grid grid-cols-2 gap-x-6 border-t border-subtle pt-2 lg:grid-cols-4" aria-label="Metrics within their limit">
          {healthy.map((key) => (
            <MetricRow key={key} def={metricDefs[key]} samples={samples} threshold={slo ? metricDefs[key].threshold(slo) : undefined} />
          ))}
        </div>
      ) : null}
    </div>
  )
}
