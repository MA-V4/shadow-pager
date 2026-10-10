import type { MetricSample, Slo } from '../api/types'
import { breachingMetrics, metricDefs, metricKeys } from '../lib/metrics'
import { cn } from '../lib/utils'
import { MetricChart, MetricTile } from './MetricChart'

// SimulationCharts gives every breaching metric a big chart and folds the healthy ones into small tiles below.
export function SimulationCharts({ samples, slo }: { samples: readonly MetricSample[]; slo: Slo | undefined }) {
  const breaching = breachingMetrics(samples.at(-1))
  const healthy = metricKeys.filter((key) => !breaching.includes(key))

  return (
    <div className="flex flex-col gap-3">
      {breaching.length > 0 ? (
        <div className={cn('grid gap-3', breaching.length > 1 && 'xl:grid-cols-2')} aria-label="Metrics over their limit">
          {breaching.map((key) => (
            <MetricChart key={key} def={metricDefs[key]} samples={samples} threshold={slo ? metricDefs[key].threshold(slo) : undefined} />
          ))}
        </div>
      ) : null}
      {healthy.length > 0 ? (
        <div className="grid grid-cols-1 gap-3 min-[420px]:grid-cols-2 xl:grid-cols-4" aria-label="Metrics within their limit">
          {healthy.map((key) => (
            <MetricTile key={key} def={metricDefs[key]} samples={samples} threshold={slo ? metricDefs[key].threshold(slo) : undefined} />
          ))}
        </div>
      ) : null}
    </div>
  )
}
