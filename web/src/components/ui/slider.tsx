import type { InputHTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

interface SliderProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'value' | 'min' | 'max' | 'onChange'> {
  value: number
  min?: number
  max?: number
  onValueChange: (value: number) => void
}

// Slider draws its own thin track and round thumb over an invisible browser slider, which still does all the work.
export function Slider({ value, min = 0, max = 1, onValueChange, className, ...props }: SliderProps) {
  const fraction = max > min ? Math.min(1, Math.max(0, (value - min) / (max - min))) : 0
  // The thumb is 14px wide and stops at the ends of the track, so it never hangs over the edge.
  const position = `calc(7px + ${fraction} * (100% - 14px))`
  return (
    <div className={cn('relative flex h-9 min-w-0 flex-1 items-center pointer-coarse:h-10', className)}>
      <div className="h-1 w-full overflow-hidden rounded-full bg-inset shadow-[inset_0_0_0_1px_var(--border-strong)]">
        <div className="h-full bg-secondary" style={{ width: position }} />
      </div>
      <input
        type="range"
        min={min}
        max={max}
        value={value}
        onChange={(e) => onValueChange(Number(e.target.value))}
        className="peer absolute inset-0 h-full w-full cursor-pointer opacity-0"
        {...props}
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute top-1/2 h-3.5 w-3.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent"
        style={{ left: position }}
      />
    </div>
  )
}
