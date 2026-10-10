import { humanize } from '../lib/format'
import { Badge, type BadgeTone } from './ui/badge'

// TONES

const severityTone: Record<string, BadgeTone> = { critical: 'bad', major: 'warn', minor: 'info' }
const incidentStatusTone: Record<string, BadgeTone> = {
  declared: 'bad',
  acknowledged: 'warn',
  mitigated: 'info',
  resolved: 'ok',
}
const simulationStateTone: Record<string, BadgeTone> = { running: 'info', resolved: 'ok', halted: 'neutral' }
const serviceLevelTone: Record<string, BadgeTone> = { operational: 'ok', degraded: 'warn', major_outage: 'bad' }

// Dot is the small coloured circle in front of a status word.
function Dot() {
  return <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-current" />
}

// PILLS

// SeverityPill shows how bad an incident is, in words and in colour.
export function SeverityPill({ severity }: { severity: string }) {
  return <Badge tone={severityTone[severity] ?? 'neutral'}>{humanize(severity)}</Badge>
}

// IncidentStatusPill shows which step an incident has reached.
export function IncidentStatusPill({ status }: { status: string }) {
  return (
    <Badge tone={incidentStatusTone[status] ?? 'neutral'}>
      <Dot />
      {humanize(status)}
    </Badge>
  )
}

// SimulationStatePill shows if a simulation is still running or how it ended.
export function SimulationStatePill({ state }: { state: string }) {
  return (
    <Badge tone={simulationStateTone[state] ?? 'neutral'}>
      <Dot />
      {humanize(state)}
    </Badge>
  )
}

// ServiceLevelPill shows how a service is doing on the status page.
export function ServiceLevelPill({ level }: { level: string }) {
  return (
    <Badge tone={serviceLevelTone[level] ?? 'neutral'}>
      <Dot />
      {humanize(level)}
    </Badge>
  )
}
