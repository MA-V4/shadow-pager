// VALUE AXIS

// The step sizes a person finds natural: 1, 2, 2.5 and 5, times any power of ten.
const niceSteps = [1, 2, 2.5, 5, 10]

// tidy removes the tiny rounding dust that adding decimals in JavaScript leaves behind.
function tidy(value: number): number {
  return Number(value.toPrecision(12))
}

// niceStep rounds a rough step up to the nearest natural one, so ticks land on numbers like 250 and never on 103.
export function niceStep(rough: number): number {
  if (!Number.isFinite(rough) || rough <= 0) {
    return 1
  }
  const magnitude = 10 ** Math.floor(Math.log10(rough))
  const fraction = rough / magnitude
  const step = niceSteps.find((candidate) => fraction <= candidate + 1e-9) ?? 10
  return tidy(step * magnitude)
}

export interface ValueScale {
  // The top of the axis, which is always one of the ticks.
  max: number
  // Evenly spaced round numbers from zero up to max.
  ticks: number[]
}

// valueScale builds a value axis from zero that fits the data and the SLO limit, with at most three round ticks above zero.
export function valueScale(dataMax: number, threshold?: number, targetTicks = 3): ValueScale {
  // The limit gets some air above it, so its line never sits on the very edge of the chart.
  const top = Math.max(Number.isFinite(dataMax) ? dataMax : 0, (threshold ?? 0) * 1.2)
  if (top <= 0) {
    return { max: 1, ticks: [0, 1] }
  }
  const step = niceStep(top / targetTicks)
  const count = Math.max(1, Math.ceil(top / step - 1e-9))
  const ticks = Array.from({ length: count + 1 }, (_, i) => tidy(i * step))
  return { max: tidy(count * step), ticks }
}

// TIME AXIS

// The gaps between time ticks that read well, in seconds.
const timeSteps = [5, 10, 15, 30, 60, 120, 300, 600]

// relativeTicks picks at most three moments to label on a time axis: now, and round steps back from it.
export function relativeTicks(fromMs: number, toMs: number, maxTicks = 3): number[] {
  if (!Number.isFinite(fromMs) || !Number.isFinite(toMs) || toMs <= fromMs) {
    return []
  }
  const spanSeconds = (toMs - fromMs) / 1000
  const stepSeconds = timeSteps.find((step) => spanSeconds / step < maxTicks) ?? 600
  const ticks: number[] = []
  for (let t = toMs; t >= fromMs && ticks.length < maxTicks; t -= stepSeconds * 1000) {
    ticks.unshift(t)
  }
  return ticks
}

// formatRelative shows a moment as its distance from now, such as -60s or -2m, and now itself as "now".
export function formatRelative(ms: number, nowMs: number): string {
  const seconds = Math.round((nowMs - ms) / 1000)
  if (seconds <= 0) {
    return 'now'
  }
  // Charts hold two minutes of samples, so minutes are only used for spans well beyond that.
  return seconds > 180 && seconds % 60 === 0 ? `-${seconds / 60}m` : `-${seconds}s`
}

// formatClock shows a moment as hours, minutes and seconds on a 24 hour clock.
export function formatClock(ms: number, timeZone?: string): string {
  return new Intl.DateTimeFormat('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
    ...(timeZone === undefined ? {} : { timeZone }),
  }).format(ms)
}

// SLO LABEL

// How much room the SLO label needs next to its line: its gap, one line of 12px text, and a little air.
export const sloLabelRoom = 20

// sloLabelSide says where the SLO label goes so that it always stays inside the plot and never reaches an axis label.
export function sloLabelSide(sloY: number, plotTop: number, plotBottom: number, room = sloLabelRoom): 'below' | 'above' {
  // Below the line is the first choice, because a breaching metric draws its newest value above it.
  if (sloY + room <= plotBottom) {
    return 'below'
  }
  // Too close to the time axis, so the label moves above the line, where the plot always has room.
  return sloY - room >= plotTop ? 'above' : 'below'
}

// SPARKLINE

export interface SparkPoint {
  // The time of the value in milliseconds.
  t: number
  // The value at that time.
  v: number
}

// sparklinePoints turns values into the points of a tiny line that uses its own range, so small changes can be seen.
export function sparklinePoints(points: readonly SparkPoint[], width: number, height: number): string {
  const first = points[0]
  const last = points.at(-1)
  if (first === undefined || last === undefined || last.t <= first.t) {
    return ''
  }
  const values = points.map((p) => p.v)
  const min = Math.min(...values)
  const max = Math.max(...values)
  // The band is never thinner than half the value, so the usual wobble of a calm metric does not look like a storm.
  const band = Math.max(max - min, Math.abs(max) * 0.5, Number.EPSILON)
  const low = (min + max) / 2 - band / 2
  // One and a half pixels are kept free on every side, so the round line ends are never cut off.
  const pad = 1.5
  return points
    .map((p) => {
      const x = pad + ((p.t - first.t) / (last.t - first.t)) * (width - 2 * pad)
      const y = height - pad - ((p.v - low) / band) * (height - 2 * pad)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

// SHARED STYLE

// Every chart uses this one text style on its axes, so all charts read as one family.
export const axisTick = {
  fontSize: 12,
  fontFamily: 'var(--font-mono)',
  fill: 'var(--text-tertiary)',
} as const
