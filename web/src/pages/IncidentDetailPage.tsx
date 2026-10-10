import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'

import { api } from '../api/client'
import type { Incident, IncidentStatus } from '../api/types'
import { useResource } from '../api/useResource'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { IncidentStatusPill, SeverityPill } from '../components/Pills'
import { StatusGlyph } from '../components/StatusGlyph'
import { formatDateTime, formatGoDuration } from '../lib/format'
import { breachLabels, failureModeLabels, incidentStatusLabels, labelOf } from '../lib/labels'
import type { PageRoute } from '../lib/routes'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/incidents/:id' }

// How often an incident is fetched again, so its story grows on screen while people work on it.
const refreshMs = 10_000

const steps: IncidentStatus[] = ['declared', 'acknowledged', 'mitigated', 'resolved']

// actorName turns the technical name of who did something into words.
export function actorName(actor: string): string {
  if (actor === 'shadow-pager') {
    return 'Shadow-Pager (automatic)'
  }
  const slackUser = /^<@([A-Z0-9]+)>$/.exec(actor)
  return slackUser ? `Slack user ${slackUser[1]}` : actor
}

// stepTime gives back when an incident reached a step, or nothing when it has not or when it skipped it.
function stepTime(incident: Incident, step: IncidentStatus): string | undefined {
  switch (step) {
    case 'declared':
      return incident.declared_at
    case 'acknowledged':
      return incident.acknowledged_at
    case 'mitigated':
      return incident.mitigated_at
    case 'resolved':
      return incident.resolved_at
  }
}

// Fact is one labelled value in the summary of an incident.
function Fact({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', className)}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words text-sm">{children}</dd>
    </div>
  )
}

// Lifecycle shows the four steps of an incident in a row, with the time each one was reached.
function Lifecycle({ incident }: { incident: Incident }) {
  const reached = steps.indexOf(incident.status)
  return (
    <ol className="grid grid-cols-2 gap-x-4 gap-y-4 sm:grid-cols-4" aria-label="Lifecycle">
      {steps.map((step, index) => {
        const at = stepTime(incident, step)
        const done = index <= reached && at !== undefined
        const skipped = index < reached && at === undefined
        return (
          <li key={step} className="flex min-w-0 flex-col gap-1 border-t-2 pt-2.5" style={{ borderColor: done ? 'var(--foreground)' : 'var(--border)' }}>
            <span className={cn('flex items-center gap-1.5 text-sm font-medium', !done && 'text-muted-foreground')}>
              <StatusGlyph tone={done ? 'ok' : 'neutral'} coloured={false} />
              {incidentStatusLabels[step]}
            </span>
            <span className="text-xs tabular-nums text-muted-foreground">
              {done ? formatDateTime(at) : skipped ? 'Skipped' : 'Not yet'}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

// IncidentView shows everything about one incident once it has been loaded.
function IncidentView({ incident }: { incident: Incident }) {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2.5">
        <h1 className="text-2xl font-semibold tracking-tight">
          {labelOf(failureModeLabels, incident.mode)} on{' '}
          <span className="font-mono" translate="no">
            {incident.service}
          </span>
        </h1>
        <div className="flex flex-wrap items-center gap-2">
          <SeverityPill severity={incident.severity} />
          <IncidentStatusPill status={incident.status} />
          <span className="font-mono text-xs text-muted-foreground" translate="no">
            {incident.id}
          </span>
        </div>
      </div>

      <section aria-label="Summary" className="flex flex-col gap-6 rounded-lg border bg-card p-4 shadow-card sm:p-5">
        <Lifecycle incident={incident} />
        <dl className="grid gap-x-6 gap-y-4 border-t pt-5 sm:grid-cols-2 lg:grid-cols-4">
          <Fact label="Time to acknowledge">
            <span className="font-mono text-lg font-semibold tabular-nums">
              {incident.time_to_acknowledge === undefined ? 'Not acknowledged' : formatGoDuration(incident.time_to_acknowledge)}
            </span>
          </Fact>
          <Fact label="Time to resolve">
            <span className="font-mono text-lg font-semibold tabular-nums">
              {incident.time_to_resolve === undefined ? 'Not resolved yet' : formatGoDuration(incident.time_to_resolve)}
            </span>
          </Fact>
          <Fact label="Acknowledged by">{incident.acknowledged_by ? actorName(incident.acknowledged_by) : '-'}</Fact>
          <Fact label="SLO breaches at declaration">
            {incident.breaches.length === 0 ? 'None' : incident.breaches.map((breach) => labelOf(breachLabels, breach)).join(', ')}
          </Fact>
          <Fact label="Simulation" className="sm:col-span-2">
            <span className="font-mono text-xs" translate="no">
              {incident.simulation_id}
            </span>
          </Fact>
        </dl>
      </section>

      <section aria-labelledby="timeline-heading" className="flex flex-col gap-4">
        <h2 id="timeline-heading" className="text-[15px] font-semibold tracking-tight">
          Timeline
        </h2>
        {incident.timeline.length === 0 ? (
          <EmptyState title="Nothing has happened yet" />
        ) : (
          <ol className="flex flex-col gap-5 border-l pl-5" aria-label="Timeline, oldest first">
            {incident.timeline.map((entry, index) => (
              <li key={`${entry.at}-${index}`} className="relative flex flex-col gap-0.5">
                <span aria-hidden="true" className="absolute -left-[1.5rem] top-1.5 h-2 w-2 rounded-full border-2 border-background bg-muted-foreground" />
                <span className="text-sm">{entry.text}</span>
                <span className="text-xs text-muted-foreground">
                  <time dateTime={entry.at} className="tabular-nums">
                    {formatDateTime(entry.at)}
                  </time>{' '}
                  by {actorName(entry.actor)}
                </span>
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  )
}

// IncidentDetailPage loads one incident by the id in the URL and shows its summary and timeline.
export default function IncidentDetailPage() {
  const { id = '' } = useParams()
  const incident = useResource((signal) => api.getIncident(id, signal), `incident:${id}`, { refreshMs })

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-5">
      <Link to="/incidents" className="self-start rounded-sm text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
        Back to all incidents
      </Link>

      {incident.phase === 'loading' ? <LoadingBlock label="Loading the incident…" lines={5} /> : null}
      {incident.phase === 'waking' ? <WakingNotice /> : null}
      {incident.phase === 'error' && incident.error?.status === 404 ? (
        <EmptyState title="This incident does not exist">
          It may have been declared before the server last restarted without a database.
        </EmptyState>
      ) : null}
      {incident.phase === 'error' && incident.error?.status !== 404 ? (
        <ErrorNotice
          title="Could not load the incident"
          message={incident.error?.message ?? 'Unknown error.'}
          onRetry={incident.reload}
        />
      ) : null}
      {incident.data !== undefined && incident.phase !== 'error' ? <IncidentView incident={incident.data} /> : null}
    </div>
  )
}
