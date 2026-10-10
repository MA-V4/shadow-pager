import { cn } from '../lib/utils'
import type { BadgeTone } from './ui/badge'

// Each meaning has its own shape as well as its own colour, so it can be told apart without seeing colour.
const shapes: Record<BadgeTone, string> = {
  ok: 'rounded-full bg-current',
  warn: 'bg-current [clip-path:polygon(50%_0,100%_100%,0_100%)]',
  bad: 'rotate-45 rounded-[1px] bg-current',
  info: 'rounded-full border-2 border-current',
  neutral: 'rounded-full border border-current',
}

const colours: Record<BadgeTone, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  bad: 'text-bad',
  info: 'text-info',
  neutral: 'text-muted-foreground',
}

interface StatusGlyphProps {
  tone: BadgeTone
  // Size in Tailwind units, where the default suits a line of small text.
  className?: string
  // Set to false when the glyph sits inside something that already has the right colour.
  coloured?: boolean
}

// StatusGlyph is the small shape in front of a status word: a dot for healthy, a triangle for a warning, a diamond for a breach.
export function StatusGlyph({ tone, className, coloured = true }: StatusGlyphProps) {
  return (
    <span
      aria-hidden="true"
      data-shape={tone}
      className={cn('inline-block h-2 w-2 shrink-0', shapes[tone], coloured && colours[tone], className)}
    />
  )
}
