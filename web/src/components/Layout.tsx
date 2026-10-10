import type { ReactNode } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'

import type { PageRoute } from '../lib/routes'
import { cn } from '../lib/utils'
import { SettingsPopover } from './SettingsPopover'
import { ThemeToggle } from './ThemeToggle'

// SHARED PARTS

// Brand is the mark and the name, and it always leads back to the start.
function Brand({ to }: { to: string }) {
  return (
    <Link to={to} translate="no" className="flex shrink-0 items-center gap-2 rounded-md font-semibold tracking-tight">
      <svg aria-hidden="true" viewBox="0 0 32 32" className="h-6 w-6 text-brand">
        <path
          d="M5 17h5l3-8 5 15 3-9h6"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      Shadow-Pager
    </Link>
  )
}

// SkipLink lets a keyboard user jump over the header straight to the content.
function SkipLink() {
  return (
    <a
      href="#main"
      className="sr-only focus:not-sr-only focus:absolute focus:left-3 focus:top-3 focus:z-50 focus:rounded-md focus:border focus:bg-card focus:px-3 focus:py-2"
    >
      Skip to content
    </a>
  )
}

// Frame is the page skeleton both layouts share: a sticky top bar, the content, and a quiet footer.
function Frame({ header, footer, width, children }: { header: ReactNode; footer: ReactNode; width: string; children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col">
      <SkipLink />
      <header className="sticky top-0 z-20 border-b bg-background/85 backdrop-blur supports-[backdrop-filter]:bg-background/70">
        <div className={cn('mx-auto flex h-14 w-full items-center gap-4 px-4 sm:px-6', width)}>{header}</div>
      </header>
      <main id="main" className={cn('mx-auto w-full flex-1 px-4 py-6 sm:px-6 sm:py-8', width)}>
        {children}
      </main>
      <footer className="border-t">
        <div
          className={cn(
            'mx-auto flex w-full flex-wrap items-center justify-between gap-x-6 gap-y-1 px-4 py-4 pb-[max(1rem,env(safe-area-inset-bottom))] text-xs text-muted-foreground sm:px-6',
            width,
          )}
        >
          {footer}
        </div>
      </footer>
    </div>
  )
}

// OPERATOR LAYOUT

// Layout is the frame around the operator pages: the name, the navigation and the settings.
export function Layout({ links }: { links: PageRoute[] }) {
  return (
    <Frame
      width="max-w-[1440px]"
      header={
        <>
          <Brand to="/" />
          <nav aria-label="Main" className="flex min-w-0 flex-1 gap-0.5 overflow-x-auto">
            {links.map((link) => (
              <NavLink
                key={link.path}
                to={link.path}
                end={link.path === '/'}
                className={({ isActive }) =>
                  cn(
                    'rounded-md px-2.5 py-1.5 text-sm font-medium whitespace-nowrap text-muted-foreground transition-colors duration-150 hover:bg-muted hover:text-foreground',
                    isActive && 'bg-muted text-foreground',
                  )
                }
              >
                {link.label}
              </NavLink>
            ))}
          </nav>
          <SettingsPopover />
        </>
      }
      footer={<p>Shadow-Pager simulates failures for practice. Nothing shown here is a real outage.</p>}
    >
      <Outlet />
    </Frame>
  )
}

// PUBLIC LAYOUT

// PublicLayout is the plain frame of the public status page, with nothing on it that changes anything.
export function PublicLayout() {
  return (
    <Frame
      width="max-w-3xl"
      header={
        <>
          <Brand to="/status" />
          <span className="text-sm text-muted-foreground">Status</span>
          <div className="ml-auto">
            <ThemeToggle />
          </div>
        </>
      }
      footer={
        <>
          <p>Every service on this page is simulated. Nothing shown here is a real outage.</p>
          <Link to="/" className="rounded-sm underline underline-offset-4 hover:text-foreground">
            Operator dashboard
          </Link>
        </>
      }
    >
      <Outlet />
    </Frame>
  )
}
