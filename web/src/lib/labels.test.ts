import { describe, expect, it } from 'vitest'

import { failureModes, severities } from '../api/types'
import {
  breachLabels,
  failureModeDescriptions,
  failureModeLabels,
  failureModePhrases,
  incidentStatusLabels,
  inSentence,
  labelOf,
  serviceLevelLabels,
  severityLabels,
  simulationStateLabels,
  startedSentence,
} from './labels'
import { breachingMetrics, isMetricKey, metricDefs, metricKeys, splitUnit, tickLabel } from './metrics'

describe('labels', () => {
  it('names every failure mode properly, with acronyms in capitals', () => {
    expect(failureModeLabels).toEqual({
      latency_spike: 'Latency spike',
      error_rate_surge: 'Error rate surge',
      memory_leak: 'Memory leak',
      cpu_saturation: 'CPU saturation',
      db_pool_exhaustion: 'DB pool exhaustion',
    })
  })

  it('has a label for every failure mode and severity the API knows', () => {
    for (const mode of failureModes) {
      expect(failureModeLabels[mode]).toBeTruthy()
    }
    for (const severity of severities) {
      expect(severityLabels[severity]).toBeTruthy()
    }
  })

  it('names statuses, states and levels', () => {
    expect(incidentStatusLabels.acknowledged).toBe('Acknowledged')
    expect(simulationStateLabels.resolved).toBe('Finished')
    expect(serviceLevelLabels.major_outage).toBe('Major outage')
    expect(breachLabels).toMatchObject({ latency_p99: 'Latency p99', error_rate: 'Error rate', cpu: 'CPU', memory: 'Memory' })
  })

  it.each([
    [failureModeLabels, 'cpu_saturation', 'CPU saturation'],
    [breachLabels, 'cpu', 'CPU'],
    [failureModeLabels, 'disk_full', 'Disk full'],
    [breachLabels, '', 'Unknown'],
    [breachLabels, '___', 'Unknown'],
  ])('looks a word up and tidies an unknown one', (labels, value, want) => {
    expect(labelOf(labels, value)).toBe(want)
  })
})

describe('sentence casing', () => {
  it('keeps labels in sentence case, with a capital only at the start and on acronyms', () => {
    for (const label of Object.values(failureModeLabels)) {
      const [first, ...rest] = label.split(' ')
      expect(first?.charAt(0)).toBe(first?.charAt(0).toUpperCase())
      for (const word of rest) {
        expect(word).toBe(word.toLowerCase())
      }
    }
  })

  it.each([
    ['Latency spike', 'latency spike'],
    ['Error rate surge', 'error rate surge'],
    ['Memory leak', 'memory leak'],
    ['CPU saturation', 'CPU saturation'],
    ['DB pool exhaustion', 'DB pool exhaustion'],
    ['A', 'a'],
    ['', ''],
  ])('writes %s as %s inside a sentence', (label, want) => {
    expect(inSentence(label)).toBe(want)
  })

  it.each([
    ['latency_spike', 'Started a latency spike on payments-api.'],
    ['error_rate_surge', 'Started an error rate surge on payments-api.'],
    ['memory_leak', 'Started a memory leak on payments-api.'],
    ['cpu_saturation', 'Started CPU saturation on payments-api.'],
    ['db_pool_exhaustion', 'Started DB pool exhaustion on payments-api.'],
    ['disk_full', 'Started disk full on payments-api.'],
  ])('says that %s started without a capital in the middle', (mode, want) => {
    expect(startedSentence(mode, 'payments-api')).toBe(want)
  })

  it('has a phrase and a one line description for every failure mode', () => {
    for (const mode of failureModes) {
      expect(failureModePhrases[mode]).toContain(inSentence(failureModeLabels[mode]))
      expect(failureModeDescriptions[mode].endsWith('.')).toBe(true)
      expect(failureModeDescriptions[mode].length).toBeLessThanOrEqual(40)
    }
  })
})

describe('metrics', () => {
  it('uses the same keys the API uses in its list of breaches', () => {
    expect(metricKeys).toEqual(['latency_p99', 'error_rate', 'cpu', 'memory'])
    expect(isMetricKey('cpu')).toBe(true)
    expect(isMetricKey('disk')).toBe(false)
  })

  it('formats values with their unit and a space that never breaks', () => {
    expect(metricDefs.latency_p99.format(950.126)).toBe('950.13 ms')
    expect(metricDefs.error_rate.format(0.0123)).toBe('1.23%')
    expect(metricDefs.cpu.format(40)).toBe('40%')
    expect(metricDefs.memory.format(2048)).toBe('2,048 MB')
  })

  it('cuts a value into its number and its unit', () => {
    expect(splitUnit(metricDefs.latency_p99.format(824))).toEqual(['824', `${String.fromCharCode(160)}ms`])
    expect(splitUnit('1.23%')).toEqual(['1.23', '%'])
    expect(splitUnit('2,048')).toEqual(['2,048', ''])
    expect(splitUnit('-')).toEqual(['-', ''])
  })

  it('labels axis ticks with plain round numbers and puts the unit on the top one only', () => {
    expect(tickLabel(metricDefs.latency_p99, 500, false)).toBe('500')
    expect(tickLabel(metricDefs.latency_p99, 1000, false)).toBe('1,000')
    expect(tickLabel(metricDefs.latency_p99, 1000, true)).toBe(`1,000${String.fromCharCode(160)}ms`)
    expect(tickLabel(metricDefs.memory, 3000, true)).toBe(`3,000${String.fromCharCode(160)}MB`)
    expect(tickLabel(metricDefs.error_rate, 0.005, false)).toBe('0.5')
    expect(tickLabel(metricDefs.error_rate, 0.015, true)).toBe('1.5%')
    expect(tickLabel(metricDefs.cpu, 0, false)).toBe('0')
    expect(tickLabel(metricDefs.cpu, 150, true)).toBe('150%')
  })

  it('reads the breaching metrics off the newest sample in a fixed order', () => {
    const sample = {
      simulation_id: 's',
      service: 'a',
      mode: 'latency_spike' as const,
      timestamp: '2026-01-02T15:04:05Z',
      latency_p99_ms: 950,
      error_rate: 0.2,
      cpu_percent: 30,
      memory_mb: 512,
      healthy: false,
      breaches: ['error_rate', 'latency_p99', 'something_new'],
    }
    expect(breachingMetrics(sample)).toEqual(['latency_p99', 'error_rate'])
    expect(breachingMetrics({ ...sample, healthy: true, breaches: [] })).toEqual([])
    expect(breachingMetrics(undefined)).toEqual([])
  })
})
