import type { ReactNode } from 'react'

import { DetailSection } from '../DetailSection'
import { IncidentList } from '../IncidentList'
import { Button } from '../atoms/Button'
import type { ActiveIncidentRecord } from '../../lib/types'

type TargetActiveIncidentsProps = {
  loaded: boolean
  incidents: ActiveIncidentRecord[]
  error: string | null
  aside?: ReactNode
  onRetry?: () => void
  retrying?: boolean
}

export function TargetActiveIncidents({
  loaded,
  incidents,
  error,
  aside,
  onRetry,
  retrying = false,
}: TargetActiveIncidentsProps) {
  const hasIncidents = loaded && !error && incidents.length > 0

  return (
    <DetailSection
      title="当前异常"
      {...(hasIncidents ? { ribbon: 'critical' as const } : {})}
      aside={aside}
    >
      {!loaded ? (
        <div className="watchtower-activity-note" role="status">
          <h3>正在加载活跃异常…</h3>
          <p>等待相关的异常读模型返回最新结果。</p>
        </div>
      ) : error ? (
        <div className="watchtower-activity-note" role="alert">
          <h3>活跃异常暂不可用</h3>
          <p>{error}</p>
          {onRetry ? (
            <Button
              variant="secondary"
              size="sm"
              disabled={retrying}
              onClick={onRetry}
              aria-label="重试加载活跃异常"
            >
              {retrying ? '重试中…' : '重试'}
            </Button>
          ) : null}
        </div>
      ) : hasIncidents ? (
        <IncidentList incidents={incidents} />
      ) : (
        <p className="watchtower-activity-quiet">未发现活跃异常</p>
      )}
    </DetailSection>
  )
}
