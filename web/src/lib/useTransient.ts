import { useCallback, useEffect, useState } from 'react'

// How long a passing message stays on screen, from DESIGN.md section 4.
export const feedbackMs = 4_000

// useTransient holds a message that removes itself after a while, so feedback never outlives what it describes.
export function useTransient<T>(ms = feedbackMs): [T | null, (value: T) => void, () => void] {
  const [value, setValue] = useState<T | null>(null)

  useEffect(() => {
    if (value === null) {
      return
    }
    const timer = setTimeout(() => setValue(null), ms)
    return () => clearTimeout(timer)
  }, [value, ms])

  const show = useCallback((next: T) => setValue(next), [])
  const clear = useCallback(() => setValue(null), [])
  return [value, show, clear]
}
