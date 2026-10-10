// TEXT

// humanize turns a word like latency_spike into Latency spike.
export function humanize(value: string): string {
  const spaced = value.replaceAll('_', ' ')
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

// TIME

const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' })
const timeOnly = new Intl.DateTimeFormat(undefined, { timeStyle: 'medium' })

// formatDateTime shows a server time in the reader's own time zone, or a dash when the time is missing or broken.
export function formatDateTime(iso: string | undefined | null): string {
  const date = iso ? new Date(iso) : null
  return date && !Number.isNaN(date.getTime()) ? dateTime.format(date) : '-'
}

// formatTime shows only the clock part of a server time.
export function formatTime(iso: string | number): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? '-' : timeOnly.format(date)
}

// formatGoDuration makes a Go duration such as 1m30.5s easier to read, like 1m 30s.
export function formatGoDuration(duration: string | undefined | null): string {
  if (!duration) {
    return '-'
  }
  const parts = [...duration.matchAll(/(\d+(?:\.\d+)?)(h|ms|m|s|µs|us|ns)/g)]
  if (parts.length === 0) {
    return duration
  }
  const all = parts.map(([, amount, unit]) => ({ amount: Number(amount), unit: unit ?? '' }))
  const shown = all
    .filter(({ amount, unit }) => amount > 0 && (unit === 'h' || unit === 'm' || unit === 's'))
    .map(({ amount, unit }) => `${Math.round(amount)}${unit}`)
  if (shown.length > 0) {
    return shown.join(' ')
  }
  // Nothing as big as a second is left, so it is either exactly zero or just a moment.
  return all.some(({ amount }) => amount > 0) ? 'under 1s' : '0s'
}

// NUMBERS

// The page is written in English, so numbers use English separators for every reader.
const metricFormat = new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 })
const compactFormat = new Intl.NumberFormat('en-US', { notation: 'compact' })

// formatMetric shows a number with at most two digits after the dot.
export function formatMetric(value: number): string {
  return metricFormat.format(value)
}

// formatCompact shortens a big number for a chart axis, like 2K for 2000.
export function formatCompact(value: number): string {
  return compactFormat.format(value)
}

// formatPercent turns a share like 0.0123 into 1.23%.
export function formatPercent(share: number): string {
  return `${metricFormat.format(share * 100)}%`
}
