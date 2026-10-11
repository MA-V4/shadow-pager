import { api } from '../api/client'
import type { ServiceLevel, StatusIncident, StatusService } from '../api/types'
import { useResource } from '../api/useResource'
import { ErrorNotice, WakingNotice } from '../components/Notice'
import { ShapeGlyph } from '../components/ShapeGlyph'
import { SignalDot, type SignalTone } from '../components/SignalDot'
import { incidentRail, IncidentStatusTag, SeverityTag } from '../components/Tags'
import { Card } from '../components/ui/card'
import { Overline } from '../components/ui/overline'
import { Skeleton } from '../components/ui/skeleton'
import { formatAgo, formatDateTime } from '../lib/format'
import { failureModeLabels, labelOf, serviceLevelLabels } from '../lib/labels'
import { metricDefs } from '../lib/metrics'
import type { PageRoute } from '../lib/routes'
import { useNow } from '../lib/useNow'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/status', label: 'Status', order: 3, layout: 'public' }

// How often the page fetches the status again, which is what keeps the numbers live.
const refreshMs = 5_000

const levelTone: Record<ServiceLevel, SignalTone> = { operational: 'ok', degraded: 'warn', major_outage: 'breach' }

// The one line at the top that sums up every service.
const heroLine: Record<ServiceLevel, string> = {
  operational: 'All systems operational',
  degraded: 'Degraded performance',
  major_outage: 'Major outage',
}

// LOADING

// StatusSkeleton holds the place of the hero line and the service rows in the shape they will have.
function StatusSkeleton() {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-12">
      <span className="sr-only">Loading the system status…</span>
      <Skeleton className="h-11 w-3/4" />
      <div className="flex flex-col gap-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-11 w-full" />
        <Skeleton className="h-11 w-full" />
      </div>
    </div>
  )
}

// SERVICES

// Reading is one current number of a service, which is said to be over its limit in words as well as in weight.
function Reading({ label, value, breaching }: { label: string; value: string; breaching: boolean }) {
  return (
    <span data-reading={label} className={cn('whitespace-nowrap', breaching ? 'font-medium text-primary' : 'text-secondary')}>
      <span className="font-sans text-tertiary">{label}</span> {value}
      {breaching ? <span className="font-sans text-tertiary"> (over limit)</span> : null}
    </span>
  )
}

// ServiceRow is one service on one line: its name, how it is doing, and its newest latency and error rate.
function ServiceRow({ service }: { service: StatusService }) {
  const m = service.metrics
  return (
    <li className="flex min-h-11 flex-wrap items-center gap-x-4 gap-y-1 border-b border-subtle py-3">
      <h3 className="min-w-0 flex-1 truncate font-mono text-body font-medium text-primary" translate="no">
        {service.name}
      </h3>
      <span className="flex shrink-0 items-center gap-2 text-body text-primary">
        <SignalDot tone={levelTone[service.status] ?? 'neutral'} />
        {labelOf(serviceLevelLabels, service.status)}
      </span>
      <p className="flex w-full flex-wrap gap-x-4 font-mono text-meta sm:w-72 sm:justify-end" aria-label={`Current metrics of ${service.name}`}>
        {m === null ? (
          <span className="font-sans text-tertiary">No recent measurements</span>
        ) : (
          <>
            <Reading label="p99" value={metricDefs.latency_p99.format(m.latency_p99_ms)} breaching={m.breaches.includes('latency_p99')} />
            <Reading label="errors" value={metricDefs.error_rate.format(m.error_rate)} breaching={m.breaches.includes('error_rate')} />
          </>
        )}
      </p>
    </li>
  )
}

// INCIDENTS

// ActiveIncident is the public part of one open incident as a card, with its newest update.
function ActiveIncident({ incident, now }: { incident: StatusIncident; now: number }) {
  const latest = incident.updates.at(-1)
  return (
    <li>
      <Card rail={incidentRail(incident.status)} className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h3 className="flex min-w-0 items-center gap-2 text-title text-primary">
            <ShapeGlyph mode={incident.mode} />
            <span className="min-w-0">
              {labelOf(failureModeLabels, incident.mode)} on{' '}
              <span className="font-mono font-medium" translate="no">
                {incident.service}
              </span>
            </span>
          </h3>
          <IncidentStatusTag status={incident.status} />
          <SeverityTag severity={incident.severity} />
        </div>
        <p className="text-body text-secondary" data-latest-update="">
          {latest === undefined ? 'No updates yet.' : latest.text}
        </p>
        <p className="text-meta text-tertiary">
          {latest === undefined ? null : (
            <>
              Updated{' '}
              <time dateTime={latest.at} title={formatDateTime(latest.at)}>
                {formatAgo(latest.at, now)}
              </time>
              ,{' '}
            </>
          )}
          started{' '}
          <time dateTime={incident.declared_at} title={formatDateTime(incident.declared_at)}>
            {formatAgo(incident.declared_at, now)}
          </time>
        </p>
      </Card>
    </li>
  )
}

// PAGE

// StatusPage is the public notice that tells anyone how every service is doing, with no controls on it.
export default function StatusPage() {
  const now = useNow()
  const status = useResource((signal) => api.getStatus(signal), 'status', { refreshMs })
  const page = status.data

  return (
    <div className="flex w-full flex-col gap-12 py-4 md:py-8">
      <h1 className="sr-only">System status</h1>

      {status.phase === 'loading' ? <StatusSkeleton /> : null}
      {status.phase === 'waking' ? <WakingNotice /> : null}
      {status.phase === 'error' ? (
        <ErrorNotice title="Could not load the status" message={status.error?.message ?? 'Unknown error.'} onRetry={status.reload} />
      ) : null}

      {page !== undefined && status.phase !== 'error' ? (
        <>
          <p role="status" aria-live="polite" aria-label="Overall status" className="flex items-start gap-4 text-hero text-primary text-balance">
            <SignalDot tone={levelTone[page.status] ?? 'neutral'} className="mt-4.25 h-2.5 w-2.5" />
            {heroLine[page.status] ?? labelOf(serviceLevelLabels, page.status)}
          </p>

          <section aria-labelledby="services-heading" className="flex flex-col gap-3">
            <Overline as="h2" id="services-heading">
              Services
            </Overline>
            {page.services.length === 0 ? (
              <p className="text-body text-secondary">No services to show yet. A service appears here once a drill has run on it.</p>
            ) : (
              <ul className="border-t border-subtle" aria-label="Services">
                {page.services.map((service) => (
                  <ServiceRow key={service.name} service={service} />
                ))}
              </ul>
            )}
          </section>

          <section aria-labelledby="incidents-heading" className="flex flex-col gap-3">
            <Overline as="h2" id="incidents-heading">
              Active incidents
            </Overline>
            {page.incidents.length === 0 ? (
              <p className="text-body text-secondary">No active incidents.</p>
            ) : (
              <ul className="flex flex-col gap-3" aria-label="Active incidents">
                {page.incidents.map((incident) => (
                  <ActiveIncident key={incident.id} incident={incident} now={now} />
                ))}
              </ul>
            )}
          </section>

          <p className="text-meta text-tertiary">
            Last updated{' '}
            <time dateTime={page.updated_at} title={formatDateTime(page.updated_at)}>
              {formatAgo(page.updated_at, now)}
            </time>
          </p>
        </>
      ) : null}
    </div>
  )
}
