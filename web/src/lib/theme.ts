import { useCallback, useEffect, useSyncExternalStore } from 'react'

import { readStored, writeStored } from './storage'

const themeKey = 'shadow-pager.theme'

export type Theme = 'dark' | 'light'

// STORE

const listeners = new Set<() => void>()

function getTheme(): Theme {
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

// paint puts a theme on the page and tells the browser and every theme control about it.
function paint(theme: Theme): void {
  document.documentElement.classList.toggle('dark', theme === 'dark')
  // The browser bar on phones takes the page colour straight from the canvas token.
  const canvas = getComputedStyle(document.documentElement).getPropertyValue('--bg-canvas').trim()
  if (canvas !== '') {
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', canvas)
  }
  for (const listener of listeners) {
    listener()
  }
}

// setTheme switches the look of the whole page, remembers the choice, and tells every theme control about it.
export function setTheme(theme: Theme): void {
  paint(theme)
  writeStored(themeKey, theme)
}

// applyDefaultTheme uses the theme a page prefers, but only for a reader who has never picked one.
export function applyDefaultTheme(theme: Theme): void {
  const stored = readStored(themeKey)
  if (stored !== 'dark' && stored !== 'light' && getTheme() !== theme) {
    paint(theme)
  }
}

// HOOKS

// useTheme gives back the current theme and a way to change it, shared by every control on the page.
export function useTheme(): [Theme, (theme: Theme) => void] {
  const theme = useSyncExternalStore(subscribe, getTheme, (): Theme => 'dark')
  const update = useCallback((next: Theme) => setTheme(next), [])
  return [theme, update]
}

// useDefaultTheme lets a frame say which theme it starts in: dark for the control room, light for the public notice.
export function useDefaultTheme(theme: Theme): void {
  useEffect(() => {
    applyDefaultTheme(theme)
  }, [theme])
}
