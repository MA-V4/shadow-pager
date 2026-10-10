import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './client'
import { giveUpAfterMs, useResource, wakingAfterMs } from './useResource'

// HELPERS

// deferred is a promise that the test finishes by hand.
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

// advance moves the pretend clock and lets React catch up.
async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

// TESTS

describe('useResource', () => {
  it('goes from loading to ready', async () => {
    const load = vi.fn(() => Promise.resolve({ value: 1 }))

    const { result } = renderHook(() => useResource(load, 'k'))
    expect(result.current.phase).toBe('loading')

    await advance(0)
    expect(result.current.phase).toBe('ready')
    expect(result.current.data).toEqual({ value: 1 })
    expect(result.current.error).toBeUndefined()
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('says the server is waking up when the first answer is slow', async () => {
    const slow = deferred<string>()
    const { result } = renderHook(() => useResource(() => slow.promise, 'k'))

    await advance(wakingAfterMs - 1)
    expect(result.current.phase).toBe('loading')

    await advance(1)
    expect(result.current.phase).toBe('waking')

    await act(async () => {
      slow.resolve('finally')
      await slow.promise
    })
    expect(result.current.phase).toBe('ready')
    expect(result.current.data).toBe('finally')
  })

  it('keeps trying while the server cannot be reached, then shows the data', async () => {
    const load = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new ApiError(0, 'unreachable'))
      .mockRejectedValueOnce(new ApiError(502, 'bad gateway'))
      .mockResolvedValue('awake')

    const { result } = renderHook(() => useResource(load, 'k'))

    await advance(0)
    expect(result.current.phase).toBe('waking')
    expect(load).toHaveBeenCalledTimes(1)

    // The pauses grow: two seconds after the first failure, four after the second.
    await advance(2_000)
    expect(load).toHaveBeenCalledTimes(2)
    expect(result.current.phase).toBe('waking')

    await advance(4_000)
    expect(load).toHaveBeenCalledTimes(3)
    expect(result.current.phase).toBe('ready')
    expect(result.current.data).toBe('awake')
    expect(result.current.error).toBeUndefined()
  })

  it('gives up on a server that never wakes', async () => {
    const load = vi.fn(() => Promise.reject(new ApiError(0, 'unreachable')))
    const { result } = renderHook(() => useResource(load, 'k'))

    await advance(giveUpAfterMs + 20_000)

    expect(result.current.phase).toBe('error')
    expect(result.current.error?.message).toBe('unreachable')
    const calls = load.mock.calls.length
    await advance(60_000)
    expect(load).toHaveBeenCalledTimes(calls)
  })

  it('shows a real error at once, without pretending the server is waking', async () => {
    const load = vi.fn(() => Promise.reject(new ApiError(404, 'incident not found')))
    const { result } = renderHook(() => useResource(load, 'k'))

    await advance(0)

    expect(result.current.phase).toBe('error')
    expect(result.current.error?.status).toBe(404)
    await advance(30_000)
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('refreshes in the background and keeps old data when a refresh fails', async () => {
    const load = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('first')
      .mockRejectedValueOnce(new ApiError(500, 'internal server error'))
      .mockResolvedValue('third')

    const { result } = renderHook(() => useResource(load, 'k', { refreshMs: 5_000 }))
    await advance(0)
    expect(result.current.data).toBe('first')

    await advance(5_000)
    expect(result.current.phase).toBe('ready')
    expect(result.current.data).toBe('first')
    expect(result.current.error?.status).toBe(500)

    await advance(5_000)
    expect(result.current.data).toBe('third')
    expect(result.current.error).toBeUndefined()
  })

  it('loads again when asked to and when the key changes', async () => {
    const load = vi.fn((_: AbortSignal) => Promise.resolve('data'))
    const { result, rerender } = renderHook(({ k }) => useResource(load, k), { initialProps: { k: 'a' } })
    await advance(0)
    expect(load).toHaveBeenCalledTimes(1)

    act(() => result.current.reload())
    await advance(0)
    expect(load).toHaveBeenCalledTimes(2)

    rerender({ k: 'b' })
    await advance(0)
    expect(load).toHaveBeenCalledTimes(3)
  })

  it('cancels the request and stops all timers when the component goes away', async () => {
    const signals: AbortSignal[] = []
    const load = vi.fn((signal: AbortSignal) => {
      signals.push(signal)
      return Promise.resolve('data')
    })
    const { unmount } = renderHook(() => useResource(load, 'k', { refreshMs: 1_000 }))
    await advance(0)

    unmount()

    expect(signals[0]?.aborted).toBe(true)
    await advance(10_000)
    expect(load).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })
})
