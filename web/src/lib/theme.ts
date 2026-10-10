import { useCallback, useSyncExternalStore } from 'react'

import { writeStored } from './storage'

const themeKey = 'shadow-pager.theme'

export type Theme = 'dark' | 'light'

// The colour behind the page in each theme, which the browser also paints around the page on phones.
const pageColour: Record<Theme, string> = { dark: '#0a0d12', light: '#f6f7f9' }

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

// setTheme switches the look of the whole page, remembers the choice, and tells every theme control about it.
export function setTheme(theme: Theme): void {
  document.documentElement.classList.toggle('dark', theme === 'dark')
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', pageColour[theme])
  writeStored(themeKey, theme)
  for (const listener of listeners) {
    listener()
  }
}

// HOOK

// useTheme gives back the current theme and a way to change it, shared by every control on the page.
export function useTheme(): [Theme, (theme: Theme) => void] {
  const theme = useSyncExternalStore(subscribe, getTheme, (): Theme => 'dark')
  const update = useCallback((next: Theme) => setTheme(next), [])
  return [theme, update]
}
