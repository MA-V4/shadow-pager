import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '../api/client'
import type { StatusPage as StatusPageData } from '../api/types'
import StatusPage from './StatusPage'

// HELPERS

const nbsp = String.fromCharCode(160)

const outage: StatusPageData = {
  status: 'major_outage',
  updated_at: new Date(Date.now() - 2_000).toISOString(),
  services: [
    { name: 'checkout', status: 'operational', metrics: null },
    {
      name: 'payments-api',
      status: 'major_outage',
      metrics: {
        latency_p99_ms: 950.12,
        error_rate: 0.0023,
        cpu_percent: 40,
        memory_mb: 512,
        breaches: ['latency_p99'],
        sampled_at: '2026-01-02T15:09:59Z',
      },
    },
  ],
  incidents: [
    {
      id: 'inc_1',
      service: 'payments-api',
      mode: 'latency_spike',
      severity: 'critical',
      status: 'acknowledged',
      breaches: ['latency_p99'],
      declared_at: new Date(Date.now() - 300_000).toISOString(),
      acknowledged_at: new Date(Date.now() - 180_000).toISOString(),
      updates: [
        { at: new Date(Date.now() - 300_000).toISOString(), text: 'Incident declared' },
        { at: new Date(Date.now() - 180_000).toISOString(), text: 'Acknowledged by the on-call engineer' },
      ],
    },
  ],
}

// hero gives back the one big line at the top of the page.
async function hero(): Promise<HTMLElement> {
  return screen.findByRole('status', { name: 'Overall status' })
}

afterEach(() => {
  vi.restoreAllMocks()
})

// TESTS

describe('StatusPage', () => {
  it('shows the hero line, one row per service and the active incident with its latest update', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue(outage)

    render(<StatusPage />)

    const line = await hero()
    expect(line.textContent).toBe('Major outage')
    expect(line.className).toContain('text-hero')
    // A quiet grey label right above the big line says that none of this is real.
    const simulated = screen.getByText('Simulated services')
    expect(simulated.className).toContain('text-tertiary')
    expect(simulated.compareDocumentPosition(line) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(simulated.parentElement).toBe(line.parentElement)
    expect(line.querySelector('[data-tone="breach"]')).toBeTruthy()

    const services = screen.getByRole('list', { name: 'Services' })
    const rows = within(services).getAllByRole('listitem')
    expect(rows).toHaveLength(2)
    expect(within(rows[0] as HTMLElement).getByText('Operational')).toBeTruthy()
    expect(rows[0]?.querySelector('[data-tone="ok"]')).toBeTruthy()
    expect(within(rows[0] as HTMLElement).getByText('No recent measurements')).toBeTruthy()
    expect(within(rows[1] as HTMLElement).getByText('Major outage')).toBeTruthy()

    // A row carries the p99 latency and the error rate, and says in words which one is over its limit.
    const metrics = screen.getByLabelText('Current metrics of payments-api')
    expect(metrics.className).toContain('font-mono')
    expect(metrics.querySelector('[data-reading="p99"]')?.textContent).toBe(`p99 950.12${nbsp}ms (over limit)`)
    expect(metrics.querySelector('[data-reading="errors"]')?.textContent).toBe('errors 0.23%')
    expect(metrics.textContent).not.toContain('MB')

    const incidents = screen.getByRole('list', { name: 'Active incidents' })
    const card = within(incidents).getByRole('listitem')
    expect(within(card).getByRole('heading', { level: 3 }).textContent).toBe('Latency spike on payments-api')
    expect(within(card).getByText('Critical')).toBeTruthy()
    expect(within(card).getByText('Acknowledged')).toBeTruthy()
    expect(card.querySelector('[data-rail="warn"]')).toBeTruthy()
    // Only the newest update is shown, not the whole story.
    expect(card.querySelector('[data-latest-update]')?.textContent).toBe('Acknowledged by the on-call engineer')
    expect(within(card).queryByText('Incident declared')).toBeNull()
    expect(within(card).getByText('3m ago')).toBeTruthy()
    expect(within(card).getByText('5m ago')).toBeTruthy()

    // The services come before the incidents.
    expect(services.compareDocumentPosition(incidents) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('says when it was last updated, as a relative time with the exact time behind it', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue(outage)

    render(<StatusPage />)
    await hero()

    const footer = screen.getByText(/^Last updated/)
    const time = footer.querySelector('time')
    expect(time?.textContent).toMatch(/^(just now|\d+s ago)$/)
    expect(time?.getAttribute('datetime')).toBe(outage.updated_at)
    expect(time?.getAttribute('title')).toBeTruthy()
  })

  it('is calm when everything is fine: a small green dot and no boxes', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue({ status: 'operational', updated_at: '2026-01-02T15:10:00Z', services: [], incidents: [] })

    const { container } = render(<StatusPage />)

    const line = await hero()
    expect(line.textContent).toBe('All systems operational')
    // Green is never used large: the state is a 10px dot, and the words stay in the text colour.
    expect(line.className).toContain('text-primary')
    expect(line.querySelector('[data-tone="ok"]')?.className).toContain('h-2.5')
    expect(screen.getByText('Simulated services')).toBeTruthy()
    expect(screen.getByText('No active incidents.')).toBeTruthy()
    expect(screen.getByText(/^No services to show yet\./)).toBeTruthy()
    expect(container.innerHTML).not.toContain('border-dashed')
    expect(container.innerHTML).not.toContain('bg-ok/')
  })

  it('says degraded when a service breaches without a serious incident', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue({ ...outage, status: 'degraded', incidents: [] })

    render(<StatusPage />)

    const line = await hero()
    expect(line.textContent).toBe('Degraded performance')
    expect(line.querySelector('[data-tone="warn"]')).toBeTruthy()
  })

  it('has no controls that change anything', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue(outage)

    render(<StatusPage />)
    await hero()

    expect(screen.queryAllByRole('button')).toHaveLength(0)
    expect(screen.queryAllByRole('textbox')).toHaveLength(0)
    expect(screen.queryAllByRole('link')).toHaveLength(0)
    expect(document.querySelector('form, input, select')).toBeNull()
  })

  it('shows the error with a way to try again', async () => {
    vi.spyOn(api, 'getStatus').mockRejectedValue(new ApiError(500, 'internal server error'))

    render(<StatusPage />)

    expect(await screen.findByText('Could not load the status')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
  })
})
