import { useState } from 'react'

import { api, toApiError } from '../api/client'
import type { MetricSample, Simulation, Slo } from '../api/types'
import { describeWriteError } from '../lib/apiMessages'
import { formatGoDuration } from '../lib/format'
import { formatRemaining, progressOf } from '../lib/grouping'
import { failureModeLabels, labelOf } from '../lib/labels'
import { ShapeGlyph } from './ShapeGlyph'
import { SignalDot } from './SignalDot'
import { SimulationCharts } from './SimulationCharts'
import { SeverityTag } from './Tags'
import { Button } from './ui/button'

// PROGRESS

// SimulationProgressBar is the 2px line under the header that fills up as the planned time runs out.
function SimulationProgressBar({ simulation, now }: { simulation: Simulation; now: number }) {
  const { fraction, remainingMs } = progressOf(simulation, now)
  const remaining = remainingMs === 0 ? 'Ending…' : `${formatRemaining(remainingMs)} left`

  return (
    <div
      role="progressbar"
      aria-label={`Time elapsed for the simulation on ${simulation.service}`}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(fraction * 100)}
      aria-valuetext={`${remaining} of ${formatGoDuration(simulation.duration)}`}
      className="h-0.5 w-full overflow-hidden rounded-full bg-strong"
    >
      <div className="h-full w-full origin-left bg-secondary transition-transform duration-1000 ease-linear" style={{ transform: `scaleX(${fraction})` }} />
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
  const { remainingMs } = progressOf(simulation, now)
  return (
    <article aria-label={`${mode} on ${simulation.service}`} className="flex min-w-0 flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
          <SeverityTag severity={simulation.severity} />
          <span className="flex min-w-0 items-center gap-2">
            <ShapeGlyph mode={simulation.mode} />
            <h4 className="truncate text-label text-primary">{mode}</h4>
          </span>
          {/* The state is said in plain words, because the red is kept for the rail and the breaching value. */}
          {breaching ? (
            <span className="text-meta font-medium text-primary">Breaching SLO</span>
          ) : samples.length > 0 ? (
            <span className="flex items-center gap-1.5 text-meta text-tertiary">
              <SignalDot tone="ok" />
              Within SLO
            </span>
          ) : null}
          <span className="font-mono text-meta text-tertiary">
            {remainingMs === 0 ? 'ending' : `${formatRemaining(remainingMs)} left`} at {Math.round(simulation.intensity * 100)}%
          </span>
        </div>
        <Button variant="destructive" onClick={() => void halt()} disabled={halting} aria-label={`Halt the simulation on ${simulation.service}`}>
          {halting ? 'Halting…' : 'Halt'}
        </Button>
      </div>

      <SimulationProgressBar simulation={simulation} now={now} />

      {error === null ? null : (
        <p role="alert" className="flex items-start gap-2 text-meta text-breach">
          <SignalDot tone="breach" className="mt-1.25" />
          <span className="min-w-0 wrap-break-word">{error}</span>
        </p>
      )}

      <SimulationCharts samples={samples} slo={slo} />
    </article>
  )
}
