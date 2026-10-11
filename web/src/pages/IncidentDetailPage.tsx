import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'

import { api } from '../api/client'
import type { Incident } from '../api/types'
import { useResource } from '../api/useResource'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { ShapeGlyph } from '../components/ShapeGlyph'
import { incidentRail, IncidentStatusTag, SeverityTag } from '../components/Tags'
import { Card } from '../components/ui/card'
import { Overline } from '../components/ui/overline'
import { formatClock } from '../lib/chartScale'
import { connectionOf, useReportConnection } from '../lib/connection'
import { formatDateTime, formatGoDuration } from '../lib/format'
import { incidentDuration } from '../lib/incidents'
import { breachLabels, failureModeLabels, labelOf } from '../lib/labels'
import type { PageRoute } from '../lib/routes'
import { useNow } from '../lib/useNow'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/incidents/:id' }

// How often an incident is fetched again, so its story grows on screen while people work on it.
const refreshMs = 10_000

// actorName turns the technical name of who did something into words.
export function actorName(actor: string): string {
  if (actor === 'shadow-pager') {
    return 'Shadow-Pager (automatic)'
  }
  const slackUser = /^<@([A-Z0-9]+)>$/.exec(actor)
  return slackUser ? `Slack user ${slackUser[1]}` : actor
}

// Readout is one of the two big numbers of an incident, or quiet words when the number does not exist yet.
function Readout({ label, value, missing }: { label: string; value: string | undefined; missing: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-meta text-tertiary">{label}</dt>
      <dd className={cn('font-mono', value === undefined ? 'text-body leading-8 text-tertiary' : 'text-readout text-primary')}>
        {value === undefined ? missing : formatGoDuration(value)}
      </dd>
    </div>
  )
}

// Fact is one small labelled value in the summary of an incident.
function Fact({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', className)}>
      <dt className="text-meta text-tertiary">{label}</dt>
      <dd className="min-w-0 wrap-break-word text-body text-primary">{children}</dd>
    </div>
  )
}

// Tape is the flight recorder of an incident: times down the left, a line with a dot per event, and what happened on the right.
function Tape({ incident }: { incident: Incident }) {
  return (
    <ol aria-label="Timeline, oldest first" className="flex flex-col">
      {incident.timeline.map((entry, index) => (
        <li key={`${entry.at}-${index}`} className="group grid grid-cols-[4.5rem_1rem_minmax(0,1fr)] gap-x-3">
          <time dateTime={entry.at} title={formatDateTime(entry.at)} className="pt-0.5 text-right font-mono text-meta text-tertiary">
            {formatClock(new Date(entry.at).getTime())}
          </time>
          <span aria-hidden="true" className="relative flex justify-center">
            <span className="absolute inset-y-0 w-px bg-strong group-last:bottom-auto group-last:h-2" />
            <span className="relative mt-1.5 h-2 w-2 rounded-full bg-secondary ring-4 ring-canvas" />
          </span>
          <span className="flex min-w-0 flex-col pb-5">
            <span className="wrap-break-word text-body text-primary">{entry.text}</span>
            <span className="text-meta text-tertiary">by {actorName(entry.actor)}</span>
          </span>
        </li>
      ))}
    </ol>
  )
}

// IncidentView shows everything about one incident once it has been loaded.
function IncidentView({ incident }: { incident: Incident }) {
  const now = useNow()
  const open = incident.status !== 'resolved'
  return (
    <div className="flex flex-col gap-8">
      <Card rail={incidentRail(incident.status)} pulse={incident.status === 'declared'} className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <h1 className="font-mono text-heading font-medium text-primary" translate="no">
              {incident.id}
            </h1>
            <IncidentStatusTag status={incident.status} />
            <SeverityTag severity={incident.severity} />
          </div>
          <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-body text-secondary">
            <ShapeGlyph mode={incident.mode} />
            <span>
              {labelOf(failureModeLabels, incident.mode)} on{' '}
              <span className="font-mono text-primary" translate="no">
                {incident.service}
              </span>
            </span>
            <span className="font-mono text-meta text-tertiary" title={`Declared ${formatDateTime(incident.declared_at)}`}>
              {open ? 'open for' : 'lasted'} {incidentDuration(incident, now)}
            </span>
          </p>
        </div>

        <dl className="grid gap-x-6 gap-y-4 border-t border-subtle pt-4 sm:grid-cols-2">
          <Readout label="Time to acknowledge" value={incident.time_to_acknowledge} missing="Not acknowledged" />
          <Readout label="Time to resolve" value={incident.time_to_resolve} missing="Not resolved yet" />
        </dl>

        <dl className="grid gap-x-6 gap-y-4 border-t border-subtle pt-4 sm:grid-cols-3">
          <Fact label="Acknowledged by">{incident.acknowledged_by ? actorName(incident.acknowledged_by) : '-'}</Fact>
          <Fact label="SLO breaches at declaration">
            {incident.breaches.length === 0 ? 'None' : incident.breaches.map((breach) => labelOf(breachLabels, breach)).join(', ')}
          </Fact>
          <Fact label="Simulation">
            <span className="font-mono" translate="no">
              {incident.simulation_id}
            </span>
          </Fact>
        </dl>
      </Card>

      <section aria-labelledby="timeline-heading" className="flex flex-col gap-3">
        <Overline as="h2" id="timeline-heading">
          Timeline
        </Overline>
        {incident.timeline.length === 0 ? <EmptyState title="Nothing has happened yet." /> : <Tape incident={incident} />}
      </section>
    </div>
  )
}

// IncidentDetailPage loads one incident by the id in the URL and shows its summary and its timeline.
export default function IncidentDetailPage() {
  const { id = '' } = useParams()
  const incident = useResource((signal) => api.getIncident(id, signal), `incident:${id}`, { refreshMs })
  useReportConnection(connectionOf(incident.phase))

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4">
      <Link to="/incidents" className="self-start rounded-sm text-label text-secondary underline-offset-4 hover:text-primary hover:underline">
        Back to all incidents
      </Link>

      {incident.phase === 'loading' ? <LoadingBlock label="Loading the incident…" lines={5} /> : null}
      {incident.phase === 'waking' ? <WakingNotice /> : null}
      {incident.phase === 'error' && incident.error?.status === 404 ? (
        <EmptyState title="This incident does not exist.">It may have been declared before the server last restarted without a database.</EmptyState>
      ) : null}
      {incident.phase === 'error' && incident.error?.status !== 404 ? (
        <ErrorNotice title="Could not load the incident" message={incident.error?.message ?? 'Unknown error.'} onRetry={incident.reload} />
      ) : null}
      {incident.data !== undefined && incident.phase !== 'error' ? <IncidentView incident={incident.data} /> : null}
    </div>
  )
}
