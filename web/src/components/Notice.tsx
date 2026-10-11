import type { ReactNode } from 'react'

import { cn } from '../lib/utils'
import { FlatLine } from './FlatLine'
import { SignalDot } from './SignalDot'
import { Button } from './ui/button'
import { Skeleton } from './ui/skeleton'

// LOADING

// LoadingBlock holds the place of content that is on its way, and tells screen readers so.
export function LoadingBlock({ label, lines = 3, className }: { label: string; lines?: number; className?: string }) {
  return (
    <div role="status" aria-live="polite" className={cn('flex flex-col gap-2', className)}>
      <span className="sr-only">{label}</span>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} className="h-11 w-full" />
      ))}
    </div>
  )
}

// WakingNotice explains the long first wait on a free server, so nobody thinks the app is broken.
export function WakingNotice() {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-1 py-2">
      <p className="flex items-center gap-2 text-body font-medium text-primary">
        <SignalDot tone="monitor" />
        Waking server…
      </p>
      <p className="max-w-[60ch] text-meta text-secondary">
        The API runs on a free plan that sleeps when nobody uses it. The first request can take up to a minute, and this
        page fills in by itself as soon as the server answers.
      </p>
      <div className="mt-3 h-0.5 w-full max-w-60 overflow-hidden rounded-full bg-inset" aria-hidden="true">
        <div className="h-full w-1/4 rounded-full bg-monitor motion-safe:animate-indeterminate" />
      </div>
    </div>
  )
}

// PROBLEMS

// ErrorNotice shows what went wrong in plain words and offers one more try.
export function ErrorNotice({ title, message, onRetry }: { title: string; message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="flex flex-col items-start gap-1 py-2">
      <p className="flex items-center gap-2 text-body font-medium text-primary">
        <SignalDot tone="breach" />
        {title}
      </p>
      <p className="max-w-[60ch] wrap-break-word text-meta text-secondary">{message}</p>
      {onRetry ? (
        <Button variant="secondary" onClick={onRetry} className="mt-3">
          Try again
        </Button>
      ) : null}
    </div>
  )
}

// EMPTY

// EmptyState is one quiet line, one short instruction and the flat heartbeat of a monitor with nothing to report.
export function EmptyState({ title, children, className }: { title: string; children?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-1 py-2', className)}>
      <p className="text-body text-secondary">{title}</p>
      {children ? <p className="max-w-[60ch] text-meta text-tertiary">{children}</p> : null}
      <FlatLine className="mt-3" />
    </div>
  )
}
