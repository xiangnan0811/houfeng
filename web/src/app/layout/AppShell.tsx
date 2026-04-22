import { useEffect } from 'react'
import { NavLink, Outlet } from 'react-router-dom'

import {
  PRIMARY_NAV_ITEMS,
  PRODUCT_FULL_NAME_ZH,
  PRODUCT_NAME_EN,
  PRODUCT_NAME_ZH,
} from '../metadata'

export function AppShell() {
  useEffect(() => {
    document.title = PRODUCT_FULL_NAME_ZH
  }, [])

  return (
    <div className="app-shell">
      <aside className="app-shell__sidebar">
        <div className="app-shell__brand">
          <p className="app-shell__brand-mark">{PRODUCT_NAME_ZH}</p>
          <p className="app-shell__brand-name">{PRODUCT_FULL_NAME_ZH}</p>
        </div>

        <nav className="app-shell__nav" aria-label="主导航">
          {PRIMARY_NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                `app-shell__nav-link${isActive ? ' is-active' : ''}`
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="app-shell__main">
        <header className="app-shell__header">
          <div>
            <p className="app-shell__eyebrow">{PRODUCT_NAME_EN}</p>
            <h1 className="app-shell__title">{PRODUCT_FULL_NAME_ZH}</h1>
          </div>
        </header>

        <main className="app-shell__content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
