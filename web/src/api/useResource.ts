import { useCallback, useEffect, useRef, useState } from 'react'

import { isAbort, toApiError, type ApiError } from './client'

// TIMING

// How long the first answer may take before we say the server is waking up.
export const wakingAfterMs = 3_000
// How long we keep trying to reach a server that is waking up before we call it an error.
export const giveUpAfterMs = 90_000
// The longest pause between two tries while the server is waking up.
const maxRetryDelayMs = 10_000

// STATE

export type ResourcePhase = 'loading' | 'waking' | 'ready' | 'error'

export interface Resource<T> {
  phase: ResourcePhase
  // The newest answer, kept while a later refresh is running or has failed.
  data: T | undefined
  // The error of the last try, also set when a background refresh failed and old data is still shown.
  error: ApiError | undefined
  // reload fetches again right now.
  reload: () => void
}

interface Options {
  // How often to fetch again in the background, or nothing to fetch only once.
  refreshMs?: number
}

// HOOK

// useResource loads data from the API, keeps it fresh, and tells apart a slow waking server from a real error.
export function useResource<T>(load: (signal: AbortSignal) => Promise<T>, key: string, options: Options = {}): Resource<T> {
  const { refreshMs } = options
  const [phase, setPhase] = useState<ResourcePhase>('loading')
  const [data, setData] = useState<T | undefined>(undefined)
  const [error, setError] = useState<ApiError | undefined>(undefined)
  const [nonce, setNonce] = useState(0)

  // The newest load function is kept in a ref, so a new function alone does not restart the loading.
  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  }, [load])

  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    let stopped = false
    let hasData = false
    let attempt = 0
    const startedAt = Date.now()

    setPhase('loading')
    setError(undefined)

    // A slow first answer is most likely a sleeping server, so we say that instead of showing a spinner forever.
    const wakingTimer = setTimeout(() => {
      if (!stopped && !hasData) {
        setPhase('waking')
      }
    }, wakingAfterMs)

    async function run(): Promise<void> {
      try {
        const result = await loadRef.current(controller.signal)
        if (stopped) {
          return
        }
        hasData = true
        attempt = 0
        setData(result)
        setError(undefined)
        setPhase('ready')
        if (refreshMs !== undefined) {
          timer = setTimeout(() => void run(), refreshMs)
        }
      } catch (err) {
        if (stopped || isAbort(err)) {
          return
        }
        const apiError = toApiError(err)
        setError(apiError)

        if (hasData) {
          // Old data stays on screen, and we simply try again at the usual pace.
          if (refreshMs !== undefined) {
            timer = setTimeout(() => void run(), refreshMs)
          }
          return
        }
        if (apiError.isWaking && Date.now() - startedAt < giveUpAfterMs) {
          setPhase('waking')
          attempt += 1
          timer = setTimeout(() => void run(), Math.min(1_000 * 2 ** attempt, maxRetryDelayMs))
          return
        }
        setPhase('error')
      }
    }
    void run()

    return () => {
      stopped = true
      controller.abort()
      clearTimeout(wakingTimer)
      clearTimeout(timer)
    }
  }, [key, nonce, refreshMs])

  const reload = useCallback(() => setNonce((n) => n + 1), [])
  return { phase, data, error, reload }
}
