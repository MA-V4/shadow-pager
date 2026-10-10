import { api } from '../api/client'
import type { ServiceLevel, StatusIncident, StatusService } from '../api/types'
import { useResource } from '../api/useResource'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { IncidentStatusPill, ServiceLevelPill, SeverityPill } from '../components/Pills'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { formatDateTime, formatMetric, formatPercent, formatTime, humanize } from '../lib/format'
import type { PageRoute } from '../lib/routes'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/status', label: 'Status', order: 3 }

// How often the page fetches the status again, which is what keeps the numbers live.
const refreshMs = 5_000

const banner: Record<ServiceLevel, { text: string; className: string }> = {
  operational: { text: 'All systems operational', className: 'border-ok/40 bg-ok/10 text-ok' },
  degraded: { text: 'Some systems are degraded', className: 'border-warn/40 bg-warn/10 text-warn' },
  major_outage: { text: 'Major outage in progress', className: 'border-bad/40 bg-bad/10 text-bad' },
}

// SERVICES

// Metric is one labelled number in a service row, in red when it is one of the broken limits.
function Metric({ label, value, breaching }: { label: string; value: string; breaching: boolean }) {
  return (
    <div className="flex flex-col">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn('font-mono text-sm tabular-nums', breaching && 'font-semibold text-bad')}>{value}</dd>
    </div>
  )
}

// ServiceRow shows one service with its status and its newest numbers.
function ServiceRow({ service }: { service: StatusService }) {
  const m = service.metrics
  return (
    <li className="flex flex-col gap-3 px-4 py-4 sm:px-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="min-w-0 truncate font-mono font-medium">{service.name}</h3>
        <ServiceLevelPill level={service.status} />
      </div>
      {m === null ? (
        <p className="text-sm text-muted-foreground">No recent measurements. Nothing is being simulated on this service right now.</p>
      ) : (
        <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4" aria-label={`Current metrics of ${service.name}`}>
          <Metric label="Latency p99" value={`${formatMetric(m.latency_p99_ms)} ms`} breaching={m.breaches.includes('latency_p99')} />
          <Metric label="Error rate" value={formatPercent(m.error_rate)} breaching={m.breaches.includes('error_rate')} />
          <Metric label="CPU" value={`${formatMetric(m.cpu_percent)}%`} breaching={m.breaches.includes('cpu')} />
          <Metric label="Memory" value={`${formatMetric(m.memory_mb)} MB`} breaching={m.breaches.includes('memory')} />
        </dl>
      )}
    </li>
  )
}

// INCIDENTS

// ActiveIncident shows the public part of one open incident with its updates, newest first.
function ActiveIncident({ incident }: { incident: StatusIncident }) {
  return (
    <li className="flex flex-col gap-3 px-4 py-4 sm:px-5">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-medium">
          {humanize(incident.mode)} on <span className="font-mono">{incident.service}</span>
        </h3>
        <SeverityPill severity={incident.severity} />
        <IncidentStatusPill status={incident.status} />
      </div>
      <ol className="flex flex-col gap-2 border-l pl-4" aria-label={`Updates for the incident on ${incident.service}, newest first`}>
        {[...incident.updates].reverse().map((update, index) => (
          <li key={`${update.at}-${index}`} className="flex flex-col">
            <span className="text-sm">{update.text}</span>
            <time dateTime={update.at} className="text-xs text-muted-foreground">
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

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">System status</h1>
        <p className="text-sm text-muted-foreground">Live health of every simulated service, refreshed every few seconds.</p>
      </div>

      {status.phase === 'loading' ? <LoadingBlock label="Loading the system status" lines={4} /> : null}
      {status.phase === 'waking' ? <WakingNotice /> : null}
      {status.phase === 'error' ? (
        <ErrorNotice
          title="The status could not be loaded"
          message={status.error?.message ?? 'Unknown error.'}
          onRetry={status.reload}
        />
      ) : null}

      {page !== undefined && status.phase !== 'error' ? (
        <>
          <div
            role="status"
            aria-live="polite"
            className={cn('flex flex-wrap items-center justify-between gap-2 rounded-xl border px-5 py-4', banner[page.status].className)}
          >
            <p className="text-lg font-semibold">{banner[page.status].text}</p>
            <p className="text-xs text-foreground/70">Updated at {formatTime(page.updated_at)}</p>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Active incidents</CardTitle>
            </CardHeader>
            {page.incidents.length === 0 ? (
              <CardContent>
                <EmptyState title="No active incidents">Every incident has been resolved.</EmptyState>
              </CardContent>
            ) : (
              <ul className="divide-y border-t" aria-label="Active incidents">
                {page.incidents.map((incident) => (
                  <ActiveIncident key={incident.id} incident={incident} />
                ))}
              </ul>
            )}
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Services</CardTitle>
            </CardHeader>
            {page.services.length === 0 ? (
              <CardContent>
                <EmptyState title="No services to show yet">
                  A service appears here once a simulation has run on it.
                </EmptyState>
              </CardContent>
            ) : (
              <ul className="divide-y border-t" aria-label="Services">
                {page.services.map((service) => (
                  <ServiceRow key={service.name} service={service} />
                ))}
              </ul>
            )}
          </Card>
        </>
      ) : null}
    </div>
  )
}
