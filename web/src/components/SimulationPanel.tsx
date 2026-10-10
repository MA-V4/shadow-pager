import { useState } from 'react'

import { api, toApiError } from '../api/client'
import type { MetricSample, Simulation, Slo } from '../api/types'
import { describeWriteError } from '../lib/apiMessages'
import { formatGoDuration } from '../lib/format'
import { formatRemaining, progressOf } from '../lib/grouping'
import { failureModeLabels, labelOf } from '../lib/labels'
import { cn } from '../lib/utils'
import { SeverityPill } from './Pills'
import { SimulationCharts } from './SimulationCharts'
import { StatusGlyph } from './StatusGlyph'
import { Button } from './ui/button'

// PROGRESS

// SimulationProgressBar shows how much of the planned time is gone and counts down what is left.
function SimulationProgressBar({ simulation, now }: { simulation: Simulation; now: number }) {
  const { fraction, remainingMs } = progressOf(simulation, now)
  const remaining = remainingMs === 0 ? 'Ending…' : `${formatRemaining(remainingMs)} left`

  return (
    <div className="flex items-center gap-3">
      <div
        role="progressbar"
        aria-label={`Time elapsed for the simulation on ${simulation.service}`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(fraction * 100)}
        aria-valuetext={`${remaining} of ${formatGoDuration(simulation.duration)}`}
        className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted"
      >
        <div
          className="h-full w-full origin-left rounded-full bg-muted-foreground/70 transition-transform duration-1000 ease-linear"
          style={{ transform: `scaleX(${fraction})` }}
        />
      </div>
      <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
        {remaining} <span className="text-muted-foreground/70">of {formatGoDuration(simulation.duration)}</span>
      </span>
    </div>
  )
}

// PANEL

interface SimulationPanelProps {
  simulation: Simulation
  samples: readonly MetricSample[]
  slo: Slo | undefined
  breaching: boolean
  token: string
  now: number
  onHalted: () => void
}

// SimulationPanel shows one running simulation: what it is, how long it has left, its metrics, and a way to stop it.
export function SimulationPanel({ simulation, samples, slo, breaching, token, now, onHalted }: SimulationPanelProps) {
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

  const mode = labelOf(failureModeLabels, simulation.mode)
  return (
    <article
      aria-label={`${mode} on ${simulation.service}`}
      className={cn(
        'flex flex-col gap-4 rounded-lg border bg-card p-4 shadow-card sm:p-5',
        // A breaching simulation gets a red edge, so it is the first thing the eye finds on the page.
        breaching && 'border-bad/50 border-l-[3px] border-l-bad',
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 flex-col gap-1.5">
          <h4 className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[15px] font-semibold tracking-tight">
            {mode}
            <SeverityPill severity={simulation.severity} />
            {breaching ? (
              <span className="flex items-center gap-1.5 text-xs font-medium text-bad">
                <StatusGlyph tone="bad" />
                Breaching SLO
              </span>
            ) : samples.length > 0 ? (
              <span className="flex items-center gap-1.5 text-xs font-normal text-muted-foreground">
                <StatusGlyph tone="ok" />
                Within SLO
              </span>
            ) : null}
          </h4>
          <p className="text-xs text-muted-foreground">
            Intensity <span className="font-mono tabular-nums">{Math.round(simulation.intensity * 100)}%</span>
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void halt()}
          disabled={halting}
          aria-label={`Halt the simulation on ${simulation.service}`}
        >
          {halting ? 'Halting…' : 'Halt simulation'}
        </Button>
      </div>

      <SimulationProgressBar simulation={simulation} now={now} />

      {error === null ? null : (
        <p role="alert" className="text-sm text-bad">
          {error}
        </p>
      )}

      <SimulationCharts samples={samples} slo={slo} />
    </article>
  )
}
