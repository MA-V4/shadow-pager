import type { ComponentType } from 'react'

// PAGES

// PageRoute is what every page file says about itself, so the app can find and link it.
export interface PageRoute {
  // The URL of the page, for example /incidents/:id.
  path: string
  // The name in the top navigation, or nothing when the page has no link there.
  label?: string
  // Pages with a smaller number come first in the navigation.
  order?: number
}

// PageModule is the shape of a file in src/pages whose name ends in Page.tsx.
export interface PageModule {
  default: ComponentType
  route: PageRoute
}

// sortedPages puts page files in navigation order, with the most specific URL first among equals.
export function sortedPages(modules: Record<string, PageModule>): PageModule[] {
  return Object.values(modules).sort(
    (a, b) => (a.route.order ?? 99) - (b.route.order ?? 99) || a.route.path.localeCompare(b.route.path),
  )
}
