import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

// The merger has to know our type scale, or it would mistake text-meta for a colour and drop it.
const merge = extendTailwindMerge({
  extend: {
    theme: {
      text: ['overline', 'meta', 'body', 'label', 'title', 'heading', 'readout', 'hero'],
    },
  },
})

// cn joins class names and lets a later Tailwind class win over an earlier one.
export function cn(...inputs: ClassValue[]): string {
  return merge(clsx(inputs))
}
