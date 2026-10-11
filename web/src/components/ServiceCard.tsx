import type { Slo } from '../api/types'
import type { ServiceGroup } from '../lib/grouping'
import { SimulationPanel } from './SimulationPanel'
import { Card } from './ui/card'

interface ServiceCardProps {
  group: ServiceGroup
  slo: Slo | undefined
  token: string
  now: number
  onHalted: () => void
}

// ServiceCard holds every running simulation of one service, and its red rail is lit while any of them is breaching.
export function ServiceCard({ group, slo, token, now, onHalted }: ServiceCardProps) {
  const count = group.simulations.length
  return (
    <section aria-label={`Service ${group.service}`}>
      <Card rail={group.breaching ? 'breach' : undefined} pulse={group.breaching} className="flex min-w-0 flex-col gap-4">
        <header className="flex flex-wrap items-baseline justify-between gap-x-4">
          <h3 className="min-w-0 truncate font-mono text-title font-medium text-primary" translate="no">
            {group.service}
          </h3>
          {count > 1 ? <span className="text-meta text-tertiary">{count} simulations</span> : null}
        </header>
        {group.simulations.map(({ simulation, samples, breaching }, index) => (
          <div key={simulation.id} className={index === 0 ? undefined : 'border-t border-subtle pt-4'}>
            <SimulationPanel
              simulation={simulation}
              samples={samples}
              slo={slo}
              breaching={breaching}
              token={token}
              now={now}
              onHalted={onHalted}
            />
          </div>
        ))}
      </Card>
    </section>
  )
}
