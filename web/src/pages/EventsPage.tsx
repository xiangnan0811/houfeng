import { useEffect, useState } from 'react'

import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { ApiError, listEvents } from '../lib/api'
import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type EventListFilter,
  type StateChangeEventType,
} from '../lib/types'

type FilterState = {
  object_type: '' | 'node' | 'target'
  severity: '' | '关注' | '告警' | '严重'
  event_type: '' | StateChangeEventType
  limit: string
  created_from: string
  created_to: string
  label: string
  notification_only: boolean
  recovery_only: boolean
  maintenance_only: boolean
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
  created_from: '',
  created_to: '',
  label: '',
  notification_only: false,
  recovery_only: false,
  maintenance_only: false,
}

const EVENT_TYPE_OPTIONS = Object.entries(STATE_CHANGE_EVENT_TYPE_LABELS) as Array<
  [StateChangeEventType, string]
>

function buildFilterQuery(filters: FilterState): EventListFilter {
  return {
    object_type: filters.object_type,
    severity: filters.severity,
    event_type: filters.event_type,
    limit: Number(filters.limit || DEFAULT_FILTERS.limit),
    created_from: filters.created_from,
    created_to: filters.created_to,
    label: filters.label,
    notification_only: filters.notification_only,
    recovery_only: filters.recovery_only,
    maintenance_only: filters.maintenance_only,
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

  function submitFilters(nextFilters: FilterState) {
    setState((current) => ({ ...current, loading: true, error: null }))
    setAppliedFilters({ ...nextFilters })
  }

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
            submitFilters(filters)
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
                {EVENT_TYPE_OPTIONS.map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
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

            <label className="summary-card">
              <span className="summary-card__label">开始时间</span>
              <input
                aria-label="开始时间"
                placeholder="2026-04-25T00:00:00Z"
                value={filters.created_from}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, created_from: event.target.value }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">结束时间</span>
              <input
                aria-label="结束时间"
                placeholder="2026-04-26T00:00:00Z"
                value={filters.created_to}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, created_to: event.target.value }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">标签</span>
              <input
                aria-label="标签"
                placeholder="edge"
                value={filters.label}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, label: event.target.value }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">通知相关</span>
              <input
                aria-label="仅看通知事件"
                type="checkbox"
                checked={filters.notification_only}
                onChange={(event) =>
                  setFilters((current) => ({
                    ...current,
                    notification_only: event.target.checked,
                  }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">恢复事件</span>
              <input
                aria-label="仅看恢复事件"
                type="checkbox"
                checked={filters.recovery_only}
                onChange={(event) =>
                  setFilters((current) => ({ ...current, recovery_only: event.target.checked }))
                }
              />
            </label>

            <label className="summary-card">
              <span className="summary-card__label">维护事件</span>
              <input
                aria-label="仅看维护事件"
                type="checkbox"
                checked={filters.maintenance_only}
                onChange={(event) =>
                  setFilters((current) => ({
                    ...current,
                    maintenance_only: event.target.checked,
                  }))
                }
              />
            </label>
          </div>

          <button type="submit">应用筛选</button>
          <button
            type="button"
            onClick={() => {
              setFilters({ ...DEFAULT_FILTERS })
              submitFilters(DEFAULT_FILTERS)
            }}
          >
            重置筛选
          </button>
        </form>
      </DetailSection>

      <DetailSection eyebrow="Timeline" title="事件流">
        <EventList events={state.events} />
      </DetailSection>
    </div>
  )
}
