import { Link } from 'react-router-dom'

import { Badge, Button, MonoDigits } from '../../components/atoms'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type StateChangeEventRecord } from '../../lib/types'
import { DEFAULT_LIMIT, TIME_RANGE_LABELS } from './eventsPageConstants'
import type { FilterState } from './types'

type EventsSupportSurfaceProps = {
  events: StateChangeEventRecord[]
  filters: FilterState
  hasActiveFilters: boolean
  onOpenFilters: () => void
}

function objectTypeLabel(value: FilterState['object_type']) {
  if (value === 'node') return '节点'
  if (value === 'target') return '目标'
  return '全部对象'
}

function activeFilterCount(filters: FilterState) {
  let count = 0
  if (filters.object_type) count += 1
  if (filters.severity) count += 1
  if (filters.event_type) count += 1
  if (filters.limit !== String(DEFAULT_LIMIT)) count += 1
  if (filters.time_range !== 'custom') count += 1
  if (filters.created_from) count += 1
  if (filters.created_to) count += 1
  if (filters.label) count += 1
  if (filters.notification_only) count += 1
  if (filters.recovery_only) count += 1
  if (filters.maintenance_only) count += 1
  if (filters.include_backfilled) count += 1
  return count
}

function timeContext(filters: FilterState) {
  if (filters.time_range !== 'custom') return TIME_RANGE_LABELS[filters.time_range]
  if (filters.created_from || filters.created_to) return '自定义时间'
  return '未限定时间'
}

function severityContext(filters: FilterState) {
  const severity = filters.severity || '全部严重度'
  const eventType = filters.event_type
    ? STATE_CHANGE_EVENT_TYPE_LABELS[filters.event_type]
    : '全部事件类型'
  return `${severity} · ${eventType}`
}

export function EventsSupportSurface({
  events,
  filters,
  hasActiveFilters,
  onOpenFilters,
}: EventsSupportSurfaceProps) {
  const filterCount = activeFilterCount(filters)

  return (
    <section className="page-panel observability-support observability-support--events">
      <div className="observability-support__header">
        <div>
          <p className="observability-support__eyebrow">DIAGNOSTIC TIMELINE</p>
          <h2 className="observability-support__title">诊断时间线</h2>
          <p className="observability-support__description">
            事件页承接 Dashboard、VPS、Node 和 Target 的深链，用于审计状态变化和定位处理路径。
          </p>
        </div>
        <div className="observability-support__scope" aria-label="当前事件筛选范围">
          <span>{hasActiveFilters ? `${filterCount} 项筛选` : '默认事件流'}</span>
          <strong>
            <MonoDigits>{events.length}</MonoDigits>
          </strong>
        </div>
      </div>

      <div className="observability-support__grid" aria-label="事件诊断上下文">
        <article className="observability-support-lane observability-support-lane--normal">
          <div className="observability-support-lane__head">
            <span>当前切片</span>
            <Badge variant="count" tone={events.length > 0 ? 'normal' : 'neutral'}>
              <MonoDigits>{events.length}</MonoDigits>
            </Badge>
          </div>
          <p>{hasActiveFilters ? '正在查看由 URL 固定的事件上下文。' : '默认显示最近状态变化，完整条件在筛选抽屉调整。'}</p>
          <div className="observability-support-lane__actions">
            <Button variant="secondary" size="sm" onClick={onOpenFilters}>
              调整筛选
            </Button>
            <Link className="observability-support-link" to="/events?time_range=24h">
              近 24 小时
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--asset">
          <div className="observability-support-lane__head">
            <span>对象上下文</span>
            <Badge variant="info" tone="neutral">{objectTypeLabel(filters.object_type)}</Badge>
          </div>
          <p>对象类型决定后续处理入口：Node 关联 VPS 证据，Target 关联服务入口证据。</p>
          <div className="observability-support-lane__actions">
            <Link className="observability-support-link" to="/nodes?abnormal=1">
              异常节点
            </Link>
            <Link className="observability-support-link" to="/targets?abnormal=1">
              异常目标
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--alert">
          <div className="observability-support-lane__head">
            <span>严重度 / 类型</span>
            <Badge variant="info" tone={filters.severity === '严重' ? 'critical' : 'neutral'}>
              {filters.severity || '全部'}
            </Badge>
          </div>
          <p>{severityContext(filters)}</p>
          <div className="observability-support-lane__actions">
            <Link className="observability-support-link" to="/events?severity=严重">
              严重事件
            </Link>
            <Link className="observability-support-link" to="/events?recovery_only=1">
              恢复事件
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--maintenance">
          <div className="observability-support-lane__head">
            <span>时间 / 来源</span>
            <Badge variant="info" tone={filters.maintenance_only ? 'maintenance' : 'neutral'}>
              {timeContext(filters)}
            </Badge>
          </div>
          <p>{filters.maintenance_only ? '当前只看维护上下文事件。' : '时间窗口保留在 URL 中，API 请求时再转换为实际时间。'}</p>
          <div className="observability-support-lane__actions">
            <Link className="observability-support-link" to="/">
              工作台
            </Link>
            <Link className="observability-support-link" to="/events?maintenance_only=1">
              维护事件
            </Link>
          </div>
        </article>
      </div>
    </section>
  )
}
