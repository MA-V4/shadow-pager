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
  public: false,
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
  it('lists open incidents first as dense rows that link to their detail pages', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [closed, open] })

    renderAt('/incidents')

    const openList = await screen.findByRole('list', { name: 'Open' })
    const first = within(openList).getByRole('link')
    expect(first.getAttribute('href')).toBe('/incidents/inc_open')
    expect(within(first).getByText('inc_open').className).toContain('font-mono')
    expect(within(first).getByText('payments-api').className).toContain('font-mono')
    expect(within(first).getByText('Latency spike')).toBeTruthy()
    expect(first.querySelector('[data-shape="spike"]')).toBeTruthy()
    expect(within(first).getByText('Critical')).toBeTruthy()
    expect(within(first).getByText('Acknowledged')).toBeTruthy()
    // The duration says in words what it measures.
    expect(first.querySelector('[data-duration]')?.textContent).toMatch(/^open for \S/)
    // Rows are 44px tall and split by hairlines, with no card around them.
    expect(first.className).toContain('min-h-11')

    const resolvedList = screen.getByRole('list', { name: 'Resolved' })
    const second = within(resolvedList).getByRole('link')
    expect(second.getAttribute('href')).toBe('/incidents/inc_closed')
    expect(within(second).getByText('Minor')).toBeTruthy()
    expect(second.querySelector('[data-duration]')?.textContent).toBe('lasted 5m')
    // The section already says "Resolved", so the rows under it do not repeat the word.
    expect(within(second).queryByText('Resolved')).toBeNull()

    // The open list comes before the resolved one on the page.
    expect(openList.compareDocumentPosition(resolvedList) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('tags the incident of a public drill in the list and on its own page', async () => {
    const visitor: Incident = { ...open, id: 'inc_public', public: true }
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [visitor, open] })
    vi.spyOn(api, 'getIncident').mockResolvedValue(visitor)

    const list = renderAt('/incidents')
    const rows = within(await screen.findByRole('list', { name: 'Open' })).getAllByRole('link')
    expect(within(rows[0] as HTMLElement).getByText('Public drill').className).toContain('text-tertiary')
    expect(within(rows[1] as HTMLElement).queryByText('Public drill')).toBeNull()
    list.unmount()

    renderAt('/incidents/inc_public')
    await screen.findByRole('heading', { level: 1, name: 'inc_public' })
    expect(screen.getByText('Public drill')).toBeTruthy()
  })

  it('leaves out a section that has nothing in it', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [closed] })

    renderAt('/incidents')

    expect(await screen.findByRole('list', { name: 'Resolved' })).toBeTruthy()
    expect(screen.queryByRole('list', { name: 'Open' })).toBeNull()
  })

  it('explains what to do when there are no incidents', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [] })

    const { container } = renderAt('/incidents')

    expect(await screen.findByText('No incidents yet.')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'dashboard' }).getAttribute('href')).toBe('/')
    expect(container.querySelector('[data-flatline]')).toBeTruthy()
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
  it('shows the header, the two readouts and the flight recorder tape', async () => {
    const get = vi.spyOn(api, 'getIncident').mockResolvedValue(closed)

    const { container } = renderAt('/incidents/inc_closed')

    const heading = await screen.findByRole('heading', { level: 1, name: 'inc_closed' })
    expect(heading.className).toContain('font-mono')
    expect(get.mock.calls[0]?.[0]).toBe('inc_closed')
    expect(screen.getByText('Resolved')).toBeTruthy()
    expect(screen.getByText('Minor')).toBeTruthy()
    expect(screen.getByText(/CPU saturation on/).textContent).toBe('CPU saturation on checkout')
    expect(screen.getByText('lasted 5m')).toBeTruthy()
    // A resolved incident has no rail.
    expect(container.querySelector('[data-rail]')).toBeNull()

    expect(term('Time to acknowledge')).toBe('1m 30s')
    expect(term('Time to resolve')).toBe('5m')
    const readout = Array.from(document.querySelectorAll('dt')).find((dt) => dt.textContent === 'Time to resolve')?.nextElementSibling
    expect(readout?.className).toContain('text-readout')
    expect(readout?.className).toContain('font-mono')
    expect(term('Acknowledged by')).toBe('Slack user U123ABC')
    expect(term('SLO breaches at declaration')).toBe('Latency p99, Error rate')
    expect(term('Simulation')).toBe('sim_1')

    const tape = screen.getByRole('list', { name: 'Timeline, oldest first' })
    const entries = within(tape).getAllByRole('listitem')
    expect(entries).toHaveLength(2)
    // Each entry is a mono time on the left, then what happened and who did it on the right.
    const time = entries[0]?.querySelector('time')
    expect(entries[0]?.firstElementChild).toBe(time)
    expect(time?.className).toContain('font-mono')
    expect(time?.textContent).toMatch(/^\d{2}:\d{2}:\d{2}$/)
    expect(time?.getAttribute('datetime')).toBe('2026-01-02T15:04:05Z')
    expect(entries[0]?.textContent).toContain('Incident declared')
    expect(entries[0]?.textContent).toContain('by Shadow-Pager (automatic)')
    expect(entries[1]?.textContent).toContain('by Slack user U123ABC')
    expect(screen.getByRole('link', { name: 'Back to all incidents' }).getAttribute('href')).toBe('/incidents')
  })

  it('says so for steps that have not happened yet, and lights a pulsing rail while nobody has picked it up', async () => {
    const declared: Incident = { ...open, status: 'declared' }
    delete declared.acknowledged_at
    delete declared.acknowledged_by
    delete declared.time_to_acknowledge
    vi.spyOn(api, 'getIncident').mockResolvedValue(declared)

    const { container } = renderAt('/incidents/inc_open')

    await screen.findByRole('heading', { level: 1 })
    expect(term('Time to acknowledge')).toBe('Not acknowledged')
    expect(term('Time to resolve')).toBe('Not resolved yet')
    expect(term('Acknowledged by')).toBe('-')
    expect(screen.getByText(/^open for /)).toBeTruthy()
    expect(container.querySelector('[data-rail="breach"]')?.className).toContain('animate-rail')
  })

  it('gives an acknowledged incident an amber rail that stays still', async () => {
    vi.spyOn(api, 'getIncident').mockResolvedValue(open)

    const { container } = renderAt('/incidents/inc_open')

    await screen.findByRole('heading', { level: 1 })
    const rail = container.querySelector('[data-rail="warn"]')
    expect(rail).toBeTruthy()
    expect(rail?.className).not.toContain('animate-rail')
  })

  it('has a friendly message for an incident that does not exist', async () => {
    vi.spyOn(api, 'getIncident').mockRejectedValue(new ApiError(404, 'get incident inc_x: incident not found'))

    renderAt('/incidents/inc_x')

    expect(await screen.findByText('This incident does not exist.')).toBeTruthy()
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
