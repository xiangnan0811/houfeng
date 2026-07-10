import { useState, useRef, useEffect } from 'react'
import { NavLink } from 'react-router-dom'
import type { User } from '../../lib/auth-client'

export interface SidebarProps {
  user: User
  anomalyCounts: { monitoring: number; targets: number }
  collapsed: boolean
  onToggle: () => void
  onLogout: () => void
  onChangePassword: () => void
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
  const [userMenuOpen, setUserMenuOpen] = useState(false)
  const userMenuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!userMenuOpen) return
    const close = (e: MouseEvent) => {
      if (!userMenuRef.current?.contains(e.target as Node)) setUserMenuOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [userMenuOpen])

  const display = user.display_name || user.username || ''
  const initial = display.slice(0, 1).toUpperCase() || '·'

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
        <div className="nav-section">
          <div className="nav-label">运营</div>
          <SidebarNavItem to="/" end label="工作台" icon={<svg viewBox="0 0 16 16"><rect x="2" y="2" width="5" height="5" rx="1"/><rect x="9" y="2" width="5" height="5" rx="1"/><rect x="2" y="9" width="5" height="5" rx="1"/><rect x="9" y="9" width="5" height="5" rx="1"/></svg>} />
        </div>
        <div className="nav-section">
          <div className="nav-label">资产</div>
          <SidebarNavItem to="/vps" label="VPS" icon={<svg viewBox="0 0 16 16"><rect x="2" y="4" width="12" height="9" rx="1.5"/><path d="M5 4V3a1 1 0 011-1h4a1 1 0 011 1v1"/></svg>} />
          <SidebarNavItem to="/archive" label="归档" icon={<svg viewBox="0 0 16 16"><path d="M3 3h10v4H3z"/><path d="M4 7v8h8V7"/><path d="M6 10h4"/></svg>} />
          <SidebarNavItem to="/providers" label="服务商" icon={<svg viewBox="0 0 16 16"><path d="M8 2v12M2 8h12"/><circle cx="8" cy="8" r="5"/></svg>} />
          <SidebarNavItem to="/subscriptions" label="订阅" icon={<svg viewBox="0 0 16 16"><path d="M3 4h10M3 8h10M3 12h6"/></svg>} />
          <SidebarNavItem to="/asset-decisions" label="资产决策" icon={<svg viewBox="0 0 16 16"><path d="M2 12l4-4 3 3 5-6"/></svg>} />
        </div>
        <div className="nav-section">
          <div className="nav-label">观测</div>
          <SidebarNavItem to="/monitoring" label="监控" badge={anomalyCounts.monitoring} icon={<svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="3"/><path d="M8 2v2M8 12v2M2 8h2M12 8h2"/></svg>} />
          <SidebarNavItem to="/targets" label="入口探测" badge={anomalyCounts.targets} icon={<svg viewBox="0 0 16 16"><path d="M2 8h3l2-4 2 8 2-4h3"/></svg>} />
          <SidebarNavItem to="/events" label="事件" icon={<svg viewBox="0 0 16 16"><path d="M4 12V7M8 12V4M12 12V9"/></svg>} />
        </div>
        <div className="nav-section">
          <div className="nav-label">系统</div>
          <SidebarNavItem to="/settings" label="设置" icon={<svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="2.5"/><path d="M8 2v1.5M8 12.5V14M2 8h1.5M12.5 8H14"/></svg>} />
        </div>
      </nav>
      <div className="sidebar-footer" ref={userMenuRef}>
        <div className="user-chip" onClick={() => setUserMenuOpen((v) => !v)}>
          <div className="user-avatar">{initial}</div>
          <div className="user-info">
            <div className="user-name">{user.display_name || user.username}</div>
            <div className="user-role">管理员</div>
          </div>
        </div>
        {userMenuOpen && (
          <div className="user-chip__menu" role="menu">
            <button type="button" className="user-chip__menu-item" role="menuitem"
              onClick={() => { setUserMenuOpen(false); onChangePassword() }}>
              修改密码
            </button>
            <div className="user-chip__divider" />
            <button type="button" className="user-chip__menu-item user-chip__menu-item--danger" role="menuitem"
              onClick={() => { setUserMenuOpen(false); onLogout() }}>
              退出登录
            </button>
          </div>
        )}
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
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
    >
      {icon}
      <span className="nav-text">{label}</span>
      {badge != null && badge > 0 && <span className="nav-badge">{badge}</span>}
    </NavLink>
  )
}
