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

        <div className="app-shell__sidebar-context" aria-label="系统形态">
          <p>运行形态</p>
          <span>单体中心</span>
          <span>PostgreSQL</span>
          <span>systemd agent</span>
        </div>
      </aside>

      <div className="app-shell__main">
        <header className="app-shell__header">
          <div>
            <p className="app-shell__eyebrow">{PRODUCT_NAME_EN}</p>
            <p className="app-shell__section-label">当前视图</p>
            <h1 className="app-shell__title">{PRODUCT_FULL_NAME_ZH}</h1>
          </div>

          <div className="app-shell__status-strip" aria-label="V1 基线状态">
            <span>V1 冻结基线</span>
            <span>中文主界面</span>
            <span>Unified / Baseline</span>
          </div>
        </header>

        <main className="app-shell__content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
