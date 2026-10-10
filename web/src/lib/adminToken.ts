import { useCallback, useSyncExternalStore } from 'react'

import { readStored, writeStored } from './storage'

const adminTokenKey = 'shadow-pager.admin-token'

// STORE

// The token lives in one place outside of React, so the settings in the header and the forms on a page always agree.
let current: string | undefined
const listeners = new Set<() => void>()

function getToken(): string {
  current ??= readStored(adminTokenKey) ?? ''
  return current
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

// setAdminToken saves a new token in this browser and tells every part of the page that shows or uses it.
export function setAdminToken(next: string): void {
  const trimmed = next.trim()
  current = trimmed
  writeStored(adminTokenKey, trimmed)
  for (const listener of listeners) {
    listener()
  }
}

// resetAdminTokenForTests forgets the token held in memory, so each test starts from what is in storage.
export function resetAdminTokenForTests(): void {
  current = undefined
}

// HOOK

// useAdminToken remembers the optional admin token in this browser, so it does not have to be typed again.
export function useAdminToken(): [string, (token: string) => void] {
  const token = useSyncExternalStore(subscribe, getToken, () => '')
  const update = useCallback((next: string) => setAdminToken(next), [])
  return [token, update]
}
