import { Link, useLocation } from 'react-router-dom'

import { Badge, Button, Timestamp } from '../../../components/atoms'
import { capabilityFlagsForSession } from '../../../lib/auth-client'
import { useAuth } from '../../../lib/auth-context'
import type { RecordWorkspaceState } from '../hooks/useRecordDraft'
import { comparisonEntryHref, comparisonSubjectsFromSources } from '../compare/comparisonQueryState'
import { RECORD_LIFECYCLE_LABELS, RECORD_TYPE_LABELS } from '../recordLabels'
import { BUSINESS_STATUS_LABELS } from '../recordWorkspaceModel'
import { displaySubject, impactLevelLabel, impactTone, statusGroupTone, subjectLabel } from './recordPresentation'

type RecordWorkspaceHeaderProps = {
  state: RecordWorkspaceState
  recordId?: string | undefined
  revisionId?: string | undefined
  /** 已规范化的记录内链接生成器，负责携带主体返回参数。 */
  recordHref: (path: string) => string
  subjectReturnHref: string | null
  subjectReturnState: unknown
  ownerLabel: string
  /** 附件仍在上传或安全检查：发布要等它们进入草稿，否则会漏掉或被后端拒绝。 */
  uploading?: boolean
  onSave: () => void
  onPublish: () => void
  onExport: () => void
  onImport: () => void
}

function RecordMark() {
  return (
    <span className="record-identity__mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        <path d="M7 3.5h7.5L19 8v12.5H7z" />
        <path d="M14.5 3.5V8H19M10 12h6M10 15.5h6" />
      </svg>
    </span>
  )
}

function syncText(state: RecordWorkspaceState, uploading: boolean): string {
  if (state.saving) return '正在保存草稿'
  if (uploading) return '附件上传中，完成后可发布'
  if (state.dirty) return '本地未同步'
  if (state.draft) return '草稿已同步'
  return '尚未创建草稿'
}

function syncTone(state: RecordWorkspaceState, uploading: boolean): string {
  if (state.saving || uploading) return 'record-identity__sync--saving'
  if (state.dirty) return 'record-identity__sync--dirty'
  return state.draft ? 'record-identity__sync--synced' : ''
}

export function RecordWorkspaceHeader({
  state,
  recordId,
  revisionId,
  recordHref,
  subjectReturnHref,
  subjectReturnState,
  ownerLabel,
  uploading = false,
  onSave,
  onPublish,
  onExport,
  onImport,
}: RecordWorkspaceHeaderProps) {
  const auth = useAuth()
  const { comparison, portability } = capabilityFlagsForSession(auth)
  const location = useLocation()
  const { mode, payload } = state
  const editable = mode === 'new' || mode === 'edit'
  const published = mode === 'revision' ? state.revision : state.record?.current
  const subject = displaySubject(published, payload)
  const title = editable
    ? (mode === 'new' ? '新建运维记录' : '编辑运维记录')
    : payload.title || '运维记录'
  const businessStatus = payload.business_status ? BUSINESS_STATUS_LABELS[payload.business_status] : ''
  const archived = mode === 'read' && state.record?.lifecycle === 'archived'
  const subjectReturn = subjectReturnHref ? (
    <Link className="text-link" to={subjectReturnHref} state={subjectReturnState}>返回主体</Link>
  ) : null

  return (
    <header className="page__head record-identity">
      <div className="record-identity__lead">
        <RecordMark />
        <div className="record-identity__copy">
          <div className="record-identity__title-row">
            <h1 className="page__title">{title}</h1>
            {editable ? null : (
              <div className="record-identity__badges">
                {mode === 'revision' && published ? (
                  <Badge variant="info" tone="notice">历史修订 #{published.revision_no}</Badge>
                ) : null}
                <Badge variant="info">{RECORD_TYPE_LABELS[payload.record_type]}</Badge>
                {businessStatus ? (
                  <Badge variant="info" tone={statusGroupTone(published?.status_group)}>{businessStatus}</Badge>
                ) : null}
                {payload.impact_level ? (
                  <Badge variant="info" tone={impactTone(payload.impact_level)}>影响 {impactLevelLabel(payload.impact_level)}</Badge>
                ) : null}
                {archived && state.record ? (
                  <Badge variant="info" tone="maintenance">{RECORD_LIFECYCLE_LABELS[state.record.lifecycle]}</Badge>
                ) : null}
              </div>
            )}
          </div>
          {editable ? (
            <p className="record-identity__meta record-identity__meta--sync">
              <span className={['record-identity__sync', syncTone(state, uploading)].filter(Boolean).join(' ')} role="status">
                {syncText(state, uploading)}{state.message ? ` · ${state.message}` : ''}
              </span>
              {mode === 'edit' && payload.title ? <span className="record-identity__editing">{payload.title}</span> : null}
              {subjectReturn}
            </p>
          ) : (
            <div className="record-identity__meta">
              <dl className="record-identity__facts" aria-label="记录身份">
                {subject ? (
                  <div className="record-identity__meta-item"><dt>主体</dt><dd>{subjectLabel(subject)}</dd></div>
                ) : null}
                {published ? (
                  <div className="record-identity__meta-item">
                    <dt>{mode === 'revision' ? '修订于' : '修订'}</dt>
                    <dd>
                      {mode === 'revision'
                        ? <Timestamp value={published.created_at} mode="both" />
                        : <span className="mono">#{published.revision_no}</span>}
                    </dd>
                  </div>
                ) : null}
                {ownerLabel ? (
                  <div className="record-identity__meta-item"><dt>负责人</dt><dd>{ownerLabel}</dd></div>
                ) : null}
                {mode === 'read' && state.record ? (
                  <div className="record-identity__meta-item">
                    <dt>更新</dt>
                    <dd><Timestamp value={state.record.updated_at} mode="both" /></dd>
                  </div>
                ) : null}
              </dl>
              {state.message ? <span role="status">{state.message}</span> : null}
              {subjectReturn}
            </div>
          )}
        </div>
      </div>
      <div className="page__actions">
        {mode === 'edit' && recordId ? (
          <Link className="btn md secondary" to={recordHref(`/records/${recordId}`)} state={location.state}>阅读</Link>
        ) : null}
        {mode === 'revision' && recordId ? (
          <Link className="btn md secondary" to={recordHref(`/records/${recordId}`)} state={location.state}>当前版本</Link>
        ) : null}
        {comparison && mode === 'revision' && recordId && revisionId ? (
          <Link
            className="btn md secondary"
            to={comparisonEntryHref({
              subjects: comparisonSubjectsFromSources(payload.subjects),
              items: [{ record_id: recordId, revision_id: revisionId }],
            })}
            state={location.state}
          >
            横向比较
          </Link>
        ) : null}
        {portability && (mode === 'read' || mode === 'revision') && recordId ? (
          <Button size="md" variant="secondary" onClick={onExport}>导出</Button>
        ) : null}
        {portability && (mode === 'read' || mode === 'revision' || mode === 'new') ? (
          <Button size="md" variant="secondary" onClick={onImport}>导入</Button>
        ) : null}
        {mode === 'read' && recordId && state.record?.capabilities.update ? (
          <Link className="btn md primary" to={recordHref(`/records/${recordId}/edit`)} state={location.state}>编辑</Link>
        ) : null}
        {editable ? (
          <>
            <Button size="md" variant="secondary" disabled={state.saving} onClick={onSave}>保存草稿</Button>
            <Button size="md" disabled={state.publishing || uploading} onClick={onPublish}>发布修订</Button>
          </>
        ) : null}
      </div>
    </header>
  )
}
