import { useCallback, useEffect, useId, useRef, useState } from 'react'

import { api, toApiError } from '../api/client'
import type { MetricSample, Simulation } from '../api/types'
import { useEventStream, type StreamStatus } from '../api/useEventStream'
import { useResource } from '../api/useResource'
import { InjectForm } from '../components/InjectForm'
import { EmptyState, ErrorNotice, LoadingBlock, WakingNotice } from '../components/Notice'
import { SeverityPill, SimulationStatePill } from '../components/Pills'
import { SimulationCharts } from '../components/SimulationCharts'
import { Alert } from '../components/ui/alert'
import { Badge, type BadgeTone } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { useAdminToken } from '../lib/adminToken'
import { describeWriteError } from '../lib/apiMessages'
import { formatDateTime, formatGoDuration, formatTime, humanize } from '../lib/format'
import type { PageRoute } from '../lib/routes'
import { addSample, keepOnly, type SampleHistory } from '../lib/samples'

export const route: PageRoute = { path: '/', label: 'Dashboard', order: 1 }

// How often the list of simulations is fetched again, so finished ones move out of the running list.
const refreshMs = 5_000

const streamBadge: Record<StreamStatus, { tone: BadgeTone; label: string }> = {
  open: { tone: 'ok', label: 'Live' },
  connecting: { tone: 'neutral', label: 'Connecting' },
  reconnecting: { tone: 'warn', label: 'Reconnecting' },
}

// ADMIN TOKEN

// AdminTokenField lets an operator enter the optional admin token, which stays in this browser.
function AdminTokenField({ token, onChange }: { token: string; onChange: (token: string) => void }) {
  const id = useId()
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>Admin token</Label>
      <Input
        id={id}
        type="password"
        value={token}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="off"
        placeholder="Optional"
        aria-describedby={`${id}-hint`}
      />
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">
        Only needed when the server was started with an admin token. It is saved in this browser and nowhere else.
      </p>
    </div>
  )
}

// RUNNING SIMULATION

interface RunningSimulationProps {
  simulation: Simulation
  samples: readonly MetricSample[]
  slo: Parameters<typeof SimulationCharts>[0]['slo']
  token: string
  onHalted: () => void
}

// RunningSimulation shows one running simulation with its live charts and a button to stop it early.
function RunningSimulation({ simulation, samples, slo, token, onHalted }: RunningSimulationProps) {
  const [halting, setHalting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function halt() {
    setHalting(true)
    setError(null)
    try {
      await api.haltSimulation(simulation.id, token)
      onHalted()
    } catch (err) {
      setError(describeWriteError(toApiError(err)))
    } finally {
      setHalting(false)
    }
  }

  const latest = samples.at(-1)
  return (
    <Card>
      <CardHeader className="sm:flex-row sm:items-start sm:justify-between sm:gap-4">
        <div className="flex min-w-0 flex-col gap-1.5">
          <CardTitle className="flex flex-wrap items-center gap-2">
            <span className="truncate font-mono">{simulation.service}</span>
            <SeverityPill severity={simulation.severity} />
            {latest === undefined ? null : (
              <Badge tone={latest.healthy ? 'ok' : 'bad'}>{latest.healthy ? 'Within SLO' : 'Breaching SLO'}</Badge>
            )}
          </CardTitle>
          <CardDescription>
            {humanize(simulation.mode)} at {Math.round(simulation.intensity * 100)}% for {formatGoDuration(simulation.duration)},
            ends at {formatTime(simulation.ends_at)}
          </CardDescription>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void halt()}
          disabled={halting}
          aria-label={`Halt the simulation on ${simulation.service}`}
        >
          {halting ? 'Halting' : 'Halt'}
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error === null ? null : <Alert tone="bad">{error}</Alert>}
        <SimulationCharts samples={samples} slo={slo} />
      </CardContent>
    </Card>
  )
}

// PAGE

// DashboardPage is where an operator injects failures, watches them live, and halts them.
export default function DashboardPage() {
  const [token, setToken] = useAdminToken()
  const simulations = useResource((signal) => api.listSimulations(signal), 'simulations', { refreshMs })
  const slo = useResource((signal) => api.getSlo(signal), 'slo')
  const [history, setHistory] = useState<SampleHistory>({})

  const all = simulations.data?.simulations ?? []
  const running = all.filter((s) => s.state === 'running')
  const finished = all.filter((s) => s.state !== 'running').slice(0, 10)

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
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
          <p className="text-sm text-muted-foreground">Inject a pretend failure and watch its telemetry live.</p>
        </div>
        <Badge tone={streamBadge[stream].tone} aria-label={`Metric stream: ${streamBadge[stream].label}`}>
          <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-current" />
          {streamBadge[stream].label}
        </Badge>
      </div>

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle>Inject a failure</CardTitle>
            <CardDescription>The engine simulates it and declares an incident when an SLO breaks.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-6">
            <InjectForm token={token} onInjected={reload} />
            <AdminTokenField token={token} onChange={setToken} />
          </CardContent>
        </Card>

        <section aria-labelledby="running-heading" className="flex min-w-0 flex-col gap-4">
          <h2 id="running-heading" className="text-lg font-semibold">
            Running simulations
          </h2>
          {simulations.phase === 'loading' ? <LoadingBlock label="Loading simulations" /> : null}
          {simulations.phase === 'waking' ? <WakingNotice /> : null}
          {simulations.phase === 'error' ? (
            <ErrorNotice
              title="The simulations could not be loaded"
              message={simulations.error?.message ?? 'Unknown error.'}
              onRetry={reload}
            />
          ) : null}
          {simulations.phase === 'ready' && running.length === 0 ? (
            <EmptyState title="Nothing is running">Use the form to inject a failure, and its charts show up here.</EmptyState>
          ) : null}
          {running.map((simulation) => (
            <RunningSimulation
              key={simulation.id}
              simulation={simulation}
              samples={history[simulation.id] ?? []}
              slo={slo.data}
              token={token}
              onHalted={reload}
            />
          ))}
        </section>
      </div>

      <section aria-labelledby="recent-heading" className="flex flex-col gap-3">
        <h2 id="recent-heading" className="text-lg font-semibold">
          Recently finished
        </h2>
        {simulations.phase === 'ready' && finished.length === 0 ? (
          <EmptyState title="No finished simulations yet">Simulations move here when they end or are halted.</EmptyState>
        ) : null}
        {finished.length > 0 ? (
          <Card>
            <ul className="divide-y">
              {finished.map((simulation) => (
                <li key={simulation.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 text-sm">
                  <span className="min-w-0 truncate font-mono font-medium">{simulation.service}</span>
                  <span className="text-muted-foreground">{humanize(simulation.mode)}</span>
                  <SimulationStatePill state={simulation.state} />
                  <span className="ml-auto text-xs text-muted-foreground">
                    Started {formatDateTime(simulation.started_at)}
                  </span>
                </li>
              ))}
            </ul>
          </Card>
        ) : null}
      </section>
    </div>
  )
}
