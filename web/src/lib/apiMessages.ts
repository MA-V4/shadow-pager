import type { ApiError } from '../api/client'

// FIELD ERRORS

export type InjectField = 'service' | 'mode' | 'severity' | 'intensity' | 'duration'

export interface InjectErrors {
  // Problems that belong to one field of the form.
  fields: Partial<Record<InjectField, string>>
  // Problems that do not belong to any single field.
  general: string[]
}

// The server names the field in every validation line, and these words tell us which one it means.
const fieldHints: [InjectField, RegExp][] = [
  ['service', /\bservice\b/i],
  ['mode', /\bfailure mode\b/i],
  ['severity', /\bseverity\b/i],
  ['intensity', /\bintensity\b/i],
  ['duration', /\bduration\b/i],
]

// cleanLine removes the technical prefixes the server puts in front of a validation message.
function cleanLine(line: string): string {
  const cleaned = line
    .replace(/^inject: /, '')
    .replace(/^invalid scenario: /, '')
    .replace(/^bad request: /, '')
    .trim()
  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1)
}

// splitInjectErrors sorts the lines of a validation error under the form fields they are about.
export function splitInjectErrors(message: string): InjectErrors {
  const result: InjectErrors = { fields: {}, general: [] }
  for (const raw of message.split('\n')) {
    const line = cleanLine(raw)
    if (line === '') {
      continue
    }
    const match = fieldHints.find(([, hint]) => hint.test(line))
    if (match && result.fields[match[0]] === undefined) {
      result.fields[match[0]] = line
    } else {
      result.general.push(line)
    }
  }
  return result
}

// WRITE ERRORS

// describeWriteError explains a failed inject or halt in words that say what to do next.
export function describeWriteError(err: ApiError): string {
  if (err.status === 401) {
    return 'This server only lets people with the admin token do that. Open Settings in the header, enter the token, and try again.'
  }
  if (err.status === 429) {
    return err.retryAfterSeconds === undefined
      ? `The server is busy: ${err.message}. Halt a running simulation or wait a little, then try again.`
      : `Too many requests. You can try again in ${err.retryAfterSeconds} ${err.retryAfterSeconds === 1 ? 'second' : 'seconds'}.`
  }
  if (err.status === 409) {
    return 'That simulation has already finished, so there is nothing left to halt.'
  }
  return err.message
}
