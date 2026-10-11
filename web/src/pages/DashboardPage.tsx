import { useCallback, useEffect, useRef, useState } from 'react'

import { api } from '../api/client'
import type { MetricSample } from '../api/types'
import { useEventStream } from '../api/useEventStream'
import { useResource } from '../api/useResource'
import { DrillComposer } from '../components/DrillComposer'
import { EmptyState, ErrorNotice, WakingNotice } from '../components/Notice'
import { RecentList } from '../components/RecentList'
import { ServiceCard } from '../components/ServiceCard'
import { SignalDot } from '../components/SignalDot'
import { Card } from '../components/ui/card'
import { Overline } from '../components/ui/overline'
import { Skeleton } from '../components/ui/skeleton'
import { useAdminToken } from '../lib/adminToken'
import { connectionOf, useReportConnection } from '../lib/connection'
import { groupByService } from '../lib/grouping'
import { addPeak, keepPeaks, type Peaks } from '../lib/outcome'
import type { PageRoute } from '../lib/routes'
import { addSample, keepOnly, type SampleHistory } from '../lib/samples'
import { useNow } from '../lib/useNow'
import { cn } from '../lib/utils'

export const route: PageRoute = { path: '/', label: 'Dashboard', order: 1 }

// How often the list of simulations is fetched again, so finished ones move out of the running list.
const refreshMs = 5_000
// How often the incidents are fetched again, so a finished simulation can link to the incident it caused.
const incidentsRefreshMs = 10_000

// Where the README explains public and admin drills.
const liveDemoUrl = 'https://github.com/MA-V4/shadow-pager#live-demo'

// plural picks the right word for a count, so the page never says "1 simulations".
function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`
}

// LOADING

// DashboardSkeleton holds the place of the running simulations in the shape they will have.
function DashboardSkeleton() {
  return (
    <div role="status" aria-live="polite">
      <span className="sr-only">Loading simulations…</span>
      <Card className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="h-9 w-16" />
        </div>
        <Skeleton className="h-0.5 w-full" />
        <div className="grid grid-cols-2 gap-x-6 gap-y-2 md:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-10" />
          ))}
        </div>
      </Card>
    </div>
  )
}

// PAGE

// DashboardPage is where an operator runs drills, watches them live, and halts them.
export default function DashboardPage() {
  const [token] = useAdminToken()
  const now = useNow()
  const simulations = useResource((signal) => api.listSimulations(signal), 'simulations', { refreshMs })
  const incidents = useResource((signal) => api.listIncidents(signal), 'incidents', { refreshMs: incidentsRefreshMs })
  const slo = useResource((signal) => api.getSlo(signal), 'slo')
  const config = useResource((signal) => api.getConfig(signal), 'config')
  const [history, setHistory] = useState<SampleHistory>({})
  const [peaks, setPeaks] = useState<Peaks>({})
  // Below the widest layout the composer can be folded away, so it does not push a breach down the page.
  const [composerOpen, setComposerOpen] = useState(true)

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
      setPeaks((p) => addPeak(p, sample))
      if (!known.current.has(sample.simulation_id)) {
        known.current.add(sample.simulation_id)
        reload()
      }
    },
    [reload],
  )
  const stream = useEventStream<MetricSample>(api.streamUrl(), 'metric', onSample)
  useReportConnection(connectionOf(simulations.phase, stream))

  // Samples of simulations that are over are dropped, so the page does not use more and more memory.
  const runningKey = running.map((s) => s.id).join(',')
  const [runningIds, setRunningIds] = useState<readonly string[]>([])
  useEffect(() => {
    const ids = runningKey === '' ? [] : runningKey.split(',')
    for (const id of ids) {
      known.current.add(id)
    }
    setRunningIds(ids)
    setHistory((h) => keepOnly(h, ids))
  }, [runningKey])

  // A peak is kept for as long as its simulation is still listed, which is longer than its samples are.
  const listedKey = all.map((s) => s.id).join(',')
  useEffect(() => {
    if (listedKey !== '') {
      setPeaks((p) => keepPeaks(p, listedKey.split(',')))
    }
  }, [listedKey])

  // On a screen too narrow for two columns the composer folds itself once, when the first load finds something running.
  const folded = useRef(false)
  useEffect(() => {
    if (folded.current || simulations.phase !== 'ready') {
      return
    }
    folded.current = true
    const narrow = typeof window.matchMedia === 'function' && !window.matchMedia('(min-width: 1280px)').matches
    if (narrow && runningKey !== '') {
      setComposerOpen(false)
    }
  }, [simulations.phase, runningKey])

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-1">
        <h1 className="text-heading text-primary">Dashboard</h1>
        {/* The summary names what is wrong first, because that is what an operator looks for. */}
        <p className="flex flex-wrap items-center gap-x-2 text-body text-secondary" aria-live="polite">
          {simulations.phase !== 'ready' ? (
            'Run a drill and watch its telemetry live.'
          ) : breachingCount > 0 ? (
            <>
              <span className="flex items-center gap-2 font-medium text-primary">
                <SignalDot tone="breach" />
                {plural(breachingCount, 'simulation is', 'simulations are')} breaching SLO
              </span>
              <span>of {running.length} running</span>
            </>
          ) : running.length > 0 ? (
            <span className="flex items-center gap-2">
              <SignalDot tone="ok" />
              {plural(running.length, 'simulation', 'simulations')} running, all within SLO
            </span>
          ) : (
            'Nothing is running.'
          )}
        </p>
      </div>

      <div className="grid items-start gap-x-6 gap-y-8 xl:grid-cols-12">
        <section aria-labelledby="drill-heading" className="xl:sticky xl:top-18 xl:col-span-4">
          <Card className="flex flex-col gap-4">
            <div className="flex items-center justify-between gap-4">
              <h2 id="drill-heading" className="text-title text-primary">
                Run a drill
              </h2>
              <button
                type="button"
                onClick={() => setComposerOpen((open) => !open)}
                aria-expanded={composerOpen}
                aria-controls="drill-form"
                className="-my-2 flex h-10 items-center rounded-sm px-2 text-label text-secondary transition-colors duration-150 ease-out hover:text-primary xl:hidden"
              >
                {composerOpen ? 'Hide form' : 'Show form'}
              </button>
            </div>
            <div id="drill-form" className={cn('flex flex-col gap-4', !composerOpen && 'hidden xl:flex')}>
              {/* This one quiet line is only shown on a server that lets visitors start drills. */}
              {config.data?.public_drills === true ? (
                <p className="text-meta text-tertiary" data-public-note="">
                  Drills started without an admin token are public and do not post to Slack.{' '}
                  <a href={liveDemoUrl} target="_blank" rel="noreferrer" className="rounded-sm text-secondary underline underline-offset-4 hover:text-primary">
                    How the live demo works
                  </a>
                </p>
              ) : null}
              <DrillComposer onStarted={reload} runningIds={runningIds} />
            </div>
          </Card>
        </section>

        <div className="flex min-w-0 flex-col gap-8 xl:col-span-8">
          <section aria-labelledby="running-heading" className="flex min-w-0 flex-col gap-3">
            <Overline as="h2" id="running-heading">
              Running
            </Overline>
            {simulations.phase === 'loading' ? <DashboardSkeleton /> : null}
            {simulations.phase === 'waking' ? <WakingNotice /> : null}
            {simulations.phase === 'error' ? (
              <ErrorNotice title="Could not load the simulations" message={simulations.error?.message ?? 'Unknown error.'} onRetry={reload} />
            ) : null}
            {simulations.phase === 'ready' && running.length === 0 ? (
              <EmptyState title="No simulations are running.">Pick a service and a failure mode, then start a drill.</EmptyState>
            ) : null}

            {groups.map((group) => (
              <ServiceCard key={group.service} group={group} slo={slo.data} token={token} now={now} onHalted={reload} />
            ))}
          </section>

          <section aria-labelledby="recent-heading" className="flex min-w-0 flex-col gap-3">
            <Overline as="h2" id="recent-heading">
              Recently finished
            </Overline>
            {simulations.phase === 'ready' && finished.length === 0 ? (
              <p className="text-meta text-tertiary">Simulations move here when they end or are halted.</p>
            ) : null}
            {finished.length > 0 ? (
              <RecentList simulations={finished} incidents={incidents.data?.incidents} peaks={peaks} now={now} />
            ) : null}
          </section>
        </div>
      </div>
    </div>
  )
}
