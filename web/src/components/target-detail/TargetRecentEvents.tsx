import type { ReactNode } from 'react'

import { DetailSection } from '../DetailSection'
import { EventList } from '../EventList'
import { Button } from '../atoms/Button'
import type { StateChangeEventRecord } from '../../lib/types'

type TargetRecentEventsProps = {
  loaded: boolean
  events: StateChangeEventRecord[]
  error: string | null
  aside?: ReactNode
  onRetry?: () => void
  retrying?: boolean
}

export function TargetRecentEvents({
  loaded,
  events,
  error,
  aside,
  onRetry,
  retrying = false,
}: TargetRecentEventsProps) {
  const hasEvents = loaded && !error && events.length > 0

  return (
    <DetailSection title="事件" aside={aside}>
      {!loaded ? (
        <div className="watchtower-activity-note" role="status">
          <h3>正在加载相关事件…</h3>
          <p>等待相关事件流返回最新记录。</p>
        </div>
      ) : error ? (
        <div className="watchtower-activity-note" role="alert">
          <h3>相关事件暂不可用</h3>
          <p>{error}</p>
          {onRetry ? (
            <Button
              variant="secondary"
              size="sm"
              disabled={retrying}
              onClick={onRetry}
              aria-label="重试加载相关事件"
            >
              {retrying ? '重试中…' : '重试'}
            </Button>
          ) : null}
        </div>
      ) : hasEvents ? (
        <EventList events={events} />
      ) : (
        <p className="watchtower-activity-quiet">未发现新的状态变更事件</p>
      )}
    </DetailSection>
  )
}
