import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react'

import { api, toApiError } from '../api/client'
import { failureModes, severities, type FailureMode, type Severity, type Simulation } from '../api/types'
import { useAdminToken } from '../lib/adminToken'
import { describeWriteError, splitInjectErrors, type InjectErrors, type InjectField } from '../lib/apiMessages'
import { failureModeDescriptions, failureModeLabels, severityLabels, startedSentence } from '../lib/labels'
import { useTransient } from '../lib/useTransient'
import { cn } from '../lib/utils'
import { ShapeGlyph } from './ShapeGlyph'
import { SignalDot } from './SignalDot'
import { Button } from './ui/button'
import { Chips, ChoiceGroup, Segmented, type Choice } from './ui/choice'
import { Input } from './ui/input'
import { Slider } from './ui/slider'

const noErrors: InjectErrors = { fields: {}, general: [] }

// The order of the fields on screen, used to find the first one with a problem.
const fieldOrder: InjectField[] = ['service', 'mode', 'severity', 'intensity', 'duration']

const modeOptions: Choice<FailureMode>[] = failureModes.map((mode) => ({ value: mode, label: failureModeLabels[mode] }))
const severityOptions: Choice<Severity>[] = severities.map((severity) => ({ value: severity, label: severityLabels[severity] }))

// The ready made durations, and one more chip that opens a field for any other.
const custom = 'custom'
const durationOptions: Choice<string>[] = [
  { value: '30s', label: '30s' },
  { value: '2m', label: '2m' },
  { value: '5m', label: '5m' },
  { value: '10m', label: '10m' },
  { value: custom, label: 'Custom' },
]

// FIELD

interface FieldProps {
  id: string
  label: string
  // True when the control is a single input that the label can point at.
  forInput?: boolean
  hint: string
  error: string | undefined
  // Something to show on the same line as the label, at the right.
  aside?: ReactNode
  children: ReactNode
}

// Field is a label above, a control, and one helper line below that always takes the same room, so fields never shift.
function Field({ id, label, forInput = false, hint, error, aside, children }: FieldProps) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex items-baseline justify-between gap-3">
        {forInput ? (
          <label id={`${id}-label`} htmlFor={id} className="text-label text-primary">
            {label}
          </label>
        ) : (
          <span id={`${id}-label`} className="text-label text-primary">
            {label}
          </span>
        )}
        {aside}
      </div>
      {children}
      <p id={`${id}-message`} className={cn('flex min-h-8 items-start gap-1.5 text-meta', error === undefined ? 'text-tertiary' : 'text-breach')}>
        {error === undefined ? null : <SignalDot tone="breach" className="mt-1.25" />}
        <span className="min-w-0 wrap-break-word">{error ?? hint}</span>
      </p>
    </div>
  )
}

// FORM

interface DrillComposerProps {
  // Called with the new simulation after the server accepted it.
  onStarted: (simulation: Simulation) => void
  // The simulations that are running right now, so a success line can leave when its simulation does.
  runningIds: readonly string[]
}

// DrillComposer starts a new pretend failure and shows the server's complaints right under the fields they are about.
export function DrillComposer({ onStarted, runningIds }: DrillComposerProps) {
  const id = useId()
  const [token] = useAdminToken()
  const [service, setService] = useState('payments-api')
  const [mode, setMode] = useState<FailureMode>('latency_spike')
  const [severity, setSeverity] = useState<Severity>('major')
  const [intensity, setIntensity] = useState(0.8)
  const [preset, setPreset] = useState('2m')
  const [customDuration, setCustomDuration] = useState('90s')

  const [submitting, setSubmitting] = useState(false)
  const [errors, setErrors] = useState<InjectErrors>(noErrors)
  const [started, showStarted, clearStarted] = useTransient<Simulation>()
  const [waitSeconds, setWaitSeconds] = useState(0)

  // After a rate limit the button stays off and counts down, and the message leaves when the wait is over.
  useEffect(() => {
    if (waitSeconds <= 0) {
      return
    }
    const timer = setTimeout(() => {
      setWaitSeconds((s) => s - 1)
      if (waitSeconds === 1) {
        setErrors(noErrors)
      }
    }, 1_000)
    return () => clearTimeout(timer)
  }, [waitSeconds])

  // When the server points at fields, the first one gets the focus, so it can be fixed without hunting for it.
  useEffect(() => {
    const first = fieldOrder.find((field) => errors.fields[field] !== undefined)
    if (first !== undefined) {
      const target = document.getElementById(`${id}-${first}-custom`) ?? document.getElementById(`${id}-${first}`)
      target?.focus()
    }
  }, [errors, id])

  // The success line goes away at once if its simulation stops, so it never sits next to an empty list.
  const seenRunning = useRef(false)
  const startedId = started?.id
  useEffect(() => {
    if (startedId === undefined) {
      seenRunning.current = false
      return
    }
    if (runningIds.includes(startedId)) {
      seenRunning.current = true
    } else if (seenRunning.current) {
      clearStarted()
    }
  }, [startedId, runningIds, clearStarted])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setErrors(noErrors)
    clearStarted()
    const duration = preset === custom ? customDuration.trim() : preset
    try {
      const simulation = await api.createSimulation({ service: service.trim(), mode, severity, intensity, duration }, token)
      showStarted(simulation)
      onStarted(simulation)
    } catch (err) {
      const apiError = toApiError(err)
      if (apiError.status === 400) {
        const split = splitInjectErrors(apiError.message)
        // A duration the server refuses can only be fixed in the custom field, so that field is opened.
        if (split.fields.duration !== undefined && preset !== custom) {
          setCustomDuration(preset)
          setPreset(custom)
        }
        setErrors(split)
      } else {
        setErrors({ fields: {}, general: [describeWriteError(apiError)] })
        setWaitSeconds(apiError.status === 429 ? (apiError.retryAfterSeconds ?? 0) : 0)
      }
    } finally {
      setSubmitting(false)
    }
  }

  // group gives every set of choices the attributes that tie it to its label and its helper line.
  const group = (field: InjectField) => ({
    id: `${id}-${field}`,
    name: field,
    labelledBy: `${id}-${field}-label`,
    describedBy: `${id}-${field}-message`,
    invalid: errors.fields[field] !== undefined,
  })

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="grid gap-x-6 gap-y-2 md:grid-cols-2 xl:grid-cols-1" aria-label="Run a drill">
      <div className="flex min-w-0 flex-col gap-2">
        <Field id={`${id}-service`} label="Service" forInput hint="Letters, digits, dots, dashes and underscores." error={errors.fields.service}>
          <Input
            id={`${id}-service`}
            name="service"
            aria-invalid={errors.fields.service !== undefined}
            aria-describedby={`${id}-service-message`}
            value={service}
            onChange={(e) => setService(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            placeholder="payments-api…"
            className="font-mono"
          />
        </Field>

        <Field id={`${id}-mode`} label="Failure mode" hint="The drawing shows how the failure builds over the drill." error={errors.fields.mode}>
          {/* Five modes do not fill a 2 by 2 grid, so the last card takes the full width and no cell is left empty. */}
          <ChoiceGroup
            {...group('mode')}
            value={mode}
            options={modeOptions}
            onChange={setMode}
            className="grid grid-cols-2 gap-2"
            optionClassName="flex min-w-0 flex-col gap-1 rounded-md border border-subtle bg-inset p-3 last:col-span-2 hover:border-strong has-[:checked]:border-strong has-[:checked]:bg-raised has-[:checked]:ring-2 has-[:checked]:ring-accent"
          >
            {(option) => (
              <>
                <ShapeGlyph mode={option.value} className="text-secondary" />
                <span className="text-label text-primary">{option.label}</span>
                <span className="text-meta text-tertiary">{failureModeDescriptions[option.value]}</span>
              </>
            )}
          </ChoiceGroup>
        </Field>
      </div>

      <div className="flex min-w-0 flex-col gap-2">
        <Field id={`${id}-severity`} label="Severity" hint="Sets how the incident is triaged." error={errors.fields.severity}>
          <Segmented {...group('severity')} value={severity} options={severityOptions} onChange={setSeverity} />
        </Field>

        <Field
          id={`${id}-intensity`}
          label="Intensity"
          forInput
          hint="Low is barely noticeable, and 100% is a total outage."
          error={errors.fields.intensity}
          aside={
            <output htmlFor={`${id}-intensity`} className="font-mono text-label text-primary">
              {Math.round(intensity * 100)}%
            </output>
          }
        >
          <Slider
            id={`${id}-intensity`}
            name="intensity"
            step={0.05}
            value={intensity}
            onValueChange={setIntensity}
            aria-invalid={errors.fields.intensity !== undefined}
            aria-describedby={`${id}-intensity-message`}
            aria-valuetext={`${Math.round(intensity * 100)} percent`}
          />
        </Field>

        <Field id={`${id}-duration`} label="Duration" hint="From 10s to 30m." error={errors.fields.duration}>
          <Chips {...group('duration')} value={preset} options={durationOptions} onChange={setPreset} />
          {preset === custom ? (
            <Input
              id={`${id}-duration-custom`}
              name="custom-duration"
              aria-label="Custom duration"
              aria-invalid={errors.fields.duration !== undefined}
              aria-describedby={`${id}-duration-message`}
              value={customDuration}
              onChange={(e) => setCustomDuration(e.target.value)}
              placeholder="90s…"
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          ) : null}
        </Field>

        <Button type="submit" disabled={submitting || waitSeconds > 0}>
          {submitting ? 'Starting…' : waitSeconds > 0 ? `Try again in ${waitSeconds}s` : 'Start drill'}
        </Button>

        {/* This line keeps its height whether or not it holds a message, so nothing below the button ever jumps. */}
        <div aria-live="polite" data-feedback="" className="min-h-12 text-meta">
          {errors.general.length > 0 ? (
            <div role="alert" className="flex items-start gap-1.5 text-breach">
              <SignalDot tone="breach" className="mt-1.25" />
              <div className="flex min-w-0 flex-col gap-1">
                {errors.general.map((line) => (
                  <p key={line} className="wrap-break-word">
                    {line}
                  </p>
                ))}
              </div>
            </div>
          ) : null}
          {started === null ? null : <p className="wrap-break-word text-secondary">{startedSentence(started.mode, started.service)}</p>}
        </div>
      </div>
    </form>
  )
}
