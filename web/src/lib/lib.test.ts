import { afterEach, describe, expect, it, vi } from 'vitest'

import { formatDateTime, formatGoDuration, formatPercent, humanize } from './format'
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
})
