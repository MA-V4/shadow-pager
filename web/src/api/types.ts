// These types mirror the JSON that the Go API sends and expects, field for field.

// SHARED WORDS

export type FailureMode = 'latency_spike' | 'error_rate_surge' | 'memory_leak' | 'cpu_saturation' | 'db_pool_exhaustion'
export type Severity = 'critical' | 'major' | 'minor'
export type SimulationState = 'running' | 'resolved' | 'halted'
export type IncidentStatus = 'declared' | 'acknowledged' | 'mitigated' | 'resolved'
export type ServiceLevel = 'operational' | 'degraded' | 'major_outage'

export const failureModes: readonly FailureMode[] = [
  'latency_spike',
  'error_rate_surge',
  'memory_leak',
  'cpu_saturation',
  'db_pool_exhaustion',
]
export const severities: readonly Severity[] = ['critical', 'major', 'minor']

// ERRORS

// ErrorBody is the body of every answer that is not a success.
export interface ErrorBody {
  error: string
}

// SIMULATIONS

// CreateSimulationRequest is what POST /simulations expects.
export interface CreateSimulationRequest {
  service: string
  mode: FailureMode
  severity: Severity
  // From 0 for barely noticeable to 1 for everything broken.
  intensity: number
  // A Go duration such as 2m or 90s, from 10s to 30m.
  duration: string
}

// Simulation is one simulation as the API describes it.
export interface Simulation {
  id: string
  service: string
  mode: FailureMode
  severity: Severity
  intensity: number
  duration: string
  state: SimulationState
  started_at: string
  ends_at: string
  // Missing while the simulation is still running.
  ended_at?: string
}

export interface SimulationList {
  simulations: Simulation[]
}

// METRICS

// MetricSample is the data of one metric event on the stream.
export interface MetricSample {
  simulation_id: string
  service: string
  mode: FailureMode
  timestamp: string
  latency_p99_ms: number
  // A share from 0 to 1.
  error_rate: number
  cpu_percent: number
  memory_mb: number
  healthy: boolean
  breaches: string[]
}

// Slo holds the limits that count as a breach when a metric goes above them.
export interface Slo {
  max_latency_p99_ms: number
  max_error_rate: number
  max_cpu_percent: number
  max_memory_mb: number
}

// INCIDENTS

export interface TimelineEntry {
  at: string
  actor: string
  text: string
}

// Incident is one incident with its whole story.
export interface Incident {
  id: string
  simulation_id: string
  service: string
  mode: FailureMode
  severity: Severity
  status: IncidentStatus
  breaches: string[]
  declared_at: string
  acknowledged_at?: string
  acknowledged_by?: string
  mitigated_at?: string
  resolved_at?: string
  // Go durations such as 1m30s, missing until the step has happened.
  time_to_acknowledge?: string
  time_to_resolve?: string
  slack_channel_id?: string
  timeline: TimelineEntry[]
}

export interface IncidentList {
  incidents: Incident[]
}

// PUBLIC STATUS

export interface StatusMetrics {
  latency_p99_ms: number
  error_rate: number
  cpu_percent: number
  memory_mb: number
  breaches: string[]
  sampled_at: string
}

export interface StatusService {
  name: string
  status: ServiceLevel
  // Null when nothing was measured lately.
  metrics: StatusMetrics | null
}

export interface StatusUpdate {
  at: string
  text: string
}

// StatusIncident is the public part of an open incident, without names or Slack details.
export interface StatusIncident {
  id: string
  service: string
  mode: FailureMode
  severity: Severity
  status: IncidentStatus
  breaches: string[]
  declared_at: string
  acknowledged_at?: string
  mitigated_at?: string
  updates: StatusUpdate[]
}

// StatusPage is the answer of GET /status.
export interface StatusPage {
  status: ServiceLevel
  updated_at: string
  services: StatusService[]
  incidents: StatusIncident[]
}
