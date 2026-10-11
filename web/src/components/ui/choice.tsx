import type { ReactNode } from 'react'

import { cn } from '../../lib/utils'

export interface Choice<T extends string> {
  value: T
  label: string
}

interface ChoiceGroupProps<T extends string> {
  // The id of the group, which is also where the focus goes when the server complains about it.
  id?: string | undefined
  name: string
  value: T
  options: readonly Choice<T>[]
  onChange: (value: T) => void
  // The id of the visible label that names the whole group.
  labelledBy: string
  describedBy?: string | undefined
  invalid?: boolean | undefined
  className?: string | undefined
  optionClassName?: string | undefined
  // What to draw inside one option, when a plain label is not enough.
  children?: (option: Choice<T>) => ReactNode
}

// ChoiceGroup is a row of real radio buttons dressed up, so the keyboard and screen readers get them for free.
export function ChoiceGroup<T extends string>(props: ChoiceGroupProps<T>) {
  const { id, name, value, options, onChange, labelledBy, describedBy, invalid, className, optionClassName, children } = props
  return (
    <div
      id={id}
      role="radiogroup"
      tabIndex={-1}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      aria-invalid={invalid}
      className={className}
    >
      {options.map((option) => (
        <label
          key={option.value}
          className={cn(
            'cursor-pointer transition-[background-color,border-color,color] duration-150 ease-out has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-accent',
            optionClassName,
          )}
        >
          <input
            type="radio"
            name={name}
            value={option.value}
            checked={value === option.value}
            onChange={() => onChange(option.value)}
            className="sr-only"
          />
          {children ? children(option) : option.label}
        </label>
      ))}
    </div>
  )
}

type PresetProps<T extends string> = Omit<ChoiceGroupProps<T>, 'optionClassName' | 'children'>

// Segmented is a switch with a few equal parts, where exactly one part is on.
export function Segmented<T extends string>({ className, ...props }: PresetProps<T>) {
  return (
    <ChoiceGroup
      {...props}
      className={cn('grid h-9 auto-cols-fr grid-flow-col gap-0.5 rounded-md border border-strong bg-inset p-0.5 pointer-coarse:h-10', className)}
      optionClassName="flex min-w-0 items-center justify-center rounded-sm px-2 text-label text-secondary hover:text-primary has-[:checked]:bg-raised has-[:checked]:text-primary has-[:checked]:shadow-[inset_0_0_0_1px_var(--border-strong)]"
    />
  )
}

// Chips are short machine values to pick from, such as 30s or 2m.
export function Chips<T extends string>({ className, ...props }: PresetProps<T>) {
  return (
    <ChoiceGroup
      {...props}
      className={cn('flex flex-wrap gap-2', className)}
      optionClassName="flex h-9 min-w-12 items-center justify-center rounded-md border border-strong px-3 font-mono text-meta text-secondary hover:text-primary has-[:checked]:border-accent has-[:checked]:bg-raised has-[:checked]:text-primary pointer-coarse:h-10"
    />
  )
}
