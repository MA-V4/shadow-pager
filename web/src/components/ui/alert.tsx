import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

const alertVariants = cva('rounded-md border px-3.5 py-3 text-sm', {
  variants: {
    tone: {
      neutral: 'border-border bg-muted text-foreground',
      ok: 'border-ok/40 bg-ok/10 text-foreground',
      warn: 'border-warn/40 bg-warn/10 text-foreground',
      bad: 'border-bad/40 bg-bad/10 text-foreground',
      info: 'border-info/40 bg-info/10 text-foreground',
    },
  },
  defaultVariants: { tone: 'neutral' },
})

export interface AlertProps extends HTMLAttributes<HTMLDivElement>, VariantProps<typeof alertVariants> {}

// Alert is announced by screen readers, politely for news and at once for problems.
export function Alert({ className, tone, role, ...props }: AlertProps) {
  return <div role={role ?? (tone === 'bad' ? 'alert' : 'status')} className={cn(alertVariants({ tone }), className)} {...props} />
}

export function AlertTitle({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn('font-semibold', className)} {...props} />
}
