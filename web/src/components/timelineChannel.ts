import type { SubjectActivityItem } from '../lib/types'

export type TimelineChannel = 'human' | 'system' | 'evidence'

const HUMAN_KINDS = new Set([
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
  if (item.source_kind === 'record_domain' || HUMAN_KINDS.has(item.event_kind)) {
    return 'human'
  }
  return 'system'
}
