import type { StateChangeEventRecord, StateChangeEventType } from '../../lib/types'

export type TimeRange = '24h' | '7d' | '30d' | 'custom'

export type FilterState = {
  object_type: '' | 'monitoring_instance' | 'target'
  object_id: string
  severity: '' | '关注' | '告警' | '严重'
  event_type: '' | StateChangeEventType
  limit: string
  created_from: string
  created_to: string
  label: string
  notification_only: boolean
  recovery_only: boolean
  maintenance_only: boolean
  include_backfilled: boolean
  time_range: TimeRange
  incident_class: string
  keyword: string
}

export type EventsPageState = {
  loading: boolean
  error: string | null
  events: StateChangeEventRecord[]
  exhausted: boolean
}
