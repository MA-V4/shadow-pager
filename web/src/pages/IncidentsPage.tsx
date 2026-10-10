import { Link } from 'react-router-dom'

import { api } from '../api/client'
import type { Incident } from '../api/types'
import { useResource } from '../api/useResource'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { IncidentStatusPill, SeverityPill } from '../components/Pills'
import { formatDateTime, formatGoDuration } from '../lib/format'
import { failureModeLabels, labelOf } from '../lib/labels'
import type { PageRoute } from '../lib/routes'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/incidents', label: 'Incidents', order: 2 }

// How often the list is fetched again, so new incidents and status changes show up by themselves.
const refreshMs = 10_000

// IncidentRow is one incident in the list, and the whole row is a link to its detail page.
function IncidentRow({ incident }: { incident: Incident }) {
  const open = incident.status !== 'resolved'
  return (
    <li>
      <Link
        to={`/incidents/${encodeURIComponent(incident.id)}`}
        className={cn(
          'flex flex-col gap-2 border-l-[3px] border-l-transparent px-4 py-3 transition-colors duration-150 hover:bg-muted/60 sm:flex-row sm:items-center sm:gap-4',
          // Only an incident that nobody has picked up yet gets the red edge.
          incident.status === 'declared' && 'border-l-bad',
        )}
      >
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className={cn('truncate', open ? 'font-medium' : 'text-muted-foreground')}>
            {labelOf(failureModeLabels, incident.mode)} on{' '}
            <span className="font-mono" translate="no">
              {incident.service}
            </span>
          </span>
          <span className="truncate text-xs text-muted-foreground">
            Declared {formatDateTime(incident.declared_at)}
            {incident.time_to_resolve === undefined ? '' : `, resolved after ${formatGoDuration(incident.time_to_resolve)}`}
          </span>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <SeverityPill severity={incident.severity} />
          <IncidentStatusPill status={incident.status} />
        </div>
      </Link>
    </li>
  )
}

// IncidentsPage lists every incident, newest first, with its severity and how far along it is.
export default function IncidentsPage() {
  const incidents = useResource((signal) => api.listIncidents(signal), 'incidents', { refreshMs })
  const list = incidents.data?.incidents ?? []
  const open = list.filter((incident) => incident.status !== 'resolved')
  const resolved = list.filter((incident) => incident.status === 'resolved')

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Incidents</h1>
        <p className="max-w-[65ch] text-sm text-muted-foreground">
          An incident is declared when a simulation breaks its SLO. Responders move it along from Slack.
        </p>
      </div>

      {incidents.phase === 'loading' ? <LoadingBlock label="Loading incidents…" lines={4} /> : null}
      {incidents.phase === 'waking' ? <WakingNotice /> : null}
      {incidents.phase === 'error' ? (
        <ErrorNotice
          title="Could not load the incidents"
          message={incidents.error?.message ?? 'Unknown error.'}
          onRetry={incidents.reload}
        />
      ) : null}
      {incidents.phase === 'ready' && list.length === 0 ? (
        <EmptyState title="No incidents yet">
          Inject a failure on the{' '}
          <Link to="/" className="underline underline-offset-4 hover:text-foreground">
            dashboard
          </Link>{' '}
          and wait for it to break its SLO.
        </EmptyState>
      ) : null}

      {list.length > 0 ? (
        <ul className="divide-y overflow-hidden rounded-lg border bg-card shadow-card" aria-label="Incidents, newest first">
          {open.map((incident) => (
            <IncidentRow key={incident.id} incident={incident} />
          ))}
          {resolved.map((incident) => (
            <IncidentRow key={incident.id} incident={incident} />
          ))}
        </ul>
      ) : null}
    </div>
  )
}
