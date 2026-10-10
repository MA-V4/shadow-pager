import { useTheme } from '../lib/theme'
import { Button } from './ui/button'

// ThemeToggle switches between the dark and the light look and remembers the choice.
export function ThemeToggle() {
  const [theme, setTheme] = useTheme()
  const dark = theme === 'dark'

  return (
    <Button variant="ghost" size="sm" onClick={() => setTheme(dark ? 'light' : 'dark')} aria-pressed={!dark} aria-label="Use light theme">
      {dark ? 'Light mode' : 'Dark mode'}
    </Button>
  )
}
