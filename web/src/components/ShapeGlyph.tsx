import { modeShapes, shapePoints, type ShapeName } from '../lib/shapes'
import { cn } from '../lib/utils'

// toPath joins the points of a curve into one line for the drawing.
function toPath(shape: ShapeName): string {
  return shapePoints(shape)
    .map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x} ${y}`)
    .join('')
}

// The drawings never change, so each one is worked out once.
const paths: Record<ShapeName, string> = {
  spike: toPath('spike'),
  gradual: toPath('gradual'),
  leak: toPath('leak'),
  cliff: toPath('cliff'),
}

interface ShapeGlyphProps {
  // A failure mode from the API, or anything else, which then gets no drawing.
  mode: string
  className?: string
}

// ShapeGlyph is the 40 by 16 line drawing of how a failure mode behaves over time, plotted from the curve the engine uses.
export function ShapeGlyph({ mode, className }: ShapeGlyphProps) {
  const shape = (modeShapes as Readonly<Record<string, ShapeName>>)[mode]
  if (shape === undefined) {
    return null
  }
  return (
    <svg
      aria-hidden="true"
      data-shape={shape}
      viewBox="0 0 40 16"
      width="40"
      height="16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={cn('shrink-0 text-tertiary', className)}
    >
      <path d={paths[shape]} />
    </svg>
  )
}
