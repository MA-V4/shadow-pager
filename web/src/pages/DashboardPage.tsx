import { useCallback, useEffect, useRef, useState } from 'react'

import { api } from '../api/client'
import type { MetricSample } from '../api/types'
import { useEventStream, type StreamStatus } from '../api/useEventStream'
import { useResource } from '../api/useResource'
import { InjectForm } from '../components/InjectForm'
import { EmptyState, ErrorNotice, WakingNotice } from '../components/Notice'
import { SimulationStatePill } from '../components/Pills'
import { SimulationPanel } from '../components/SimulationPanel'
import { StatusGlyph } from '../components/StatusGlyph'
import type { BadgeTone } from '../components/ui/badge'
import { Skeleton } from '../components/ui/skeleton'
import { useAdminToken } from '../lib/adminToken'
import { formatDateTime } from '../lib/format'
import { groupByService } from '../lib/grouping'
import { failureModeLabels, labelOf } from '../lib/labels'
import type { PageRoute } from '../lib/routes'
import { addSample, keepOnly, type SampleHistory } from '../lib/samples'
import { useNow } from '../lib/useNow'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/', label: 'Dashboard', order: 1 }

// How often the list of simulations is fetched again, so finished ones move out of the running list.
const refreshMs = 5_000

const streamState: Record<StreamStatus, { tone: BadgeTone; label: string }> = {
  open: { tone: 'ok', label: 'Live' },
  connecting: { tone: 'neutral', label: 'Connecting…' },
  reconnecting: { tone: 'warn', label: 'Reconnecting…' },
}

// plural picks the right word for a count, so the page never says "1 simulations".
function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`
}

// LOADING

// DashboardSkeleton holds the place of the running simulations in the shape they will have.
function DashboardSkeleton() {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-4">
      <span className="sr-only">Loading simulations…</span>
      <Skeleton className="h-5 w-40" />
      <div className="flex flex-col gap-4 rounded-lg border bg-card p-4 sm:p-5">
        <div className="flex items-center justify-between gap-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-28" />
        </div>
        <Skeleton className="h-1.5 w-full rounded-full" />
        <div className="grid grid-cols-1 gap-3 min-[420px]:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-[4.5rem]" />
          ))}
        </div>
      </div>
    </div>
  )
}

// PAGE

// DashboardPage is where an operator injects failures, watches them live, and halts them.
export default function DashboardPage() {
  const [token] = useAdminToken()
  const now = useNow()
  const simulations = useResource((signal) => api.listSimulations(signal), 'simulations', { refreshMs })
  const slo = useResource((signal) => api.getSlo(signal), 'slo')
  const [history, setHistory] = useState<SampleHistory>({})

  const all = simulations.data?.simulations ?? []
  const running = all.filter((s) => s.state === 'running')
  const finished = all.filter((s) => s.state !== 'running').slice(0, 8)
  const groups = groupByService(running, history)
  const breachingCount = groups.reduce((n, g) => n + g.simulations.filter((s) => s.breaching).length, 0)

  // Samples can arrive for a simulation someone else just started, and then the list is fetched again at once.
  const known = useRef(new Set<string>())
  const { reload } = simulations
  const onSample = useCallback(
    (sample: MetricSample) => {
      setHistory((h) => addSample(h, sample))
      if (!known.current.has(sample.simulation_id)) {
        known.current.add(sample.simulation_id)
        reload()
      }
    },
    [reload],
  )
  const stream = useEventStream<MetricSample>(api.streamUrl(), 'metric', onSample)

  // Samples of simulations that are over are dropped, so the page does not use more and more memory.
  const runningIds = running.map((s) => s.id).join(',')
  useEffect(() => {
    const ids = runningIds === '' ? [] : runningIds.split(',')
    for (const id of ids) {
      known.current.add(id)
    }
    setHistory((h) => keepOnly(h, ids))
  }, [runningIds])

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
          {/* The summary names what is wrong first, because that is what an operator looks for. */}
          <p className="flex flex-wrap items-center gap-x-2 text-sm text-muted-foreground" aria-live="polite">
            {simulations.phase !== 'ready' ? (
              'Inject a pretend failure and watch its telemetry live.'
            ) : breachingCount > 0 ? (
              <>
                <span className="flex items-center gap-1.5 font-medium text-bad">
                  <StatusGlyph tone="bad" />
                  {plural(breachingCount, 'simulation is', 'simulations are')} breaching SLO
                </span>
                <span>of {running.length} running</span>
              </>
            ) : running.length > 0 ? (
              <span className="flex items-center gap-1.5">
                <StatusGlyph tone="ok" />
                {plural(running.length, 'simulation', 'simulations')} running, all within SLO
              </span>
            ) : (
              'Nothing is running. Inject a failure to begin.'
            )}
          </p>
        </div>
        <p
          className="flex items-center gap-2 text-xs text-muted-foreground"
          aria-label={`Metric stream: ${streamState[stream].label}`}
        >
          <StatusGlyph tone={streamState[stream].tone} />
          {streamState[stream].label}
        </p>
      </div>

      <div className="grid items-start gap-6 lg:grid-cols-[20rem_minmax(0,1fr)] xl:grid-cols-[22rem_minmax(0,1fr)]">
        <section aria-labelledby="inject-heading" className="rounded-lg border bg-card p-4 shadow-card sm:p-5 lg:sticky lg:top-20">
          <h2 id="inject-heading" className="text-[15px] font-semibold tracking-tight">
            Inject a failure
          </h2>
          <p className="mb-4 mt-1 text-sm text-muted-foreground">An incident is declared when an SLO breaks.</p>
          <InjectForm onInjected={reload} />
        </section>

        <div className="flex min-w-0 flex-col gap-8">
          <section aria-labelledby="running-heading" className="flex min-w-0 flex-col gap-5">
            <h2 id="running-heading" className="sr-only">
              Running simulations
            </h2>
            {simulations.phase === 'loading' ? <DashboardSkeleton /> : null}
            {simulations.phase === 'waking' ? <WakingNotice /> : null}
            {simulations.phase === 'error' ? (
              <ErrorNotice
                title="Could not load the simulations"
                message={simulations.error?.message ?? 'Unknown error.'}
                onRetry={reload}
              />
            ) : null}
            {simulations.phase === 'ready' && running.length === 0 ? (
              <EmptyState title="Nothing is running">
                Pick a service and a failure mode in the form, then inject it. Live charts for each simulation show up here,
                grouped by service, with anything over its limit at the top.
              </EmptyState>
            ) : null}

            {groups.map((group) => (
              <section key={group.service} aria-label={`Service ${group.service}`} className="flex min-w-0 flex-col gap-3">
                <header className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <h3 className="min-w-0 truncate font-mono text-sm font-semibold" translate="no">
                    {group.service}
                  </h3>
                  <span className={cn('flex items-center gap-1.5 text-xs', group.breaching ? 'font-medium text-bad' : 'text-muted-foreground')}>
                    <StatusGlyph tone={group.breaching ? 'bad' : 'ok'} />
                    {group.breaching ? 'Breaching' : 'Healthy'}
                  </span>
                  <span className="text-xs text-muted-foreground">{plural(group.simulations.length, 'simulation', 'simulations')}</span>
                </header>
                {group.simulations.map(({ simulation, samples, breaching }) => (
                  <SimulationPanel
                    key={simulation.id}
                    simulation={simulation}
                    samples={samples}
                    slo={slo.data}
                    breaching={breaching}
                    token={token}
                    now={now}
                    onHalted={reload}
                  />
                ))}
              </section>
            ))}
          </section>

          <section aria-labelledby="recent-heading" className="flex flex-col gap-3">
            <h2 id="recent-heading" className="text-sm font-semibold text-muted-foreground">
              Recently finished
            </h2>
            {simulations.phase === 'ready' && finished.length === 0 ? (
              <p className="text-sm text-muted-foreground">Simulations move here when they end or are halted.</p>
            ) : null}
            {finished.length > 0 ? (
              <ul className="grid gap-x-8 gap-y-0 sm:grid-cols-2">
                {finished.map((simulation) => (
                  <li key={simulation.id} className="flex min-w-0 items-center gap-3 border-t py-2.5 text-sm">
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate font-mono font-medium" translate="no">
                        {simulation.service}
                      </span>
                      <span className="truncate text-xs text-muted-foreground">
                        {labelOf(failureModeLabels, simulation.mode)}, started {formatDateTime(simulation.started_at)}
                      </span>
                    </span>
                    <SimulationStatePill state={simulation.state} />
                  </li>
                ))}
              </ul>
            ) : null}
          </section>
        </div>
      </div>
    </div>
  )
}
