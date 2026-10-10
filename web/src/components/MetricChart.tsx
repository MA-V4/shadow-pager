import { CartesianGrid, Line, LineChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import type { MetricSample } from '../api/types'
import { axisTick, formatClock, timeTicks, valueScale } from '../lib/chartScale'
import type { MetricDef } from '../lib/metrics'
import { StatusGlyph } from './StatusGlyph'

// MetricPoint is one moment on a chart.
export interface MetricPoint {
  // The time of the sample in milliseconds.
  t: number
  // The value of the metric at that time.
  v: number
}

// toPoints picks one metric out of the samples and turns it into chart points.
export function toPoints(samples: readonly MetricSample[], def: MetricDef): MetricPoint[] {
  return samples.map((sample) => ({ t: new Date(sample.timestamp).getTime(), v: def.pick(sample) }))
}

// describeMetric puts the newest value of a metric into one sentence, for screen readers and for tests.
export function describeMetric(def: MetricDef, latest: number | undefined, threshold: number | undefined): string {
  if (latest === undefined) {
    return `${def.label}: waiting for data`
  }
  if (threshold === undefined) {
    return `${def.label}: ${def.format(latest)}`
  }
  return `${def.label}: ${def.format(latest)}, ${latest > threshold ? 'above' : 'within'} the limit of ${def.format(threshold)}`
}

interface MetricChartProps {
  def: MetricDef
  samples: readonly MetricSample[]
  // The SLO limit, drawn as a dashed line with the breach zone shaded above it.
  threshold: number | undefined
}

// LARGE CHART

// MetricChart is the big chart for a metric that is over its limit, with a time axis, round ticks and the SLO line.
export function MetricChart({ def, samples, threshold }: MetricChartProps) {
  const points = toPoints(samples, def)
  const latest = points.at(-1)
  const first = points[0]
  const scale = valueScale(Math.max(0, ...points.map((p) => p.v)), threshold)
  const xTicks = first && latest ? timeTicks(first.t, latest.t) : []

  return (
    <figure className="flex min-w-0 flex-col gap-2 rounded-md border border-bad/40 bg-bad/[0.04] p-3 sm:p-4" aria-label={describeMetric(def, latest?.v, threshold)}>
      <figcaption className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <span className="flex items-center gap-2 text-sm font-medium">
          <StatusGlyph tone="bad" />
          {def.label}
          <span className="text-xs font-normal text-bad">Over the limit</span>
        </span>
        <span className="font-mono text-2xl font-semibold tabular-nums tracking-tight text-bad">
          {latest === undefined ? '-' : def.format(latest.v)}
        </span>
      </figcaption>
      <div className="h-48 w-full" aria-hidden="true">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={points} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
            <CartesianGrid stroke="var(--chart-grid)" vertical={false} />
            <XAxis
              dataKey="t"
              type="number"
              domain={['dataMin', 'dataMax']}
              ticks={xTicks}
              tickFormatter={(value: number) => formatClock(value)}
              tick={axisTick}
              tickLine={false}
              axisLine={{ stroke: 'var(--chart-grid)' }}
              height={24}
              minTickGap={24}
            />
            <YAxis
              width={52}
              domain={[0, scale.max]}
              ticks={scale.ticks}
              tickFormatter={(value: number) => def.formatTick(value)}
              tick={axisTick}
              tickLine={false}
              axisLine={false}
            />
            <Tooltip
              isAnimationActive={false}
              cursor={{ stroke: 'var(--muted-foreground)', strokeDasharray: '2 3' }}
              labelFormatter={(label) => formatClock(Number(label))}
              formatter={(value) => [def.format(Number(value)), def.label]}
              contentStyle={{
                background: 'var(--card)',
                border: '1px solid var(--border)',
                borderRadius: 6,
                fontSize: 12,
                fontVariantNumeric: 'tabular-nums',
              }}
            />
            {threshold === undefined ? null : (
              <>
                <ReferenceArea y1={threshold} y2={scale.max} fill="var(--bad)" fillOpacity={0.07} ifOverflow="hidden" />
                <ReferenceLine
                  y={threshold}
                  stroke="var(--bad)"
                  strokeDasharray="5 4"
                  label={{ value: `SLO ${def.format(threshold)}`, position: 'insideBottomRight', fontSize: 11, fill: 'var(--bad)' }}
                />
              </>
            )}
            <Line type="monotone" dataKey="v" stroke="var(--bad)" strokeWidth={2} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </figure>
  )
}

// COMPACT TILE

// MetricTile is the small form for a metric that is fine: its name, its value and a tiny line of where it has been.
export function MetricTile({ def, samples, threshold }: MetricChartProps) {
  const points = toPoints(samples, def)
  const latest = points.at(-1)
  const scale = valueScale(Math.max(0, ...points.map((p) => p.v)), threshold)

  return (
    <figure className="flex min-w-0 items-center justify-between gap-3 rounded-md border px-3 py-2.5" aria-label={describeMetric(def, latest?.v, threshold)}>
      <figcaption className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-xs text-muted-foreground">{def.label}</span>
        <span className="font-mono text-sm font-medium tabular-nums">{latest === undefined ? 'Waiting…' : def.format(latest.v)}</span>
        {latest === undefined ? null : (
          <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <StatusGlyph tone="ok" className="h-1.5 w-1.5" />
            Within limit
          </span>
        )}
      </figcaption>
      <div className="h-10 w-24 shrink-0 sm:w-28" aria-hidden="true">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={points} margin={{ top: 3, right: 2, bottom: 3, left: 2 }}>
            <XAxis dataKey="t" type="number" domain={['dataMin', 'dataMax']} hide />
            <YAxis domain={[0, scale.max]} hide />
            <Line type="monotone" dataKey="v" stroke="var(--chart-line)" strokeWidth={1.5} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </figure>
  )
}
