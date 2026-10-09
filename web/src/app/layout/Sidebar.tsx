import { Link, matchPath, NavLink, useLocation, type Location } from 'react-router-dom'
import type { User } from '../../lib/auth-client'
import { UserChip } from './UserChip'

export interface SidebarProps {
  user: User
  anomalyCounts: { monitoring: number; targets: number; unobservedTargets: number; staleTargets: number }
  collapsed: boolean
  onToggle: () => void
  onLogout: () => void
  onChangePassword: () => void
}

// 与路由同口径：大小写不敏感、允许尾斜杠。
function isTargetsPath(location: Location): boolean {
  return matchPath({ path: '/targets', end: false }, location.pathname) != null
}

// 全部入口按“资产 / 观测 / 记录”分组常驻展开，不再把常用页面收进“更多”。
export function Sidebar({
  user,
  anomalyCounts,
  collapsed,
  onToggle,
  onLogout,
  onChangePassword,
}: SidebarProps) {
  return (
    <aside className="sidebar">
      <button
        className="sidebar-toggle"
        onClick={onToggle}
        aria-label={collapsed ? '展开侧边栏' : '折叠侧边栏'}
        aria-expanded={!collapsed}
      >
        <svg viewBox="0 0 24 24"><polyline points="15 18 9 12 15 6" /></svg>
      </button>
      <div className="logo">
        <div className="logo-mark">
          <svg viewBox="0 0 16 16"><path d="M8 2L14 6V12L8 14L2 12V6Z" /></svg>
        </div>
        <span className="logo-text">候风</span>
      </div>
      <nav aria-label="主导航">
        <SidebarNavItem to="/" end label="工作台" icon={<svg viewBox="0 0 16 16"><rect x="2" y="2" width="5" height="5" rx="1"/><rect x="9" y="2" width="5" height="5" rx="1"/><rect x="2" y="9" width="5" height="5" rx="1"/><rect x="9" y="9" width="5" height="5" rx="1"/></svg>} />
        <SidebarNavGroup id="sidebar-group-assets" label="资产">
          <SidebarNavItem to="/vps" label="VPS" icon={<svg viewBox="0 0 16 16"><rect x="2" y="4" width="12" height="9" rx="1.5"/><path d="M5 4V3a1 1 0 011-1h4a1 1 0 011 1v1"/></svg>} />
          <SidebarNavItem to="/subscriptions" label="订阅" icon={<svg viewBox="0 0 16 16"><rect x="2" y="3.5" width="12" height="9" rx="1.5"/><path d="M2 6.5h12M4.5 10h3"/></svg>} />
          <SidebarNavItem to="/providers" label="服务商" icon={<svg viewBox="0 0 16 16"><path d="M3 14V5.5L8 2.5l5 3V14"/><path d="M6.5 14v-3.5h3V14"/></svg>} />
          <SidebarNavItem to="/asset-decisions" label="资产决策" icon={<svg viewBox="0 0 16 16"><path d="M2 12l4-4 3 3 5-6"/></svg>} />
          <SidebarNavItem to="/archive" label="归档" icon={<svg viewBox="0 0 16 16"><path d="M3 3h10v4H3z"/><path d="M4 7v8h8V7"/><path d="M6 10h4"/></svg>} />
        </SidebarNavGroup>
        <SidebarNavGroup id="sidebar-group-observability" label="观测">
          <SidebarNavItem to="/monitoring" label="监控" badge={anomalyCounts.monitoring} icon={<svg viewBox="0 0 16 16"><path d="M2 8h3l2-4 2 8 2-4h3"/></svg>} />
          <TargetsNavItem
            abnormal={anomalyCounts.targets}
            unobserved={anomalyCounts.unobservedTargets}
            stale={anomalyCounts.staleTargets}
          />
          <SidebarNavItem to="/events" label="事件" icon={<svg viewBox="0 0 16 16"><path d="M9 2L4 9h4l-1 5 5-7H8l1-5z"/></svg>} />
        </SidebarNavGroup>
        <SidebarNavGroup id="sidebar-group-records" label="记录">
          {user.runtime_capabilities.records ? (
            <SidebarNavItem to="/records" label="运维记录" icon={<svg viewBox="0 0 16 16"><path d="M4 2h6l2 2v10H4z"/><path d="M6 6h4M6 9h4M6 12h2"/></svg>} />
          ) : null}
          <SidebarNavItem to="/command-audit" label="命令审计" icon={<svg viewBox="0 0 16 16"><path d="M3 5l3 3-3 3M8 11h5"/></svg>} />
        </SidebarNavGroup>
        <div className="nav-group nav-group--end">
          <SidebarNavItem to="/settings" label="设置" icon={<svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="2"/><path d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.6 3.6l1.1 1.1M11.3 11.3l1.1 1.1M3.6 12.4l1.1-1.1M11.3 4.7l1.1-1.1"/></svg>} />
        </div>
      </nav>
      <div className="sidebar-footer">
        <UserChip user={user} onLogout={onLogout} onChangePassword={onChangePassword} />
      </div>
    </aside>
  )
}

function SidebarNavGroup({ id, label, children }: { id: string; label: string; children: React.ReactNode }) {
  return (
    <div className="nav-group" role="group" aria-labelledby={id}>
      <p className="nav-group__label" id={id}>{label}</p>
      {children}
    </div>
  )
}

// 尚无观测、观测过期是“入口探测”的分类徽标，不再作为随数据出现/消失的独立导航项，
// 导航条目因此固定；所有 /targets 路径（含两个快捷视图）都由“入口探测”承接当前项。
function TargetsNavItem({ abnormal, unobserved, stale }: { abnormal: number; unobserved: number; stale: number }) {
  const location = useLocation()
  const active = isTargetsPath(location)
  const counts = [
    abnormal > 0 ? `${abnormal} 个异常` : null,
    unobserved > 0 ? `${unobserved} 个尚无观测` : null,
    stale > 0 ? `${stale} 个观测过期` : null,
  ].filter(Boolean)
  // 窄栏只显示图标，分类徽标随之隐藏；主链接的名称带上全部计数，窄栏另用圆点提示。
  const gap = unobserved > 0 || stale > 0
  return (
    <div className="nav-item-row">
      <Link
        to="/targets"
        aria-label={counts.length > 0 ? `入口探测，${counts.join('，')}` : '入口探测'}
        {...(active ? { 'aria-current': 'page' as const } : {})}
        className={`nav-item${active ? ' active' : ''}${gap && abnormal === 0 ? ` nav-item--gap-${stale > 0 ? 'stale' : 'unobserved'}` : ''}`}
      >
        <svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="5.5"/><circle cx="8" cy="8" r="2"/></svg>
        <span className="nav-text">入口探测</span>
        {abnormal > 0 ? <span className="nav-badge">{abnormal}</span> : null}
      </Link>
      {gap ? (
        <span className="nav-subbadges">
          {unobserved > 0 ? (
            <Link
              to="/targets?view=unobserved"
              className="nav-subbadge nav-subbadge--unobserved"
              aria-label={`尚无观测，${unobserved} 个尚无观测`}
              title="尚无观测"
            >
              {unobserved}
            </Link>
          ) : null}
          {stale > 0 ? (
            <Link
              to="/targets?view=stale"
              className="nav-subbadge nav-subbadge--stale"
              aria-label={`观测过期，${stale} 个观测过期`}
              title="观测过期"
            >
              {stale}
            </Link>
          ) : null}
        </span>
      ) : null}
    </div>
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

  const content = (
    <>
      {icon}
      <span className="nav-text">{label}</span>
      {badge != null && badge > 0 && <span className="nav-badge">{badge}</span>}
    </>
  )
  return (
    <NavLink
      to={to}
      {...(end === undefined ? {} : { end })}
      aria-label={accessibleLabel}
      className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
    >
      {content}
    </NavLink>
  )
}
