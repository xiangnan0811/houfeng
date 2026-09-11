import { useLayoutEffect, useRef } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import type { User } from '../../lib/auth-client'
import { UserChip } from './UserChip'

export interface SidebarProps {
  user: User
  anomalyCounts: { monitoring: number; targets: number }
  collapsed: boolean
  onToggle: () => void
  onLogout: () => void
  onChangePassword: () => void
}

const MORE_PREFIXES = [
  '/asset-decisions',
  '/subscriptions',
  '/providers',
  '/archive',
  '/records',
  '/record-inbox',
  '/events',
  '/command-audit',
]

function pathInMore(pathname: string): boolean {
  return MORE_PREFIXES.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`))
}

export function Sidebar({
  user,
  anomalyCounts,
  collapsed: _collapsed,
  onToggle,
  onLogout,
  onChangePassword,
}: SidebarProps) {
  void _collapsed
  const { pathname } = useLocation()
  const moreActive = pathInMore(pathname)
  const moreDetailsRef = useRef<HTMLDetailsElement | null>(null)

  useLayoutEffect(() => {
    if (moreActive && moreDetailsRef.current) {
      moreDetailsRef.current.open = true
    }
  }, [moreActive])

  return (
    <aside className="sidebar">
      <button className="sidebar-toggle" onClick={onToggle} aria-label="折叠侧边栏">
        <svg viewBox="0 0 24 24"><polyline points="15 18 9 12 15 6" /></svg>
      </button>
      <div className="logo">
        <div className="logo-mark">
          <svg viewBox="0 0 16 16"><path d="M8 2L14 6V12L8 14L2 12V6Z" /></svg>
        </div>
        <span className="logo-text">候风</span>
      </div>
      <nav>
        <SidebarNavItem to="/" end label="工作台" icon={<svg viewBox="0 0 16 16"><rect x="2" y="2" width="5" height="5" rx="1"/><rect x="9" y="2" width="5" height="5" rx="1"/><rect x="2" y="9" width="5" height="5" rx="1"/><rect x="9" y="9" width="5" height="5" rx="1"/></svg>} />
        <SidebarNavItem to="/vps" label="VPS" icon={<svg viewBox="0 0 16 16"><rect x="2" y="4" width="12" height="9" rx="1.5"/><path d="M5 4V3a1 1 0 011-1h4a1 1 0 011 1v1"/></svg>} />
        <SidebarNavItem to="/monitoring" label="监控" badge={anomalyCounts.monitoring} icon={<svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="3"/><path d="M8 2v2M8 12v2M2 8h2M12 8h2"/></svg>} />
        <SidebarNavItem to="/targets" label="入口探测" badge={anomalyCounts.targets} icon={<svg viewBox="0 0 16 16"><path d="M2 8h3l2-4 2 8 2-4h3"/></svg>} />
        <details
          ref={moreDetailsRef}
          className="sidebar-more"
        >
          <summary className="sidebar-more__summary" aria-label="更多">
            <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true"><path d="M3 8h10M8 3v10" fill="none" stroke="currentColor" strokeWidth="1.5"/></svg>
            <span className="nav-text">更多</span>
          </summary>
          <SidebarNavItem to="/asset-decisions" label="资产决策" icon={<svg viewBox="0 0 16 16"><path d="M2 12l4-4 3 3 5-6"/></svg>} />
          <SidebarNavItem to="/subscriptions" label="订阅" icon={<svg viewBox="0 0 16 16"><path d="M3 4h10M3 8h10M3 12h6"/></svg>} />
          <SidebarNavItem to="/providers" label="服务商" icon={<svg viewBox="0 0 16 16"><path d="M8 2v12M2 8h12"/><circle cx="8" cy="8" r="5"/></svg>} />
          <SidebarNavItem to="/archive" label="归档" icon={<svg viewBox="0 0 16 16"><path d="M3 3h10v4H3z"/><path d="M4 7v8h8V7"/><path d="M6 10h4"/></svg>} />
          <SidebarNavItem to="/records" label="运维记录" icon={<svg viewBox="0 0 16 16"><path d="M4 2h6l2 2v10H4z"/><path d="M6 6h4M6 9h4M6 12h2"/></svg>} />
          <SidebarNavItem to="/events" label="事件" icon={<svg viewBox="0 0 16 16"><path d="M4 12V7M8 12V4M12 12V9"/></svg>} />
          <SidebarNavItem to="/command-audit" label="命令审计" icon={<svg viewBox="0 0 16 16"><path d="M3 3h10v10H3zM5 8h6"/></svg>} />
        </details>
        <SidebarNavItem to="/settings" label="设置" icon={<svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="2.5"/><path d="M8 2v1.5M8 12.5V14M2 8h1.5M12.5 8H14"/></svg>} />
      </nav>
      <div className="sidebar-footer">
        <UserChip user={user} onLogout={onLogout} onChangePassword={onChangePassword} />
      </div>
    </aside>
  )
}

interface SidebarNavItemProps {
  to: string
  label: string
  icon: React.ReactNode
  badge?: number
  end?: boolean
}

function SidebarNavItem({ to, label, icon, badge, end }: SidebarNavItemProps) {
  const accessibleLabel = badge != null && badge > 0 ? `${label}，${badge} 个异常` : label

  return (
    <NavLink
      to={to}
      {...(end === undefined ? {} : { end })}
      aria-label={accessibleLabel}
      className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
    >
      {icon}
      <span className="nav-text">{label}</span>
      {badge != null && badge > 0 && <span className="nav-badge">{badge}</span>}
    </NavLink>
  )
}
