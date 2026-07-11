import type { SegmentedItem } from '../../components/atoms'
import type { FilterSelectOption } from '../../components/filters'
import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type StateChangeEventType,
} from '../../lib/types'
import type { FilterState, TimeRange } from './types'

export const DEFAULT_LIMIT = 200
export const PAGE_SIZE = 20

export const OBJECT_TYPE_OPTIONS: FilterSelectOption[] = [
  { value: 'monitoring_instance', label: '监控实例' },
  { value: 'target', label: '目标' },
]
export const SEVERITY_OPTIONS: FilterSelectOption[] = [
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
]

export const INCIDENT_CLASS_OPTIONS: FilterSelectOption[] = [
  { value: 'connectivity', label: '连通性' },
  { value: 'certificate', label: '证书' },
  { value: 'performance', label: '性能' },
  { value: 'availability', label: '可用性' },
]

export const DEFAULT_FILTERS: FilterState = {
  object_type: '',
  severity: '',
  event_type: '',
  limit: String(DEFAULT_LIMIT),
  created_from: '',
  created_to: '',
  label: '',
  notification_only: false,
  recovery_only: false,
  maintenance_only: false,
  include_backfilled: false,
  time_range: 'custom',
  incident_class: '',
  keyword: '',
}

export const EVENT_TYPE_OPTIONS = Object.entries(STATE_CHANGE_EVENT_TYPE_LABELS) as Array<
  [StateChangeEventType, string]
>
export const EVENT_TYPE_SELECT_OPTIONS: FilterSelectOption[] = EVENT_TYPE_OPTIONS.map(
  ([value, label]) => ({ value, label }),
)

export const TIME_RANGE_TABS: SegmentedItem<TimeRange>[] = [
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: 'custom', label: '自定义' },
]

export const TIME_RANGE_DURATIONS_MS: Record<Exclude<TimeRange, 'custom'>, number> = {
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
}

export const TIME_RANGE_LABELS: Record<TimeRange, string> = {
  '24h': '近 24 小时',
  '7d': '近 7 天',
  '30d': '近 30 天',
  custom: '自定义',
}

export const ALLOWED_EVENT_TYPES = new Set<StateChangeEventType>(
  EVENT_TYPE_OPTIONS.map(([value]) => value),
)
export const ALLOWED_TIME_RANGES = new Set<TimeRange>(['24h', '7d', '30d', 'custom'])
