import { CartesianGrid, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { formatCompact, formatTime } from '../lib/format'
import { cn } from '../lib/utils'

// MetricPoint is one moment on a chart.
export interface MetricPoint {
  // The time of the sample in milliseconds.
  t: number
  // The value of the metric at that time.
  v: number
}

interface MetricChartProps {
  title: string
  points: MetricPoint[]
  // The SLO limit, drawn as a dashed line.
  threshold: number | undefined
  // format turns a value into text with its unit, for example 950 ms.
  format: (value: number) => string
}

// MetricChart draws one metric over time, with its SLO limit as a line to compare against.
export function MetricChart({ title, points, threshold, format }: MetricChartProps) {
  const latest = points.at(-1)
  const breaching = latest !== undefined && threshold !== undefined && latest.v > threshold

  // The summary puts the chart into words, for screen readers and for anyone who just wants the number.
  const summary =
    latest === undefined
      ? `${title}: waiting for data`
      : `${title}: ${format(latest.v)}${threshold === undefined ? '' : `, ${breaching ? 'above' : 'within'} the limit of ${format(threshold)}`}`

  return (
    <figure className="flex min-w-0 flex-col gap-1" aria-label={summary}>
      <figcaption className="flex items-baseline justify-between gap-2 text-sm">
        <span className="text-muted-foreground">{title}</span>
        <span className={cn('font-mono tabular-nums', breaching && 'font-semibold text-bad')}>
          {latest === undefined ? 'Waiting' : format(latest.v)}
        </span>
      </figcaption>
      <div className="h-32 w-full" aria-hidden="true">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={points} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid stroke="var(--chart-grid)" strokeDasharray="3 3" vertical={false} />
            <XAxis dataKey="t" type="number" domain={['dataMin', 'dataMax']} hide />
            <YAxis
              width={44}
              tick={{ fontSize: 10, fill: 'var(--muted-foreground)' }}
              tickLine={false}
              axisLine={false}
              // The limit always stays in view, so a healthy line visibly sits below it.
              domain={[0, (dataMax: number) => Math.max(dataMax, (threshold ?? 0) * 1.15)]}
              tickFormatter={(value: number) => formatCompact(value)}
            />
            <Tooltip
              isAnimationActive={false}
              labelFormatter={(label) => formatTime(Number(label))}
              formatter={(value) => [format(Number(value)), title]}
              contentStyle={{ background: 'var(--card)', border: '1px solid var(--border)', borderRadius: 8, fontSize: 12 }}
            />
            {threshold === undefined ? null : (
              <ReferenceLine y={threshold} stroke="var(--bad)" strokeDasharray="4 4" ifOverflow="extendDomain" />
            )}
            <Line type="monotone" dataKey="v" stroke="var(--chart-line)" strokeWidth={2} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </figure>
  )
}
