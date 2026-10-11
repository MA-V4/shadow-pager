import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '../api/client'
import { AppRoutes } from '../App'
import { resetAdminTokenForTests } from '../lib/adminToken'
import { setConnection } from '../lib/connection'

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
  act(() => setConnection('idle'))
  vi.restoreAllMocks()
})

// TESTS

describe('settings in the header', () => {
  it('opens a panel with the theme and the admin token, and remembers the token', () => {
    renderAt('/nowhere')
    const button = screen.getByRole('button', { name: 'Settings' })
    expect(button.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByRole('dialog')).toBeNull()

    fireEvent.click(button)

    const panel = screen.getByRole('dialog', { name: 'Settings' })
    expect(button.getAttribute('aria-expanded')).toBe('true')
    const token = within(panel).getByLabelText('Admin token')
    expect(token.getAttribute('type')).toBe('password')
    expect(button.querySelector('[data-token-mark]')).toBeNull()

    fireEvent.change(token, { target: { value: ' s3cret ' } })

    expect(window.localStorage.getItem('shadow-pager.admin-token')).toBe('s3cret')
    // The button says a token is saved without showing it.
    expect(button.getAttribute('aria-label')).toBe('Settings, admin token saved')
    expect(button.querySelector('[data-token-mark]')).toBeTruthy()
    expect(button.outerHTML).not.toContain('s3cret')
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

    fireEvent.pointerDown(screen.getByText('This page does not exist.'))
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

describe('connection indicator', () => {
  it('lives in the header and says how the link to the server is doing', () => {
    renderAt('/nowhere')
    const header = screen.getByRole('banner')
    // A page that does not talk to the server shows no indicator at all.
    expect(within(header).queryByRole('status')).toBeNull()

    act(() => setConnection('live'))
    expect(within(header).getByRole('status', { name: 'Connection: Live' }).textContent).toBe('Live')

    act(() => setConnection('reconnecting'))
    expect(within(header).getByRole('status', { name: 'Connection: Reconnecting' })).toBeTruthy()

    act(() => setConnection('waking'))
    const waking = within(header).getByRole('status', { name: 'Connection: Waking server' })
    expect(waking.querySelector('[data-tone="monitor"]')).toBeTruthy()
  })

  it('is fed by the page on screen, and only the header shows it', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [] })
    renderAt('/incidents')

    await screen.findByText('No incidents yet.')

    expect(within(screen.getByRole('banner')).getByRole('status', { name: 'Connection: Live' })).toBeTruthy()
    expect(within(screen.getByRole('main')).queryByText('Live')).toBeNull()
  })
})

describe('layouts', () => {
  it('marks the current page in the one navigation that serves as tabs and as the bottom bar', async () => {
    vi.spyOn(api, 'listIncidents').mockResolvedValue({ incidents: [] })
    renderAt('/incidents')

    expect(screen.getAllByRole('navigation')).toHaveLength(1)
    const nav = screen.getByRole('navigation', { name: 'Main' })
    expect(within(nav).getByRole('link', { name: 'Incidents' }).getAttribute('aria-current')).toBe('page')
    expect(within(nav).getByRole('link', { name: 'Dashboard' }).getAttribute('aria-current')).toBeNull()
    expect(within(nav).getByRole('link', { name: 'Status' }).getAttribute('href')).toBe('/status')
    // On small screens it is pinned to the bottom, and from 768px up it sits in the header.
    expect(nav.className).toContain('fixed')
    expect(nav.className).toContain('bottom-0')
    expect(nav.className).toContain('md:static')
    await screen.findByText('No incidents yet.')
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
    expect(screen.getByRole('button', { name: 'Dark mode' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Operator dashboard' }).getAttribute('href')).toBe('/')
    expect(screen.getByRole('link', { name: 'Skip to content' })).toBeTruthy()
  })
})

describe('default themes', () => {
  it('shows the status page light and the operator pages dark to a reader who never picked a theme', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue({ status: 'operational', updated_at: '2026-01-02T15:10:00Z', services: [], incidents: [] })

    const status = renderAt('/status')
    await screen.findByText('All systems operational')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    // A default is not a choice, so nothing is saved.
    expect(window.localStorage.getItem('shadow-pager.theme')).toBeNull()
    status.unmount()

    renderAt('/nowhere')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('keeps the theme a reader picked, on every page', async () => {
    vi.spyOn(api, 'getStatus').mockResolvedValue({ status: 'operational', updated_at: '2026-01-02T15:10:00Z', services: [], incidents: [] })
    window.localStorage.setItem('shadow-pager.theme', 'dark')

    const status = renderAt('/status')
    await screen.findByText('All systems operational')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    status.unmount()

    window.localStorage.setItem('shadow-pager.theme', 'light')
    document.documentElement.classList.remove('dark')
    renderAt('/nowhere')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
