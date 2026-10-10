import { useEffect, useId, useRef, useState } from 'react'

import { useAdminToken } from '../lib/adminToken'
import { useTheme, type Theme } from '../lib/theme'
import { cn } from '../lib/utils'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'

const themes: { value: Theme; label: string }[] = [
  { value: 'dark', label: 'Dark' },
  { value: 'light', label: 'Light' },
]

// SettingsPopover is the small panel in the header for things you set once: the admin token and the theme.
export function SettingsPopover() {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [token, setToken] = useAdminToken()
  const [theme, setTheme] = useTheme()
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)

  // While the panel is open, a click anywhere else or the Escape key closes it again.
  useEffect(() => {
    if (!open) {
      return
    }
    function onPointerDown(event: PointerEvent) {
      if (root.current && event.target instanceof Node && !root.current.contains(event.target)) {
        setOpen(false)
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setOpen(false)
        trigger.current?.focus()
      }
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div ref={root} className="relative">
      <Button
        ref={trigger}
        variant="ghost"
        size="sm"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-controls={`${id}-panel`}
        aria-haspopup="dialog"
      >
        Settings
        {token === '' ? null : (
          <span className="rounded-full border px-1.5 text-[11px] leading-4 text-muted-foreground">
            <span className="sr-only">, </span>token set
          </span>
        )}
      </Button>

      {open ? (
        <div
          id={`${id}-panel`}
          role="dialog"
          aria-label="Settings"
          className="absolute right-0 top-full z-30 mt-2 flex w-[min(22rem,calc(100vw-2rem))] flex-col gap-5 overscroll-contain rounded-lg border bg-card p-4 shadow-pop"
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`${id}-token`}>Admin token</Label>
            <Input
              id={`${id}-token`}
              name="admin-token"
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              autoComplete="off"
              spellCheck={false}
              placeholder="Paste the token…"
              aria-describedby={`${id}-token-hint`}
            />
            <p id={`${id}-token-hint`} className="text-xs text-muted-foreground">
              Only needed when the server was started with an admin token. It is saved in this browser and nowhere else.
            </p>
          </div>

          <fieldset className="flex flex-col gap-1.5">
            <legend className="mb-1.5 text-sm font-medium leading-none">Theme</legend>
            <div className="grid grid-cols-2 gap-1 rounded-md border bg-muted p-1">
              {themes.map((option) => (
                <label
                  key={option.value}
                  className={cn(
                    'flex h-8 cursor-pointer items-center justify-center rounded-[4px] text-sm font-medium text-muted-foreground transition-colors duration-150 hover:text-foreground has-[:focus-visible]:outline has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-ring',
                    theme === option.value && 'bg-card text-foreground shadow-card',
                  )}
                >
                  <input
                    type="radio"
                    name={`${id}-theme`}
                    value={option.value}
                    checked={theme === option.value}
                    onChange={() => setTheme(option.value)}
                    className="sr-only"
                  />
                  {option.label}
                </label>
              ))}
            </div>
          </fieldset>
        </div>
      ) : null}
    </div>
  )
}
