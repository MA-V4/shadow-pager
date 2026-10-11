import type { ReactNode } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'

import type { PageRoute } from '../lib/routes'
import { useDefaultTheme } from '../lib/theme'
import { cn } from '../lib/utils'
import { ConnectionIndicator } from './ConnectionIndicator'
import { SettingsPopover } from './SettingsPopover'
import { ThemeToggle } from './ThemeToggle'

// SHARED PARTS

// Brand is the mark and the name, and it always leads back to the start.
function Brand({ to }: { to: string }) {
  return (
    <Link to={to} translate="no" className="flex shrink-0 items-center gap-2 rounded-sm text-body font-semibold text-primary">
      <svg aria-hidden="true" viewBox="0 0 32 32" className="h-5 w-5">
        <path d="M5 17h5l3-8 5 15 3-9h6" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
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
      className="sr-only focus:not-sr-only focus:absolute focus:left-3 focus:top-2 focus:z-40 focus:rounded-md focus:border focus:border-strong focus:bg-raised focus:px-3 focus:py-2 focus:text-label"
    >
      Skip to content
    </a>
  )
}

interface FrameProps {
  header: ReactNode
  footer: ReactNode
  width: string
  // True when a tab bar sits at the bottom of small screens, so the content must stay clear of it.
  tabBar?: boolean
  children: ReactNode
}

// Frame is the page skeleton both layouts share: a sticky 48px top bar, the content, and a quiet footer.
function Frame({ header, footer, width, tabBar = false, children }: FrameProps) {
  return (
    <div className="flex min-h-dvh flex-col">
      <SkipLink />
      <header className="sticky top-0 z-20 border-b border-subtle bg-canvas">
        <div className={cn('mx-auto flex h-12 w-full items-center gap-4 px-4 md:gap-6 md:px-6', width)}>{header}</div>
      </header>
      <main id="main" className={cn('mx-auto w-full flex-1 px-4 py-6 md:px-6 md:py-8', width)}>
        {children}
      </main>
      <footer className="border-t border-subtle">
        <div
          className={cn(
            'mx-auto flex w-full flex-wrap items-center justify-between gap-x-6 gap-y-1 px-4 pt-4 text-meta text-tertiary md:px-6',
            tabBar ? 'pb-[calc(4.5rem+env(safe-area-inset-bottom))] md:pb-4' : 'pb-[max(1rem,env(safe-area-inset-bottom))]',
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

// Layout is the frame around the operator pages: the name, the navigation, the connection and the settings.
export function Layout({ links }: { links: PageRoute[] }) {
  useDefaultTheme('dark')
  return (
    <Frame
      width="max-w-[1440px]"
      tabBar
      header={
        <>
          <Brand to="/" />
          {/* The same navigation is a row of tabs in the header on wide screens and a tab bar at the bottom of small ones. */}
          <nav
            aria-label="Main"
            className="fixed inset-x-0 bottom-0 z-20 flex border-t border-subtle bg-canvas pb-[env(safe-area-inset-bottom)] md:static md:z-auto md:h-12 md:border-t-0 md:pb-0"
          >
            {links.map((link) => (
              <NavLink
                key={link.path}
                to={link.path}
                end={link.path === '/'}
                className="relative flex h-14 flex-1 items-center justify-center px-3 text-label whitespace-nowrap text-secondary transition-colors duration-150 ease-out after:absolute after:inset-x-3 after:top-0 after:h-0.5 after:bg-accent after:opacity-0 hover:text-primary aria-[current=page]:text-primary aria-[current=page]:after:opacity-100 md:h-12 md:flex-none md:after:top-auto md:after:bottom-0"
              >
                {link.label}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <ConnectionIndicator />
            <SettingsPopover />
          </div>
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
  useDefaultTheme('light')
  return (
    <Frame
      width="max-w-[760px]"
      header={
        <>
          <Brand to="/status" />
          <span className="text-label text-secondary">Status</span>
          <div className="ml-auto">
            <ThemeToggle />
          </div>
        </>
      }
      footer={
        <>
          <p>Every service on this page is simulated. Nothing shown here is a real outage.</p>
          <Link to="/" className="rounded-sm text-secondary underline underline-offset-4 hover:text-primary">
            Operator dashboard
          </Link>
        </>
      }
    >
      <Outlet />
    </Frame>
  )
}
