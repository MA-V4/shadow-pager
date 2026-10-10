import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '../api/client'
import type { Incident } from '../api/types'
import IncidentDetailPage, { actorName } from './IncidentDetailPage'
import IncidentsPage from './IncidentsPage'

// HELPERS

const open: Incident = {
  id: 'inc_open',
  simulation_id: 'sim_1',
  service: 'payments-api',
  mode: 'latency_spike',
  severity: 'critical',
  status: 'acknowledged',
  breaches: ['latency_p99', 'error_rate'],
  declared_at: '2026-01-02T15:04:05Z',
  acknowledged_at: '2026-01-02T15:05:35Z',
  acknowledged_by: '<@U123ABC>',
  time_to_acknowledge: '1m30s',
  slack_channel_id: 'C123',
  timeline: [
    { at: '2026-01-02T15:04:05Z', actor: 'shadow-pager', text: 'Incident declared' },
    { at: '2026-01-02T15:05:35Z', actor: '<@U123ABC>', text: 'Acknowledged' },
  ],
}
const closed: Incident = {
  ...open,
  id: 'inc_closed',
  service: 'checkout',
  mode: 'cpu_saturation',
  severity: 'minor',
  status: 'resolved',
  resolved_at: '2026-01-02T15:09:05Z',
  time_to_resolve: '5m0s',
}

// renderAt shows the incident pages at the given URL, the way the app would.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/incidents" element={<IncidentsPage />} />
        <Route path="/incidents/:id" element={<IncidentDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

// term gives back the value that the summary shows under the given label.
function term(label: string): string | null | undefined {
  return Array.from(document.querySelectorAll('dt')).find((dt) => dt.textContent === label)?.nextElementSibling?.textContent
}

afterEach(() => {
  vi.restoreAllMocks()
})

// LIST TESTS

describe('IncidentsPage', () => {
  it('lists incidents with badges and links to their detail pages', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [open, closed] })

    renderAt('/incidents')

    const list = await screen.findByRole('list', { name: 'Incidents, newest first' })
    const rows = within(list).getAllByRole('link')
    expect(rows).toHaveLength(2)

    const first = rows[0]
    if (!first) {
      throw new Error('the first row is missing')
    }
    expect(first.getAttribute('href')).toBe('/incidents/inc_open')
    expect(within(first).getByText('Critical')).toBeTruthy()
    expect(within(first).getByText('Acknowledged')).toBeTruthy()
    expect(first.textContent).toContain('Latency spike on payments-api')

    const second = rows[1]
    if (!second) {
      throw new Error('the second row is missing')
    }
    expect(within(second).getByText('Minor')).toBeTruthy()
    expect(within(second).getByText('Resolved')).toBeTruthy()
    expect(second.textContent).toContain('resolved after 5m')
  })

  it('explains what to do when there are no incidents', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [] })

    renderAt('/incidents')

    expect(await screen.findByText('No incidents yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'dashboard' }).getAttribute('href')).toBe('/')
  })

  it('shows the error when the list cannot be loaded', async () => {
    vi.spyOn(api, 'listIncidents').mockRejectedValue(new ApiError(500, 'internal server error'))

    renderAt('/incidents')

    expect(await screen.findByText('Could not load the incidents')).toBeTruthy()
    expect(screen.getByText('internal server error')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
  })
})

// DETAIL TESTS

describe('IncidentDetailPage', () => {
  it('shows the summary, the durations and the timeline', async () => {
    const get = vi.spyOn(api, 'getIncident').mockResolvedValue(closed)

    renderAt('/incidents/inc_closed')

    expect(await screen.findByRole('heading', { level: 1, name: 'CPU saturation on checkout' })).toBeTruthy()
    expect(get.mock.calls[0]?.[0]).toBe('inc_closed')

    expect(term('Time to acknowledge')).toBe('1m 30s')
    expect(term('Time to resolve')).toBe('5m')
    expect(term('Acknowledged by')).toBe('Slack user U123ABC')
    expect(term('SLO breaches at declaration')).toBe('Latency p99, Error rate')
    expect(term('Simulation')).toBe('sim_1')

    // This incident went from acknowledged straight to resolved, and the lifecycle says so.
    const steps = within(screen.getByRole('list', { name: 'Lifecycle' })).getAllByRole('listitem')
    expect(steps.map((s) => s.firstElementChild?.textContent)).toEqual(['Declared', 'Acknowledged', 'Mitigated', 'Resolved'])
    expect(steps[2]?.textContent).toContain('Skipped')
    expect(steps[3]?.textContent).not.toContain('Not yet')

    const timeline = screen.getByRole('list', { name: 'Timeline, oldest first' })
    const entries = within(timeline).getAllByRole('listitem')
    expect(entries).toHaveLength(2)
    expect(entries[0]?.textContent).toContain('Incident declared')
    expect(entries[0]?.textContent).toContain('by Shadow-Pager (automatic)')
    expect(entries[1]?.textContent).toContain('by Slack user U123ABC')
    expect(screen.getByRole('link', { name: 'Back to all incidents' }).getAttribute('href')).toBe('/incidents')
  })

  it('says so for steps that have not happened yet', async () => {
    const declared: Incident = { ...open, status: 'declared' }
    delete declared.acknowledged_at
    delete declared.acknowledged_by
    delete declared.time_to_acknowledge
    vi.spyOn(api, 'getIncident').mockResolvedValue(declared)

    renderAt('/incidents/inc_open')

    await screen.findByRole('heading', { level: 1 })
    expect(term('Time to acknowledge')).toBe('Not acknowledged')
    expect(term('Time to resolve')).toBe('Not resolved yet')
    expect(term('Acknowledged by')).toBe('-')
    const steps = within(screen.getByRole('list', { name: 'Lifecycle' })).getAllByRole('listitem')
    expect(steps.slice(1).every((s) => s.textContent?.includes('Not yet'))).toBe(true)
  })

  it('has a friendly message for an incident that does not exist', async () => {
    vi.spyOn(api, 'getIncident').mockRejectedValue(new ApiError(404, 'get incident inc_x: incident not found'))

    renderAt('/incidents/inc_x')

    expect(await screen.findByText('This incident does not exist')).toBeTruthy()
    expect(screen.queryByText('Could not load the incident')).toBeNull()
  })

  it('shows other errors with a way to try again', async () => {
    vi.spyOn(api, 'getIncident').mockRejectedValue(new ApiError(500, 'internal server error'))

    renderAt('/incidents/inc_x')

    expect(await screen.findByText('Could not load the incident')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
  })
})

describe('actorName', () => {
  it('puts technical names into words', () => {
    expect(actorName('shadow-pager')).toBe('Shadow-Pager (automatic)')
    expect(actorName('<@U123ABC>')).toBe('Slack user U123ABC')
    expect(actorName('ada')).toBe('ada')
  })
})
