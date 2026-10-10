import { useEffect, useId, useState, type FormEvent, type ReactNode } from 'react'

import { api, toApiError } from '../api/client'
import { failureModes, severities, type FailureMode, type Severity, type Simulation } from '../api/types'
import { useAdminToken } from '../lib/adminToken'
import { describeWriteError, splitInjectErrors, type InjectErrors, type InjectField } from '../lib/apiMessages'
import { failureModeLabels, severityLabels } from '../lib/labels'
import { cn } from '../lib/utils'
import { StatusGlyph } from './StatusGlyph'
import { Button } from './ui/button'
import { Input, Select } from './ui/input'
import { Label } from './ui/label'

const noErrors: InjectErrors = { fields: {}, general: [] }

// The order of the fields on screen, used to find the first one with a problem.
const fieldOrder: InjectField[] = ['service', 'mode', 'severity', 'duration', 'intensity']

// FIELD

interface FieldProps {
  id: string
  label: string
  hint: string
  error: string | undefined
  className?: string
  children: ReactNode
}

// Field is a label, a control and one line below it that shows a hint or an error, so the form keeps its height.
function Field({ id, label, hint, error, className, children }: FieldProps) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1.5', className)}>
      <Label htmlFor={id}>{label}</Label>
      {children}
      <p id={`${id}-message`} className={cn('min-h-8 text-xs leading-4', error === undefined ? 'text-muted-foreground' : 'text-bad')}>
        {error ?? hint}
      </p>
    </div>
  )
}

// FORM

interface InjectFormProps {
  // Called with the new simulation after the server accepted it.
  onInjected: (simulation: Simulation) => void
}

// InjectForm starts a new pretend failure and shows the server's complaints right next to the fields they are about.
export function InjectForm({ onInjected }: InjectFormProps) {
  const id = useId()
  const [token] = useAdminToken()
  const [service, setService] = useState('payments-api')
  const [mode, setMode] = useState<FailureMode>('latency_spike')
  const [severity, setSeverity] = useState<Severity>('major')
  const [intensity, setIntensity] = useState(0.8)
  const [duration, setDuration] = useState('2m')

  const [submitting, setSubmitting] = useState(false)
  const [errors, setErrors] = useState<InjectErrors>(noErrors)
  const [success, setSuccess] = useState<string | null>(null)
  const [waitSeconds, setWaitSeconds] = useState(0)

  // After a rate limit the button stays off and counts down, so nobody has to guess when to try again.
  useEffect(() => {
    if (waitSeconds <= 0) {
      return
    }
    const timer = setTimeout(() => setWaitSeconds((s) => s - 1), 1_000)
    return () => clearTimeout(timer)
  }, [waitSeconds])

  // When the server points at fields, the first one gets the focus, so it can be fixed without hunting for it.
  useEffect(() => {
    const first = fieldOrder.find((field) => errors.fields[field] !== undefined)
    if (first !== undefined) {
      document.getElementById(`${id}-${first}`)?.focus()
    }
  }, [errors, id])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setErrors(noErrors)
    setSuccess(null)
    try {
      const simulation = await api.createSimulation({ service: service.trim(), mode, severity, intensity, duration: duration.trim() }, token)
      setSuccess(`Started ${failureModeLabels[simulation.mode] ?? simulation.mode} on ${simulation.service}.`)
      onInjected(simulation)
    } catch (err) {
      const apiError = toApiError(err)
      if (apiError.status === 400) {
        setErrors(splitInjectErrors(apiError.message))
      } else {
        setErrors({ fields: {}, general: [describeWriteError(apiError)] })
        setWaitSeconds(apiError.status === 429 ? (apiError.retryAfterSeconds ?? 0) : 0)
      }
    } finally {
      setSubmitting(false)
    }
  }

  // control gives every input the attributes that tie it to its label and its message line.
  const control = (field: InjectField) => ({
    id: `${id}-${field}`,
    name: field,
    'aria-invalid': errors.fields[field] !== undefined,
    'aria-describedby': `${id}-${field}-message`,
  })

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-2" aria-label="Inject a failure">
      <Field id={`${id}-service`} label="Service" hint="Letters, digits, dots, dashes and underscores." error={errors.fields.service}>
        <Input
          {...control('service')}
          value={service}
          onChange={(e) => setService(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          placeholder="payments-api…"
          className="font-mono"
        />
      </Field>

      <Field id={`${id}-mode`} label="Failure mode" hint="Decides which metrics move and how fast." error={errors.fields.mode}>
        <Select {...control('mode')} value={mode} onChange={(e) => setMode(e.target.value as FailureMode)}>
          {failureModes.map((m) => (
            <option key={m} value={m}>
              {failureModeLabels[m]}
            </option>
          ))}
        </Select>
      </Field>

      <div className="grid grid-cols-2 gap-3">
        <Field id={`${id}-severity`} label="Severity" hint="Sets how the incident is triaged." error={errors.fields.severity}>
          <Select {...control('severity')} value={severity} onChange={(e) => setSeverity(e.target.value as Severity)}>
            {severities.map((s) => (
              <option key={s} value={s}>
                {severityLabels[s]}
              </option>
            ))}
          </Select>
        </Field>

        <Field id={`${id}-duration`} label="Duration" hint="From 10s to 30m, like 90s or 2m." error={errors.fields.duration}>
          <Input
            {...control('duration')}
            value={duration}
            onChange={(e) => setDuration(e.target.value)}
            placeholder="2m…"
            autoComplete="off"
            spellCheck={false}
            className="font-mono"
          />
        </Field>
      </div>

      <div className="flex min-w-0 flex-col gap-1.5">
        <div className="flex items-baseline justify-between">
          <Label htmlFor={`${id}-intensity`}>Intensity</Label>
          <output htmlFor={`${id}-intensity`} className="font-mono text-sm tabular-nums">
            {Math.round(intensity * 100)}%
          </output>
        </div>
        <input
          {...control('intensity')}
          type="range"
          min={0}
          max={1}
          step={0.05}
          value={intensity}
          onChange={(e) => setIntensity(Number(e.target.value))}
          className="h-8 w-full accent-foreground"
          aria-valuetext={`${Math.round(intensity * 100)} percent`}
        />
        <p
          id={`${id}-intensity-message`}
          className={cn('min-h-8 text-xs leading-4', errors.fields.intensity === undefined ? 'text-muted-foreground' : 'text-bad')}
        >
          {errors.fields.intensity ?? 'Low values are barely noticeable, and 100% is a total outage.'}
        </p>
      </div>

      <Button type="submit" disabled={submitting || waitSeconds > 0}>
        {submitting ? 'Injecting…' : waitSeconds > 0 ? `Try again in ${waitSeconds}s` : 'Inject failure'}
      </Button>

      {/* This area keeps its height whether or not it holds a message, so the page below the form never jumps. */}
      <div aria-live="polite" className="min-h-16 text-sm">
        {errors.general.length > 0 ? (
          <div role="alert" className="flex items-start gap-2 text-bad">
            <StatusGlyph tone="bad" className="mt-1.5" />
            <div className="flex min-w-0 flex-col gap-1">
              {errors.general.map((line) => (
                <p key={line} className="break-words">
                  {line}
                </p>
              ))}
            </div>
          </div>
        ) : null}
        {success !== null ? (
          <p className="flex items-start gap-2 text-muted-foreground">
            <StatusGlyph tone="ok" className="mt-1.5" />
            <span className="min-w-0 break-words">{success} An incident is declared if it breaks its SLO.</span>
          </p>
        ) : null}
      </div>
    </form>
  )
}
