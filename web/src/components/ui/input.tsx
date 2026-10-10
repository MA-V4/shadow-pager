import type { InputHTMLAttributes, SelectHTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

const fieldClass =
  'h-10 w-full rounded-md border border-input bg-background px-3 text-sm placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50 aria-[invalid=true]:border-bad'

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cn(fieldClass, className)} {...props} />
}

// Select is the browser's own select with our look, because it works well with keyboards and phones.
export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cn(fieldClass, 'pr-8', className)} {...props} />
}
