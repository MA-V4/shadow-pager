import { useId } from 'react'
import { Area, CartesianGrid, ComposedChart, Line, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import type { MetricSample } from '../api/types'
import { axisTick, formatClock, formatRelative, relativeTicks, valueScale } from '../lib/chartScale'
import { tickLabel, type MetricDef } from '../lib/metrics'
import { Value } from './Value'

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
  // The SLO limit, drawn as a dashed line that is labelled in place.
  threshold: number | undefined
}

// LARGE CHART

// The chart is always this tall, which lets us work out where the SLO line sits in pixels.
const chartHeight = 120
const plotTop = 8
const axisHeight = 20
const plotBottom = chartHeight - axisHeight

// MetricChart is the big chart for a metric that is over its limit, where only the part above the SLO line is red.
export function MetricChart({ def, samples, threshold }: MetricChartProps) {
  const gradientId = useId().replaceAll(':', '')
  const points = toPoints(samples, def)
  const latest = points.at(-1)
  const first = points[0]
  const scale = valueScale(Math.max(0, ...points.map((p) => p.v)), threshold)
  const xTicks = first && latest ? relativeTicks(first.t, latest.t) : []
  // How far down the plot the SLO line sits, from 0 at the top to 1 at the bottom.
  const sloOffset = threshold === undefined ? 0 : Math.min(1, Math.max(0, 1 - threshold / scale.max))

  return (
    <figure className="flex min-w-0 flex-col gap-2" aria-label={describeMetric(def, latest?.v, threshold)}>
      <figcaption className="flex flex-wrap items-baseline justify-between gap-x-4">
        <span className="text-label text-secondary">
          {def.label}
          <span className="sr-only">, over the limit</span>
        </span>
        {latest === undefined ? (
          <span className="font-mono text-readout text-tertiary">-</span>
        ) : (
          <Value text={def.format(latest.v)} className="text-readout text-breach" unitClassName="text-body" />
        )}
      </figcaption>
      <div className="h-30 w-full" aria-hidden="true">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart data={points} margin={{ top: plotTop, right: 14, bottom: 0, left: 0 }}>
            {/* These are hard colour switches at the SLO line, not fades: red above it and grey below it. */}
            <defs>
              <linearGradient id={`${gradientId}-line`} gradientUnits="userSpaceOnUse" x1="0" y1={plotTop} x2="0" y2={plotBottom}>
                <stop offset={sloOffset} className="[stop-color:var(--signal-breach)]" />
                <stop offset={sloOffset} className="[stop-color:var(--chart-line)]" />
              </linearGradient>
              <linearGradient id={`${gradientId}-tint`} gradientUnits="userSpaceOnUse" x1="0" y1={plotTop} x2="0" y2={plotBottom}>
                <stop offset={sloOffset} className="[stop-color:var(--signal-breach)] [stop-opacity:0.1]" />
                <stop offset={sloOffset} className="[stop-color:var(--signal-breach)] [stop-opacity:0]" />
              </linearGradient>
            </defs>
            <CartesianGrid stroke="var(--border-subtle)" vertical={false} />
            <XAxis
              dataKey="t"
              type="number"
              domain={['dataMin', 'dataMax']}
              ticks={xTicks}
              interval={0}
              tickFormatter={(value: number) => formatRelative(value, latest?.t ?? value)}
              tick={axisTick}
              tickLine={false}
              axisLine={false}
              height={axisHeight}
            />
            <YAxis
              width={68}
              domain={[0, scale.max]}
              ticks={scale.ticks}
              interval={0}
              tickFormatter={(value: number) => tickLabel(def, value, value === scale.max)}
              tick={axisTick}
              tickLine={false}
              axisLine={false}
            />
            <Tooltip
              isAnimationActive={false}
              cursor={{ stroke: 'var(--text-tertiary)', strokeDasharray: '2 3' }}
              labelFormatter={(label) => formatClock(Number(label))}
              formatter={(value) => [def.format(Number(value)), def.label]}
              contentStyle={{
                background: 'var(--bg-raised)',
                border: '1px solid var(--border-subtle)',
                borderRadius: 6,
                boxShadow: 'var(--shadow-pop)',
                fontFamily: 'var(--font-mono)',
                fontSize: 12,
              }}
            />
            {threshold === undefined ? null : (
              <>
                <Area type="linear" dataKey="v" stroke="none" fill={`url(#${gradientId}-tint)`} fillOpacity={1} isAnimationActive={false} activeDot={false} />
                <ReferenceLine
                  y={threshold}
                  stroke="var(--signal-breach)"
                  strokeOpacity={0.6}
                  strokeWidth={1}
                  strokeDasharray="4 4"
                  label={{
                    value: `SLO ${def.format(threshold)}`,
                    // The newest value of a breaching metric is above the line, so the label sits just below it.
                    position: 'insideTopRight',
                    fontSize: 12,
                    fontFamily: 'var(--font-mono)',
                    fill: 'var(--text-secondary)',
                  }}
                />
              </>
            )}
            <Line
              type="linear"
              dataKey="v"
              stroke={threshold === undefined ? 'var(--chart-line)' : `url(#${gradientId}-line)`}
              strokeWidth={1.5}
              dot={false}
              isAnimationActive={false}
            />
          </ComposedChart>
        </ResponsiveContainer>
      </div>
    </figure>
  )
}

// SPARKLINE

const sparkWidth = 64
const sparkHeight = 20

// Sparkline is a 64 by 20 line of where a healthy metric has been, with no axes and no colour.
export function Sparkline({ points, max }: { points: readonly MetricPoint[]; max: number }) {
  const first = points[0]
  const last = points.at(-1)
  const span = first && last ? last.t - first.t : 0
  const line =
    first === undefined || span <= 0 || max <= 0
      ? ''
      : points
          .map((p) => {
            const x = 1 + ((p.t - first.t) / span) * (sparkWidth - 2)
            const y = sparkHeight - 1 - Math.min(1, Math.max(0, p.v / max)) * (sparkHeight - 2)
            return `${x.toFixed(1)},${y.toFixed(1)}`
          })
          .join(' ')
  return (
    <svg
      aria-hidden="true"
      viewBox={`0 0 ${sparkWidth} ${sparkHeight}`}
      width={sparkWidth}
      height={sparkHeight}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0 text-tertiary"
    >
      {line === '' ? <path d={`M1 ${sparkHeight / 2}H${sparkWidth - 1}`} strokeDasharray="1 4" /> : <polyline points={line} />}
    </svg>
  )
}

// COMPACT ROW

// MetricRow is the small form for a metric that is fine: its name, its value and a tiny line of where it has been.
export function MetricRow({ def, samples, threshold }: MetricChartProps) {
  const points = toPoints(samples, def)
  const latest = points.at(-1)
  const scale = valueScale(Math.max(0, ...points.map((p) => p.v)), threshold)

  return (
    <figure className="flex min-w-0 items-end justify-between gap-2 py-2" aria-label={describeMetric(def, latest?.v, threshold)}>
      <figcaption className="flex min-w-0 flex-col">
        <span className="text-meta whitespace-nowrap text-tertiary">{def.label}</span>
        {latest === undefined ? (
          <span className="text-body text-tertiary">Waiting…</span>
        ) : (
          <Value text={def.format(latest.v)} className="text-body text-primary" />
        )}
      </figcaption>
      {/* On a narrow phone the tiny line gives way before the number does. */}
      <div className="flex min-w-0 flex-1 justify-end overflow-hidden">
        <Sparkline points={points} max={scale.max} />
      </div>
    </figure>
  )
}
