import type { Incident } from '../api/types'
import { formatElapsed, formatGoDuration } from './format'

// incidentDuration says how long an incident has been open, or how long it took to resolve.
export function incidentDuration(incident: Pick<Incident, 'declared_at' | 'time_to_resolve'>, nowMs: number): string {
  if (incident.time_to_resolve !== undefined) {
    return formatGoDuration(incident.time_to_resolve)
  }
  return formatElapsed(nowMs - new Date(incident.declared_at).getTime())
}
