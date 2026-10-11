import { BrowserRouter, Link, Route, Routes } from 'react-router-dom'

import { Layout, PublicLayout } from './components/Layout'
import { EmptyState } from './components/Notice'
import { sortedPages, type PageModule } from './lib/routes'

// PAGES

// Every file in src/pages that ends in Page.tsx is picked up here, so adding a page never means editing this file.
const pages = sortedPages(import.meta.glob<PageModule>('./pages/*Page.tsx', { eager: true }))

// NotFound is shown for any URL that no page claims.
function NotFound() {
  return (
    <EmptyState title="This page does not exist.">
      <Link to="/" className="rounded-sm text-accent underline underline-offset-4">
        Go back to the start
      </Link>
    </EmptyState>
  )
}

// APP

// AppRoutes maps every URL to its page, inside the frame that the page asked for.
export function AppRoutes() {
  const links = pages.map((page) => page.route).filter((route) => route.label !== undefined)
  const appPages = pages.filter((page) => page.route.layout !== 'public')
  const publicPages = pages.filter((page) => page.route.layout === 'public')
  return (
    <Routes>
      <Route element={<PublicLayout />}>
        {publicPages.map(({ route, default: Page }) => (
          <Route key={route.path} path={route.path} element={<Page />} />
        ))}
      </Route>
      <Route element={<Layout links={links} />}>
        {appPages.map(({ route, default: Page }) => (
          <Route key={route.path} path={route.path} element={<Page />} />
        ))}
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}

// App is the whole application with browser navigation switched on.
export function App() {
  return (
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
  )
}
