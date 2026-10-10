import type { ReactNode } from 'react'

import { Alert, AlertTitle } from './ui/alert'
import { Button } from './ui/button'
import { Skeleton } from './ui/skeleton'

// LOADING

// LoadingBlock holds the place of content that is on its way, and tells screen readers so.
export function LoadingBlock({ label, lines = 3 }: { label: string; lines?: number }) {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-3">
      <span className="sr-only">{label}</span>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  )
}

// WakingNotice explains the long first wait on a free server, so nobody thinks the app is broken.
export function WakingNotice() {
  return (
    <Alert tone="info" aria-live="polite">
      <AlertTitle>Waking up the server</AlertTitle>
      <p className="mt-1 text-muted-foreground">
        The API runs on a free plan that goes to sleep when nobody uses it. The first request can take up to a minute, and
        this page fills in by itself as soon as the server answers.
      </p>
    </Alert>
  )
}

// PROBLEMS

// ErrorNotice shows what went wrong in plain words and offers one more try.
export function ErrorNotice({ title, message, onRetry }: { title: string; message: string; onRetry?: () => void }) {
  return (
    <Alert tone="bad">
      <AlertTitle>{title}</AlertTitle>
      <p className="mt-1">{message}</p>
      {onRetry ? (
        <Button variant="outline" size="sm" className="mt-3" onClick={onRetry}>
          Try again
        </Button>
      ) : null}
    </Alert>
  )
}

// EMPTY

// EmptyState fills a list that has nothing in it yet with a hint about what to do next.
export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed px-4 py-8 text-center">
      <p className="font-medium">{title}</p>
      {children ? <div className="mt-1 text-sm text-muted-foreground">{children}</div> : null}
    </div>
  )
}
