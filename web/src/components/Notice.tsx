import type { ReactNode } from 'react'

import { cn } from '../lib/utils'
import { StatusGlyph } from './StatusGlyph'
import type { BadgeTone } from './ui/badge'
import { Button } from './ui/button'
import { Skeleton } from './ui/skeleton'

// PANEL

interface StatePanelProps {
  tone: BadgeTone
  title: string
  children?: ReactNode
  action?: ReactNode
  // How screen readers announce the panel: politely for news, at once for problems.
  role?: 'status' | 'alert'
  className?: string
}

// StatePanel is the shared frame for every state that is not the real content: empty, waking and broken.
function StatePanel({ tone, title, children, action, role, className }: StatePanelProps) {
  return (
    <div
      role={role}
      aria-live={role === 'status' ? 'polite' : undefined}
      className={cn('flex items-start gap-3.5 rounded-lg border bg-card px-4 py-4 sm:px-5', className)}
    >
      <span
        aria-hidden="true"
        className={cn(
          'mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md border',
          tone === 'bad' && 'border-bad/30 bg-bad/10',
          tone === 'info' && 'border-info/30 bg-info/10',
          tone === 'neutral' && 'bg-muted',
        )}
      >
        <StatusGlyph tone={tone} className="h-2.5 w-2.5" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="font-medium leading-snug">{title}</p>
        {children ? <div className="max-w-[65ch] text-sm text-muted-foreground">{children}</div> : null}
        {action ? <div className="mt-2">{action}</div> : null}
      </div>
    </div>
  )
}

// LOADING

// LoadingBlock holds the place of content that is on its way, and tells screen readers so.
export function LoadingBlock({ label, lines = 3, className }: { label: string; lines?: number; className?: string }) {
  return (
    <div role="status" aria-live="polite" className={cn('flex flex-col gap-3', className)}>
      <span className="sr-only">{label}</span>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} className="h-14 w-full rounded-lg" />
      ))}
    </div>
  )
}

// WakingNotice explains the long first wait on a free server, so nobody thinks the app is broken.
export function WakingNotice() {
  return (
    <StatePanel tone="info" title="Waking up the server…" role="status">
      <p>
        The API runs on a free plan that sleeps when nobody uses it. The first request can take up to a minute, and this
        page fills in by itself as soon as the server answers.
      </p>
      <div className="mt-3 h-1 w-full max-w-xs overflow-hidden rounded-full bg-muted" aria-hidden="true">
        <div className="h-full w-1/4 rounded-full bg-info motion-safe:animate-indeterminate" />
      </div>
    </StatePanel>
  )
}

// PROBLEMS

// ErrorNotice shows what went wrong in plain words and offers one more try.
export function ErrorNotice({ title, message, onRetry }: { title: string; message: string; onRetry?: () => void }) {
  return (
    <StatePanel
      tone="bad"
      title={title}
      role="alert"
      action={
        onRetry ? (
          <Button variant="outline" size="sm" onClick={onRetry}>
            Try again
          </Button>
        ) : undefined
      }
    >
      <p className="break-words">{message}</p>
    </StatePanel>
  )
}

// EMPTY

// EmptyState fills a list that has nothing in it yet with a hint about what to do next.
export function EmptyState({ title, children, className }: { title: string; children?: ReactNode; className?: string }) {
  return (
    <StatePanel tone="neutral" title={title} className={cn('border-dashed bg-transparent', className)}>
      {children}
    </StatePanel>
  )
}
