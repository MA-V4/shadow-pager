import { api } from '../api/client'
import type { ServiceLevel, StatusIncident, StatusService } from '../api/types'
import { useResource } from '../api/useResource'
import { ErrorNotice, WakingNotice } from '../components/Notice'
import { IncidentStatusPill, serviceLevelTone, SeverityPill } from '../components/Pills'
import { StatusGlyph } from '../components/StatusGlyph'
import type { BadgeTone } from '../components/ui/badge'
import { Skeleton } from '../components/ui/skeleton'
import { formatClock } from '../lib/chartScale'
import { formatDateTime } from '../lib/format'
import { failureModeLabels, labelOf, serviceLevelLabels } from '../lib/labels'
import { metricDefs, metricKeys, type MetricKey } from '../lib/metrics'
import type { PageRoute } from '../lib/routes'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/status', label: 'Status', order: 3, layout: 'public' }

// How often the page fetches the status again, which is what keeps the numbers live.
const refreshMs = 5_000

const banner: Record<ServiceLevel, { title: string; body: string; tone: BadgeTone; className: string }> = {
  operational: {
    title: 'All systems operational',
    body: 'Every service is within its limits.',
    tone: 'ok',
    className: 'border-ok/40 bg-ok/[0.07]',
  },
  degraded: {
    title: 'Some systems are degraded',
    body: 'At least one service is over a limit.',
    tone: 'warn',
    className: 'border-warn/50 bg-warn/[0.08]',
  },
  major_outage: {
    title: 'Major outage in progress',
    body: 'A service is over a limit and an incident is open.',
    tone: 'bad',
    className: 'border-bad/50 bg-bad/[0.08]',
  },
}

// The public status API names each number with its own field, and this reads the right one for a metric.
const statusValue: Record<MetricKey, (m: NonNullable<StatusService['metrics']>) => number> = {
  latency_p99: (m) => m.latency_p99_ms,
  error_rate: (m) => m.error_rate,
  cpu: (m) => m.cpu_percent,
  memory: (m) => m.memory_mb,
}

// LOADING

// StatusSkeleton holds the place of the banner and the service rows in the shape they will have.
function StatusSkeleton() {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-8">
      <span className="sr-only">Loading the system status…</span>
      <Skeleton className="h-[5.5rem] w-full rounded-xl" />
      <div className="flex flex-col gap-3">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-24 w-full rounded-lg" />
        <Skeleton className="h-24 w-full rounded-lg" />
      </div>
    </div>
  )
}

// SERVICES

// ServiceRow shows one service with its status and its newest numbers, marking the ones over their limit.
function ServiceRow({ service }: { service: StatusService }) {
  const m = service.metrics
  const tone = serviceLevelTone[service.status] ?? 'neutral'
  return (
    <li className="flex flex-col gap-3 px-4 py-4 sm:px-5">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <h3 className="min-w-0 truncate font-mono text-sm font-semibold" translate="no">
          {service.name}
        </h3>
        <span className={cn('flex items-center gap-2 text-sm', tone === 'ok' ? 'text-muted-foreground' : 'font-medium', tone === 'warn' && 'text-warn', tone === 'bad' && 'text-bad')}>
          <StatusGlyph tone={tone} />
          {labelOf(serviceLevelLabels, service.status)}
        </span>
      </div>
      {m === null ? (
        <p className="text-sm text-muted-foreground">No recent measurements. Nothing is being simulated on this service right now.</p>
      ) : (
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4" aria-label={`Current metrics of ${service.name}`}>
          {metricKeys.map((key) => {
            const breaching = m.breaches.includes(key)
            return (
              <div key={key} className="flex min-w-0 flex-col gap-0.5">
                <dt className="truncate text-xs text-muted-foreground">{metricDefs[key].label}</dt>
                <dd className={cn('font-mono text-sm tabular-nums', breaching && 'font-semibold text-bad')}>
                  {metricDefs[key].format(statusValue[key](m))}
                </dd>
                {/* The words repeat what the red colour says, for anyone who cannot tell the colours apart. */}
                {breaching ? (
                  <dd className="flex items-center gap-1.5 text-[11px] font-medium text-bad">
                    <StatusGlyph tone="bad" className="h-1.5 w-1.5" />
                    Over limit
                  </dd>
                ) : null}
              </div>
            )
          })}
        </dl>
      )}
    </li>
  )
}

// INCIDENTS

// ActiveIncident shows the public part of one open incident with its updates, newest first.
function ActiveIncident({ incident }: { incident: StatusIncident }) {
  return (
    <li className="flex flex-col gap-4 px-4 py-4 sm:px-5">
      <div className="flex flex-col gap-2">
        <h3 className="font-medium">
          {labelOf(failureModeLabels, incident.mode)} on{' '}
          <span className="font-mono" translate="no">
            {incident.service}
          </span>
        </h3>
        <div className="flex flex-wrap items-center gap-2">
          <SeverityPill severity={incident.severity} />
          <IncidentStatusPill status={incident.status} />
          <span className="text-xs text-muted-foreground">
            Started <time dateTime={incident.declared_at}>{formatDateTime(incident.declared_at)}</time>
          </span>
        </div>
      </div>
      <ol className="flex flex-col gap-3 border-l pl-4" aria-label={`Updates for the incident on ${incident.service}, newest first`}>
        {[...incident.updates].reverse().map((update, index) => (
          <li key={`${update.at}-${index}`} className="flex flex-col gap-0.5">
            <span className="text-sm">{update.text}</span>
            <time dateTime={update.at} className="text-xs tabular-nums text-muted-foreground">
              {formatDateTime(update.at)}
            </time>
          </li>
        ))}
      </ol>
    </li>
  )
}

// PAGE

// StatusPage is the public page that tells anyone how every service is doing, with no controls on it.
export default function StatusPage() {
  const status = useResource((signal) => api.getStatus(signal), 'status', { refreshMs })
  const page = status.data
  const look = page === undefined ? undefined : banner[page.status]

  return (
    <div className="flex w-full flex-col gap-8">
      <h1 className="sr-only">System status</h1>

      {status.phase === 'loading' ? <StatusSkeleton /> : null}
      {status.phase === 'waking' ? <WakingNotice /> : null}
      {status.phase === 'error' ? (
        <ErrorNotice
          title="Could not load the status"
          message={status.error?.message ?? 'Unknown error.'}
          onRetry={status.reload}
        />
      ) : null}

      {page !== undefined && look !== undefined && status.phase !== 'error' ? (
        <>
          <section
            role="status"
            aria-live="polite"
            aria-label="Overall status"
            className={cn('flex items-center gap-4 rounded-xl border px-5 py-5 sm:px-6', look.className)}
          >
            <span aria-hidden="true" className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border bg-card">
              <StatusGlyph tone={look.tone} className="h-3.5 w-3.5" />
            </span>
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <p className="text-xl font-semibold tracking-tight sm:text-2xl">{look.title}</p>
              <p className="text-sm text-muted-foreground">{look.body}</p>
            </div>
            <p className="hidden shrink-0 text-right text-xs tabular-nums text-muted-foreground sm:block">
              Updated at
              <br />
              <time dateTime={page.updated_at}>{formatClock(new Date(page.updated_at).getTime())}</time>
            </p>
          </section>

          <section aria-labelledby="incidents-heading" className="flex flex-col gap-3">
            <h2 id="incidents-heading" className="text-sm font-semibold">
              Active incidents
            </h2>
            {page.incidents.length === 0 ? (
              <p className="flex items-center gap-2 rounded-lg border border-dashed px-4 py-3.5 text-sm text-muted-foreground sm:px-5">
                <StatusGlyph tone="ok" />
                No active incidents. Every incident has been resolved.
              </p>
            ) : (
              <ul className="divide-y overflow-hidden rounded-lg border bg-card shadow-card" aria-label="Active incidents">
                {page.incidents.map((incident) => (
                  <ActiveIncident key={incident.id} incident={incident} />
                ))}
              </ul>
            )}
          </section>

          <section aria-labelledby="services-heading" className="flex flex-col gap-3">
            <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
              <h2 id="services-heading" className="text-sm font-semibold">
                Services
              </h2>
              <p className="text-xs text-muted-foreground sm:hidden">
                Updated at <time dateTime={page.updated_at}>{formatClock(new Date(page.updated_at).getTime())}</time>
              </p>
            </div>
            {page.services.length === 0 ? (
              <p className="rounded-lg border border-dashed px-4 py-3.5 text-sm text-muted-foreground sm:px-5">
                No services to show yet. A service appears here once a simulation has run on it.
              </p>
            ) : (
              <ul className="divide-y overflow-hidden rounded-lg border bg-card shadow-card" aria-label="Services">
                {page.services.map((service) => (
                  <ServiceRow key={service.name} service={service} />
                ))}
              </ul>
            )}
          </section>
        </>
      ) : null}
    </div>
  )
}
