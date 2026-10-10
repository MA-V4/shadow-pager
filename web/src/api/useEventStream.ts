import { useEffect, useRef, useState } from 'react'

// TIMING

// The first pause before reconnecting, which doubles after every failed try.
export const firstReconnectDelayMs = 1_000
// The longest pause between two tries to reconnect.
export const maxReconnectDelayMs = 30_000

// reconnectDelay gives back how long to wait before the given try, counting from zero.
export function reconnectDelay(attempt: number): number {
  return Math.min(firstReconnectDelayMs * 2 ** attempt, maxReconnectDelayMs)
}

// HOOK

export type StreamStatus = 'connecting' | 'open' | 'reconnecting'

// useEventStream listens to a server-sent event stream, reconnects with growing pauses, and hangs up when the component goes away.
export function useEventStream<T>(url: string | null, eventName: string, onEvent: (payload: T) => void): StreamStatus {
  const [status, setStatus] = useState<StreamStatus>('connecting')

  // The newest handler is kept in a ref, so a new function alone does not reopen the connection.
  const onEventRef = useRef(onEvent)
  useEffect(() => {
    onEventRef.current = onEvent
  }, [onEvent])

  useEffect(() => {
    // Without an address, or in a browser that has no EventSource, there is nothing to connect to.
    if (url === null || typeof EventSource === 'undefined') {
      return
    }
    const target = url
    let source: EventSource | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    let closed = false

    function connect(): void {
      source = new EventSource(target)

      source.onopen = () => {
        attempt = 0
        setStatus('open')
      }

      source.addEventListener(eventName, (event: MessageEvent<string>) => {
        try {
          onEventRef.current(JSON.parse(event.data) as T)
        } catch {
          // One broken message must not stop the stream, so we skip it and wait for the next.
        }
      })

      source.onerror = () => {
        // The browser would retry by itself at a fixed pace, so we close and take over with our own growing pauses.
        source?.close()
        if (closed) {
          return
        }
        setStatus('reconnecting')
        timer = setTimeout(connect, reconnectDelay(attempt))
        attempt += 1
      }
    }

    setStatus('connecting')
    connect()

    return () => {
      closed = true
      clearTimeout(timer)
      source?.close()
    }
  }, [url, eventName])

  return status
}
