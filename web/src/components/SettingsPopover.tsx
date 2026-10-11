import { useEffect, useId, useRef, useState } from 'react'

import { useAdminToken } from '../lib/adminToken'
import { useTheme, type Theme } from '../lib/theme'
import { Button } from './ui/button'
import type { Choice } from './ui/choice'
import { Segmented } from './ui/choice'
import { Input } from './ui/input'
import { Label } from './ui/label'

const themes: Choice<Theme>[] = [
  { value: 'dark', label: 'Dark' },
  { value: 'light', label: 'Light' },
]

// SettingsIcon is two sliders, drawn from plain lines and circles because the app ships no icon package.
function SettingsIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
      <path d="M2 4.5h6.5M13 4.5h1M2 11.5h1M7.5 11.5H14" />
      <circle cx="10.75" cy="4.5" r="1.75" />
      <circle cx="5.25" cy="11.5" r="1.75" />
    </svg>
  )
}

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
    <div ref={root} className="relative shrink-0">
      <Button
        ref={trigger}
        variant="ghost"
        size="icon"
        onClick={() => setOpen((o) => !o)}
        aria-label={token === '' ? 'Settings' : 'Settings, admin token saved'}
        title="Settings"
        aria-expanded={open}
        aria-controls={`${id}-panel`}
        aria-haspopup="dialog"
        className="relative"
      >
        <SettingsIcon />
        {/* A small mark says that a token is saved, without ever showing the token. */}
        {token === '' ? null : <span aria-hidden="true" data-token-mark="" className="absolute right-1.5 top-1.5 h-1.5 w-1.5 rounded-full bg-accent" />}
      </Button>

      {open ? (
        <div
          id={`${id}-panel`}
          role="dialog"
          aria-label="Settings"
          className="absolute right-0 top-full z-30 mt-2 flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-4 overscroll-contain rounded-lg border border-subtle bg-raised p-4 shadow-pop"
        >
          <div className="flex flex-col gap-2">
            <span id={`${id}-theme`} className="text-label text-primary">
              Theme
            </span>
            <Segmented name={`${id}-theme`} labelledBy={`${id}-theme`} value={theme} options={themes} onChange={setTheme} />
          </div>

          <div className="flex flex-col gap-2">
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
              className="font-mono"
            />
            <p id={`${id}-token-hint`} className="text-meta text-tertiary">
              Only needed when the server was started with an admin token. It is saved in this browser and nowhere else.
            </p>
          </div>
        </div>
      ) : null}
    </div>
  )
}
