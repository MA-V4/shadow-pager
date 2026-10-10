import type { HTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

// Skeleton is a grey block that holds the place of something that is still loading.
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div aria-hidden="true" className={cn('animate-pulse rounded-md bg-muted', className)} {...props} />
}
