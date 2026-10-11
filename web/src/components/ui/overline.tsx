import type { HTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

interface OverlineProps extends HTMLAttributes<HTMLElement> {
  // The element to draw, which is a heading when the label names a section.
  as?: 'h2' | 'h3' | 'p' | 'span'
}

// Overline is the small capital label that names a section and nothing else.
export function Overline({ as: Comp = 'p', className, ...props }: OverlineProps) {
  return <Comp className={cn('font-mono text-overline uppercase text-secondary', className)} {...props} />
}
