import { useEffect, useState } from 'react'

// useNow gives back the current time and updates it on a steady beat, so countdowns move without a page reload.
export function useNow(intervalMs = 1_000): number {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(timer)
  }, [intervalMs])

  return now
}
