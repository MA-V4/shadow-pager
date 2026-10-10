import { useEffect, useId, useState, type FormEvent } from 'react'

import { api, toApiError } from '../api/client'
import { failureModes, severities, type FailureMode, type Severity, type Simulation } from '../api/types'
import { describeWriteError, splitInjectErrors, type InjectErrors, type InjectField } from '../lib/apiMessages'
import { humanize } from '../lib/format'
import { Alert } from './ui/alert'
import { Button } from './ui/button'
import { Input, Select } from './ui/input'
import { Label } from './ui/label'

const noErrors: InjectErrors = { fields: {}, general: [] }

// FieldError is the red line under a field, tied to the field so screen readers read it out.
function FieldError({ id, message }: { id: string; message: string | undefined }) {
  return message === undefined ? null : (
    <p id={id} className="text-xs text-bad">
      {message}
    </p>
  )
}

interface InjectFormProps {
  // The admin token to send along, or an empty text when there is none.
  token: string
  // Called with the new simulation after the server accepted it.
  onInjected: (simulation: Simulation) => void
}

// InjectForm starts a new pretend failure and shows the server's complaints right next to the fields they are about.
export function InjectForm({ token, onInjected }: InjectFormProps) {
  const id = useId()
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

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setErrors(noErrors)
    setSuccess(null)
    try {
      const simulation = await api.createSimulation({ service: service.trim(), mode, severity, intensity, duration: duration.trim() }, token)
      setSuccess(`Started ${humanize(simulation.mode).toLowerCase()} on ${simulation.service}. An incident is declared if it breaks its SLO.`)
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

  // describedBy ties a field to its error line only while that line is on screen.
  const describedBy = (field: InjectField) => (errors.fields[field] === undefined ? undefined : `${id}-${field}-error`)

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-4" aria-label="Inject a failure">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`${id}-service`}>Service</Label>
          <Input
            id={`${id}-service`}
            value={service}
            onChange={(e) => setService(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            aria-invalid={errors.fields.service !== undefined}
            aria-describedby={describedBy('service')}
          />
          <FieldError id={`${id}-service-error`} message={errors.fields.service} />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`${id}-mode`}>Failure mode</Label>
          <Select
            id={`${id}-mode`}
            value={mode}
            onChange={(e) => setMode(e.target.value as FailureMode)}
            aria-invalid={errors.fields.mode !== undefined}
            aria-describedby={describedBy('mode')}
          >
            {failureModes.map((m) => (
              <option key={m} value={m}>
                {humanize(m)}
              </option>
            ))}
          </Select>
          <FieldError id={`${id}-mode-error`} message={errors.fields.mode} />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`${id}-severity`}>Severity</Label>
          <Select
            id={`${id}-severity`}
            value={severity}
            onChange={(e) => setSeverity(e.target.value as Severity)}
            aria-invalid={errors.fields.severity !== undefined}
            aria-describedby={describedBy('severity')}
          >
            {severities.map((s) => (
              <option key={s} value={s}>
                {humanize(s)}
              </option>
            ))}
          </Select>
          <FieldError id={`${id}-severity-error`} message={errors.fields.severity} />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`${id}-duration`}>Duration</Label>
          <Input
            id={`${id}-duration`}
            value={duration}
            onChange={(e) => setDuration(e.target.value)}
            placeholder="2m"
            autoComplete="off"
            spellCheck={false}
            aria-invalid={errors.fields.duration !== undefined}
            aria-describedby={`${id}-duration-hint${errors.fields.duration === undefined ? '' : ` ${id}-duration-error`}`}
          />
          <p id={`${id}-duration-hint`} className="text-xs text-muted-foreground">
            From 10s to 30m, written like 90s or 2m.
          </p>
          <FieldError id={`${id}-duration-error`} message={errors.fields.duration} />
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <div className="flex items-baseline justify-between">
          <Label htmlFor={`${id}-intensity`}>Intensity</Label>
          <output htmlFor={`${id}-intensity`} className="font-mono text-sm tabular-nums">
            {Math.round(intensity * 100)}%
          </output>
        </div>
        <input
          id={`${id}-intensity`}
          type="range"
          min={0}
          max={1}
          step={0.05}
          value={intensity}
          onChange={(e) => setIntensity(Number(e.target.value))}
          className="h-10 w-full accent-primary"
          aria-valuetext={`${Math.round(intensity * 100)} percent`}
          aria-invalid={errors.fields.intensity !== undefined}
          aria-describedby={describedBy('intensity')}
        />
        <p className="text-xs text-muted-foreground">Low values are barely noticeable, and 100% is a total outage.</p>
        <FieldError id={`${id}-intensity-error`} message={errors.fields.intensity} />
      </div>

      {errors.general.length > 0 ? (
        <Alert tone="bad">
          {errors.general.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </Alert>
      ) : null}
      {success !== null ? <Alert tone="ok">{success}</Alert> : null}

      <div>
        <Button type="submit" disabled={submitting || waitSeconds > 0}>
          {submitting ? 'Injecting' : waitSeconds > 0 ? `Try again in ${waitSeconds}s` : 'Inject failure'}
        </Button>
      </div>
    </form>
  )
}
