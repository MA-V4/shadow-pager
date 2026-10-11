import type { InputHTMLAttributes } from 'react'

import { cn } from '../../lib/utils'

// Input is the one text field of the app: sunk into the page, with a strong edge that turns red when it is wrong.
export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'h-9 w-full min-w-0 rounded-md border border-strong bg-inset px-3 text-base text-primary transition-[border-color] duration-150 ease-out placeholder:text-tertiary hover:border-tertiary disabled:cursor-not-allowed disabled:opacity-50 aria-[invalid=true]:border-breach sm:text-body pointer-coarse:h-10',
        className,
      )}
      {...props}
    />
  )
}
