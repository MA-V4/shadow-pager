import { Link } from 'react-router-dom'

import { api } from '../api/client'
import type { Incident } from '../api/types'
import { useResource } from '../api/useResource'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { ShapeGlyph } from '../components/ShapeGlyph'
import { IncidentStatusTag, PublicTag, SeverityTag } from '../components/Tags'
import { Overline } from '../components/ui/overline'
import { connectionOf, useReportConnection } from '../lib/connection'
import { formatAgo, formatDateTime } from '../lib/format'
import { incidentDuration } from '../lib/incidents'
import { failureModeLabels, labelOf } from '../lib/labels'
import type { PageRoute } from '../lib/routes'
import { useNow } from '../lib/useNow'

export const route: PageRoute = { path: '/incidents', label: 'Incidents', order: 2 }

// How often the list is fetched again, so new incidents and status changes show up by themselves.
const refreshMs = 10_000

// IncidentRow is one incident in the list, and the whole row is a link to its detail page.
function IncidentRow({ incident, now }: { incident: Incident; now: number }) {
  const open = incident.status !== 'resolved'
  return (
    <li className="border-b border-subtle">
      <Link
        to={`/incidents/${encodeURIComponent(incident.id)}`}
        className="grid min-h-11 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 px-2 py-2 transition-colors duration-150 ease-out hover:bg-raised lg:grid-cols-[8.5rem_minmax(0,1fr)_minmax(0,1.3fr)_auto_5.5rem_4.5rem] lg:py-0"
      >
        <span className="truncate font-mono text-body text-primary" translate="no">
          {incident.id}
        </span>
        <span className="order-3 col-span-2 truncate font-mono text-body text-secondary lg:order-0 lg:col-span-1" translate="no">
          {incident.service}
        </span>
        <span className="order-4 col-span-2 flex min-w-0 items-center gap-2 text-body text-secondary lg:order-0 lg:col-span-1">
          <ShapeGlyph mode={incident.mode} />
          <span className="truncate">{labelOf(failureModeLabels, incident.mode)}</span>
        </span>
        <span className="order-5 flex items-center gap-2 lg:order-0 lg:justify-end">
          {incident.public ? <PublicTag /> : null}
          <SeverityTag severity={incident.severity} />
          {/* A row under "Resolved" does not repeat that word, and an open row says how far along it is. */}
          {open ? <IncidentStatusTag status={incident.status} /> : null}
        </span>
        <span className="order-6 text-right font-mono text-meta text-secondary lg:order-0" title={open ? 'Open for' : 'Time to resolve'}>
          <span className="sr-only">{open ? 'open for ' : 'resolved after '}</span>
          {incidentDuration(incident, now)}
        </span>
        <time dateTime={incident.declared_at} title={formatDateTime(incident.declared_at)} className="order-2 text-right text-meta text-tertiary lg:order-0">
          {formatAgo(incident.declared_at, now)}
        </time>
      </Link>
    </li>
  )
}

// IncidentList is one dense group of incidents under a small section label.
function IncidentList({ id, label, incidents, now }: { id: string; label: string; incidents: Incident[]; now: number }) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-3">
      <Overline as="h2" id={id}>
        {label}
      </Overline>
      <ul aria-labelledby={id} className="border-t border-subtle">
        {incidents.map((incident) => (
          <IncidentRow key={incident.id} incident={incident} now={now} />
        ))}
      </ul>
    </section>
  )
}

// IncidentsPage lists every incident, open ones first, with its severity and how far along it is.
export default function IncidentsPage() {
  const now = useNow()
  const incidents = useResource((signal) => api.listIncidents(signal), 'incidents', { refreshMs })
  useReportConnection(connectionOf(incidents.phase))
  const list = incidents.data?.incidents ?? []
  const open = list.filter((incident) => incident.status !== 'resolved')
  const resolved = list.filter((incident) => incident.status === 'resolved')

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-8">
      <div className="flex flex-col gap-1">
        <h1 className="text-heading text-primary">Incidents</h1>
        <p className="max-w-[65ch] text-body text-secondary">
          An incident is declared when a simulation breaks its SLO. Responders move it along from Slack.
        </p>
      </div>

      {incidents.phase === 'loading' ? <LoadingBlock label="Loading incidents…" lines={4} /> : null}
      {incidents.phase === 'waking' ? <WakingNotice /> : null}
      {incidents.phase === 'error' ? (
        <ErrorNotice title="Could not load the incidents" message={incidents.error?.message ?? 'Unknown error.'} onRetry={incidents.reload} />
      ) : null}
      {incidents.phase === 'ready' && list.length === 0 ? (
        <EmptyState title="No incidents yet.">
          Run a drill on the{' '}
          <Link to="/" className="rounded-sm text-accent underline underline-offset-4">
            dashboard
          </Link>{' '}
          and wait for it to break its SLO.
        </EmptyState>
      ) : null}

      {open.length > 0 ? <IncidentList id="open-incidents" label="Open" incidents={open} now={now} /> : null}
      {resolved.length > 0 ? <IncidentList id="resolved-incidents" label="Resolved" incidents={resolved} now={now} /> : null}
    </div>
  )
}
