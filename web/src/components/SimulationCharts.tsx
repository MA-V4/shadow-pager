import type { MetricSample, Slo } from '../api/types'
import { formatMetric, formatPercent } from '../lib/format'
import { MetricChart, type MetricPoint } from './MetricChart'

// points picks one metric out of the samples and turns it into chart points.
function points(samples: readonly MetricSample[], pick: (sample: MetricSample) => number): MetricPoint[] {
  return samples.map((sample) => ({ t: new Date(sample.timestamp).getTime(), v: pick(sample) }))
}

// SimulationCharts draws the four key metrics of one simulation side by side.
export function SimulationCharts({ samples, slo }: { samples: readonly MetricSample[]; slo: Slo | undefined }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <MetricChart
        title="Latency p99"
        points={points(samples, (s) => s.latency_p99_ms)}
        threshold={slo?.max_latency_p99_ms}
        format={(v) => `${formatMetric(v)} ms`}
      />
      <MetricChart
        title="Error rate"
        points={points(samples, (s) => s.error_rate)}
        threshold={slo?.max_error_rate}
        format={formatPercent}
      />
      <MetricChart
        title="CPU"
        points={points(samples, (s) => s.cpu_percent)}
        threshold={slo?.max_cpu_percent}
        format={(v) => `${formatMetric(v)}%`}
      />
      <MetricChart
        title="Memory"
        points={points(samples, (s) => s.memory_mb)}
        threshold={slo?.max_memory_mb}
        format={(v) => `${formatMetric(v)} MB`}
      />
    </div>
  )
}
