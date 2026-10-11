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

// The same names inside a sentence, where only acronyms keep their capitals.
export const failureModePhrases: Record<FailureMode, string> = {
  latency_spike: 'a latency spike',
  error_rate_surge: 'an error rate surge',
  memory_leak: 'a memory leak',
  cpu_saturation: 'CPU saturation',
  db_pool_exhaustion: 'DB pool exhaustion',
}

// One short line per failure mode that says what the drill will look like.
export const failureModeDescriptions: Record<FailureMode, string> = {
  latency_spike: 'Latency jumps at once and holds.',
  error_rate_surge: 'Errors climb fast and stay high.',
  memory_leak: 'Memory grows and never recovers.',
  cpu_saturation: 'CPU builds slowly, then eases.',
  db_pool_exhaustion: 'Fine at first, then a sudden collapse.',
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

// SENTENCES

// inSentence makes a label fit the middle of a sentence: the first letter goes small unless the word is an acronym.
export function inSentence(label: string): string {
  const first = label.split(' ')[0] ?? ''
  const acronym = first.length > 1 && first === first.toUpperCase()
  return acronym ? label : label.charAt(0).toLowerCase() + label.slice(1)
}

// startedSentence is the line shown after a drill starts, such as "Started an error rate surge on payments-api."
export function startedSentence(mode: string, service: string): string {
  const phrase = (failureModePhrases as Readonly<Record<string, string>>)[mode] ?? inSentence(labelOf(failureModeLabels, mode))
  return `Started ${phrase} on ${service}.`
}
