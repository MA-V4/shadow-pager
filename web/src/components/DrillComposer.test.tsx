import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '../api/client'
import type { Simulation } from '../api/types'
import { resetAdminTokenForTests } from '../lib/adminToken'
import { DrillComposer } from './DrillComposer'

// HELPERS

const created: Simulation = {
  id: 'sim_9',
  service: 'payments-api',
  mode: 'error_rate_surge',
  severity: 'major',
  intensity: 0.8,
  duration: '2m0s',
  state: 'running',
  started_at: '2026-01-02T15:04:05Z',
  ends_at: '2026-01-02T15:06:05Z',
  public: false,
}

const sentence = 'Started an error rate surge on payments-api.'

// start presses the button and waits for the server's answer to reach the screen.
async function start() {
  fireEvent.click(screen.getByRole('button', { name: 'Start drill' }))
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

beforeEach(() => {
  window.localStorage.clear()
  resetAdminTokenForTests()
  vi.useFakeTimers()
  vi.spyOn(api, 'createSimulation').mockResolvedValue(created)
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

// TESTS

describe('DrillComposer feedback', () => {
  it('names the failure mode in lower case inside the sentence', async () => {
    render(<DrillComposer onStarted={() => {}} runningIds={[]} />)

    await start()

    expect(screen.getByText(sentence)).toBeTruthy()
    expect(screen.queryByText(/Error rate surge on/)).toBeNull()
  })

  it('removes the success line after four seconds', async () => {
    render(<DrillComposer onStarted={() => {}} runningIds={[]} />)
    await start()
    expect(screen.getByText(sentence)).toBeTruthy()

    act(() => {
      vi.advanceTimersByTime(3_900)
    })
    expect(screen.getByText(sentence)).toBeTruthy()

    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(screen.queryByText(sentence)).toBeNull()
  })

  it('removes the success line at once when its simulation stops running', async () => {
    const { rerender } = render(<DrillComposer onStarted={() => {}} runningIds={[]} />)
    await start()

    // The new simulation shows up in the list, and the line is still true.
    rerender(<DrillComposer onStarted={() => {}} runningIds={['sim_9']} />)
    expect(screen.getByText(sentence)).toBeTruthy()

    // It is halted a second later, long before the four seconds are over.
    act(() => {
      vi.advanceTimersByTime(1_000)
    })
    rerender(<DrillComposer onStarted={() => {}} runningIds={[]} />)

    expect(screen.queryByText(sentence)).toBeNull()
  })

  it('clears the old success line when the next drill is started', async () => {
    render(<DrillComposer onStarted={() => {}} runningIds={[]} />)
    await start()
    vi.mocked(api.createSimulation).mockReturnValue(new Promise(() => {}))

    fireEvent.click(screen.getByRole('button', { name: 'Start drill' }))

    expect(screen.queryByText(sentence)).toBeNull()
    expect(screen.getByRole('button', { name: 'Starting…' })).toBeTruthy()
  })
})

describe('DrillComposer fields', () => {
  it('lays the fields out in the order of the design, each with a helper line of the same reserved height', () => {
    render(<DrillComposer onStarted={() => {}} runningIds={[]} />)
    const form = screen.getByRole('form', { name: 'Run a drill' })

    const labels = Array.from(form.querySelectorAll('[id$="-label"]')).map((label) => label.textContent)
    expect(labels).toEqual(['Service', 'Failure mode', 'Severity', 'Intensity', 'Duration'])

    const helpers = Array.from(form.querySelectorAll('[id$="-message"]'))
    expect(helpers).toHaveLength(5)
    for (const helper of helpers) {
      expect(helper.className).toContain('min-h-8')
      // Helper text is the 12px meta style, never smaller.
      expect(helper.className).toContain('text-meta')
      expect(helper.textContent).not.toBe('')
    }
  })

  it('offers duration chips and only shows the custom field when asked', () => {
    render(<DrillComposer onStarted={() => {}} runningIds={[]} />)
    const chips = screen.getByRole('radiogroup', { name: 'Duration' })
    expect(Array.from(chips.querySelectorAll('label')).map((chip) => chip.textContent)).toEqual(['30s', '2m', '5m', '10m', 'Custom'])
    expect(screen.queryByLabelText('Custom duration')).toBeNull()

    fireEvent.click(screen.getByRole('radio', { name: 'Custom' }))

    expect(screen.getByLabelText('Custom duration').className).toContain('font-mono')
  })

  it('sends the chosen chip as the duration', async () => {
    render(<DrillComposer onStarted={() => {}} runningIds={[]} />)

    fireEvent.click(screen.getByRole('radio', { name: '5m' }))
    await start()

    expect(vi.mocked(api.createSimulation).mock.calls[0]?.[0]).toEqual({
      service: 'payments-api',
      mode: 'latency_spike',
      severity: 'major',
      intensity: 0.8,
      duration: '5m',
    })
  })

  it('uses no browser default select or bare slider', () => {
    const { container } = render(<DrillComposer onStarted={() => {}} runningIds={[]} />)
    expect(container.querySelector('select')).toBeNull()
    // The browser slider is still there for the keyboard, but it is invisible under our own track and thumb.
    expect(container.querySelector('input[type="range"]')?.className).toContain('opacity-0')
  })
})
