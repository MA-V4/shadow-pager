import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '../api/client'
import type { StatusPage as StatusPageData } from '../api/types'
import StatusPage from './StatusPage'

// HELPERS

const outage: StatusPageData = {
  status: 'major_outage',
  updated_at: '2026-01-02T15:10:00Z',
  services: [
    { name: 'checkout', status: 'operational', metrics: null },
    {
      name: 'payments-api',
      status: 'major_outage',
      metrics: {
        latency_p99_ms: 950.12,
        error_rate: 0.0123,
        cpu_percent: 40,
        memory_mb: 512,
        breaches: ['latency_p99', 'error_rate'],
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
      declared_at: '2026-01-02T15:04:05Z',
      acknowledged_at: '2026-01-02T15:05:35Z',
      updates: [
        { at: '2026-01-02T15:04:05Z', text: 'Incident declared' },
        { at: '2026-01-02T15:05:35Z', text: 'Acknowledged' },
      ],
    },
  ],
}

afterEach(() => {
  vi.restoreAllMocks()
})

// TESTS

describe('StatusPage', () => {
  it('shows the banner, the active incident and every service with its numbers', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue(outage)

    render(<StatusPage />)

    expect(await screen.findByText('Major outage in progress')).toBeTruthy()

    const incidents = screen.getByRole('list', { name: 'Active incidents' })
    expect(within(incidents).getByText('Critical')).toBeTruthy()
    const updates = within(incidents).getByRole('list', { name: 'Updates for the incident on payments-api, newest first' })
    const lines = within(updates).getAllByRole('listitem')
    expect(lines[0]?.textContent).toContain('Acknowledged')
    expect(lines[1]?.textContent).toContain('Incident declared')

    const services = screen.getByRole('list', { name: 'Services' })
    expect(within(services).getByText('Operational')).toBeTruthy()
    expect(within(services).getByText('Major outage')).toBeTruthy()
    expect(within(services).getByText(/No recent measurements/)).toBeTruthy()

    const metrics = screen.getByLabelText('Current metrics of payments-api')
    const value = (label: string) => within(metrics).getByText(label).nextElementSibling
    expect(value('Latency p99')?.textContent).toBe('950.12 ms')
    expect(value('Error rate')?.textContent).toBe('1.23%')
    expect(value('CPU')?.textContent).toBe('40%')
    expect(value('Memory')?.textContent).toBe('512 MB')
    // Only the numbers that broke their limit are marked.
    expect(value('Latency p99')?.className).toContain('text-bad')
    expect(value('CPU')?.className).not.toContain('text-bad')
  })

  it('has calm empty states when everything is fine', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue({ status: 'operational', updated_at: '2026-01-02T15:10:00Z', services: [], incidents: [] })

    render(<StatusPage />)

    expect(await screen.findByText('All systems operational')).toBeTruthy()
    expect(screen.getByText('No active incidents')).toBeTruthy()
    expect(screen.getByText('No services to show yet')).toBeTruthy()
  })

  it('says degraded when a service breaches without a serious incident', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue({ ...outage, status: 'degraded', incidents: [] })

    render(<StatusPage />)

    expect(await screen.findByText('Some systems are degraded')).toBeTruthy()
  })

  it('has no controls that change anything', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue(outage)

    render(<StatusPage />)
    await screen.findByText('Major outage in progress')

    expect(screen.queryAllByRole('button')).toHaveLength(0)
    expect(screen.queryAllByRole('textbox')).toHaveLength(0)
    expect(document.querySelector('form, input, select')).toBeNull()
  })

  it('shows the error with a way to try again', async () => {
    vi.spyOn(api, 'getStatus').mockRejectedValue(new ApiError(500, 'internal server error'))

    render(<StatusPage />)

    expect(await screen.findByText('The status could not be loaded')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
  })
})
