import { Link } from 'react-router-dom'

import { STATE_CHANGE_EVENT_TYPE_LABELS, type StateChangeEventRecord } from '../../lib/types'
import { PAGE_SIZE } from './eventsPageConstants'

type EventsStreamSectionProps = {
  events: StateChangeEventRecord[]
  exhausted: boolean
  loadingMore: boolean
  hasActiveFilters: boolean
  page: number
  nameMap: Map<string, string>
  onPageChange: (page: number) => void
  onLoadMore: () => void
  onClearFilters: () => void
}

function formatEventTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:${min}:${ss}`
}

function severityClass(severity: string): string {
  if (severity === '严重') return 'badge alert'
  if (severity === '告警') return 'badge notice'
  if (severity === '关注') return 'badge warn'
  return 'badge'
}

function eventTypeLabel(value: StateChangeEventRecord['event_type']): string {
  return STATE_CHANGE_EVENT_TYPE_LABELS[value] ?? value
}

function objectLink(
  objectType: string,
  objectId: string,
  nameMap: Map<string, string>,
): { to: string; label: string } {
  const name = nameMap.get(objectId) || objectId
  if (objectType === 'monitoring_instance') return { to: `/monitoring/${objectId}`, label: `监控实例 · ${name}` }
  if (objectType === 'target') return { to: `/targets/${objectId}`, label: `目标 · ${name}` }
  return { to: '#', label: `${objectType} · ${name}` }
}

export function EventsStreamSection({
  events,
  exhausted,
  loadingMore,
  hasActiveFilters,
  page,
  nameMap,
  onPageChange,
  onLoadMore,
  onClearFilters,
}: EventsStreamSectionProps) {
  const totalPages = Math.max(1, Math.ceil(events.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const startIdx = (currentPage - 1) * PAGE_SIZE
  const displayEvents = events.slice(startIdx, startIdx + PAGE_SIZE)
  const isLastPage = currentPage >= totalPages

  if (events.length === 0) {
    return (
      <div className="card events-stream-empty">
        <p className="events-stream-empty__message">
          {hasActiveFilters ? '当前筛选没有匹配的事件' : '最近没有状态变更事件'}
        </p>
        {hasActiveFilters && (
          <button type="button" className="btn sm secondary events-stream-empty__reset" onClick={onClearFilters}>
            重置筛选
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="card events-stream-card">
      <p id="events-table-scroll-hint" className="events-table-scroll-hint">
        表格可横向滚动查看完整事件字段
      </p>
      <div
        className="events-table-scroll"
        role="region"
        aria-labelledby="events-page-title"
        aria-describedby="events-table-scroll-hint"
        tabIndex={0}
      >
        <table className="table">
          <thead>
            <tr>
              <th className="time">时间</th>
              <th>严重度</th>
              <th>事件类型</th>
              <th>异常类别</th>
              <th>摘要</th>
              <th>对象</th>
            </tr>
          </thead>
          <tbody>
            {displayEvents.map((evt) => {
              const link = objectLink(evt.object_type, evt.object_id, nameMap)
              return (
                <tr key={evt.event_id ?? `${evt.created_at}-${evt.incident_id}-${evt.event_type}`}>
                  <td className="time mono">{formatEventTime(evt.created_at)}</td>
                  <td><span className={severityClass(evt.severity)}>{evt.severity || '—'}</span></td>
                  <td>{eventTypeLabel(evt.event_type)}</td>
                  <td>{evt.incident_class || '—'}</td>
                  <td className="name">{evt.summary || '暂无摘要'}</td>
                  <td><Link to={link.to} className="mono">{link.label}</Link></td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      <div className="table-pagination">
        <button
          type="button"
          className="btn sm ghost"
          disabled={currentPage <= 1}
          onClick={() => onPageChange(currentPage - 1)}
        >
          上一页
        </button>
        <span className="table-pagination__info">第 {currentPage}/{totalPages} 页</span>
        {isLastPage && !exhausted ? (
          <button
            type="button"
            className="btn sm secondary"
            disabled={loadingMore}
            onClick={onLoadMore}
          >
            {loadingMore ? '加载中…' : '加载更多'}
          </button>
        ) : (
          <button
            type="button"
            className="btn sm ghost"
            disabled={currentPage >= totalPages}
            onClick={() => onPageChange(currentPage + 1)}
          >
            下一页
          </button>
        )}
      </div>
    </div>
  )
}
