import type {
  CreateSimulationRequest,
  Incident,
  IncidentList,
  ServerConfig,
  Simulation,
  SimulationList,
  Slo,
  StatusPage,
} from './types'

// ADDRESS

// apiRoot is where every API call starts, built from VITE_API_URL with any trailing slash removed.
export const apiRoot = `${(import.meta.env.VITE_API_URL ?? '').replace(/\/+$/, '')}/api/v1`

// ERRORS

// ApiError is the one error every API call throws, carrying the HTTP status and a message a person can read.
export class ApiError extends Error {
  // The HTTP status, or 0 when the server could not be reached at all.
  readonly status: number
  // How many seconds the server asked us to wait, when it said so.
  readonly retryAfterSeconds: number | undefined

  constructor(status: number, message: string, retryAfterSeconds?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.retryAfterSeconds = retryAfterSeconds
  }

  // isWaking is true for the answers a sleeping free server gives while it starts up.
  get isWaking(): boolean {
    return this.status === 0 || this.status === 502 || this.status === 503 || this.status === 504
  }
}

// toApiError turns anything that was thrown into an ApiError, so callers only handle one kind.
export function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) {
    return err
  }
  return new ApiError(0, err instanceof Error ? err.message : 'Something unexpected went wrong.')
}

// isAbort tells us if a request stopped because we cancelled it ourselves.
export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

// REQUESTS

interface RequestOptions {
  method?: 'GET' | 'POST'
  body?: unknown
  // The admin token, sent as a bearer token when the server asks for one.
  token?: string | undefined
  signal?: AbortSignal | undefined
}

// errorMessage reads the message out of an error answer, and falls back to the status when there is none.
async function errorMessage(res: Response): Promise<string> {
  try {
    const body: unknown = await res.json()
    if (typeof body === 'object' && body !== null && 'error' in body && typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // The answer was not JSON, which happens when a proxy answers instead of our server.
  }
  return `The server answered with status ${res.status}.`
}

// retryAfter reads the Retry-After header as whole seconds.
function retryAfter(res: Response): number | undefined {
  const seconds = Number.parseInt(res.headers.get('Retry-After') ?? '', 10)
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined
}

// request sends one call to the API and gives back the decoded JSON, or throws an ApiError.
async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  if (options.token) {
    headers['Authorization'] = `Bearer ${options.token}`
  }

  let res: Response
  try {
    res = await fetch(apiRoot + path, {
      method: options.method ?? 'GET',
      headers,
      body: options.body === undefined ? null : JSON.stringify(options.body),
      signal: options.signal ?? null,
    })
  } catch (err) {
    if (isAbort(err)) {
      throw err
    }
    throw new ApiError(0, 'The server could not be reached. It may still be waking up, or your connection dropped.')
  }

  if (!res.ok) {
    throw new ApiError(res.status, await errorMessage(res), retryAfter(res))
  }
  return (await res.json()) as T
}

// ENDPOINTS

// api has one function for every endpoint of the Shadow-Pager API.
export const api = {
  listSimulations: (signal?: AbortSignal, limit?: number) =>
    request<SimulationList>(`/simulations${limit === undefined ? '' : `?limit=${limit}`}`, { signal }),

  getSimulation: (id: string, signal?: AbortSignal) => request<Simulation>(`/simulations/${encodeURIComponent(id)}`, { signal }),

  createSimulation: (body: CreateSimulationRequest, token?: string) =>
    request<Simulation>('/simulations', { method: 'POST', body, token }),

  haltSimulation: (id: string, token?: string) =>
    request<Simulation>(`/simulations/${encodeURIComponent(id)}/halt`, { method: 'POST', token }),

  listIncidents: (signal?: AbortSignal, limit?: number) =>
    request<IncidentList>(`/incidents${limit === undefined ? '' : `?limit=${limit}`}`, { signal }),

  getIncident: (id: string, signal?: AbortSignal) => request<Incident>(`/incidents/${encodeURIComponent(id)}`, { signal }),

  getStatus: (signal?: AbortSignal) => request<StatusPage>('/status', { signal }),

  getSlo: (signal?: AbortSignal) => request<Slo>('/slo', { signal }),

  getConfig: (signal?: AbortSignal) => request<ServerConfig>('/config', { signal }),

  // streamUrl is the address of the live metric stream, for all simulations or for one.
  streamUrl: (simulationId?: string) =>
    `${apiRoot}/stream${simulationId === undefined ? '' : `?simulation_id=${encodeURIComponent(simulationId)}`}`,
}
