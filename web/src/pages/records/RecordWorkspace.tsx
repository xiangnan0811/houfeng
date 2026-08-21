import { lazy, Suspense, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { RecordActionPanel } from '../../components/RecordActionPanel'
import { RecordCommentThread } from '../../components/RecordCommentThread'
import { RecordRevisionCollaborationControls } from '../../components/RecordRevisionCollaborationControls'
import { RecordWatchControl } from '../../components/RecordWatchControl'
import { PageState } from '../../components/PageState'
import { Button, Input, Select } from '../../components/atoms'
import { useAuth } from '../../lib/auth-context'
import {
  createRecordAction,
  createRecordComment,
  editRecordComment,
  getRecordWatch,
  listRecordActions,
  listRecordComments,
  redactRecordComment,
  setRecordWatch,
  transitionRecordAction,
  updateRecordAction,
} from '../../lib/recordCollaborationApi'
import { ApiError } from '../../lib/apiRequest'
import type { RecordAction, RecordBusinessStatus, RecordComment, RecordType, RecordWatch } from '../../lib/types'
import type { RecordCollaborationSurfaceState } from '../../components/RecordCollaborationState'
import { MarkdownSourceEditor } from './editor/MarkdownSourceEditor'

const MarkdownPreview = lazy(() => import('./editor/MarkdownPreview').then((module) => ({
  default: module.MarkdownPreview,
})))
const RecordExportPanel = lazy(() => import('./RecordExportPanel').then((module) => ({
  default: module.RecordExportPanel,
})))
const RecordImportPanel = lazy(() => import('./RecordImportPanel').then((module) => ({
  default: module.RecordImportPanel,
})))
import { PromoteChecklistActionDialog } from './editor/PromoteChecklistActionDialog'
import { RecordConflictResolver } from './editor/RecordConflictResolver'
import { decodeRenderModelStatusV1, insertMaterialToken } from '../../lib/documentMarkdown'
import { RecordMaterialDrawer, type RecordMaterialItem } from './editor/RecordMaterialDrawer'
import { RecordOutline } from './editor/RecordOutline'
import { RecordSaveImpact } from './editor/RecordSaveImpact'
import { RevisionDiff } from './editor/RevisionDiff'
import { useRecordDraft, type RecordWorkspaceMode } from './hooks/useRecordDraft'
import { comparisonEntryHref, comparisonSubjectsFromSources } from './compare/comparisonQueryState'
import { labelOptions, RECORD_TYPE_LABELS } from './recordLabels'
import {
  applyRecordTypeChange,
  BUSINESS_STATUS_LABELS,
  businessStatusesForType,
  insertMarkdownSnippet,
  patchPrimarySubject,
  templateMarkdownForType,
  typeSupportsBusinessStatus,
} from './recordWorkspaceModel'

type RecordWorkspaceProps = {
  mode: RecordWorkspaceMode
  recordId?: string
  revisionId?: string
}

export function RecordWorkspace({ mode, recordId, revisionId }: RecordWorkspaceProps) {
  const { user } = useAuth()
  const navigate = useNavigate()
  const userId = user?.user_id ?? ''
  const { state, commands } = useRecordDraft({
    mode,
    userId,
    ...(recordId ? { recordId } : {}),
    ...(revisionId ? { revisionId } : {}),
  })
  const [layout, setLayout] = useState<'edit' | 'split' | 'preview'>('split')
  const [materialsOpen, setMaterialsOpen] = useState(false)
  const [promoteOpen, setPromoteOpen] = useState(false)
  const [restoreReason, setRestoreReason] = useState('restore known good')
  const [actions, setActions] = useState<RecordAction[]>([])
  const [comments, setComments] = useState<RecordComment[]>([])
  const [watch, setWatch] = useState<RecordWatch | null>(null)
  const [collabState, setCollabState] = useState<RecordCollaborationSurfaceState>(
    !recordId || mode === 'new' ? 'empty' : 'loading',
  )
  const [collabBusy, setCollabBusy] = useState(false)
  const editable = mode === 'new' || mode === 'edit'
  const title = mode === 'new' ? '新建运维记录' : mode === 'edit' ? '编辑运维记录' : mode === 'revision' ? '历史修订' : '运维记录'

  const members = (() => {
    const options = new Map<string, string>()
    if (user) options.set(user.user_id, user.display_name || user.username)
    if (state.payload.owner_id) options.set(state.payload.owner_id, state.payload.owner_id)
    for (const participant of state.record?.current.participants ?? state.revision?.participants ?? []) {
      options.set(participant.participant_id, participant.display_name || participant.participant_id)
    }
    return [...options.entries()].map(([id, label]) => ({ id, label }))
  })()
  const evidenceIds = mode === 'revision'
    ? [...(state.revision?.evidence_snapshot_ids ?? [])]
    : [...(state.record?.current.evidence_snapshot_ids ?? [])]
  const materials: RecordMaterialItem[] = [
    ...state.payload.attachment_ids.map((id) => ({
      kind: 'attachment' as const,
      id,
      label: `附件 ${id}`,
      available: true,
    })),
    ...evidenceIds.map((id) => ({
      kind: 'evidence' as const,
      id,
      label: `证据 ${id}`,
      available: true,
    })),
  ]

  useEffect(() => {
    if (!recordId || mode === 'new') {
      return
    }
    let active = true
    Promise.all([
      listRecordActions(recordId),
      listRecordComments(recordId),
      getRecordWatch(recordId),
    ]).then(([actionList, commentList, nextWatch]) => {
      if (!active) return
      setActions(actionList.items)
      setComments(commentList.comments)
      setWatch(nextWatch)
      setCollabState('ready')
    }).catch((error: unknown) => {
      if (!active) return
      if (error instanceof ApiError && (error.status === 403 || error.status === 404)) setCollabState('revoked')
      else setCollabState('error')
    })
    return () => {
      active = false
    }
  }, [mode, recordId])

  useEffect(() => {
    if (mode === 'new' && state.publishedRecordId) {
      navigate(`/records/${state.publishedRecordId}`, { replace: true })
    }
  }, [mode, navigate, state.publishedRecordId])

  useEffect(() => {
    if (mode === 'revision' && state.restoredToRecordId) {
      navigate(`/records/${state.restoredToRecordId}`, { replace: true })
    }
  }, [mode, navigate, state.restoredToRecordId])

  if (state.status === 'loading') return <PageState kind="loading" title="正在读取运维记录" />
  if (state.status === 'empty') return <PageState kind="empty" title="记录不存在" description="没有可打开的记录。" />
  if (state.status === 'revoked') return <PageState kind="empty" title="记录访问已撤销" description="当前内容已收起。" />
  if (state.status === 'error') return <PageState kind="error" title="记录工作区暂不可用" description={state.message} />

  const subject = state.payload.subjects[0]
  const showsPublishedRevision = mode === 'read' || mode === 'revision'
  const previewModel = showsPublishedRevision ? state.revision?.render_model : undefined
  // While editing, the body differs from the published revision, so a stale status
  // from that revision must not be reported against the draft being written.
  const previewModelStatus = showsPublishedRevision
    ? decodeRenderModelStatusV1(state.revision?.render_model_status)
    : undefined

  return (
    <div className="page-stack record-workspace animate-in">
      <div className="page-header">
        <div>
          <div className="page-eyebrow">RECORDS · MARKDOWN</div>
          <h1 className="page-title">{state.payload.title || title}</h1>
          <p className="page-subtitle">人写的运维记录与系统活动分离；正式保存写入新的修订。</p>
          <p role="status">
            {state.saving ? '正在保存草稿' : state.dirty ? '本地未同步' : state.draft ? '草稿已同步' : '尚未创建草稿'}
            {state.message ? ` · ${state.message}` : ''}
          </p>
        </div>
        <div className="page-form-actions">
          {recordId ? <Link className="btn sm secondary" to={`/records/${recordId}`}>阅读</Link> : null}
          {recordId && mode === 'read' && state.record?.capabilities.update ? (
            <Link className="btn sm secondary" to={`/records/${recordId}/edit`}>编辑</Link>
          ) : null}
          {editable ? (
            <>
              <Button size="lg" variant="secondary" disabled={state.saving} onClick={() => void commands.saveDraft()}>保存草稿</Button>
              <Button size="lg" disabled={state.publishing} onClick={() => void commands.publish()}>发布修订</Button>
            </>
          ) : null}
          {mode === 'revision' && recordId && revisionId ? (
            <Link className="btn lg secondary" to={comparisonEntryHref({
              subjects: comparisonSubjectsFromSources(state.payload.subjects),
              items: [{ record_id: recordId, revision_id: revisionId }],
            })}>
              横向比较
            </Link>
          ) : null}
          {mode === 'revision' ? (
            <Button size="lg" disabled={state.publishing} onClick={() => void commands.restore(restoreReason)}>恢复为新修订</Button>
          ) : null}
        </div>
      </div>

      {editable ? (
        <form className="vps-create-form" onSubmit={(event) => event.preventDefault()}>
          <div className="vps-create-form__row">
            <Input label="标题" value={state.payload.title} onChange={(event) => commands.patchPayload({ title: event.target.value })} />
            <Select
              label="记录类型"
              value={state.payload.record_type}
              onChange={(event) => commands.patchPayload(applyRecordTypeChange(event.target.value as RecordType))}
            >
              {labelOptions(RECORD_TYPE_LABELS).map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </Select>
            {typeSupportsBusinessStatus(state.payload.record_type) ? (
              <Select
                label="业务状态"
                value={state.payload.business_status}
                onChange={(event) => commands.patchPayload({ business_status: event.target.value as RecordBusinessStatus })}
              >
                {businessStatusesForType(state.payload.record_type).map((status) => (
                  <option key={status} value={status}>{BUSINESS_STATUS_LABELS[status]}</option>
                ))}
              </Select>
            ) : (
              <Input label="影响级别" value={state.payload.impact_level} onChange={(event) => commands.patchPayload({ impact_level: event.target.value })} />
            )}
          </div>
          <div className="vps-create-form__row">
            {typeSupportsBusinessStatus(state.payload.record_type) ? (
              <Input label="影响级别" value={state.payload.impact_level} onChange={(event) => commands.patchPayload({ impact_level: event.target.value })} />
            ) : null}
            <Input
              label="主体 ID"
              value={subject?.source_id ?? ''}
              onChange={(event) => commands.patchPayload({
                subjects: patchPrimarySubject(state.payload.subjects, event.target.value),
              })}
            />
            <Select
              label="可见性"
              value={state.payload.visibility.kind}
              onChange={(event) => commands.patchPayload({
                visibility: { ...state.payload.visibility, kind: event.target.value as 'project' | 'restricted' },
              })}
            >
              <option value="project">项目内</option>
              <option value="restricted">受限</option>
            </Select>
            <Input label="保存原因" value={state.payload.save_reason} onChange={(event) => commands.patchPayload({ save_reason: event.target.value })} />
          </div>
          <RecordRevisionCollaborationControls
            state="ready"
            members={members}
            ownerId={state.payload.owner_id}
            participantIds={state.payload.participant_ids}
            followUpAt={toDateTimeLocal(state.payload.follow_up_at)}
            onOwnerChange={(ownerId) => commands.patchPayload({ owner_id: ownerId })}
            onParticipantToggle={(participantId, selected) => commands.patchPayload({
              participant_ids: selected
                ? [...new Set([...state.payload.participant_ids, participantId])]
                : state.payload.participant_ids.filter((id) => id !== participantId),
            })}
            onFollowUpChange={(followUpAt) => commands.patchPayload({ follow_up_at: followUpAt ? new Date(followUpAt).toISOString() : null })}
          />
        </form>
      ) : (
        <section className="card">
          <p>类型 {state.payload.record_type} · 影响 {state.payload.impact_level}</p>
          <p>主体 {subject?.source_id || '未指定'}</p>
        </section>
      )}

      {editable ? (
        <div className="page-form-actions" role="toolbar" aria-label="编辑布局">
          <Button size="sm" variant={layout === 'edit' ? 'secondary' : 'ghost'} onClick={() => setLayout('edit')}>编辑</Button>
          <Button size="sm" variant={layout === 'split' ? 'secondary' : 'ghost'} onClick={() => setLayout('split')}>分栏</Button>
          <Button size="sm" variant={layout === 'preview' ? 'secondary' : 'ghost'} onClick={() => setLayout('preview')}>预览</Button>
        </div>
      ) : null}

      <div className="archive-detail-two-col">
        {editable && layout !== 'preview' ? (
          <MarkdownSourceEditor
            value={state.payload.body_markdown}
            onChange={commands.setBody}
            onSave={() => { void commands.saveDraft() }}
            onInsertTemplate={() => commands.setBody(insertMarkdownSnippet(
              state.payload.body_markdown,
              templateMarkdownForType(state.payload.record_type),
            ))}
          />
        ) : null}
        {!editable || layout !== 'edit' ? (
          <Suspense fallback={<section className="card" aria-label="Markdown 预览">正在加载预览</section>}>
            <MarkdownPreview
              source={state.payload.body_markdown}
              model={previewModel}
              modelStatus={previewModelStatus}
              references={materials}
            />
          </Suspense>
        ) : null}
      </div>

      <div className="metadata-list">
        <RecordOutline source={state.payload.body_markdown} model={previewModel} />
        {editable ? <RecordSaveImpact baseline={state.record?.current ?? null} payload={state.payload} /> : null}
        {mode === 'revision' && state.revision && state.record ? (
          <RevisionDiff base={state.revision} local={state.record.current} />
        ) : null}
        {mode === 'revision' ? (
          <Input label="恢复原因" value={restoreReason} onChange={(event) => setRestoreReason(event.target.value)} />
        ) : null}
        {(mode === 'read' || mode === 'revision') && recordId ? (
          <Suspense fallback={<section className="card" aria-label="记录导出">正在加载导出</section>}>
            <RecordExportPanel
              recordId={recordId}
              {...(revisionId ? { revisionId } : {})}
              snapshotIds={evidenceIds}
            />
          </Suspense>
        ) : null}
        {mode === 'read' || mode === 'revision' || mode === 'new' ? (
          <Suspense fallback={<section className="card" aria-label="记录导入">正在加载导入</section>}>
            <RecordImportPanel />
          </Suspense>
        ) : null}
        <div className="page-form-actions">
          <Button size="lg" variant="secondary" onClick={() => setMaterialsOpen(true)}>材料与引用</Button>
          {editable && recordId && mode !== 'new' ? (
            <Button size="lg" variant="ghost" onClick={() => setPromoteOpen(true)}>提升勾选为行动</Button>
          ) : null}
        </div>
      </div>

      {recordId && mode !== 'new' ? (
        <div className="page-stack">
          <RecordActionPanel
            state={collabState}
            actions={actions}
            members={members}
            busy={collabBusy}
            onCreate={(values) => {
              setCollabBusy(true)
              void createRecordAction(recordId, values, crypto.randomUUID())
                .then(() => listRecordActions(recordId))
                .then((response) => setActions(response.items))
                .catch(() => setCollabState('error'))
                .finally(() => setCollabBusy(false))
            }}
            onUpdate={(action, values) => {
              setCollabBusy(true)
              void updateRecordAction(recordId, action.action_id, values, values.version, crypto.randomUUID())
                .then(() => listRecordActions(recordId))
                .then((response) => setActions(response.items))
                .catch(() => setCollabState('error'))
                .finally(() => setCollabBusy(false))
            }}
            onTransition={(action, transition) => {
              setCollabBusy(true)
              void transitionRecordAction(recordId, action.action_id, transition, action.version, crypto.randomUUID())
                .then(() => listRecordActions(recordId))
                .then((response) => setActions(response.items))
                .catch(() => setCollabState('error'))
                .finally(() => setCollabBusy(false))
            }}
          />
          <RecordCommentThread
            state={collabState}
            comments={comments}
            currentUserId={userId}
            members={members}
            busy={collabBusy}
            onSubmit={(input) => {
              setCollabBusy(true)
              const request = input.mode === 'edit'
                ? editRecordComment(recordId, input.comment_id, {
                  body_markdown: input.body_markdown,
                  mention_user_ids: input.mention_user_ids,
                }, input.version, crypto.randomUUID())
                : createRecordComment(recordId, {
                  body_markdown: input.body_markdown,
                  reply_to_comment_id: input.reply_to_comment_id,
                  mention_user_ids: input.mention_user_ids,
                }, crypto.randomUUID())
              void request.then(() => listRecordComments(recordId))
                .then((response) => setComments(response.comments))
                .catch(() => setCollabState('error'))
                .finally(() => setCollabBusy(false))
            }}
            onRedact={(comment) => {
              setCollabBusy(true)
              void redactRecordComment(recordId, comment.comment_id, comment.version, crypto.randomUUID())
                .then(() => listRecordComments(recordId))
                .then((response) => setComments(response.comments))
                .catch(() => setCollabState('error'))
                .finally(() => setCollabBusy(false))
            }}
          />
          <RecordWatchControl
            state={collabState}
            watch={watch}
            busy={collabBusy}
            onChange={(preference) => {
              if (!watch) return
              setCollabBusy(true)
              void setRecordWatch(recordId, preference, watch.version, crypto.randomUUID())
                .then(setWatch)
                .catch(() => setCollabState('error'))
                .finally(() => setCollabBusy(false))
            }}
          />
        </div>
      ) : null}

      <RecordMaterialDrawer
        open={materialsOpen}
        onClose={() => setMaterialsOpen(false)}
        items={materials}
        readOnly={!editable}
        onInsert={(item) => {
          if (!editable) return
          commands.setBody(insertMaterialToken(state.payload.body_markdown, item))
        }}
        onRemove={(item) => {
          if (!editable || item.kind !== 'attachment') return
          commands.patchPayload({
            attachment_ids: state.payload.attachment_ids.filter((id) => id !== item.id),
          })
        }}
      />
      <PromoteChecklistActionDialog
        open={promoteOpen}
        source={state.payload.body_markdown}
        busy={collabBusy}
        onClose={() => setPromoteOpen(false)}
        onConfirm={(values) => {
          if (!recordId) return
          setCollabBusy(true)
          void createRecordAction(recordId, {
            title: values.title,
            details: values.details,
            assignee_id: userId,
            due_at: null,
            subject_revision_id: state.record?.current_revision_id ?? state.revision?.revision_id ?? '',
          }, crypto.randomUUID())
            .then(() => listRecordActions(recordId))
            .then((response) => {
              setActions(response.items)
              setPromoteOpen(false)
            })
            .catch(() => setCollabState('error'))
            .finally(() => setCollabBusy(false))
        }}
      />
      <RecordConflictResolver
        open={state.status === 'conflict' && state.conflictPayload !== null}
        local={state.conflictPayload ?? state.payload}
        server={state.conflictServer ?? state.record?.current ?? state.payload}
        onClose={commands.dismissConflict}
        onResolve={commands.resolveConflict}
      />
    </div>
  )
}

function toDateTimeLocal(value?: string | null): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
