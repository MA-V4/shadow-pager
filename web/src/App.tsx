import { BrowserRouter, Link, Route, Routes } from 'react-router-dom'

import { Layout } from './components/Layout'
import { EmptyState } from './components/Notice'
import { sortedPages, type PageModule } from './lib/routes'

// PAGES

// Every file in src/pages that ends in Page.tsx is picked up here, so adding a page never means editing this file.
const pages = sortedPages(import.meta.glob<PageModule>('./pages/*Page.tsx', { eager: true }))

// NotFound is shown for any URL that no page claims.
function NotFound() {
  return (
    <EmptyState title="This page does not exist">
      <Link to="/" className="underline underline-offset-4">
        Go back to the start
      </Link>
    </EmptyState>
  )
}

// APP

// AppRoutes maps every URL to its page, inside the shared frame.
export function AppRoutes() {
  const links = pages.map((page) => page.route).filter((route) => route.label !== undefined)
  return (
    <Routes>
      <Route element={<Layout links={links} />}>
        {pages.map(({ route, default: Page }) => (
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
