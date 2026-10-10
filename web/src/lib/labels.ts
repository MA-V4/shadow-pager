import type { FailureMode, IncidentStatus, ServiceLevel, Severity, SimulationState } from '../api/types'

// LABELS

// Every machine word the API sends has one proper name here, so the page never guesses at capital letters.
export const failureModeLabels: Record<FailureMode, string> = {
  latency_spike: 'Latency spike',
  error_rate_surge: 'Error rate surge',
  memory_leak: 'Memory leak',
  cpu_saturation: 'CPU saturation',
  db_pool_exhaustion: 'DB pool exhaustion',
}

export const severityLabels: Record<Severity, string> = {
  critical: 'Critical',
  major: 'Major',
  minor: 'Minor',
}

export const incidentStatusLabels: Record<IncidentStatus, string> = {
  declared: 'Declared',
  acknowledged: 'Acknowledged',
  mitigated: 'Mitigated',
  resolved: 'Resolved',
}

export const simulationStateLabels: Record<SimulationState, string> = {
  running: 'Running',
  resolved: 'Finished',
  halted: 'Halted',
}

export const serviceLevelLabels: Record<ServiceLevel, string> = {
  operational: 'Operational',
  degraded: 'Degraded',
  major_outage: 'Major outage',
}

// The API names a broken limit with a short key, and this is how each key reads to a person.
export const breachLabels: Record<string, string> = {
  latency_p99: 'Latency p99',
  error_rate: 'Error rate',
  cpu: 'CPU',
  memory: 'Memory',
}

// labelOf looks a word up in a mapping, and tidies it up by hand only when the mapping does not know it.
export function labelOf(labels: Readonly<Record<string, string>>, value: string): string {
  const known = labels[value]
  if (known !== undefined) {
    return known
  }
  const spaced = value.replaceAll('_', ' ').trim()
  return spaced === '' ? 'Unknown' : spaced.charAt(0).toUpperCase() + spaced.slice(1)
}
