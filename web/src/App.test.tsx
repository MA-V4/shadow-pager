import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { AppRoutes } from './App'

describe('AppRoutes', () => {
  it('shows the frame and a friendly message for a URL that no page claims', () => {
    render(
      <MemoryRouter initialEntries={['/this/does/not/exist']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'Shadow-Pager' })).toBeTruthy()
    expect(screen.getByRole('navigation', { name: 'Main' })).toBeTruthy()
    expect(screen.getByText('This page does not exist')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Go back to the start' }).getAttribute('href')).toBe('/')
  })

  it('lets a keyboard user jump straight to the content', () => {
    render(
      <MemoryRouter initialEntries={['/nowhere']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'Skip to content' }).getAttribute('href')).toBe('#main')
    expect(document.getElementById('main')).toBeTruthy()
  })
})
