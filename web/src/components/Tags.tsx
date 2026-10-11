import { incidentStatusLabels, labelOf, severityLabels } from '../lib/labels'
import type { RailTone } from './ui/card'
import { Tag, type TagTone } from './ui/tag'

// TONES

export const severityTone: Record<string, TagTone> = { critical: 'breach', major: 'warn', minor: 'neutral' }
export const incidentStatusTone: Record<string, TagTone> = {
  declared: 'breach',
  acknowledged: 'warn',
  mitigated: 'monitor',
  resolved: 'neutral',
}

// incidentRail picks the signal rail for an incident card, and a resolved incident gets none.
export function incidentRail(status: string): RailTone | undefined {
  const tone = incidentStatusTone[status]
  return tone === undefined || tone === 'neutral' ? undefined : tone
}

// TAGS

// SeverityTag shows how bad an incident is.
export function SeverityTag({ severity }: { severity: string }) {
  return <Tag tone={severityTone[severity] ?? 'neutral'}>{labelOf(severityLabels, severity)}</Tag>
}

// PublicTag marks a drill that a visitor started, in plain grey because it is a fact and not a warning.
export function PublicTag() {
  return <Tag tone="neutral">Public drill</Tag>
}

// IncidentStatusTag shows which step an incident has reached.
export function IncidentStatusTag({ status }: { status: string }) {
  return <Tag tone={incidentStatusTone[status] ?? 'neutral'}>{labelOf(incidentStatusLabels, status)}</Tag>
}
