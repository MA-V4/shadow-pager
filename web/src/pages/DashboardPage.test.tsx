import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '../api/client'
import type { MetricSample, Simulation } from '../api/types'
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

const running: Simulation = {
  id: 'sim_1',
  service: 'payments-api',
  mode: 'latency_spike',
  severity: 'major',
  intensity: 0.9,
  duration: '2m0s',
  state: 'running',
  started_at: '2026-01-02T15:04:05Z',
  ends_at: '2026-01-02T15:06:05Z',
}
const halted: Simulation = { ...running, id: 'sim_0', service: 'checkout', state: 'halted', ended_at: '2026-01-02T15:05:00Z' }

function sample(latency: number, healthy: boolean, simulationId = 'sim_1'): MetricSample {
  return {
    simulation_id: simulationId,
    service: 'payments-api',
    mode: 'latency_spike',
    timestamp: '2026-01-02T15:04:10Z',
    latency_p99_ms: latency,
    error_rate: 0.002,
    cpu_percent: 30,
    memory_mb: 512,
    healthy,
    breaches: healthy ? [] : ['latency_p99'],
  }
}

beforeEach(() => {
  window.localStorage.clear()
  vi.spyOn(api, 'getSlo').mockResolvedValue({ max_latency_p99_ms: 500, max_error_rate: 0.01, max_cpu_percent: 90, max_memory_mb: 2048 })
  vi.spyOn(api, 'listSimulations').mockResolvedValue({ simulations: [running, halted] })
})

afterEach(() => {
  vi.restoreAllMocks()
})

// PAGE TESTS

describe('DashboardPage', () => {
  it('shows running and finished simulations and draws live samples against the SLO', async () => {
    render(<DashboardPage />)

    expect(await screen.findByRole('button', { name: 'Halt the simulation on payments-api' })).toBeTruthy()
    expect(screen.getByText('checkout')).toBeTruthy()
    expect(screen.getByLabelText('Metric stream: Live')).toBeTruthy()
    expect(screen.getByRole('figure', { name: 'Latency p99: waiting for data' })).toBeTruthy()

    act(() => pushSample(sample(950, false)))

    expect(await screen.findByRole('figure', { name: 'Latency p99: 950 ms, above the limit of 500 ms' })).toBeTruthy()
    expect(screen.getByRole('figure', { name: 'Error rate: 0.2%, within the limit of 1%' })).toBeTruthy()
    expect(screen.getByRole('figure', { name: 'CPU: 30%, within the limit of 90%' })).toBeTruthy()
    expect(screen.getByRole('figure', { name: 'Memory: 512 MB, within the limit of 2,048 MB' })).toBeTruthy()
    expect(screen.getByText('Breaching SLO')).toBeTruthy()
  })

  it('says so when nothing is running', async () => {
    vi.mocked(api.listSimulations).mockResolvedValue({ simulations: [] })

    render(<DashboardPage />)

    expect(await screen.findByText('Nothing is running')).toBeTruthy()
    expect(screen.getByText('No finished simulations yet')).toBeTruthy()
  })

  it('shows the error with a way to try again when the list cannot be loaded', async () => {
    vi.mocked(api.listSimulations).mockRejectedValueOnce(new ApiError(500, 'internal server error'))

    render(<DashboardPage />)

    expect(await screen.findByText('The simulations could not be loaded')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('button', { name: 'Halt the simulation on payments-api' })).toBeTruthy()
  })

  it('halts a simulation with the saved admin token', async () => {
    const halt = vi.spyOn(api, 'haltSimulation').mockResolvedValue({ ...running, state: 'halted' })
    render(<DashboardPage />)

    fireEvent.change(await screen.findByLabelText('Admin token'), { target: { value: ' s3cret ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Halt the simulation on payments-api' }))

    await waitFor(() => expect(halt).toHaveBeenCalledWith('sim_1', 's3cret'))
    expect(window.localStorage.getItem('shadow-pager.admin-token')).toBe('s3cret')
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
  it('sends the form values and the token, then confirms and refreshes', async () => {
    const create = vi.spyOn(api, 'createSimulation').mockResolvedValue({ ...running, id: 'sim_2', service: 'search' })
    render(<DashboardPage />)

    fireEvent.change(await screen.findByLabelText('Service'), { target: { value: ' search ' } })
    fireEvent.change(screen.getByLabelText('Failure mode'), { target: { value: 'cpu_saturation' } })
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'critical' } })
    fireEvent.change(screen.getByLabelText('Intensity'), { target: { value: '0.5' } })
    fireEvent.change(screen.getByLabelText('Duration'), { target: { value: '90s' } })
    fireEvent.change(screen.getByLabelText('Admin token'), { target: { value: 's3cret' } })
    fireEvent.click(screen.getByRole('button', { name: 'Inject failure' }))

    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(
        { service: 'search', mode: 'cpu_saturation', severity: 'critical', intensity: 0.5, duration: '90s' },
        's3cret',
      ),
    )
    expect(await screen.findByText(/Started latency spike on search/)).toBeTruthy()
  })

  it('shows server validation errors next to the fields they are about', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(
      new ApiError(
        400,
        'inject: invalid scenario: service must be at most 63 letters, digits, dots, dashes or underscores\ninvalid scenario: duration 5s outside [10s, 30m0s]',
      ),
    )
    render(<DashboardPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Inject failure' }))

    const service = await screen.findByLabelText('Service')
    await waitFor(() => expect(service.getAttribute('aria-invalid')).toBe('true'))
    const serviceError = document.getElementById(service.getAttribute('aria-describedby') ?? '')
    expect(serviceError?.textContent).toBe('Service must be at most 63 letters, digits, dots, dashes or underscores')

    const duration = screen.getByLabelText('Duration')
    expect(duration.getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByText('Duration 5s outside [10s, 30m0s]')).toBeTruthy()
    expect(screen.getByLabelText('Severity').getAttribute('aria-invalid')).toBe('false')
  })

  it('shows the retry time and holds the button after a rate limit', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(new ApiError(429, 'too many requests, try again in 30 seconds', 30))
    render(<DashboardPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Inject failure' }))

    expect(await screen.findByText('Too many requests. You can try again in 30 seconds.')).toBeTruthy()
    const button = screen.getByRole('button', { name: 'Try again in 30s' })
    expect(button.hasAttribute('disabled')).toBe(true)
  })

  it('explains a missing admin token', async () => {
    vi.spyOn(api, 'createSimulation').mockRejectedValue(new ApiError(401, 'a valid admin token is required for this request'))
    render(<DashboardPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Inject failure' }))

    expect(await screen.findByText(/only lets people with the admin token do that/)).toBeTruthy()
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
