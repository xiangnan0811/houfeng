import { NavLink, Outlet } from 'react-router-dom'

type NavigationItem = {
  to: string
  label: string
  end?: boolean
}

const navigationItems: NavigationItem[] = [
  { to: '/', label: '集群概览', end: true },
  { to: '/nodes', label: '节点' },
  { to: '/targets', label: '目标' },
  { to: '/events', label: '事件' },
  { to: '/settings', label: '设置' },
]

export function AppShell() {
  return (
    <div className="app-shell">
      <aside className="app-shell__sidebar">
        <div className="app-shell__brand">
          <p className="app-shell__brand-mark">候风</p>
          <p className="app-shell__brand-name">Houfeng Fleet Control Plane</p>
        </div>

        <nav className="app-shell__nav" aria-label="主导航">
          {navigationItems.map((item) => (
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
            <p className="app-shell__eyebrow">控制平面</p>
            <h1 className="app-shell__title">Houfeng Fleet Control Plane</h1>
          </div>
        </header>

        <main className="app-shell__content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
