import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '../api/client'
import type { Incident, MetricSample, Simulation } from '../api/types'
import { resetAdminTokenForTests, setAdminToken } from '../lib/adminToken'
import { splitInjectErrors } from '../lib/apiMessages'
import { getConnection } from '../lib/connection'
import { addSample, keepOnly, maxSamplesPerSimulation } from '../lib/samples'
import DashboardPage from './DashboardPage'

// HELPERS

// The stream hook is replaced, so a test can hand samples to the page whenever it wants.
let pushSample: (sample: MetricSample) => void = () => {}
vi.mock('../api/useEventStream', () => ({
  useEventStream: (_url: string, _name: string, onEvent: (sample: MetricSample) => void) => {
    pushSample = onEvent
    return 'open'
  },
}))

// A space that never breaks sits between a number and its unit.
const nbsp = String.fromCharCode(160)

// runningSimulation builds a simulation that started 30 seconds ago and runs for two minutes.
function runningSimulation(id: string, service: string): Simulation {
  const now = Date.now()
  return {
    id,
    service,
    mode: 'cpu_saturation',
    severity: 'major',
    intensity: 0.9,
    duration: '2m0s',
    state: 'running',
    started_at: new Date(now - 30_000).toISOString(),
    ends_at: new Date(now + 90_000).toISOString(),
    public: false,
  }
}

// finishedSimulation builds a simulation that ran for 45 seconds and ended two minutes ago.
function finishedSimulation(id: string, service: string, state: 'halted' | 'resolved'): Simulation {
  const now = Date.now()
  return {
    ...runningSimulation(id, service),
    state,
    started_at: new Date(now - 165_000).toISOString(),
    ended_at: new Date(now - 120_000).toISOString(),
  }
}

function sample(latency: number, healthy: boolean, simulationId = 'sim_1'): MetricSample {
  return {
    simulation_id: simulationId,
    service: 'payments-api',
    mode: 'cpu_saturation',
    timestamp: new Date().toISOString(),
    latency_p99_ms: latency,
    error_rate: 0.002,
    cpu_percent: 30,
    memory_mb: 512,
    healthy,
    breaches: healthy ? [] : ['latency_p99'],
  }
}

function incidentFor(id: string, simulationId: string): Incident {
  return {
    id,
    simulation_id: simulationId,
    service: 'checkout',
    mode: 'cpu_saturation',
    severity: 'major',
    status: 'mitigated',
    breaches: ['cpu'],
    declared_at: new Date().toISOString(),
    public: false,
    timeline: [],
  }
}

// renderPage shows the dashboard inside a router, because its lists link to other pages.
function renderPage() {
  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

// recentRows gives back the rows of the recently finished list.
async function recentRows(): Promise<HTMLElement[]> {
  const list = await screen.findByRole('list', { name: 'Recently finished simulations' })
  return within(list).getAllByRole('listitem')
}

let running: Simulation
let halted: Simulation

beforeEach(() => {
  window.localStorage.clear()
  resetAdminTokenForTests()
  running = runningSimulation('sim_1', 'payments-api')
  halted = finishedSimulation('sim_0', 'checkout', 'halted')
  vi.spyOn(api, 'getSlo').mockResolvedValue({ max_latency_p99_ms: 500, max_error_rate: 0.01, max_cpu_percent: 90, max_memory_mb: 2048 })
  vi.spyOn(api, 'listSimulations').mockResolvedValue({ simulations: [running, halted] })
  vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [] })
  vi.spyOn(api, 'getConfig').mockResolvedValue({ public_drills: false })
})

afterEach(() => {
  vi.restoreAllMocks()
})

// PAGE TESTS

describe('DashboardPage', () => {
  it('shows healthy metrics as compact rows and gives a breaching metric the big chart', async () => {
    renderPage()

    const panel = await screen.findByRole('article', { name: 'CPU saturation on payments-api' })
    const card = screen.getByRole('region', { name: 'Service payments-api' })
    expect(within(panel).getByRole('figure', { name: 'Latency p99: waiting for data' })).toBeTruthy()
    expect(within(panel).queryByLabelText('Metrics over their limit')).toBeNull()
    expect(within(within(panel).getByLabelText('Metrics within their limit')).getAllByRole('figure')).toHaveLength(4)
    // A healthy card has no rail at all.
    expect(card.querySelector('[data-rail]')).toBeNull()

    act(() => pushSample(sample(120, true)))
    expect(await within(panel).findByText('Within SLO')).toBeTruthy()
    expect(screen.getByText('1 simulation running, all within SLO')).toBeTruthy()

    act(() => pushSample(sample(950, false)))

    const loud = await within(panel).findByLabelText('Metrics over their limit')
    const big = within(loud).getAllByRole('figure')
    expect(big).toHaveLength(1)
    expect(big[0]?.getAttribute('aria-label')).toBe(`Latency p99: 950${nbsp}ms, above the limit of 500${nbsp}ms`)
    // The breach is said in words too, not only in red.
    expect(big[0]?.textContent).toContain('over the limit')
    expect(big[0]?.textContent).toContain(`950${nbsp}ms`)

    const calm = within(within(panel).getByLabelText('Metrics within their limit')).getAllByRole('figure')
    expect(calm.map((f) => f.getAttribute('aria-label'))).toEqual([
      'Error rate: 0.2%, within the limit of 1%',
      'CPU: 30%, within the limit of 90%',
      `Memory: 512${nbsp}MB, within the limit of 2,048${nbsp}MB`,
    ])
    expect(within(panel).getByText('Breaching SLO')).toBeTruthy()
    expect(screen.getByText('1 simulation is breaching SLO')).toBeTruthy()
    // The red rail is lit, and it pulses.
    expect(card.querySelector('[data-rail="breach"]')?.className).toContain('animate-rail')
  })

  it('keeps only one red thing in a breaching card besides its rail: the breaching value', async () => {
    renderPage()
    const card = await screen.findByRole('region', { name: 'Service payments-api' })
    act(() => pushSample(sample(950, false)))
    await within(card).findByLabelText('Metrics over their limit')

    const red = Array.from(card.querySelectorAll('[class*="text-breach"]')).filter((el) => !el.className.includes('hover:text-breach'))
    expect(red).toHaveLength(1)
    expect(red[0]?.textContent).toBe(`950${nbsp}ms`)
  })

  it('groups running simulations by service and puts the breaching service first', async () => {
    const quiet = runningSimulation('sim_2', 'checkout')
    const second = runningSimulation('sim_3', 'payments-api')
    vi.mocked(api.listSimulations).mockResolvedValue({ simulations: [quiet, running, second] })
    renderPage()
    await screen.findByRole('article', { name: 'CPU saturation on checkout' })

    act(() => {
      pushSample(sample(120, true, 'sim_2'))
      pushSample(sample(950, false, 'sim_3'))
    })

    await screen.findByText('1 simulation is breaching SLO')
    const groups = screen.getAllByRole('region', { name: /^Service / })
    expect(groups.map((g) => g.getAttribute('aria-label'))).toEqual(['Service payments-api', 'Service checkout'])

    const payments = groups[0]
    if (!payments) {
      throw new Error('the payments group is missing')
    }
    expect(within(payments).getByRole('heading', { level: 3, name: 'payments-api' }).className).toContain('font-mono')
    expect(within(payments).getByText('2 simulations')).toBeTruthy()
    expect(within(payments).getAllByRole('article')).toHaveLength(2)
    expect(payments.querySelector('[data-rail="breach"]')).toBeTruthy()
    expect(groups[1]?.querySelector('[data-rail]')).toBeNull()
  })

  it('shows the time remaining as progress', async () => {
    renderPage()

    const bar = await screen.findByRole('progressbar', { name: 'Time elapsed for the simulation on payments-api' })
    const now = Number(bar.getAttribute('aria-valuenow'))
    expect(now).toBeGreaterThanOrEqual(24)
    expect(now).toBeLessThanOrEqual(27)
    expect(bar.getAttribute('aria-valuetext')).toMatch(/^1m (29|30)s left of 2m$/)
  })

  it('tells the header that the stream is live', async () => {
    renderPage()
    await screen.findByRole('article', { name: 'CPU saturation on payments-api' })

    expect(getConnection()).toBe('live')
    // The indicator belongs to the header, so the page itself does not draw one.
    expect(screen.queryByText('Live')).toBeNull()
  })

  it('says so when nothing is running, with a flat line and no leftover success message', async () => {
    vi.mocked(api.listSimulations).mockResolvedValue({ simulations: [] })

    const { container } = renderPage()

    expect(await screen.findByText('No simulations are running.')).toBeTruthy()
    expect(screen.getByText('Nothing is running.')).toBeTruthy()
    expect(screen.getByText('Simulations move here when they end or are halted.')).toBeTruthy()
    expect(container.querySelector('[data-flatline]')).toBeTruthy()
    expect(screen.queryByText(/^Started /)).toBeNull()
    expect(container.querySelector('[data-feedback]')?.textContent).toBe('')
  })

  it('shows the error with a way to try again when the list cannot be loaded', async () => {
    vi.mocked(api.listSimulations).mockRejectedValueOnce(new ApiError(500, 'internal server error'))

    renderPage()

    expect(await screen.findByText('Could not load the simulations')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('button', { name: 'Halt the simulation on payments-api' })).toBeTruthy()
  })

  it('halts a simulation with the admin token from the settings', async () => {
    setAdminToken(' s3cret ')
    const halt = vi.spyOn(api, 'haltSimulation').mockResolvedValue({ ...running, state: 'halted' })
    renderPage()

    const button = await screen.findByRole('button', { name: 'Halt the simulation on payments-api' })
    // Halt is never a solid red button: it only turns red at its edge and its text when pointed at.
    expect(button.className).toContain('hover:border-breach')
    expect(button.className).not.toContain('bg-breach')
    fireEvent.click(button)

    await waitFor(() => expect(halt).toHaveBeenCalledWith('sim_1', 's3cret'))
    await waitFor(() => expect(vi.mocked(api.listSimulations).mock.calls.length).toBeGreaterThan(1))
  })

  it('shows the retry time when halting is rate limited', async () => {
    vi.spyOn(api, 'haltSimulation').mockRejectedValue(new ApiError(429, 'too many requests, try again in 12 seconds', 12))
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Halt the simulation on payments-api' }))

    expect(await screen.findByText('Too many requests. You can try again in 12 seconds.')).toBeTruthy()
  })

  it('fetches the list again when samples arrive for a simulation it has not seen', async () => {
    renderPage()
    await screen.findByRole('button', { name: 'Halt the simulation on payments-api' })
    const before = vi.mocked(api.listSimulations).mock.calls.length

    act(() => pushSample(sample(120, true, 'sim_from_slack')))

    await waitFor(() => expect(vi.mocked(api.listSimulations).mock.calls.length).toBe(before + 1))
  })
})

// PUBLIC DRILL TESTS

describe('public drills on the dashboard', () => {
  it('tags a drill that a visitor started, in plain grey, and leaves an admin drill untagged', async () => {
    const visitor = { ...runningSimulation('sim_public', 'checkout'), public: true }
    vi.mocked(api.listSimulations).mockResolvedValue({ simulations: [running, visitor] })
    renderPage()

    const publicPanel = await screen.findByRole('article', { name: 'CPU saturation on checkout' })
    const tag = within(publicPanel).getByText('Public drill')
    expect(tag.className).toContain('text-tertiary')
    expect(tag.className).not.toContain('breach')

    const adminPanel = screen.getByRole('article', { name: 'CPU saturation on payments-api' })
    expect(within(adminPanel).queryByText('Public drill')).toBeNull()
  })

  it('explains public drills in one quiet line, only on a server that allows them', async () => {
    vi.mocked(api.getConfig).mockResolvedValue({ public_drills: true })
    const { container } = renderPage()

    const link = await screen.findByRole('link', { name: 'How the live demo works' })
    expect(link.getAttribute('href')).toBe('https://github.com/MA-V4/shadow-pager#live-demo')
    // A link that opens another site must not hand that site a way back into this page.
    expect(link.getAttribute('rel')).toBe('noreferrer')
    const note = container.querySelector('[data-public-note]')
    expect(note?.textContent).toContain('do not post to Slack')
    expect(note?.className).toContain('text-meta')
  })

  it('says nothing about public drills on a server that does not allow them, or that cannot say', async () => {
    const { container, unmount } = renderPage()
    await screen.findByRole('article', { name: 'CPU saturation on payments-api' })
    expect(container.querySelector('[data-public-note]')).toBeNull()
    unmount()

    vi.mocked(api.getConfig).mockRejectedValue(new ApiError(500, 'internal server error'))
    const second = renderPage()
    await screen.findByRole('article', { name: 'CPU saturation on payments-api' })
    expect(second.container.querySelector('[data-public-note]')).toBeNull()
    // The rest of the dashboard must not care that this small question failed.
    expect(screen.getByRole('button', { name: 'Start drill' })).toBeTruthy()
  })
})

// RECENTLY FINISHED TESTS

describe('recently finished on the dashboard', () => {
  it('says that a simulation breached and links the incident it caused', async () => {
    vi.mocked(api.listIncidents).mockResolvedValue({ incidents: [incidentFor('inc_7', 'sim_0')] })
    renderPage()

    const rows = await recentRows()
    expect(rows).toHaveLength(1)
    const row = rows[0] as HTMLElement
    await waitFor(() => expect(row.querySelector('[data-outcome]')?.textContent).toBe('Breached SLO, incident inc_7, halted early'))
    expect(within(row).getByRole('link', { name: 'inc_7' }).getAttribute('href')).toBe('/incidents/inc_7')
    expect(within(row).getByText('checkout').className).toContain('font-mono')
    expect(within(row).getByText('CPU saturation')).toBeTruthy()
    expect(row.querySelector('[data-shape="gradual"]')).toBeTruthy()
    // It ran for 45 seconds and ended two minutes ago.
    expect(within(row).getByText('45s')).toBeTruthy()
    expect(within(row).getByText('2m ago')).toBeTruthy()
  })

  it('says that a simulation stayed healthy when no incident points at it', async () => {
    const quiet = finishedSimulation('sim_q', 'search', 'resolved')
    vi.mocked(api.listSimulations).mockResolvedValue({ simulations: [quiet, halted] })
    renderPage()

    const rows = await recentRows()
    await waitFor(() => expect(rows[0]?.querySelector('[data-outcome]')?.textContent).toBe('Stayed healthy'))
    expect(rows[1]?.querySelector('[data-outcome]')?.textContent).toBe('Stayed healthy, halted early')
    expect(within(rows[0] as HTMLElement).queryByRole('link')).toBeNull()
    // Nothing repeats the word "Finished" on every row.
    expect(screen.queryByText('Finished')).toBeNull()
    expect(screen.queryByText('Halted')).toBeNull()
  })

  it('does not guess the outcome when the incidents cannot be loaded', async () => {
    vi.mocked(api.listIncidents).mockRejectedValue(new ApiError(500, 'internal server error'))
    renderPage()

    const rows = await recentRows()
    expect(rows[0]?.querySelector('[data-outcome]')?.textContent).toBe('Outcome unknown, halted early')
  })

  it('shows the peak that this browser saw, and a dash for a simulation nobody watched', async () => {
    renderPage()
    const rows = await recentRows()
    const peak = () => rows[0]?.querySelector('[data-peak]')
    expect(peak()?.textContent).toBe('Peak-')
    expect(peak()?.getAttribute('title')).toMatch(/not watched in this browser/)

    // For CPU saturation the peak is the highest CPU value.
    act(() => {
      pushSample({ ...sample(120, true, 'sim_0'), cpu_percent: 72 })
      pushSample({ ...sample(120, true, 'sim_0'), cpu_percent: 97.5 })
      pushSample({ ...sample(120, true, 'sim_0'), cpu_percent: 40 })
    })

    await waitFor(() => expect(peak()?.textContent).toBe('Peak97.5%'))
  })
})

// FORM TESTS

describe('drill composer on the dashboard', () => {
  it('is titled "Run a drill" and offers every failure mode as a card with its drawing', async () => {
    renderPage()

    expect(await screen.findByRole('heading', { level: 2, name: 'Run a drill' })).toBeTruthy()
    const modes = screen.getByRole('radiogroup', { name: 'Failure mode' })
    const cards = Array.from(modes.querySelectorAll('label'))
    expect(cards.map((card) => card.querySelector('span')?.textContent)).toEqual([
      'Latency spike',
      'Error rate surge',
      'Memory leak',
      'CPU saturation',
      'DB pool exhaustion',
    ])
    expect(cards.every((card) => card.querySelector('svg[data-shape]') !== null)).toBe(true)
    // Five cards sit in two columns, and the last one fills its row so no cell is left empty.
    expect(modes.className).toContain('grid-cols-2')
    expect(cards[4]?.className).toContain('last:col-span-2')

    const severity = screen.getByRole('radiogroup', { name: 'Severity' })
    expect(Array.from(severity.querySelectorAll('label')).map((s) => s.textContent)).toEqual(['Critical', 'Major', 'Minor'])
    expect(screen.queryByLabelText('Admin token')).toBeNull()
  })

  it('sends the form values and the saved token, then confirms in a sentence and refreshes', async () => {
    setAdminToken('s3cret')
    const create = vi.spyOn(api, 'createSimulation').mockResolvedValue({ ...running, id: 'sim_2', service: 'search' })
    renderPage()

    fireEvent.change(await screen.findByLabelText('Service'), { target: { value: ' search ' } })
    fireEvent.click(screen.getByRole('radio', { name: /^CPU saturation/ }))
    fireEvent.click(screen.getByRole('radio', { name: 'Critical' }))
    fireEvent.change(screen.getByLabelText('Intensity'), { target: { value: '0.5' } })
    fireEvent.click(screen.getByRole('radio', { name: 'Custom' }))
    fireEvent.change(screen.getByLabelText('Custom duration'), { target: { value: ' 90s ' } })
    const before = vi.mocked(api.listSimulations).mock.calls.length
    fireEvent.click(screen.getByRole('button', { name: 'Start drill' }))

    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(
        { service: 'search', mode: 'cpu_saturation', severity: 'critical', intensity: 0.5, duration: '90s' },
        's3cret',
      ),
    )
    expect(await screen.findByText('Started CPU saturation on search.')).toBeTruthy()
    await waitFor(() => expect(vi.mocked(api.listSimulations).mock.calls.length).toBeGreaterThan(before))
  })

  it('shows server validation errors in the line under each field and moves to the first one', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(
      new ApiError(
        400,
        'inject: invalid scenario: service must be at most 63 letters, digits, dots, dashes or underscores\ninvalid scenario: duration 5s outside [10s, 30m0s]',
      ),
    )
    renderPage()

    const service = await screen.findByLabelText('Service')
    const serviceLine = document.getElementById(service.getAttribute('aria-describedby') ?? '')
    const duration = screen.getByRole('radiogroup', { name: 'Duration' })
    const durationLine = document.getElementById(duration.getAttribute('aria-describedby') ?? '')
    // The lines exist before anything goes wrong, holding the hints.
    expect(serviceLine?.textContent).toBe('Letters, digits, dots, dashes and underscores.')
    expect(durationLine?.textContent).toBe('From 10s to 30m.')

    fireEvent.click(screen.getByRole('button', { name: 'Start drill' }))

    await waitFor(() => expect(service.getAttribute('aria-invalid')).toBe('true'))
    // The same lines now hold the errors, so nothing was added to the page and nothing moved.
    expect(serviceLine?.textContent).toBe('Service must be at most 63 letters, digits, dots, dashes or underscores')
    expect(durationLine?.textContent).toBe('Duration 5s outside [10s, 30m0s]')
    // An error starts with a red dot, so it is not told apart by colour alone.
    expect(serviceLine?.querySelector('[data-tone="breach"]')).toBeTruthy()
    expect(duration.getAttribute('aria-invalid')).toBe('true')
    // The custom field opens with the refused value in it, ready to be fixed.
    expect((screen.getByLabelText('Custom duration') as HTMLInputElement).value).toBe('2m')
    expect(screen.getByRole('radiogroup', { name: 'Severity' }).getAttribute('aria-invalid')).toBe('false')
    await waitFor(() => expect(document.activeElement).toBe(service))
  })

  it('keeps room for messages, so the form does not jump when one appears', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(new ApiError(429, 'too many requests, try again in 30 seconds', 30))
    renderPage()
    const form = await screen.findByRole('form', { name: 'Run a drill' })
    const messages = form.querySelector('[data-feedback]')
    expect(messages?.className).toContain('min-h-12')
    expect(messages?.getAttribute('aria-live')).toBe('polite')
    expect(messages?.textContent).toBe('')

    fireEvent.click(screen.getByRole('button', { name: 'Start drill' }))

    expect(await within(form).findByText('Too many requests. You can try again in 30 seconds.')).toBeTruthy()
    expect(form.querySelector('[data-feedback]')).toBe(messages)
    const button = screen.getByRole('button', { name: 'Try again in 30s' })
    expect(button.hasAttribute('disabled')).toBe(true)
  })

  it('points at the settings when the admin token is missing', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(new ApiError(401, 'a valid admin token is required for this request'))
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Start drill' }))

    expect(await screen.findByText(/Open Settings in the header, enter the token, and try again\./)).toBeTruthy()
  })

  it('can be folded away below the widest layout', async () => {
    renderPage()
    const toggle = await screen.findByRole('button', { name: 'Hide form' })
    const body = document.getElementById(toggle.getAttribute('aria-controls') ?? '')
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(body?.className ?? '').not.toContain('hidden')

    fireEvent.click(toggle)

    expect(screen.getByRole('button', { name: 'Show form' }).getAttribute('aria-expanded')).toBe('false')
    // On the widest layout the composer is always shown, whatever the switch says.
    expect(body?.className).toContain('hidden xl:flex')
  })
})

// PURE HELPERS

describe('splitInjectErrors', () => {
  it('sorts each line under its field and keeps the rest as general', () => {
    const result = splitInjectErrors(
      'inject: invalid scenario: unknown failure mode "meteor"\ninvalid scenario: unknown severity "spicy"\ninvalid scenario: intensity 2.00 outside [0, 1]\nsomething else entirely',
    )

    expect(result.fields).toEqual({
      mode: 'Unknown failure mode "meteor"',
      severity: 'Unknown severity "spicy"',
      intensity: 'Intensity 2.00 outside [0, 1]',
    })
    expect(result.general).toEqual(['Something else entirely'])
  })

  it('recognises a duration that could not be read at all', () => {
    const result = splitInjectErrors('bad request: parse duration: time: invalid duration "soon"')
    expect(result.fields.duration).toBe('Parse duration: time: invalid duration "soon"')
  })
})

describe('sample history', () => {
  it('keeps only the newest samples of each simulation', () => {
    let history = {}
    for (let i = 0; i < maxSamplesPerSimulation + 5; i++) {
      history = addSample(history, sample(i, true))
    }
    const kept = (history as Record<string, MetricSample[]>)['sim_1'] ?? []
    expect(kept).toHaveLength(maxSamplesPerSimulation)
    expect(kept[0]?.latency_p99_ms).toBe(5)
  })

  it('forgets simulations that are no longer wanted', () => {
    const history = addSample(addSample({}, sample(1, true, 'a')), sample(2, true, 'b'))
    expect(Object.keys(keepOnly(history, ['b']))).toEqual(['b'])
    expect(keepOnly(history, ['a', 'b'])).toBe(history)
  })
})
