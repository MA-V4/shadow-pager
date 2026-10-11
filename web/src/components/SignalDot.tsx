import { cn } from '../lib/utils'

export type SignalTone = 'ok' | 'warn' | 'breach' | 'monitor' | 'neutral'

const colours: Record<SignalTone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  breach: 'bg-breach',
  monitor: 'bg-monitor',
  neutral: 'bg-tertiary',
}

// SignalDot is the 6px mark in front of a status word, and the word beside it always says the same thing.
export function SignalDot({ tone, className }: { tone: SignalTone; className?: string }) {
  return <span aria-hidden="true" data-tone={tone} className={cn('inline-block h-1.5 w-1.5 shrink-0 rounded-full', colours[tone], className)} />
}
