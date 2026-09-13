import type { RecordSubjectKind, SubjectActivityEventKind, SubjectActivityItem, SubjectActivitySourceKind } from '../lib/types'

export type TimelineChannel = 'human' | 'system' | 'evidence'

export const TIMELINE_CHANNEL_LABELS: Record<TimelineChannel, string> = {
  human: '人工记录',
  system: '系统事实',
  evidence: '不可变证据',
}

export const SUBJECT_KIND_LABELS: Record<RecordSubjectKind, string> = {
  vps: 'VPS',
  monitoring_instance: '监控实例',
  target: '入口探测',
}

export const SOURCE_KIND_LABELS: Record<SubjectActivitySourceKind, string> = {
  record_domain: '人工记录',
  evidence_snapshot: '证据快照',
  asset_history: '资产事实',
  monitoring_event: '监控事件',
  command_audit: '命令审计',
}

export const SOURCE_STATE_LABELS: Record<string, string> = {
  ready: '就绪',
  stale: '过期',
  unavailable: '不可用',
  degraded: '降级',
}

export const HUMAN_EVENT_KINDS = new Set<SubjectActivityEventKind>([
  'record_created',
  'record_revised',
  'record_restored',
  'record_archived',
  'record_unarchived',
  'record_owner_changed',
  'record_participant_changed',
  'record_follow_up_changed',
  'comment_created',
  'comment_edited',
  'comment_redacted',
  'action_created',
  'action_updated',
  'action_completed',
  'action_cancelled',
  'action_reopened',
])

export function timelineChannel(item: SubjectActivityItem): TimelineChannel {
  if (item.event_kind === 'evidence_captured' || item.source_kind === 'evidence_snapshot') {
    return 'evidence'
  }
  if (item.source_kind === 'record_domain' || HUMAN_EVENT_KINDS.has(item.event_kind)) {
    return 'human'
  }
  return 'system'
}
