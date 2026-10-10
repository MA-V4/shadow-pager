import {
  incidentStatusLabels,
  labelOf,
  serviceLevelLabels,
  severityLabels,
  simulationStateLabels,
} from '../lib/labels'
import { StatusGlyph } from './StatusGlyph'
import { Badge, type BadgeTone } from './ui/badge'

// TONES

export const severityTone: Record<string, BadgeTone> = { critical: 'bad', major: 'warn', minor: 'neutral' }
export const incidentStatusTone: Record<string, BadgeTone> = {
  declared: 'bad',
  acknowledged: 'warn',
  mitigated: 'info',
  resolved: 'ok',
}
export const simulationStateTone: Record<string, BadgeTone> = { running: 'info', resolved: 'neutral', halted: 'neutral' }
export const serviceLevelTone: Record<string, BadgeTone> = { operational: 'ok', degraded: 'warn', major_outage: 'bad' }

// StatusPill is a word with its shape, where the shape repeats what the colour says.
function StatusPill({ tone, label }: { tone: BadgeTone; label: string }) {
  return (
    <Badge tone={tone}>
      <StatusGlyph tone={tone} coloured={false} className="h-1.5 w-1.5" />
      {label}
    </Badge>
  )
}

// PILLS

// SeverityPill shows how bad an incident is, in words and in colour.
export function SeverityPill({ severity }: { severity: string }) {
  return <StatusPill tone={severityTone[severity] ?? 'neutral'} label={labelOf(severityLabels, severity)} />
}

// IncidentStatusPill shows which step an incident has reached.
export function IncidentStatusPill({ status }: { status: string }) {
  return <StatusPill tone={incidentStatusTone[status] ?? 'neutral'} label={labelOf(incidentStatusLabels, status)} />
}

// SimulationStatePill shows if a simulation is still running or how it ended.
export function SimulationStatePill({ state }: { state: string }) {
  return <StatusPill tone={simulationStateTone[state] ?? 'neutral'} label={labelOf(simulationStateLabels, state)} />
}

// ServiceLevelPill shows how a service is doing on the status page.
export function ServiceLevelPill({ level }: { level: string }) {
  return <StatusPill tone={serviceLevelTone[level] ?? 'neutral'} label={labelOf(serviceLevelLabels, level)} />
}
