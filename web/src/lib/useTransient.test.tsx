import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { feedbackMs, useTransient } from './useTransient'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useTransient', () => {
  it('holds a message for four seconds and then lets it go', () => {
    const { result } = renderHook(() => useTransient<string>())
    expect(feedbackMs).toBe(4_000)
    expect(result.current[0]).toBeNull()

    act(() => result.current[1]('Started a latency spike on payments-api.'))
    expect(result.current[0]).toBe('Started a latency spike on payments-api.')

    act(() => {
      vi.advanceTimersByTime(3_999)
    })
    expect(result.current[0]).toBe('Started a latency spike on payments-api.')

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(result.current[0]).toBeNull()
  })

  it('starts the four seconds again for a newer message', () => {
    const { result } = renderHook(() => useTransient<string>())

    act(() => result.current[1]('first'))
    act(() => {
      vi.advanceTimersByTime(3_000)
    })
    act(() => result.current[1]('second'))
    act(() => {
      vi.advanceTimersByTime(3_000)
    })
    expect(result.current[0]).toBe('second')

    act(() => {
      vi.advanceTimersByTime(1_000)
    })
    expect(result.current[0]).toBeNull()
  })

  it('can be cleared early, and leaves no timer behind', () => {
    const { result, unmount } = renderHook(() => useTransient<string>(500))

    act(() => result.current[1]('gone soon'))
    act(() => result.current[2]())
    expect(result.current[0]).toBeNull()

    act(() => result.current[1]('still here'))
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
