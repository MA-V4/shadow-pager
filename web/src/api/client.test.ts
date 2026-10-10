import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError, isAbort, toApiError } from './client'

// HELPERS

const fetchMock = vi.fn<typeof fetch>()

// jsonResponse builds an answer the way the Go server sends it.
function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } })
}

// lastCall gives back the address and the options of the newest fetch call.
function lastCall(): { url: string; init: RequestInit } {
  const call = fetchMock.mock.calls.at(-1)
  if (!call) {
    throw new Error('fetch was never called')
  }
  return { url: String(call[0]), init: call[1] ?? {} }
}

// caught runs a call that must fail and gives back what it threw.
async function caught(call: Promise<unknown>): Promise<unknown> {
  try {
    await call
  } catch (err) {
    return err
  }
  throw new Error('the call did not fail')
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// TESTS

describe('api requests', () => {
  it('reads a list from the right address', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { simulations: [] }))

    const result = await api.listSimulations()

    expect(result).toEqual({ simulations: [] })
    const { url, init } = lastCall()
    expect(url).toBe('/api/v1/simulations')
    expect(init.method).toBe('GET')
    expect(init.headers).toEqual({ Accept: 'application/json' })
  })

  it('adds the limit and escapes ids', async () => {
    fetchMock.mockImplementation(() => Promise.resolve(jsonResponse(200, {})))

    await api.listIncidents(undefined, 20)
    expect(lastCall().url).toBe('/api/v1/incidents?limit=20')

    await api.getIncident('inc/../1')
    expect(lastCall().url).toBe('/api/v1/incidents/inc%2F..%2F1')

    expect(api.streamUrl()).toBe('/api/v1/stream')
    expect(api.streamUrl('sim a&b')).toBe('/api/v1/stream?simulation_id=sim%20a%26b')
  })

  it('sends the body and the admin token when injecting', async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, { id: 'sim_1' }))
    const body = { service: 'payments-api', mode: 'latency_spike', severity: 'major', intensity: 0.9, duration: '2m' } as const

    await api.createSimulation(body, 's3cret')

    const { url, init } = lastCall()
    expect(url).toBe('/api/v1/simulations')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify(body))
    expect(init.headers).toEqual({
      Accept: 'application/json',
      'Content-Type': 'application/json',
      Authorization: 'Bearer s3cret',
    })
  })

  it('sends no Authorization header without a token', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { id: 'sim_1' }))

    await api.haltSimulation('sim_1', '')

    const { url, init } = lastCall()
    expect(url).toBe('/api/v1/simulations/sim_1/halt')
    expect(init.headers).toEqual({ Accept: 'application/json' })
  })
})

describe('api errors', () => {
  it('carries the status and the server message', async () => {
    fetchMock.mockResolvedValue(jsonResponse(400, { error: 'inject: invalid scenario: service is required' }))

    const err = await caught(api.createSimulation({ service: '', mode: 'latency_spike', severity: 'major', intensity: 1, duration: '2m' }))

    expect(err).toBeInstanceOf(ApiError)
    const apiError = err as ApiError
    expect(apiError.status).toBe(400)
    expect(apiError.message).toBe('inject: invalid scenario: service is required')
    expect(apiError.retryAfterSeconds).toBeUndefined()
    expect(apiError.isWaking).toBe(false)
  })

  it('reads the retry time of a rate limit', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(429, { error: 'too many requests, try again in 17 seconds' }, { 'Retry-After': '17' }),
    )

    const err = (await caught(api.haltSimulation('sim_1'))) as ApiError

    expect(err.status).toBe(429)
    expect(err.retryAfterSeconds).toBe(17)
  })

  it.each([
    ['a missing header', {}],
    ['a header that is not a number', { 'Retry-After': 'soon' }],
    ['a header of zero', { 'Retry-After': '0' }],
  ])('has no retry time for %s', async (_name, headers) => {
    fetchMock.mockResolvedValue(jsonResponse(429, { error: 'active simulation limit reached' }, headers))

    const err = (await caught(api.createSimulation({ service: 'a', mode: 'latency_spike', severity: 'major', intensity: 1, duration: '2m' }))) as ApiError

    expect(err.retryAfterSeconds).toBeUndefined()
  })

  it('falls back to the status when a proxy answers with something that is not JSON', async () => {
    fetchMock.mockResolvedValue(new Response('<html>Bad Gateway</html>', { status: 502 }))

    const err = (await caught(api.getStatus())) as ApiError

    expect(err.status).toBe(502)
    expect(err.message).toBe('The server answered with status 502.')
    expect(err.isWaking).toBe(true)
  })

  it('falls back to the status when the error field is missing or empty', async () => {
    fetchMock.mockResolvedValue(jsonResponse(500, { error: '' }))

    const err = (await caught(api.getSlo())) as ApiError

    expect(err.message).toBe('The server answered with status 500.')
    expect(err.isWaking).toBe(false)
  })

  it('reports a server that cannot be reached as status 0', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'))

    const err = (await caught(api.listSimulations())) as ApiError

    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(0)
    expect(err.isWaking).toBe(true)
    expect(err.message).toContain('could not be reached')
  })

  it('lets a cancelled request through as an abort, not as an ApiError', async () => {
    fetchMock.mockRejectedValue(new DOMException('The operation was aborted.', 'AbortError'))

    const err = await caught(api.listSimulations(new AbortController().signal))

    expect(err).not.toBeInstanceOf(ApiError)
    expect(isAbort(err)).toBe(true)
  })

  it('wraps anything else that was thrown', () => {
    const known = new ApiError(404, 'incident not found')
    expect(toApiError(known)).toBe(known)
    expect(toApiError(new Error('boom')).message).toBe('boom')
    expect(toApiError('just a string').status).toBe(0)
  })
})
