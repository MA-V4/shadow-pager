import { describe, expect, it } from 'vitest'

import { failureModes, severities } from '../api/types'
import { breachLabels, failureModeLabels, incidentStatusLabels, labelOf, serviceLevelLabels, severityLabels, simulationStateLabels } from './labels'
import { breachingMetrics, isMetricKey, metricDefs, metricKeys } from './metrics'

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

  it('formats axis ticks as plain round numbers, never shortened ones like 2.4K', () => {
    expect(metricDefs.latency_p99.formatTick(1000)).toBe('1,000')
    expect(metricDefs.memory.formatTick(2500)).toBe('2,500')
    expect(metricDefs.error_rate.formatTick(0.015)).toBe('1.5%')
    expect(metricDefs.error_rate.formatTick(0)).toBe('0%')
    expect(metricDefs.cpu.formatTick(100)).toBe('100%')
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
