import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { failureModes } from '../api/types'
import { FlatLine } from './FlatLine'
import { EmptyState } from './Notice'
import { ShapeGlyph } from './ShapeGlyph'
import { incidentRail, IncidentStatusTag, SeverityTag } from './Tags'
import { Card } from './ui/card'
import { Chips, Segmented } from './ui/choice'
import { Slider } from './ui/slider'
import { Value } from './Value'

describe('ShapeGlyph', () => {
  it('draws the curve of each failure mode, and the two spike modes share one drawing', () => {
    const { container } = render(
      <>
        {failureModes.map((mode) => (
          <ShapeGlyph key={mode} mode={mode} />
        ))}
      </>,
    )
    const glyphs = Array.from(container.querySelectorAll('svg'))
    expect(glyphs.map((g) => g.getAttribute('data-shape'))).toEqual(['spike', 'spike', 'leak', 'gradual', 'cliff'])
    expect(glyphs.every((g) => g.getAttribute('viewBox') === '0 0 40 16' && g.getAttribute('aria-hidden') === 'true')).toBe(true)
    const paths = glyphs.map((g) => g.querySelector('path')?.getAttribute('d'))
    expect(new Set(paths).size).toBe(4)
    expect(paths[0]).toBe(paths[1])
  })

  it('draws nothing for a failure mode it does not know', () => {
    const { container } = render(<ShapeGlyph mode="meteor" />)
    expect(container.querySelector('svg')).toBeNull()
  })
})

describe('Card', () => {
  it('has no rail when it is healthy, and a rail that only pulses when asked', () => {
    const { container } = render(
      <>
        <Card>calm</Card>
        <Card rail="warn">warned</Card>
        <Card rail="breach" pulse>
          loud
        </Card>
      </>,
    )
    const rails = Array.from(container.querySelectorAll('[data-rail]'))
    expect(rails.map((r) => r.getAttribute('data-rail'))).toEqual(['warn', 'breach'])
    expect(rails[0]?.className).not.toContain('animate-rail')
    // The pulse is only switched on for people who have not asked for less motion.
    expect(rails[1]?.className).toContain('motion-safe:animate-rail')
  })
})

describe('tags', () => {
  it('keeps colour for meaning and leaves finished things grey', () => {
    render(
      <>
        <SeverityTag severity="critical" />
        <SeverityTag severity="minor" />
        <IncidentStatusTag status="mitigated" />
        <IncidentStatusTag status="resolved" />
        <IncidentStatusTag status="something_new" />
      </>,
    )
    expect(screen.getByText('Critical').className).toContain('text-breach')
    expect(screen.getByText('Minor').className).toContain('text-tertiary')
    expect(screen.getByText('Mitigated').className).toContain('text-monitor')
    expect(screen.getByText('Resolved').className).toContain('text-tertiary')
    expect(screen.getByText('Something new').className).toContain('text-tertiary')
  })

  it('gives an incident card a rail until it is resolved', () => {
    expect(incidentRail('declared')).toBe('breach')
    expect(incidentRail('acknowledged')).toBe('warn')
    expect(incidentRail('mitigated')).toBe('monitor')
    expect(incidentRail('resolved')).toBeUndefined()
    expect(incidentRail('something_new')).toBeUndefined()
  })
})

describe('choices', () => {
  function Harness() {
    const [severity, setSeverity] = useState<'critical' | 'major'>('major')
    const [duration, setDuration] = useState('2m')
    return (
      <>
        <span id="sev">Severity</span>
        <Segmented
          name="severity"
          labelledBy="sev"
          value={severity}
          onChange={setSeverity}
          options={[
            { value: 'critical', label: 'Critical' },
            { value: 'major', label: 'Major' },
          ]}
        />
        <span id="dur">Duration</span>
        <Chips
          name="duration"
          labelledBy="dur"
          value={duration}
          onChange={setDuration}
          options={[
            { value: '30s', label: '30s' },
            { value: '2m', label: '2m' },
          ]}
        />
      </>
    )
  }

  it('are real radio buttons under a named group', () => {
    render(<Harness />)

    const severity = screen.getByRole('radiogroup', { name: 'Severity' })
    expect(severity.querySelectorAll('input[type="radio"]')).toHaveLength(2)
    const critical = screen.getByRole('radio', { name: 'Critical' }) as HTMLInputElement
    const major = screen.getByRole('radio', { name: 'Major' }) as HTMLInputElement
    expect(major.checked).toBe(true)

    fireEvent.click(critical)
    expect(critical.checked).toBe(true)
    expect(major.checked).toBe(false)

    fireEvent.click(screen.getByRole('radio', { name: '30s' }))
    expect((screen.getByRole('radio', { name: '30s' }) as HTMLInputElement).checked).toBe(true)
    // The two groups do not share a name, so picking in one leaves the other alone.
    expect(critical.checked).toBe(true)
  })
})

describe('Slider', () => {
  it('keeps the browser slider for the work and moves its own thumb with the value', () => {
    function Harness() {
      const [value, setValue] = useState(0.5)
      return <Slider aria-label="Intensity" step={0.05} value={value} onValueChange={setValue} />
    }
    const { container } = render(<Harness />)
    const input = screen.getByLabelText('Intensity') as HTMLInputElement
    const thumb = container.querySelector('span[aria-hidden="true"]') as HTMLElement
    expect(input.type).toBe('range')
    expect(thumb.style.left).toContain('0.5')

    fireEvent.change(input, { target: { value: '1' } })

    expect(input.value).toBe('1')
    expect(thumb.style.left).toContain('1 *')
  })
})

describe('small parts', () => {
  it('draws the unit of a value apart from its number', () => {
    const { container } = render(<Value text={`824${String.fromCharCode(160)}ms`} />)
    const unit = container.querySelector('span span')
    expect(container.textContent).toBe(`824${String.fromCharCode(160)}ms`)
    expect(unit?.textContent?.trim()).toBe('ms')
    expect(unit?.className).toContain('text-tertiary')
  })

  it('shows an empty state as one line, one instruction and a flat line, with no box around it', () => {
    const { container } = render(<EmptyState title="No simulations are running.">Start a drill.</EmptyState>)
    expect(screen.getByText('No simulations are running.')).toBeTruthy()
    expect(screen.getByText('Start a drill.')).toBeTruthy()
    expect(container.querySelector('[data-flatline]')).toBeTruthy()
    expect(container.innerHTML).not.toContain('border-dashed')
  })

  it('hides the moving mark of the flat line from people who asked for less motion', () => {
    const { container } = render(<FlatLine />)
    const mark = container.querySelectorAll('[data-flatline] span')[1]
    expect(mark?.className).toContain('motion-safe:animate-sweep')
    expect(mark?.className).toContain('motion-reduce:hidden')
  })
})
