import { splitUnit } from '../lib/metrics'
import { cn } from '../lib/utils'

interface ValueProps {
  // A formatted value with its unit, such as 824 ms.
  text: string
  className?: string
  unitClassName?: string
}

// Value shows a number in mono with its unit beside it, smaller and quieter.
export function Value({ text, className, unitClassName }: ValueProps) {
  const [number, unit] = splitUnit(text)
  return (
    <span className={cn('font-mono whitespace-nowrap', className)}>
      {number}
      {unit === '' ? null : <span className={cn('text-meta text-tertiary', unitClassName)}>{unit}</span>}
    </span>
  )
}
