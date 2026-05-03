import { DetailSection } from '../DetailSection'
import { IncidentList } from '../IncidentList'
import type { ActiveIncidentRecord } from '../../lib/types'

type TargetActiveIncidentsProps = {
  loaded: boolean
  incidents: ActiveIncidentRecord[]
  error: string | null
}

export function TargetActiveIncidents({ loaded, incidents, error }: TargetActiveIncidentsProps) {
  return (
    <DetailSection eyebrow="当前异常" title="当前异常">
      {!loaded ? (
        <div className="empty-state">
          <h3>正在加载活跃异常…</h3>
          <p>等待目标相关的异常读模型返回最新结果。</p>
        </div>
      ) : error ? (
        <div className="empty-state">
          <h3>活跃异常暂不可用</h3>
          <p>{error}</p>
        </div>
      ) : (
        <IncidentList incidents={incidents} />
      )}
    </DetailSection>
  )
}
