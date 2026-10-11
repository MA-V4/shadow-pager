import { cn } from '../lib/utils'

// FlatLine is the idle heartbeat of an empty screen: a flat line with a small mark that drifts along it.
export function FlatLine({ className }: { className?: string }) {
  return (
    <div aria-hidden="true" data-flatline="" className={cn('relative h-4 w-full max-w-60 overflow-hidden', className)}>
      <span className="absolute inset-x-0 top-1/2 h-[1.5px] -translate-y-1/2 bg-chart-line" />
      <span className="absolute left-0 top-1/2 -mt-0.5 h-1 w-5 rounded-full bg-chart-line motion-safe:animate-sweep motion-reduce:hidden" />
    </div>
  )
}
