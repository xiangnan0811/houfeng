import { useEffect, useState } from 'react'

import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { ApiError, listEvents } from '../lib/api'
import type { EventListFilter, StateChangeEventType } from '../lib/types'

type FilterState = {
  object_type: '' | 'node' | 'target'
  severity: '' | '关注' | '告警' | '严重'
  event_type: '' | StateChangeEventType
  limit: string
}

type State = {
  loading: boolean
  error: string | null
  events: Awaited<ReturnType<typeof listEvents>>
}

const DEFAULT_FILTERS: FilterState = {
  object_type: '',
  severity: '',
  event_type: '',
  limit: '50',
}

function buildFilterQuery(filters: FilterState): EventListFilter {
  return {
    object_type: filters.object_type,
    severity: filters.severity,
    event_type: filters.event_type,
    limit: Number(filters.limit || DEFAULT_FILTERS.limit),
  }
}

export function EventsPage() {
  const [filters, setFilters] = useState<FilterState>(DEFAULT_FILTERS)
  const [appliedFilters, setAppliedFilters] = useState<FilterState>(DEFAULT_FILTERS)
  const [state, setState] = useState<State>({
    loading: true,
    error: null,
    events: [],
  })

  useEffect(() => {
    let cancelled = false

    listEvents(buildFilterQuery(appliedFilters))
      .then((events) => {
        if (cancelled) return
        setState({ loading: false, error: null, events })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message = error instanceof ApiError ? error.message : '加载事件失败'
        setState({ loading: false, error: message, events: [] })
      })

    return () => {
      cancelled = true
    }
  }, [appliedFilters])

  if (state.loading) {
    return <section className="page-panel">正在加载事件…</section>
  }

  if (state.error) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">Events</p>
        <h2 className="page-panel__title">事件不可用</h2>
        <p className="page-panel__description">{state.error}</p>
      </section>
    )
  }

  return (
    <div className="page-stack">
      <section className="page-panel">
        <p className="page-panel__eyebrow">Events</p>
        <h2 className="page-panel__title">事件</h2>
        <p className="page-panel__description">
          查看最新状态变更事件，并按对象类型、严重程度、事件类型与数量快速筛选。
        </p>
      </section>

      <DetailSection eyebrow="Filters" title="筛选条件">
        <form
          onSubmit={(event) => {
            event.preventDefault()
            setState((current) => ({ ...current, loading: true, error: null }))
            setAppliedFilters({ ...filters })
          }}
        >
          <div className="summary-grid">
            <label className="summary-card">
              <span className="summary-card__label">对象类型</span>
              <select
                aria-label="对象类型"
                value={filters.object_type}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, object_type: event.target.value as FilterState['object_type'] }))
                }
              >
                <option value="">全部</option>
                <option value="node">节点</option>
                <option value="target">目标</option>
              </select>
            </label>

            <label className="summary-card">
              <span className="summary-card__label">严重程度</span>
              <select
                aria-label="严重程度"
                value={filters.severity}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, severity: event.target.value as FilterState['severity'] }))
                }
              >
                <option value="">全部</option>
                <option value="关注">关注</option>
                <option value="告警">告警</option>
                <option value="严重">严重</option>
              </select>
            </label>

            <label className="summary-card">
              <span className="summary-card__label">事件类型</span>
              <select
                aria-label="事件类型"
                value={filters.event_type}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, event_type: event.target.value as FilterState['event_type'] }))
                }
              >
                <option value="">全部</option>
                <option value="incident_started">异常开始</option>
                <option value="incident_escalated">异常升级</option>
                <option value="incident_recovered">异常恢复</option>
                <option value="node_binding_rebind_confirmed">确认重新绑定</option>
                <option value="node_binding_pending_rejected">拒绝待确认指纹</option>
                <option value="node_binding_reset">绑定已重置</option>
              </select>
            </label>

            <label className="summary-card">
              <span className="summary-card__label">数量</span>
              <select
                aria-label="数量"
                value={filters.limit}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, limit: event.target.value }))
                }
              >
                <option value="10">10</option>
                <option value="25">25</option>
                <option value="50">50</option>
                <option value="100">100</option>
              </select>
            </label>
          </div>

          <button type="submit">应用筛选</button>
        </form>
      </DetailSection>

      <DetailSection eyebrow="Timeline" title="事件流">
        <EventList events={state.events} />
      </DetailSection>
    </div>
  )
}
