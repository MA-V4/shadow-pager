import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '../api/client'
import { AppRoutes } from '../App'
import { resetAdminTokenForTests } from '../lib/adminToken'
import { StatusGlyph } from './StatusGlyph'

// HELPERS

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AppRoutes />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  window.localStorage.clear()
  resetAdminTokenForTests()
  document.documentElement.classList.add('dark')
})

afterEach(() => {
  vi.restoreAllMocks()
})

// TESTS

describe('settings in the header', () => {
  it('opens a panel with the admin token and the theme, and remembers the token', () => {
    renderAt('/nowhere')
    const button = screen.getByRole('button', { name: 'Settings' })
    expect(button.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByRole('dialog')).toBeNull()

    fireEvent.click(button)

    const panel = screen.getByRole('dialog', { name: 'Settings' })
    expect(button.getAttribute('aria-expanded')).toBe('true')
    const token = within(panel).getByLabelText('Admin token')
    expect(token.getAttribute('type')).toBe('password')

    fireEvent.change(token, { target: { value: ' s3cret ' } })

    expect(window.localStorage.getItem('shadow-pager.admin-token')).toBe('s3cret')
    // The button says a token is saved without showing it.
    expect(button.textContent).toContain('token set')
    expect(button.textContent).not.toContain('s3cret')
  })

  it('closes with the Escape key and gives the focus back to the button', () => {
    renderAt('/nowhere')
    const button = screen.getByRole('button', { name: 'Settings' })
    fireEvent.click(button)

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(button)
  })

  it('closes when something outside of it is pressed, and stays open for presses inside', () => {
    renderAt('/nowhere')
    fireEvent.click(screen.getByRole('button', { name: 'Settings' }))

    fireEvent.pointerDown(screen.getByLabelText('Admin token'))
    expect(screen.getByRole('dialog')).toBeTruthy()

    fireEvent.pointerDown(screen.getByText('This page does not exist'))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('switches the theme and remembers the choice', () => {
    renderAt('/nowhere')
    fireEvent.click(screen.getByRole('button', { name: 'Settings' }))
    const dark = screen.getByRole('radio', { name: 'Dark' }) as HTMLInputElement
    const light = screen.getByRole('radio', { name: 'Light' }) as HTMLInputElement
    expect(dark.checked).toBe(true)

    fireEvent.click(light)

    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(window.localStorage.getItem('shadow-pager.theme')).toBe('light')
    expect(light.checked).toBe(true)

    fireEvent.click(dark)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})

describe('layouts', () => {
  it('marks the current page in the navigation', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [] })
    renderAt('/incidents')

    const nav = screen.getByRole('navigation', { name: 'Main' })
    expect(within(nav).getByRole('link', { name: 'Incidents' }).getAttribute('aria-current')).toBe('page')
    expect(within(nav).getByRole('link', { name: 'Dashboard' }).getAttribute('aria-current')).toBeNull()
    expect(within(nav).getByRole('link', { name: 'Status' }).getAttribute('href')).toBe('/status')
    await screen.findByText('No incidents yet')
  })

  it('gives the public status page a plain frame with no settings and no admin token', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue({ status: 'operational', updated_at: '2026-01-02T15:10:00Z', services: [], incidents: [] })
    renderAt('/status')

    expect(await screen.findByText('All systems operational')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Settings' })).toBeNull()
    expect(screen.queryByRole('navigation', { name: 'Main' })).toBeNull()
    expect(screen.queryByLabelText('Admin token')).toBeNull()
    // The only control is the theme switch, and the only way out is a plain link.
    expect(screen.getAllByRole('button')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Use light theme' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Operator dashboard' }).getAttribute('href')).toBe('/')
    expect(screen.getByRole('link', { name: 'Skip to content' })).toBeTruthy()
  })
})

describe('StatusGlyph', () => {
  it('gives every meaning its own shape, so colour is never the only cue', () => {
    const { container } = render(
      <>
        <StatusGlyph tone="ok" />
        <StatusGlyph tone="warn" />
        <StatusGlyph tone="bad" />
        <StatusGlyph tone="info" />
        <StatusGlyph tone="neutral" />
      </>,
    )
    const glyphs = Array.from(container.querySelectorAll('[data-shape]'))
    const shapeClasses = glyphs.map((g) => g.className.replace(/text-\S+/g, '').replace(/\s+/g, ' ').trim())

    expect(new Set(shapeClasses).size).toBe(5)
    expect(glyphs.every((g) => g.getAttribute('aria-hidden') === 'true')).toBe(true)
  })
})
