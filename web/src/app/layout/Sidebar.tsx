import { NavLink } from 'react-router-dom'
import {
  PRODUCT_NAME_ZH,
  PRODUCT_FULL_NAME_EN,
  PRIMARY_NAV_ITEMS,
} from '../metadata'
import { Badge } from '../../components/atoms'
import type { User } from '../../lib/auth-client'
import { SyncStatus, type SyncStatusProps } from './SyncStatus'
import { UserChip } from './UserChip'

export interface SidebarProps {
  user: User
  sync: SyncStatusProps
  anomalyCounts: { nodes: number; targets: number }
  onLogout: () => void
  onChangePassword: () => void
}

export function Sidebar({
  user,
  sync,
  anomalyCounts,
  onLogout,
  onChangePassword,
}: SidebarProps) {
  return (
    <aside className="sidebar">
      <div className="sidebar__brand">
        <p className="sidebar__brand-zh">{PRODUCT_NAME_ZH}</p>
        <p className="sidebar__brand-en">{PRODUCT_FULL_NAME_EN.toUpperCase()}</p>
      </div>
      <nav className="sidebar__nav" aria-label="主导航">
        {PRIMARY_NAV_ITEMS.map((item) => {
          const count =
            item.to === '/nodes'
              ? anomalyCounts.nodes
              : item.to === '/targets'
                ? anomalyCounts.targets
                : 0
          return (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                `sidebar__nav-link${isActive ? ' is-active' : ''}`
              }
            >
              <span>{item.label}</span>
              {count > 0 && (
                /* v2: tone fixed to 'neutral'. Nav items must not carry
                 * alarm semantics — the count is informational, not a state
                 * indicator. Visual emphasis on the active route comes from
                 * the accent ribbon + soft background, not Badge color. */
                <Badge variant="count" tone="neutral">
                  {count}
                </Badge>
              )}
            </NavLink>
          )
        })}
      </nav>
      <div className="sidebar__spacer" />
      <SyncStatus {...sync} />
      <UserChip user={user} onLogout={onLogout} onChangePassword={onChangePassword} />
    </aside>
  )
}
