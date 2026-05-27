import { useLocation } from 'react-router-dom'
import { GlobalSearch } from './GlobalSearch'
import type { SyncStatusProps } from './SyncStatus'

const PAGE_TITLES: Record<string, string> = {
  '/': '工作台',
  '/nodes': '节点观测',
  '/targets': '目标观测',
  '/events': '事件流',
  '/vps': 'VPS 资产',
  '/providers': '服务商',
  '/subscriptions': '订阅',
  '/asset-decisions': '资产决策',
  '/settings': '设置',
}

interface TopBarProps {
  sync: SyncStatusProps
}

export function TopBar({ sync }: TopBarProps) {
  const location = useLocation()
  const pageTitle = derivePageTitle(location.pathname)

  const syncTitle = sync.label
  const syncClass = `tp-sync tp-sync--${sync.state}`

  return (
    <header className="topbar">
      <span className="tp-page">{pageTitle}</span>
      <div className="tp-spacer" />
      <GlobalSearch />
      <div className="tp-divider" />
      <span className={syncClass} title={syncTitle} />
    </header>
  )
}

function derivePageTitle(pathname: string): string {
  if (PAGE_TITLES[pathname]) return PAGE_TITLES[pathname]
  if (pathname.startsWith('/nodes/')) return '节点详情'
  if (pathname.startsWith('/targets/')) return '目标详情'
  if (pathname.startsWith('/vps/')) return 'VPS 详情'
  return '候风'
}
