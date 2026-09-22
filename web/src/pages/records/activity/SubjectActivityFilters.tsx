import type { SubjectActivityEventKind, SubjectActivitySourceKind, SubjectActivityView } from '../../../lib/types'
import type { SubjectActivityFilters } from './activityQueryState'

type Props = {
  value: SubjectActivityFilters
  onChange: (next: SubjectActivityFilters) => void
  disabled?: boolean
  view?: SubjectActivityView
}

const EVENT_KIND_LABELS: Record<SubjectActivityEventKind, string> = {
  record_created: '记录创建',
  record_revised: '记录修订',
  record_restored: '记录恢复',
  record_archived: '记录归档',
  record_unarchived: '取消归档',
  record_owner_changed: '负责人变更',
  record_participant_changed: '参与者变更',
  record_follow_up_changed: '跟进变更',
  comment_created: '评论创建',
  comment_edited: '评论编辑',
  comment_redacted: '评论撤回',
  action_created: '待办创建',
  action_updated: '待办更新',
  action_completed: '待办完成',
  action_cancelled: '待办取消',
  action_reopened: '待办重开',
  evidence_captured: '证据捕获',
  asset_fact_changed: '资产事实变更',
  monitoring_state_changed: '监控状态变更',
  command_executed: '命令执行',
}

const SOURCE_KIND_LABELS: Record<SubjectActivitySourceKind, string> = {
  record_domain: '人工记录',
  evidence_snapshot: '证据快照',
  asset_history: '资产事实',
  monitoring_event: '监控事件',
  command_audit: '命令审计',
}

/** Mirrors backend recordsViewEventKinds: comments and actions stay on activity. */
const RECORDS_VIEW_EVENT_KINDS: readonly SubjectActivityEventKind[] = [
  'record_created',
  'record_revised',
  'record_restored',
  'record_archived',
  'record_unarchived',
  'record_owner_changed',
  'record_participant_changed',
  'record_follow_up_changed',
]

const ACTIVITY_SOURCE_KINDS: readonly SubjectActivitySourceKind[] = [
  'record_domain',
  'evidence_snapshot',
  'asset_history',
  'monitoring_event',
  'command_audit',
]

function eventKindOptions(view: SubjectActivityView): SubjectActivityEventKind[] {
  if (view === 'records') return [...RECORDS_VIEW_EVENT_KINDS]
  if (view === 'evidence') return ['evidence_captured']
  return (Object.keys(EVENT_KIND_LABELS) as SubjectActivityEventKind[])
}

function sourceKindOptions(view: SubjectActivityView): SubjectActivitySourceKind[] {
  if (view === 'records') return ['record_domain']
  if (view === 'evidence') return ['evidence_snapshot']
  return [...ACTIVITY_SOURCE_KINDS]
}

function withCurrentOption<T extends string>(allowed: readonly T[], current: T | undefined): readonly T[] {
  if (current && !allowed.includes(current)) return [...allowed, current]
  return allowed
}


/**
 * Lightweight filter controls for subject activity. Changing any filter must
 * clear the cursor at the page layer — this component only edits filter state.
 * The URL codec still accepts the full vocabulary; interactive options match
 * the backend view contract so records/evidence cannot pick empty intersections.
 */
export function SubjectActivityFilters({
  value,
  onChange,
  disabled = false,
  view = 'activity',
}: Props) {
  const showEventKind = view !== 'evidence' || Boolean(value.event_kind?.[0])
  const allowedEventKinds = eventKindOptions(view)
  const allowedSourceKinds = sourceKindOptions(view)
  const eventKinds = withCurrentOption(allowedEventKinds, value.event_kind?.[0])
  const sourceKinds = withCurrentOption(allowedSourceKinds, value.source?.[0])

  return (
    <div className="subject-activity-filters">
      <label className="subject-activity-filters__field">
        <span>来源</span>
        <select
          disabled={disabled}
          value={value.source?.[0] ?? ''}
          onChange={(event) => {
            const next = event.target.value
            if (!next) {
              const rest = { ...value }
              delete rest.source
              onChange(rest)
              return
            }
            onChange({
              ...value,
              source: [next as SubjectActivitySourceKind],
            })
          }}
        >
          <option value="">全部来源</option>
          {sourceKinds.map((kind) => (
            <option key={kind} value={kind} disabled={!allowedSourceKinds.includes(kind)}>{SOURCE_KIND_LABELS[kind]}</option>
          ))}
        </select>
      </label>
      {showEventKind ? (
        <label className="subject-activity-filters__field">
          <span>事件类型</span>
          <select
            disabled={disabled}
            value={value.event_kind?.[0] ?? ''}
            onChange={(event) => {
              const next = event.target.value
              if (!next) {
                const rest = { ...value }
                delete rest.event_kind
                onChange(rest)
                return
              }
              onChange({
                ...value,
                event_kind: [next as SubjectActivityEventKind],
              })
            }}
          >
            <option value="">全部类型</option>
            {eventKinds.map((kind) => (
              <option key={kind} value={kind} disabled={!allowedEventKinds.includes(kind)}>{EVENT_KIND_LABELS[kind]}</option>
            ))}
          </select>
        </label>
      ) : null}
      <label className="subject-activity-filters__field">
        <span>版本</span>
        <select
          disabled={disabled}
          value={value.versions ?? 'history'}
          onChange={(event) => {
            const next = event.target.value as 'history' | 'current'
            if (next === 'history') {
              const rest = { ...value }
              delete rest.versions
              onChange(rest)
              return
            }
            onChange({
              ...value,
              versions: next,
            })
          }}
        >
          <option value="history">完整历史</option>
          <option value="current">当前有效</option>
        </select>
      </label>
    </div>
  )
}
