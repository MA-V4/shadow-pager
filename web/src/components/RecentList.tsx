import { Link } from 'react-router-dom'

import type { Incident, Simulation } from '../api/types'
import { formatAgo, formatDateTime, formatElapsed, formatGoDuration } from '../lib/format'
import { failureModeLabels, labelOf } from '../lib/labels'
import { formatPeak, outcomeLead, outcomeOf, type Peaks } from '../lib/outcome'
import { ShapeGlyph } from './ShapeGlyph'
import { Value } from './Value'

// ranFor says how long a simulation really ran, which is shorter than planned when it was halted.
function ranFor(simulation: Simulation): string {
  const start = new Date(simulation.started_at).getTime()
  const end = simulation.ended_at === undefined ? Number.NaN : new Date(simulation.ended_at).getTime()
  return Number.isFinite(start) && Number.isFinite(end) && end >= start ? formatElapsed(end - start) : formatGoDuration(simulation.duration)
}

interface RecentListProps {
  simulations: readonly Simulation[]
  // The known incidents, or nothing when they could not be loaded.
  incidents: readonly Incident[] | undefined
  peaks: Peaks
  now: number
}

// RecentList is the dense list of finished simulations: what ran, how it ended, how high it peaked and for how long.
export function RecentList({ simulations, incidents, peaks, now }: RecentListProps) {
  return (
    <ul aria-label="Recently finished simulations" className="border-t border-subtle">
      {simulations.map((simulation) => {
        const outcome = outcomeOf(simulation, incidents)
        const peak = formatPeak(simulation, peaks)
        const ended = simulation.ended_at ?? simulation.ends_at
        return (
          <li
            key={simulation.id}
            className="grid min-h-11 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 border-b border-subtle px-2 py-2 transition-colors duration-150 ease-out hover:bg-raised lg:grid-cols-[minmax(0,1fr)_11rem_minmax(0,2.2fr)_5rem_3.5rem_4.5rem]"
          >
            <span className="truncate font-mono text-body text-primary" translate="no">
              {simulation.service}
            </span>
            <span className="order-3 col-span-2 flex min-w-0 items-center gap-2 text-body text-secondary lg:order-0 lg:col-span-1">
              <ShapeGlyph mode={simulation.mode} />
              <span className="truncate">{labelOf(failureModeLabels, simulation.mode)}</span>
            </span>
            <span data-outcome="" className="order-4 col-span-2 min-w-0 text-body text-secondary lg:order-0 lg:col-span-1">
              {outcomeLead(outcome)}
              {outcome.incidentId === undefined ? null : (
                <>
                  , incident{' '}
                  <Link
                    to={`/incidents/${encodeURIComponent(outcome.incidentId)}`}
                    className="rounded-sm font-mono text-accent underline-offset-4 hover:underline"
                    translate="no"
                  >
                    {outcome.incidentId}
                  </Link>
                </>
              )}
              {outcome.halted ? ', halted early' : null}
            </span>
            <span
              data-peak=""
              className="order-5 text-left lg:order-0 lg:text-right"
              title={peak === '-' ? 'This simulation was not watched in this browser, so its peak is not known.' : 'Highest value seen in this browser'}
            >
              <span className="mr-2 text-meta text-tertiary lg:hidden">Peak</span>
              <Value text={peak} className="text-body text-primary" />
            </span>
            <span className="order-6 text-right font-mono text-meta text-secondary lg:order-0" title="How long it ran">
              {ranFor(simulation)}
            </span>
            <time dateTime={ended} title={formatDateTime(ended)} className="order-2 text-right text-meta text-tertiary lg:order-0">
              {formatAgo(ended, now)}
            </time>
          </li>
        )
      })}
    </ul>
  )
}
