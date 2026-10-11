import { afterEach, describe, expect, it, vi } from 'vitest'

import { connectionOf } from './connection'
import { formatAgo, formatDateTime, formatElapsed, formatGoDuration, formatPercent, humanize } from './format'
import { incidentDuration } from './incidents'
import { sortedPages, type PageModule } from './routes'
import { readStored, writeStored } from './storage'
import { cn } from './utils'

describe('format', () => {
  it('turns machine words into readable ones', () => {
    expect(humanize('latency_spike')).toBe('Latency spike')
    expect(humanize('major_outage')).toBe('Major outage')
    expect(humanize('running')).toBe('Running')
  })

  it.each([
    ['2m0s', '2m'],
    ['1m30s', '1m 30s'],
    ['1h0m0s', '1h'],
    ['1h2m3s', '1h 2m 3s'],
    ['45.6s', '46s'],
    ['0s', '0s'],
    ['250ms', 'under 1s'],
    ['not a duration', 'not a duration'],
  ])('shows the Go duration %s as %s', (input, want) => {
    expect(formatGoDuration(input)).toBe(want)
  })

  it('shows a dash for a missing duration or time', () => {
    expect(formatGoDuration(undefined)).toBe('-')
    expect(formatDateTime(undefined)).toBe('-')
    expect(formatDateTime('garbage')).toBe('-')
  })

  it('turns a share into a percentage', () => {
    expect(formatPercent(0.0123)).toBe('1.23%')
    expect(formatPercent(0)).toBe('0%')
  })
})

describe('relative time', () => {
  const now = new Date('2026-01-02T15:10:00Z').getTime()

  it.each([
    ['2026-01-02T15:09:58Z', 'just now'],
    ['2026-01-02T15:09:18Z', '42s ago'],
    ['2026-01-02T15:08:00Z', '2m ago'],
    ['2026-01-02T12:09:00Z', '3h ago'],
    ['2025-12-29T15:10:00Z', '4d ago'],
    ['2026-01-02T15:11:00Z', 'just now'],
  ])('says %s was %s', (iso, want) => {
    expect(formatAgo(iso, now)).toBe(want)
  })

  it('shows a dash for a missing or broken time', () => {
    expect(formatAgo(undefined, now)).toBe('-')
    expect(formatAgo('garbage', now)).toBe('-')
  })

  it.each([
    [0, '0s'],
    [45_400, '45s'],
    [120_000, '2m'],
    [125_000, '2m 5s'],
    [3_600_000, '1h'],
    [3_725_000, '1h 2m'],
  ])('shows %d ms as %s', (ms, want) => {
    expect(formatElapsed(ms)).toBe(want)
  })

  it('shows a dash for a length of time that makes no sense', () => {
    expect(formatElapsed(-1)).toBe('-')
    expect(formatElapsed(Number.NaN)).toBe('-')
  })

  it('counts an open incident up and shows the final time of a resolved one', () => {
    expect(incidentDuration({ declared_at: '2026-01-02T15:08:30Z' }, now)).toBe('1m 30s')
    expect(incidentDuration({ declared_at: '2026-01-02T15:08:30Z', time_to_resolve: '5m0s' }, now)).toBe('5m')
  })
})

describe('connectionOf', () => {
  it.each([
    ['ready', 'open', 'live'],
    ['ready', 'connecting', 'connecting'],
    ['ready', 'reconnecting', 'reconnecting'],
    ['loading', 'open', 'live'],
    ['waking', 'open', 'waking'],
    ['waking', 'reconnecting', 'waking'],
    ['error', 'open', 'offline'],
    ['ready', undefined, 'live'],
    ['loading', undefined, 'connecting'],
    ['waking', undefined, 'waking'],
    ['error', undefined, 'offline'],
  ] as const)('reads data %s and stream %s as %s', (phase, stream, want) => {
    expect(connectionOf(phase, stream)).toBe(want)
  })
})

describe('storage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.localStorage.clear()
  })

  it('saves, reads and removes a value', () => {
    writeStored('k', 'v')
    expect(readStored('k')).toBe('v')
    writeStored('k', '')
    expect(readStored('k')).toBeNull()
  })

  it('stays quiet when the browser blocks storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })

    expect(readStored('k')).toBeNull()
    expect(() => writeStored('k', 'v')).not.toThrow()
  })
})

describe('routes and class names', () => {
  it('orders pages for the navigation', () => {
    const page = (path: string, order?: number): PageModule => ({
      default: () => null,
      route: order === undefined ? { path } : { path, order },
    })
    const sorted = sortedPages({ c: page('/status', 3), a: page('/', 1), d: page('/incidents/:id'), b: page('/incidents', 2) })
    expect(sorted.map((p) => p.route.path)).toEqual(['/', '/incidents', '/status', '/incidents/:id'])
  })

  it('lets the later Tailwind class win', () => {
    expect(cn('px-2', false && 'hidden', 'px-4')).toBe('px-4')
  })

  it('knows that a type style and a text colour are two different things', () => {
    expect(cn('text-meta', 'text-tertiary')).toBe('text-meta text-tertiary')
    expect(cn('text-meta', 'text-body')).toBe('text-body')
    expect(cn('text-secondary', 'text-breach')).toBe('text-breach')
  })
})
