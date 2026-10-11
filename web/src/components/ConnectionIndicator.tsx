import { connectionLabels, useConnection, type ConnectionState } from '../lib/connection'
import { SignalDot, type SignalTone } from './SignalDot'

const tones: Record<Exclude<ConnectionState, 'idle'>, SignalTone> = {
  live: 'ok',
  connecting: 'neutral',
  reconnecting: 'warn',
  waking: 'monitor',
  offline: 'breach',
}

// ConnectionIndicator is the dot and the word in the header that say how the link to the server is doing.
export function ConnectionIndicator() {
  const state = useConnection()
  if (state === 'idle') {
    return null
  }
  return (
    <p role="status" aria-label={`Connection: ${connectionLabels[state]}`} className="flex shrink-0 items-center gap-2 text-meta text-secondary">
      <SignalDot tone={tones[state]} />
      {connectionLabels[state]}
    </p>
  )
}
