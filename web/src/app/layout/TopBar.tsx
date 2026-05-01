import { Breadcrumb } from './Breadcrumb'
import { GlobalSearch } from './GlobalSearch'

/**
 * Sticky top bar for the main content column.
 *
 * Layout: [breadcrumb] [spacer] [global search]
 *
 * The breadcrumb auto-hides on root and level-1 routes (see Breadcrumb.tsx
 * for rationale). On those pages the bar still renders the search slot —
 * the spacer collapses, and the search alone right-aligns.
 */
export function TopBar() {
  return (
    <header className="top-bar" role="banner">
      <Breadcrumb />
      <div className="top-bar__spacer" />
      <GlobalSearch />
    </header>
  )
}
