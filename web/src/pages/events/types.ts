import type { StateChangeEventRecord, StateChangeEventType } from '../../lib/types'

export type TimeRange = '24h' | '7d' | '30d' | 'custom'

export type FilterState = {
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
  include_backfilled: boolean
  // Time range segmented control. 'custom' preserves the original behavior
  // (user-controlled date inputs) — keep that as default so first load keeps
  // the previous "all recent events" semantics.
  time_range: TimeRange
}

export type EventsPageState = {
  loading: boolean
  error: string | null
  events: StateChangeEventRecord[]
  // True after a load-more fetch returns fewer rows than requested — meaning
  // backend has no more events to give for the current filter.
  exhausted: boolean
}

export type EventEvidenceLeadTone =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'offline'

export type EventEvidenceLead = {
  eyebrow: string
  title: string
  description: string
  actionKind: 'filters' | 'clear' | 'event' | 'timeRange'
  actionLabel: string
  actionHref?: string
  tone: EventEvidenceLeadTone
}

export type EventEvidenceItem = {
  event: StateChangeEventRecord
  title: string
  reason: string
  meta: string
  route: string
  actionLabel: string
}
