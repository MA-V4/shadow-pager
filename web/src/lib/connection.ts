import { useEffect, useSyncExternalStore } from 'react'

import type { StreamStatus } from '../api/useEventStream'
import type { ResourcePhase } from '../api/useResource'

// STATE

// idle means that the page on screen does not talk to the server at all.
export type ConnectionState = 'idle' | 'connecting' | 'live' | 'reconnecting' | 'waking' | 'offline'

export const connectionLabels: Record<Exclude<ConnectionState, 'idle'>, string> = {
  connecting: 'Connecting',
  live: 'Live',
  reconnecting: 'Reconnecting',
  waking: 'Waking server',
  offline: 'Offline',
}

// connectionOf boils the state of a page's data and of its live stream down to the one word the header shows.
export function connectionOf(phase: ResourcePhase, stream?: StreamStatus): ConnectionState {
  if (phase === 'waking') {
    return 'waking'
  }
  if (phase === 'error') {
    return 'offline'
  }
  if (stream === undefined) {
    return phase === 'ready' ? 'live' : 'connecting'
  }
  return stream === 'open' ? 'live' : stream
}

// STORE

// The state lives outside of React, so the page that knows it and the header that shows it stay apart.
let current: ConnectionState = 'idle'
const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

// getConnection gives back what the header shows right now.
export function getConnection(): ConnectionState {
  return current
}

// setConnection changes what the header shows and tells it to draw again.
export function setConnection(next: ConnectionState): void {
  if (next === current) {
    return
  }
  current = next
  for (const listener of listeners) {
    listener()
  }
}

// HOOKS

// useConnection is how the header reads the state.
export function useConnection(): ConnectionState {
  return useSyncExternalStore(subscribe, getConnection, (): ConnectionState => 'idle')
}

// useReportConnection is how a page tells the header how its link to the server is doing.
export function useReportConnection(state: ConnectionState): void {
  useEffect(() => {
    setConnection(state)
  }, [state])
  // A page that goes away takes its state with it, so the header never shows a stale word.
  useEffect(() => () => setConnection('idle'), [])
}
