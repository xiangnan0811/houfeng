import type { ReactNode } from 'react'

import { DetailSection } from '../DetailSection'
import { EventList } from '../EventList'
import type { StateChangeEventRecord } from '../../lib/types'

type TargetRecentEventsProps = {
  loaded: boolean
  events: StateChangeEventRecord[]
  error: string | null
  aside?: ReactNode
}

export function TargetRecentEvents({ loaded, events, error, aside }: TargetRecentEventsProps) {
  return (
    <DetailSection eyebrow="事件证据" title="事件" ribbon="accent-2" aside={aside}>
      {!loaded ? (
        <div className="empty-state">
          <h3>正在加载相关事件…</h3>
          <p>等待目标相关的事件流返回最新记录。</p>
        </div>
      ) : error ? (
        <div className="empty-state">
          <h3>相关事件暂不可用</h3>
          <p>{error}</p>
        </div>
      ) : (
        <EventList events={events} />
      )}
    </DetailSection>
  )
}
