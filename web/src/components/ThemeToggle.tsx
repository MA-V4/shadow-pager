import { useState } from 'react'

import { writeStored } from '../lib/storage'
import { Button } from './ui/button'

const themeKey = 'shadow-pager.theme'

// ThemeToggle switches between the dark and the light look and remembers the choice.
export function ThemeToggle() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))

  function toggle() {
    const next = !dark
    document.documentElement.classList.toggle('dark', next)
    writeStored(themeKey, next ? 'dark' : 'light')
    setDark(next)
  }

  return (
    <Button variant="ghost" size="sm" onClick={toggle} aria-pressed={!dark} aria-label="Use light theme">
      {dark ? 'Light mode' : 'Dark mode'}
    </Button>
  )
}
