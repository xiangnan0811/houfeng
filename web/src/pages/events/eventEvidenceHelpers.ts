import type { HealthState } from '../../components/atoms'
import { formatDateTime } from '../../lib/format'
import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type StateChangeEventRecord,
} from '../../lib/types'
import { DEFAULT_LIMIT, TIME_RANGE_LABELS } from './eventsPageConstants'
import type { EventEvidenceItem, EventEvidenceLead, FilterState } from './types'

const INCIDENT_EVENT_TYPES = new Set<StateChangeEventRecord['event_type']>([
  'incident_started',
  'incident_escalated',
  'incident_recovered',
])

const MAINTENANCE_EVENT_TYPES = new Set<StateChangeEventRecord['event_type']>([
  'node_monitoring_maintenance_entered',
  'node_monitoring_maintenance_exited',
  'target_maintenance_entered',
  'target_maintenance_exited',
])

const RUNTIME_EVENT_TYPES = new Set<StateChangeEventRecord['event_type']>([
  'node_monitoring_paused',
  'node_monitoring_resumed',
  'target_paused',
  'target_resumed',
  'target_archived',
  'target_restored_to_paused',
])

export function isMaintenanceEventType(value: StateChangeEventRecord['event_type']) {
  return MAINTENANCE_EVENT_TYPES.has(value)
}

function eventTypeLabel(value: StateChangeEventRecord['event_type']) {
  return STATE_CHANGE_EVENT_TYPE_LABELS[value] ?? value
}

function objectTypeLabel(value: StateChangeEventRecord['object_type']) {
  if (value === 'node') return 'Node'
  if (value === 'target') return 'Target'
  return value
}

function severityRank(value: StateChangeEventRecord['severity']) {
  if (value === '严重') return 0
  if (value === '告警') return 1
  if (value === '关注') return 2
  if (value === '正常') return 4
  return 5
}

function eventTypeRank(value: StateChangeEventRecord['event_type']) {
  if (value === 'incident_started' || value === 'incident_escalated') return 0
  if (value === 'incident_recovered') return 1
  if (MAINTENANCE_EVENT_TYPES.has(value)) return 2
  if (RUNTIME_EVENT_TYPES.has(value)) return 3
  return 4
}

function eventMillis(value: string) {
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? 0 : parsed
}

export function eventEvidenceGlyphState(event: StateChangeEventRecord): HealthState {
  if (event.severity === '严重') return 'critical'
  if (event.severity === '告警') return 'alert'
  if (event.severity === '关注') return 'notice'
  if (event.severity === '正常') return 'normal'
  if (MAINTENANCE_EVENT_TYPES.has(event.event_type)) return 'maintenance'
  return 'offline'
}

function eventEvidenceRoute(event: StateChangeEventRecord) {
  if (event.object_type === 'node') return `/nodes/${event.object_id}`
  if (event.object_type === 'target') return `/targets/${event.object_id}`
  return '/events'
}

function eventEvidenceReason(event: StateChangeEventRecord) {
  if (event.summary.trim()) return event.summary
  if (INCIDENT_EVENT_TYPES.has(event.event_type)) return '异常生命周期事件，需要结合对象详情核对。'
  if (MAINTENANCE_EVENT_TYPES.has(event.event_type)) return '维护窗口事件，用来解释观测空窗。'
  if (RUNTIME_EVENT_TYPES.has(event.event_type)) return '运行控制事件，用来审计人为状态变更。'
  return '状态变更事件，需要结合对象详情核对。'
}

function eventEvidenceMeta(event: StateChangeEventRecord) {
  const severity = event.severity || '未标记严重度'
  const incident = event.incident_class.trim() || '无异常类型'
  return [severity, incident, formatDateTime(event.created_at)].join(' · ')
}

export function pickTopEventEvidence(events: StateChangeEventRecord[]): EventEvidenceItem | null {
  const candidates = [...events]
    .filter(
      (event) =>
        event.severity === '严重' ||
        event.severity === '告警' ||
        event.severity === '关注' ||
        INCIDENT_EVENT_TYPES.has(event.event_type) ||
        MAINTENANCE_EVENT_TYPES.has(event.event_type),
    )
    .sort((left, right) => {
      const severityDiff = severityRank(left.severity) - severityRank(right.severity)
      if (severityDiff !== 0) return severityDiff
      const typeDiff = eventTypeRank(left.event_type) - eventTypeRank(right.event_type)
      if (typeDiff !== 0) return typeDiff
      return eventMillis(right.created_at) - eventMillis(left.created_at)
    })

  const event = candidates[0]
  if (!event) return null

  return {
    event,
    title: `${objectTypeLabel(event.object_type)} · ${eventTypeLabel(event.event_type)}`,
    reason: eventEvidenceReason(event),
    meta: eventEvidenceMeta(event),
    route: eventEvidenceRoute(event),
    actionLabel: event.object_type === 'node' ? '查看 Node 证据' : '查看 Target 证据',
  }
}

export function describeEventFilterContext(filters: FilterState): string[] {
  const items: string[] = []
  if (filters.object_type) items.push(`对象 ${objectTypeLabel(filters.object_type)}`)
  if (filters.severity) items.push(`严重度 ${filters.severity}`)
  if (filters.event_type) items.push(`类型 ${eventTypeLabel(filters.event_type)}`)
  if (filters.limit !== String(DEFAULT_LIMIT)) items.push(`数量 ${filters.limit}`)
  if (filters.time_range !== 'custom') items.push(`时间 ${TIME_RANGE_LABELS[filters.time_range]}`)
  if (filters.created_from) items.push(`开始 ${filters.created_from}`)
  if (filters.created_to) items.push(`结束 ${filters.created_to}`)
  if (filters.label) items.push(`标签 ${filters.label}`)
  if (filters.notification_only) items.push('仅通知')
  if (filters.recovery_only) items.push('仅恢复')
  if (filters.maintenance_only) items.push('仅维护')
  if (filters.include_backfilled) items.push('包含补传')
  return items
}

export function buildEventEvidenceLead(args: {
  events: StateChangeEventRecord[]
  filters: FilterState
  hasActiveFilters: boolean
  topEvidence: EventEvidenceItem | null
}): EventEvidenceLead {
  const { events, filters, hasActiveFilters, topEvidence } = args

  if (events.length === 0 && hasActiveFilters) {
    return {
      eyebrow: '当前筛选',
      title: '没有匹配当前诊断条件',
      description: '当前 URL 筛选没有返回事件。先清空或调整条件，再继续核对诊断时间线。',
      actionKind: 'clear',
      actionLabel: '清空事件筛选',
      tone: 'offline',
    }
  }

  if (events.length === 0) {
    return {
      eyebrow: '时间线稳定',
      title: '事件时间线当前稳定',
      description: '当前事件流为空。可以切换到近 24 小时窗口，或回到工作台查看资产与观测入口。',
      actionKind: 'timeRange',
      actionLabel: '查看 24h 事件',
      actionHref: '/events?time_range=24h',
      tone: 'normal',
    }
  }

  if (topEvidence?.event.severity === '严重') {
    return {
      eyebrow: '优先诊断',
      title: '先核对严重事件时间线',
      description: '严重事件会影响资产和服务入口判断，先从优先事件进入对象详情，再回到事件流追溯上下文。',
      actionKind: 'event',
      actionLabel: topEvidence.actionLabel,
      actionHref: topEvidence.route,
      tone: 'critical',
    }
  }

  if (topEvidence?.event.severity === '告警' || topEvidence?.event.severity === '关注') {
    return {
      eyebrow: '诊断线索',
      title: '当前切片存在需要核对的事件',
      description: '事件流里有告警或关注级别变化，优先核对对象详情，再结合筛选条件追溯前后状态。',
      actionKind: 'event',
      actionLabel: topEvidence.actionLabel,
      actionHref: topEvidence.route,
      tone: topEvidence.event.severity === '告警' ? 'alert' : 'notice',
    }
  }

  if (
    filters.maintenance_only ||
    (topEvidence ? isMaintenanceEventType(topEvidence.event.event_type) : false)
  ) {
    return {
      eyebrow: '维护上下文',
      title: '当前切片用于解释维护窗口',
      description: '维护事件帮助区分人为窗口与真实故障，继续沿对象详情核对观测空窗。',
      actionKind: topEvidence ? 'event' : 'filters',
      actionLabel: topEvidence ? topEvidence.actionLabel : '调整筛选',
      actionHref: topEvidence?.route,
      tone: 'maintenance',
    }
  }

  return {
    eyebrow: hasActiveFilters ? '筛选切片' : '时间线稳定',
    title: hasActiveFilters ? '当前诊断切片可继续追溯' : '事件时间线当前稳定',
    description: hasActiveFilters
      ? '当前 URL 筛选已承接到事件流，可以继续打开高级筛选扩展审计范围。'
      : '当前加载事件没有严重、告警或关注级别线索。继续从工作台、Node 或 Target 入口查看上游证据。',
    actionKind: 'filters',
    actionLabel: '调整筛选',
    tone: 'normal',
  }
}
