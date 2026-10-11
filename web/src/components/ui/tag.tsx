import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

// A tag is a signal colour at 10% behind its own text, and plain grey for anything healthy or finished.
const tagVariants = cva('inline-flex h-5 shrink-0 items-center rounded-sm border px-1.5 font-mono text-overline whitespace-nowrap uppercase', {
  variants: {
    tone: {
      neutral: 'border-subtle text-tertiary',
      breach: 'border-breach/30 bg-breach/10 text-breach',
      warn: 'border-warn/30 bg-warn/10 text-warn',
      monitor: 'border-monitor/30 bg-monitor/10 text-monitor',
    },
  },
  defaultVariants: { tone: 'neutral' },
})

export type TagTone = NonNullable<VariantProps<typeof tagVariants>['tone']>

export interface TagProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof tagVariants> {}

export function Tag({ className, tone, ...props }: TagProps) {
  return <span className={cn(tagVariants({ tone }), className)} {...props} />
}
