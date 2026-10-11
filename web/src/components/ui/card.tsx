import type { HTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

// The states a card can signal on its left edge, where a healthy card signals nothing at all.
export type RailTone = 'breach' | 'warn' | 'monitor'

const railShadow: Record<RailTone, string> = {
  breach: 'shadow-[inset_3px_0_0_var(--signal-breach)]',
  warn: 'shadow-[inset_3px_0_0_var(--signal-warn)]',
  monitor: 'shadow-[inset_3px_0_0_var(--signal-monitor)]',
}

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  // The signal rail, a 3px bar on the left edge that is the only coloured border in the app.
  rail?: RailTone | undefined
  // A pulsing rail is kept for a breach that is happening right now.
  pulse?: boolean
}

// Card is a flat panel that is lifted by its border and its surface, never by a shadow.
export function Card({ rail, pulse = false, className, children, ...props }: CardProps) {
  return (
    <div className={cn('relative rounded-lg border border-subtle bg-surface p-4', className)} {...props}>
      {rail === undefined ? null : (
        <span
          aria-hidden="true"
          data-rail={rail}
          className={cn('pointer-events-none absolute -inset-px rounded-lg', railShadow[rail], pulse && 'motion-safe:animate-rail')}
        />
      )}
      {children}
    </div>
  )
}
