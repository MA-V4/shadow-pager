import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { maxReconnectDelayMs, reconnectDelay, useEventStream } from './useEventStream'

// FAKE

// FakeEventSource stands in for the browser's EventSource, so tests decide when it opens, speaks and breaks.
class FakeEventSource {
  static instances: FakeEventSource[] = []

  readonly url: string
  closed = false
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private listeners = new Map<string, ((event: MessageEvent<string>) => void)[]>()

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(name: string, listener: (event: MessageEvent<string>) => void): void {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), listener])
  }

  close(): void {
    this.closed = true
  }

  // emit delivers one named event with the given text as its data.
  emit(name: string, data: string): void {
    for (const listener of this.listeners.get(name) ?? []) {
      listener(new MessageEvent(name, { data }))
    }
  }
}

// newest gives back the connection that was opened last.
function newest(): FakeEventSource {
  const source = FakeEventSource.instances.at(-1)
  if (!source) {
    throw new Error('no connection was opened')
  }
  return source
}

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

// TESTS

describe('reconnectDelay', () => {
  it('doubles every time and stops growing at the limit', () => {
    expect([0, 1, 2, 3, 4].map(reconnectDelay)).toEqual([1_000, 2_000, 4_000, 8_000, 16_000])
    expect(reconnectDelay(5)).toBe(maxReconnectDelayMs)
    expect(reconnectDelay(50)).toBe(maxReconnectDelayMs)
  })
})

describe('useEventStream', () => {
  it('connects, reports open, and hands parsed events to the handler', () => {
    const onEvent = vi.fn()
    const { result } = renderHook(() => useEventStream('/api/v1/stream', 'metric', onEvent))

    expect(FakeEventSource.instances).toHaveLength(1)
    expect(newest().url).toBe('/api/v1/stream')
    expect(result.current).toBe('connecting')

    act(() => newest().onopen?.())
    expect(result.current).toBe('open')

    act(() => newest().emit('metric', '{"simulation_id":"sim_1","latency_p99_ms":950}'))
    expect(onEvent).toHaveBeenCalledWith({ simulation_id: 'sim_1', latency_p99_ms: 950 })
  })

  it('ignores other event names and skips a broken message without stopping', () => {
    const onEvent = vi.fn()
    renderHook(() => useEventStream('/api/v1/stream', 'metric', onEvent))

    act(() => {
      newest().emit('something_else', '{"a":1}')
      newest().emit('metric', 'this is not json')
      newest().emit('metric', '{"ok":true}')
    })

    expect(onEvent).toHaveBeenCalledTimes(1)
    expect(onEvent).toHaveBeenCalledWith({ ok: true })
  })

  it('reconnects with growing pauses and starts over after a good connection', () => {
    const { result } = renderHook(() => useEventStream('/api/v1/stream', 'metric', vi.fn()))
    const first = newest()

    act(() => first.onerror?.())
    expect(first.closed).toBe(true)
    expect(result.current).toBe('reconnecting')

    // The first retry comes after one second and not a moment sooner.
    act(() => vi.advanceTimersByTime(999))
    expect(FakeEventSource.instances).toHaveLength(1)
    act(() => vi.advanceTimersByTime(1))
    expect(FakeEventSource.instances).toHaveLength(2)

    // The second failure in a row waits twice as long.
    act(() => newest().onerror?.())
    act(() => vi.advanceTimersByTime(1_999))
    expect(FakeEventSource.instances).toHaveLength(2)
    act(() => vi.advanceTimersByTime(1))
    expect(FakeEventSource.instances).toHaveLength(3)

    // Once a connection works, the next failure starts again at one second.
    act(() => newest().onopen?.())
    expect(result.current).toBe('open')
    act(() => newest().onerror?.())
    act(() => vi.advanceTimersByTime(1_000))
    expect(FakeEventSource.instances).toHaveLength(4)
  })

  it('hangs up and cancels a waiting reconnect when the component goes away', () => {
    const { unmount } = renderHook(() => useEventStream('/api/v1/stream', 'metric', vi.fn()))
    const first = newest()
    act(() => first.onerror?.())

    unmount()

    expect(first.closed).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
    act(() => vi.advanceTimersByTime(60_000))
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('closes an open connection when the component goes away', () => {
    const { unmount } = renderHook(() => useEventStream('/api/v1/stream', 'metric', vi.fn()))
    act(() => newest().onopen?.())

    unmount()

    expect(newest().closed).toBe(true)
  })

  it('opens a new connection when the address changes, and none without an address', () => {
    const { rerender } = renderHook(({ url }: { url: string | null }) => useEventStream(url, 'metric', vi.fn()), {
      initialProps: { url: null as string | null },
    })
    expect(FakeEventSource.instances).toHaveLength(0)

    rerender({ url: '/api/v1/stream?simulation_id=sim_1' })
    expect(FakeEventSource.instances).toHaveLength(1)
    const first = newest()

    rerender({ url: '/api/v1/stream?simulation_id=sim_2' })
    expect(first.closed).toBe(true)
    expect(FakeEventSource.instances).toHaveLength(2)
    expect(newest().url).toBe('/api/v1/stream?simulation_id=sim_2')
  })

  it('always calls the newest handler without reconnecting', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = renderHook(({ handler }) => useEventStream('/api/v1/stream', 'metric', handler), {
      initialProps: { handler: first },
    })

    rerender({ handler: second })
    act(() => newest().emit('metric', '{"n":1}'))

    expect(FakeEventSource.instances).toHaveLength(1)
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledWith({ n: 1 })
  })
})
