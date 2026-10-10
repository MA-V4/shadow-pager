import { useCallback, useState } from 'react'

import { readStored, writeStored } from './storage'

const adminTokenKey = 'shadow-pager.admin-token'

// useAdminToken remembers the optional admin token in this browser, so it does not have to be typed again.
export function useAdminToken(): [string, (token: string) => void] {
  const [token, setToken] = useState(() => readStored(adminTokenKey) ?? '')

  const update = useCallback((next: string) => {
    const trimmed = next.trim()
    writeStored(adminTokenKey, trimmed)
    setToken(trimmed)
  }, [])

  return [token, update]
}
