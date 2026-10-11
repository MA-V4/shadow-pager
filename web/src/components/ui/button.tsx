import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import type { ButtonHTMLAttributes, Ref } from 'react'

import { cn } from '../../lib/utils'

// Buttons are 36px tall with a mouse and 40px under a finger, so they are always easy to hit.
const buttonVariants = cva(
  'inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md text-label transition-[background-color,border-color,color,transform] duration-150 ease-out active:translate-y-px disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        primary: 'bg-primary text-canvas hover:bg-primary/90',
        secondary: 'border border-strong bg-transparent text-primary hover:bg-raised',
        destructive: 'border border-strong bg-transparent text-primary hover:border-breach hover:text-breach',
        ghost: 'bg-transparent text-secondary hover:bg-raised hover:text-primary',
      },
      size: {
        default: 'h-9 px-4 pointer-coarse:h-10',
        icon: 'h-9 w-9 pointer-coarse:h-10 pointer-coarse:w-10',
      },
    },
    defaultVariants: { variant: 'primary', size: 'default' },
  },
)

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  // asChild puts the button look on the element inside, for example on a link.
  asChild?: boolean
  ref?: Ref<HTMLButtonElement>
}

export function Button({ className, variant, size, asChild = false, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : 'button'
  return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />
}
