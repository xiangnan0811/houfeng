import { Link } from 'react-router-dom'

import { Timestamp } from '../../components/atoms'

type MonitoringHeroProps = {
  snapshotReadAt: Date | null
  refreshing: boolean
  refreshLocked?: boolean
  onRefresh: () => void
}

export function MonitoringHero({ snapshotReadAt, refreshing, refreshLocked = false, onRefresh }: MonitoringHeroProps) {
  return (
    <header className="page__head monitoring-page__head">
      <h1 className="page__title">监控</h1>
      <div className="page__actions monitoring-page__head-actions">
        <Link className="btn sm primary" to="/vps?view=unlinked" title="从尚未关联监控的 VPS 创建实例并接入 agent">
          从未关联 VPS 接入
        </Link>
        <p className="monitoring-page__snapshot">
          {snapshotReadAt ? (
            <>
              列表读取 <Timestamp value={snapshotReadAt} mode="absolute" />
            </>
          ) : (
            '尚未完成列表读取'
          )}
        </p>
        <button type="button" className="btn sm ghost" onClick={onRefresh} disabled={refreshing || refreshLocked}>
          {refreshing ? '正在刷新…' : '刷新'}
        </button>
      </div>
    </header>
  )
}
