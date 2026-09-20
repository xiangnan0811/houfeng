import { useRef } from 'react'
import { Link } from 'react-router-dom'

import { ObservabilityNoticeRow } from '../../components/observability'
import { PageState } from '../../components/PageState'
import { incidentClassLabel, severityTone } from '../../lib/observabilityLabels'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type StateChangeEventRecord } from '../../lib/types'
import { EventsStreamPagination } from './EventsStreamPagination'
import { PAGE_SIZE } from './eventsPageConstants'

type EventsStreamSectionProps = {
  events: StateChangeEventRecord[]
  exhausted: boolean
  loadingMore: boolean
  loadMoreError?: string | null
  hasActiveFilters: boolean
  page: number
  nameMap: Map<string, string>
  onPageChange: (page: number) => void
  onLoadMore: () => void
  onClearFilters: () => void
}

function scrollStreamIntoView(node: HTMLElement | null) {
  if (!node || typeof node.scrollIntoView !== 'function') return
  const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  node.scrollIntoView({ block: 'start', behavior: reduced ? 'auto' : 'smooth' })
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
  loadMoreError = null,
  hasActiveFilters,
  page,
  nameMap,
  onPageChange,
  onLoadMore,
  onClearFilters,
}: EventsStreamSectionProps) {
  const listRef = useRef<HTMLUListElement>(null)
  const totalPages = Math.max(1, Math.ceil(events.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const startIdx = (currentPage - 1) * PAGE_SIZE
  const displayEvents = events.slice(startIdx, startIdx + PAGE_SIZE)

  function goToPage(nextPage: number) {
    if (nextPage === currentPage) return
    onPageChange(nextPage)
    scrollStreamIntoView(listRef.current)
  }

  if (events.length === 0) {
    return (
      <PageState
        kind="empty"
        surface="empty"
        title={hasActiveFilters ? '当前筛选没有匹配的事件' : '最近没有状态变更事件'}
        description={
          hasActiveFilters
            ? '请尝试调整筛选条件，或清空筛选恢复完整事件流。'
            : '系统暂时没有新的状态变更事件。'
        }
        action={
          hasActiveFilters ? (
            <button type="button" className="btn sm secondary" onClick={onClearFilters}>
              清空筛选
            </button>
          ) : null
        }
      />
    )
  }

  return (
    <div className="events-stream">
      <ul ref={listRef} className="events-stream__list" aria-labelledby="events-page-title">
        {displayEvents.map((evt) => {
          const link = objectLink(evt.object_type, evt.object_id, nameMap)
          const tone = severityTone(evt.severity)
          const classLabel = incidentClassLabel(evt.incident_class)
          return (
            <ObservabilityNoticeRow
              key={evt.event_id ?? `${evt.created_at}-${evt.incident_id}-${evt.event_type}`}
              tone={tone}
              mark={evt.severity || '事件'}
              title={eventTypeLabel(evt.event_type)}
              detail={evt.summary || '暂无摘要'}
              meta={(
                <>
                  {classLabel ? <>{classLabel} · </> : null}
                  <Link to={link.to}>{link.label}</Link>
                </>
              )}
              time={evt.created_at}
            />
          )
        })}
      </ul>

      <EventsStreamPagination
        page={currentPage}
        total={events.length}
        exhausted={exhausted}
        loadingMore={loadingMore}
        loadMoreError={loadMoreError}
        onPageChange={goToPage}
        onLoadMore={onLoadMore}
      />
    </div>
  )
}
