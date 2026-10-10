import { Link } from 'react-router-dom'

import { api } from '../api/client'
import { useResource } from '../api/useResource'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { IncidentStatusPill, SeverityPill } from '../components/Pills'
import { Card } from '../components/ui/card'
import { formatDateTime, formatGoDuration, humanize } from '../lib/format'
import type { PageRoute } from '../lib/routes'

export const route: PageRoute = { path: '/incidents', label: 'Incidents', order: 2 }

// How often the list is fetched again, so new incidents and status changes show up by themselves.
const refreshMs = 10_000

// IncidentsPage lists every incident, newest first, with its severity and how far along it is.
export default function IncidentsPage() {
  const incidents = useResource((signal) => api.listIncidents(signal), 'incidents', { refreshMs })
  const list = incidents.data?.incidents ?? []

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Incidents</h1>
        <p className="text-sm text-muted-foreground">
          An incident is declared when a simulation breaks its SLO. Responders move it along from Slack.
        </p>
      </div>

      {incidents.phase === 'loading' ? <LoadingBlock label="Loading incidents" lines={4} /> : null}
      {incidents.phase === 'waking' ? <WakingNotice /> : null}
      {incidents.phase === 'error' ? (
        <ErrorNotice
          title="The incidents could not be loaded"
          message={incidents.error?.message ?? 'Unknown error.'}
          onRetry={incidents.reload}
        />
      ) : null}
      {incidents.phase === 'ready' && list.length === 0 ? (
        <EmptyState title="No incidents yet">
          Inject a failure on the <Link to="/" className="underline underline-offset-4">dashboard</Link> and wait for it to break its SLO.
        </EmptyState>
      ) : null}

      {list.length > 0 ? (
        <Card>
          <ul className="divide-y" aria-label="Incidents, newest first">
            {list.map((incident) => (
              <li key={incident.id}>
                <Link
                  to={`/incidents/${encodeURIComponent(incident.id)}`}
                  className="flex flex-col gap-2 px-4 py-3 hover:bg-muted/60 sm:flex-row sm:items-center sm:gap-4"
                >
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="truncate font-medium">
                      {humanize(incident.mode)} on <span className="font-mono">{incident.service}</span>
                    </span>
                    <span className="text-xs text-muted-foreground">
                      Declared {formatDateTime(incident.declared_at)}
                      {incident.time_to_resolve === undefined ? '' : `, resolved after ${formatGoDuration(incident.time_to_resolve)}`}
                    </span>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <SeverityPill severity={incident.severity} />
                    <IncidentStatusPill status={incident.status} />
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        </Card>
      ) : null}
    </div>
  )
}
