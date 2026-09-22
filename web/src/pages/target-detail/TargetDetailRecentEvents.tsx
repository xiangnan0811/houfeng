import type { ReactNode } from 'react'

import { ObservabilityNoticeRow } from '../../components/observability'
import { Button } from '../../components/atoms/Button'
import { PageState } from '../../components/PageState'
import { incidentClassLabel, severityTone } from '../../lib/observabilityLabels'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type StateChangeEventRecord } from '../../lib/types'

type Props = {
  loaded: boolean
  events: StateChangeEventRecord[]
  error: string | null
  aside?: ReactNode
  onRetry?: () => void
  retrying?: boolean
}

export function TargetDetailRecentEvents({
  loaded,
  events,
  error,
  aside,
  onRetry,
  retrying = false,
}: Props) {
  const visible = events.slice(0, 3)

  return (
    <section className="monitoring-detail-section" aria-label="近期事件">
      <header className="monitoring-detail-section__head">
        <h2>近期事件</h2>
        {aside}
      </header>
      {!loaded ? (
        <PageState
          kind="loading"
          title="正在加载相关事件…"
          description="等待相关事件流返回最新记录。"
          surface="empty"
          compact
        />
      ) : error ? (
        <PageState
          kind="error"
          title="相关事件暂不可用"
          description={error}
          surface="empty"
          compact
          action={
            onRetry ? (
              <Button
                variant="secondary"
                size="sm"
                disabled={retrying}
                onClick={onRetry}
                aria-label="重试加载相关事件"
              >
                {retrying ? '重试中…' : '重试'}
              </Button>
            ) : null
          }
        />
      ) : visible.length > 0 ? (
        <ul className="monitoring-detail-history__list">
          {visible.map((event, index) => (
            <ObservabilityNoticeRow
              key={event.event_id ?? `${event.created_at}-${index}`}
              tone={severityTone(event.severity)}
              mark={event.severity || '事件'}
              title={STATE_CHANGE_EVENT_TYPE_LABELS[event.event_type] ?? event.event_type}
              detail={event.summary || '暂无摘要'}
              meta={incidentClassLabel(event.incident_class) || undefined}
              time={event.created_at}
            />
          ))}
        </ul>
      ) : (
        <p className="watchtower-activity-quiet">未发现新的状态变更事件</p>
      )}
    </section>
  )
}
