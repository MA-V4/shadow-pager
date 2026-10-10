import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '../api/client'
import type { MetricSample, Simulation } from '../api/types'
import { resetAdminTokenForTests, setAdminToken } from '../lib/adminToken'
import { splitInjectErrors } from '../lib/apiMessages'
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
const nbsp = ' '

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

let running: Simulation
let halted: Simulation

beforeEach(() => {
  window.localStorage.clear()
  resetAdminTokenForTests()
  running = runningSimulation('sim_1', 'payments-api')
  halted = { ...runningSimulation('sim_0', 'checkout'), state: 'halted', ended_at: new Date().toISOString() }
  vi.spyOn(api, 'getSlo').mockResolvedValue({ max_latency_p99_ms: 500, max_error_rate: 0.01, max_cpu_percent: 90, max_memory_mb: 2048 })
  vi.spyOn(api, 'listSimulations').mockResolvedValue({ simulations: [running, halted] })
})

afterEach(() => {
  vi.restoreAllMocks()
})

// PAGE TESTS

describe('DashboardPage', () => {
  it('shows healthy metrics as compact tiles and gives a breaching metric the big chart', async () => {
    render(<DashboardPage />)

    const panel = await screen.findByRole('article', { name: 'CPU saturation on payments-api' })
    expect(screen.getByLabelText('Metric stream: Live')).toBeTruthy()
    expect(within(panel).getByRole('figure', { name: 'Latency p99: waiting for data' })).toBeTruthy()
    expect(within(panel).queryByLabelText('Metrics over their limit')).toBeNull()
    expect(within(within(panel).getByLabelText('Metrics within their limit')).getAllByRole('figure')).toHaveLength(4)

    act(() => pushSample(sample(120, true)))
    expect(await within(panel).findByText('Within SLO')).toBeTruthy()
    expect(screen.getByText('1 simulation running, all within SLO')).toBeTruthy()

    act(() => pushSample(sample(950, false)))

    const loud = await within(panel).findByLabelText('Metrics over their limit')
    const big = within(loud).getAllByRole('figure')
    expect(big).toHaveLength(1)
    expect(big[0]?.getAttribute('aria-label')).toBe(`Latency p99: 950${nbsp}ms, above the limit of 500${nbsp}ms`)
    // The breach is said in words too, not only in red.
    expect(within(loud).getByText('Over the limit')).toBeTruthy()

    const calm = within(within(panel).getByLabelText('Metrics within their limit')).getAllByRole('figure')
    expect(calm.map((f) => f.getAttribute('aria-label'))).toEqual([
      'Error rate: 0.2%, within the limit of 1%',
      'CPU: 30%, within the limit of 90%',
      `Memory: 512${nbsp}MB, within the limit of 2,048${nbsp}MB`,
    ])
    expect(within(panel).getByText('Breaching SLO')).toBeTruthy()
    expect(screen.getByText('1 simulation is breaching SLO')).toBeTruthy()
  })

  it('groups running simulations by service and puts the breaching service first', async () => {
    const quiet = runningSimulation('sim_2', 'checkout')
    const second = runningSimulation('sim_3', 'payments-api')
    vi.mocked(api.listSimulations).mockResolvedValue({ simulations: [quiet, running, second] })
    render(<DashboardPage />)
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
    expect(within(payments).getByText('Breaching')).toBeTruthy()
    expect(within(payments).getByText('2 simulations')).toBeTruthy()
    expect(within(payments).getAllByRole('article')).toHaveLength(2)
    expect(within(groups[1] as HTMLElement).getByText('Healthy')).toBeTruthy()
  })

  it('shows the time remaining as progress', async () => {
    render(<DashboardPage />)

    const bar = await screen.findByRole('progressbar', { name: 'Time elapsed for the simulation on payments-api' })
    const now = Number(bar.getAttribute('aria-valuenow'))
    expect(now).toBeGreaterThanOrEqual(24)
    expect(now).toBeLessThanOrEqual(27)
    expect(bar.getAttribute('aria-valuetext')).toMatch(/^1m (29|30)s left of 2m$/)
  })

  it('lists recently finished simulations with proper labels', async () => {
    render(<DashboardPage />)

    const recent = (await screen.findByRole('heading', { name: 'Recently finished' })).parentElement
    if (!recent) {
      throw new Error('the recent section is missing')
    }
    expect(within(recent).getByText('checkout')).toBeTruthy()
    expect(within(recent).getByText(/^CPU saturation, started /)).toBeTruthy()
    expect(within(recent).getByText('Halted')).toBeTruthy()
  })

  it('says so when nothing is running', async () => {
    vi.mocked(api.listSimulations).mockResolvedValue({ simulations: [] })

    render(<DashboardPage />)

    expect(await screen.findByText('Nothing is running')).toBeTruthy()
    expect(screen.getByText('Nothing is running. Inject a failure to begin.')).toBeTruthy()
    expect(screen.getByText('Simulations move here when they end or are halted.')).toBeTruthy()
  })

  it('shows the error with a way to try again when the list cannot be loaded', async () => {
    vi.mocked(api.listSimulations).mockRejectedValueOnce(new ApiError(500, 'internal server error'))

    render(<DashboardPage />)

    expect(await screen.findByText('Could not load the simulations')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('button', { name: 'Halt the simulation on payments-api' })).toBeTruthy()
  })

  it('halts a simulation with the admin token from the settings', async () => {
    setAdminToken(' s3cret ')
    const halt = vi.spyOn(api, 'haltSimulation').mockResolvedValue({ ...running, state: 'halted' })
    render(<DashboardPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Halt the simulation on payments-api' }))

    await waitFor(() => expect(halt).toHaveBeenCalledWith('sim_1', 's3cret'))
    await waitFor(() => expect(vi.mocked(api.listSimulations).mock.calls.length).toBeGreaterThan(1))
  })

  it('shows the retry time when halting is rate limited', async () => {
    vi.spyOn(api, 'haltSimulation').mockRejectedValue(new ApiError(429, 'too many requests, try again in 12 seconds', 12))
    render(<DashboardPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Halt the simulation on payments-api' }))

    expect(await screen.findByText('Too many requests. You can try again in 12 seconds.')).toBeTruthy()
  })

  it('fetches the list again when samples arrive for a simulation it has not seen', async () => {
    render(<DashboardPage />)
    await screen.findByRole('button', { name: 'Halt the simulation on payments-api' })
    const before = vi.mocked(api.listSimulations).mock.calls.length

    act(() => pushSample(sample(120, true, 'sim_from_slack')))

    await waitFor(() => expect(vi.mocked(api.listSimulations).mock.calls.length).toBe(before + 1))
  })
})

// FORM TESTS

describe('inject form on the dashboard', () => {
  it('offers every failure mode by its proper name and has no admin token field', async () => {
    render(<DashboardPage />)

    const mode = await screen.findByLabelText('Failure mode')
    const options = within(mode).getAllByRole('option').map((o) => o.textContent)
    expect(options).toEqual(['Latency spike', 'Error rate surge', 'Memory leak', 'CPU saturation', 'DB pool exhaustion'])
    expect(within(screen.getByLabelText('Severity')).getAllByRole('option').map((o) => o.textContent)).toEqual(['Critical', 'Major', 'Minor'])
    expect(screen.queryByLabelText('Admin token')).toBeNull()
  })

  it('sends the form values and the saved token, then confirms and refreshes', async () => {
    setAdminToken('s3cret')
    const create = vi.spyOn(api, 'createSimulation').mockResolvedValue({ ...running, id: 'sim_2', service: 'search' })
    render(<DashboardPage />)

    fireEvent.change(await screen.findByLabelText('Service'), { target: { value: ' search ' } })
    fireEvent.change(screen.getByLabelText('Failure mode'), { target: { value: 'cpu_saturation' } })
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'critical' } })
    fireEvent.change(screen.getByLabelText('Intensity'), { target: { value: '0.5' } })
    fireEvent.change(screen.getByLabelText('Duration'), { target: { value: '90s' } })
    fireEvent.click(screen.getByRole('button', { name: 'Inject failure' }))

    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(
        { service: 'search', mode: 'cpu_saturation', severity: 'critical', intensity: 0.5, duration: '90s' },
        's3cret',
      ),
    )
    expect(await screen.findByText(/Started CPU saturation on search\./)).toBeTruthy()
  })

  it('shows server validation errors in the line under each field and moves to the first one', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(
      new ApiError(
        400,
        'inject: invalid scenario: service must be at most 63 letters, digits, dots, dashes or underscores\ninvalid scenario: duration 5s outside [10s, 30m0s]',
      ),
    )
    render(<DashboardPage />)

    const service = await screen.findByLabelText('Service')
    const serviceLine = document.getElementById(service.getAttribute('aria-describedby') ?? '')
    const duration = screen.getByLabelText('Duration')
    const durationLine = document.getElementById(duration.getAttribute('aria-describedby') ?? '')
    // The lines exist before anything goes wrong, holding the hints.
    expect(serviceLine?.textContent).toBe('Letters, digits, dots, dashes and underscores.')
    expect(durationLine?.textContent).toBe('From 10s to 30m, like 90s or 2m.')

    fireEvent.click(screen.getByRole('button', { name: 'Inject failure' }))

    await waitFor(() => expect(service.getAttribute('aria-invalid')).toBe('true'))
    // The same lines now hold the errors, so nothing was added to the page and nothing moved.
    expect(serviceLine?.textContent).toBe('Service must be at most 63 letters, digits, dots, dashes or underscores')
    expect(durationLine?.textContent).toBe('Duration 5s outside [10s, 30m0s]')
    expect(duration.getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByLabelText('Severity').getAttribute('aria-invalid')).toBe('false')
    await waitFor(() => expect(document.activeElement).toBe(service))
  })

  it('keeps room for messages, so the form does not jump when one appears', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(new ApiError(429, 'too many requests, try again in 30 seconds', 30))
    render(<DashboardPage />)
    const form = await screen.findByRole('form', { name: 'Inject a failure' })
    const messages = form.querySelector('[aria-live="polite"]')
    expect(messages?.className).toContain('min-h-16')
    expect(messages?.textContent).toBe('')

    fireEvent.click(screen.getByRole('button', { name: 'Inject failure' }))

    expect(await within(form).findByText('Too many requests. You can try again in 30 seconds.')).toBeTruthy()
    expect(form.querySelector('[aria-live="polite"]')).toBe(messages)
    const button = screen.getByRole('button', { name: 'Try again in 30s' })
    expect(button.hasAttribute('disabled')).toBe(true)
  })

  it('points at the settings when the admin token is missing', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(new ApiError(401, 'a valid admin token is required for this request'))
    render(<DashboardPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Inject failure' }))

    expect(await screen.findByText(/Open Settings in the header, enter the token, and try again\./)).toBeTruthy()
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
