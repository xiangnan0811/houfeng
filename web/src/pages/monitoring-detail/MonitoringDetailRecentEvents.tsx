import { Link, useLocation } from 'react-router-dom'

import { Button } from '../../components/atoms/Button'
import { Timestamp } from '../../components/atoms/Mono'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type StateChangeEventRecord } from '../../lib/types'
import { withReturnVPSQuery } from './monitoringDetailHelpers'

type Props = {
  subjectBase: string
  returnVPSId: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
  eventsLoaded: boolean
  eventsRetrying: boolean
  onRetryEvents: () => void
  onOpenHistory: () => void
}

const RECENT_EVENT_LIMIT = 3

export function MonitoringDetailRecentEvents({
  subjectBase,
  returnVPSId,
  events,
  eventsError,
  eventsLoaded,
  eventsRetrying,
  onRetryEvents,
  onOpenHistory,
}: Props) {
  const location = useLocation()
  const links = (
    <nav className="monitoring-detail-recent__links" aria-label="历史、活动、记录与证据">
      <button type="button" className="text-link" onClick={onOpenHistory}>历史</button>
      <Link className="text-link" to={withReturnVPSQuery(`${subjectBase}/activity`, returnVPSId)} state={location.state}>活动</Link>
      <Link className="text-link" to={withReturnVPSQuery(`${subjectBase}/records`, returnVPSId)} state={location.state}>记录</Link>
      <Link className="text-link" to={withReturnVPSQuery(`${subjectBase}/evidence`, returnVPSId)} state={location.state}>证据</Link>
    </nav>
  )

  const empty = !eventsLoaded || eventsError || events.length === 0

  return (
    <section
      className={['monitoring-detail-recent', empty && 'monitoring-detail-recent--quiet'].filter(Boolean).join(' ')}
      aria-label="近期事件"
    >
      <header className="monitoring-detail-recent__head">
        <h2>近期事件</h2>
        {links}
      </header>
      {empty ? (
        <p className="monitoring-detail-recent__quiet" role="status">
          {!eventsLoaded ? (
            '正在加载相关事件…'
          ) : eventsError ? (
            <>
              {eventsError}
              <Button variant="ghost" size="sm" aria-label="重试加载相关事件" disabled={eventsRetrying} onClick={onRetryEvents}>
                {eventsRetrying ? '重试中…' : '重试'}
              </Button>
            </>
          ) : (
            '暂无新的状态变更'
          )}
        </p>
      ) : (
        <ul className="monitoring-detail-recent__list">
          {events.slice(0, RECENT_EVENT_LIMIT).map((event, index) => (
            <li
              key={event.event_id ?? `${event.created_at}-${index}`}
              className="monitoring-detail-recent__item"
            >
              <span className="monitoring-detail-recent__marker" aria-hidden />
              <div className="monitoring-detail-recent__body">
                <p className="monitoring-detail-recent__type">
                  {STATE_CHANGE_EVENT_TYPE_LABELS[event.event_type] ?? event.event_type}
                </p>
                <p className="monitoring-detail-recent__summary">{event.summary || '暂无摘要'}</p>
                <p className="monitoring-detail-recent__time">
                  <Timestamp value={event.created_at} mode="absolute" />
                </p>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
