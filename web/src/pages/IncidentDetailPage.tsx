import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'

import { api } from '../api/client'
import type { Incident } from '../api/types'
import { useResource } from '../api/useResource'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { IncidentStatusPill, SeverityPill } from '../components/Pills'
import { Badge } from '../components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { formatDateTime, formatGoDuration, humanize } from '../lib/format'
import type { PageRoute } from '../lib/routes'

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

// Fact is one labelled value in the summary of an incident.
function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  )
}

// IncidentView shows everything about one incident once it has been loaded.
function IncidentView({ incident }: { incident: Incident }) {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">
          {humanize(incident.mode)} on <span className="font-mono">{incident.service}</span>
        </h1>
        <div className="flex flex-wrap items-center gap-2">
          <SeverityPill severity={incident.severity} />
          <IncidentStatusPill status={incident.status} />
          <span className="font-mono text-xs text-muted-foreground">{incident.id}</span>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Summary</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Fact label="Time to acknowledge">
              {incident.time_to_acknowledge === undefined ? 'Not acknowledged' : formatGoDuration(incident.time_to_acknowledge)}
            </Fact>
            <Fact label="Time to resolve">
              {incident.time_to_resolve === undefined ? 'Not resolved yet' : formatGoDuration(incident.time_to_resolve)}
            </Fact>
            <Fact label="Declared">{formatDateTime(incident.declared_at)}</Fact>
            <Fact label="Acknowledged">
              {incident.acknowledged_at === undefined
                ? '-'
                : `${formatDateTime(incident.acknowledged_at)}${incident.acknowledged_by ? ` by ${actorName(incident.acknowledged_by)}` : ''}`}
            </Fact>
            <Fact label="Mitigated">{formatDateTime(incident.mitigated_at)}</Fact>
            <Fact label="Resolved">{formatDateTime(incident.resolved_at)}</Fact>
            <Fact label="SLO breaches at declaration">
              {incident.breaches.length === 0 ? (
                'None'
              ) : (
                <span className="flex flex-wrap gap-1">
                  {incident.breaches.map((breach) => (
                    <Badge key={breach} tone="bad">
                      {humanize(breach)}
                    </Badge>
                  ))}
                </span>
              )}
            </Fact>
            <Fact label="Simulation">
              <span className="font-mono text-xs">{incident.simulation_id}</span>
            </Fact>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Timeline</CardTitle>
        </CardHeader>
        <CardContent>
          {incident.timeline.length === 0 ? (
            <EmptyState title="Nothing has happened yet" />
          ) : (
            <ol className="flex flex-col gap-4 border-l pl-4" aria-label="Timeline, oldest first">
              {incident.timeline.map((entry, index) => (
                <li key={`${entry.at}-${index}`} className="relative flex flex-col gap-0.5">
                  <span aria-hidden="true" className="absolute -left-[1.3rem] top-1.5 h-2 w-2 rounded-full bg-primary" />
                  <span className="text-sm">{entry.text}</span>
                  <span className="text-xs text-muted-foreground">
                    <time dateTime={entry.at}>{formatDateTime(entry.at)}</time> by {actorName(entry.actor)}
                  </span>
                </li>
              ))}
            </ol>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// IncidentDetailPage loads one incident by the id in the URL and shows its summary and timeline.
export default function IncidentDetailPage() {
  const { id = '' } = useParams()
  const incident = useResource((signal) => api.getIncident(id, signal), `incident:${id}`, { refreshMs })

  return (
    <div className="flex flex-col gap-4">
      <Link to="/incidents" className="text-sm text-muted-foreground underline-offset-4 hover:underline">
        Back to all incidents
      </Link>

      {incident.phase === 'loading' ? <LoadingBlock label="Loading the incident" lines={5} /> : null}
      {incident.phase === 'waking' ? <WakingNotice /> : null}
      {incident.phase === 'error' && incident.error?.status === 404 ? (
        <EmptyState title="This incident does not exist">
          It may have been declared before the server last restarted without a database.
        </EmptyState>
      ) : null}
      {incident.phase === 'error' && incident.error?.status !== 404 ? (
        <ErrorNotice
          title="The incident could not be loaded"
          message={incident.error?.message ?? 'Unknown error.'}
          onRetry={incident.reload}
        />
      ) : null}
      {incident.data !== undefined && incident.phase !== 'error' ? <IncidentView incident={incident.data} /> : null}
    </div>
  )
}
