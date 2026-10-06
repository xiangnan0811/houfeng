import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { RecordActionPanel } from '../../components/RecordActionPanel'
import { RecordCommentThread } from '../../components/RecordCommentThread'
import { RecordWatchControl } from '../../components/RecordWatchControl'
import { PageState } from '../../components/PageState'
import { useAuth } from '../../lib/auth-context'
import { decodeRenderModelStatusV1, insertMaterialToken } from '../../lib/documentMarkdown'
import { AttachmentPreviewDialog } from './attachments/AttachmentPreview'
import { useAttachmentMetadata } from './attachments/useAttachmentMetadata'
import { useRecordAttachmentUploads } from './attachments/useRecordAttachmentUploads'
import { PromoteChecklistActionDialog } from './editor/PromoteChecklistActionDialog'
import { RecordConflictResolver } from './editor/RecordConflictResolver'
import { RecordMaterialDrawer, type RecordMaterialItem } from './editor/RecordMaterialDrawer'
import { RevisionDiff } from './editor/RevisionDiff'
import { useRecordDraft, type RecordWorkspaceMode } from './hooks/useRecordDraft'
import { parseSubjectActivityRoute, type SubjectRouteRef } from './activity/activityQueryState'
import { returnVPSIdFromNavigationState, withReturnVPSQuery } from '../monitoring-detail/monitoringDetailHelpers'
import { insertMarkdownSnippet, templateMarkdownForType } from './recordWorkspaceModel'
import { recordSubjectPrefillFromSearchParams } from './searchFilterModel'
import { draftBufferRecordId } from './draftBuffer'
import { RecordEditorAside } from './workspace/RecordEditorAside'
import { RecordEditorPanel, type RecordEditorLayout } from './workspace/RecordEditorPanel'
import { RecordReadingAside } from './workspace/RecordReadingAside'
import { RecordToolDialogs, type RecordTool } from './workspace/RecordToolDialogs'
import { RecordWorkspaceHeader } from './workspace/RecordWorkspaceHeader'
import { useRecordCollaboration } from './workspace/useRecordCollaboration'
import './RecordWorkspace.css'

const MarkdownPreview = lazy(() => import('./editor/MarkdownPreview').then((module) => ({
  default: module.MarkdownPreview,
})))

type RecordWorkspaceProps = {
  mode: RecordWorkspaceMode
  recordId?: string
  revisionId?: string
}

function withSubjectReturnQuery(path: string, subjectReturn: SubjectRouteRef | null): string {
  if (!subjectReturn) return path
  const params = new URLSearchParams()
  params.set('return_to', `${subjectReturn.basePath}/${subjectReturn.view}`)
  return `${path}?${params.toString()}`
}

export function RecordWorkspace(props: RecordWorkspaceProps) {
  const [searchParams] = useSearchParams()
  const seedKey = props.mode !== 'new'
    ? ''
    : (() => {
      const prefill = recordSubjectPrefillFromSearchParams(searchParams)
      return prefill ? `${prefill.kind}:${prefill.role}:${prefill.source_id}` : ''
    })()
  return (
    <RecordWorkspaceSession
      key={`${props.mode}:${props.recordId ?? ''}:${props.revisionId ?? ''}:${seedKey || draftBufferRecordId(props.recordId)}`}
      {...props}
    />
  )
}

function RecordWorkspaceSession({ mode, recordId, revisionId }: RecordWorkspaceProps) {
  const { user } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const userId = user?.user_id ?? ''
  const seedSubjects = useMemo(() => {
    if (mode !== 'new') return undefined
    const prefill = recordSubjectPrefillFromSearchParams(searchParams)
    if (!prefill) return undefined
    return [{
      registry_version: 1,
      kind: prefill.kind,
      role: prefill.role,
      source_id: prefill.source_id,
      primary: prefill.primary,
    }]
  }, [mode, searchParams])
  const subjectReturn = useMemo(() => {
    const raw = searchParams.get('return_to')?.trim() ?? ''
    if (!raw.startsWith('/') || raw.startsWith('//') || raw.includes('?') || raw.includes('#') || raw.includes('\\')) {
      return null
    }
    return parseSubjectActivityRoute(raw)
  }, [searchParams])
  const { state, commands } = useRecordDraft({
    mode,
    userId,
    ...(recordId ? { recordId } : {}),
    ...(revisionId ? { revisionId } : {}),
    ...(seedSubjects ? { seedSubjects } : {}),
  })
  const collaboration = useRecordCollaboration(recordId, mode)
  const [layout, setLayout] = useState<RecordEditorLayout>('split')
  const [materialsOpen, setMaterialsOpen] = useState(false)
  const [previewing, setPreviewing] = useState<RecordMaterialItem | null>(null)
  const [promoteOpen, setPromoteOpen] = useState(false)
  const [tool, setTool] = useState<RecordTool>(null)
  const [restoreReason, setRestoreReason] = useState('恢复历史修订')
  const editable = mode === 'new' || mode === 'edit'
  const attachmentMetadata = useAttachmentMetadata(state.payload.attachment_ids)
  const uploads = useRecordAttachmentUploads({
    ensureDraft: commands.ensureDraft,
    isPublishing: commands.isPublishing,
    onAvailable: commands.addAttachment,
  })
  const { reset: resetUploads } = uploads
  // 草稿被发布或丢弃后，它名下剩下的失败/取消项随之作废，不能再对已消费的草稿重试。
  const draftId = state.draft?.draft_id ?? null
  const uploadDraftIdRef = useRef(draftId)
  useEffect(() => {
    if (uploadDraftIdRef.current && uploadDraftIdRef.current !== draftId) resetUploads()
    uploadDraftIdRef.current = draftId
  }, [draftId, resetUploads])

  const members = (() => {
    const options = new Map<string, string>()
    const remember = (id: string, label: string) => {
      const current = options.get(id)
      if (!current || current === id) options.set(id, label || id)
    }
    if (user) remember(user.user_id, user.display_name || user.username)
    if (state.payload.owner_id) remember(state.payload.owner_id, state.payload.owner_id)
    for (const participant of state.record?.current.participants ?? state.revision?.participants ?? []) {
      remember(participant.participant_id, participant.display_name || participant.participant_id)
    }
    return [...options.entries()].map(([id, label]) => ({ id, label }))
  })()
  const evidenceIds = mode === 'revision'
    ? [...(state.revision?.evidence_snapshot_ids ?? [])]
    : [...(state.record?.current.evidence_snapshot_ids ?? [])]
  const materials: RecordMaterialItem[] = [
    ...state.payload.attachment_ids.map((id): RecordMaterialItem => {
      const entry = attachmentMetadata.get(id)
      const metadata = entry?.status === 'ready' ? entry.metadata : undefined
      return {
        kind: 'attachment',
        id,
        // 元数据未到或读不到时只显示"附件"，不露出原始 ID。
        label: metadata?.display_name ?? '附件',
        available: metadata?.state === 'available',
        pending: !entry || entry.status === 'loading',
        ...(metadata ? { attachment: metadata } : {}),
      }
    }),
    ...evidenceIds.map((id) => ({
      kind: 'evidence' as const,
      id,
      label: `证据 ${id}`,
      available: true,
    })),
  ]

  useEffect(() => {
    if (mode === 'new' && state.publishedRecordId) {
      navigate(withSubjectReturnQuery(`/records/${state.publishedRecordId}`, subjectReturn), {
        replace: true,
        state: location.state,
      })
    }
  }, [location.state, mode, navigate, state.publishedRecordId, subjectReturn])

  useEffect(() => {
    if (mode === 'revision' && state.restoredToRecordId) {
      navigate(withSubjectReturnQuery(`/records/${state.restoredToRecordId}`, subjectReturn), {
        replace: true,
        state: location.state,
      })
    }
  }, [location.state, mode, navigate, state.restoredToRecordId, subjectReturn])

  if (state.status === 'loading') return <PageState kind="loading" title="正在读取运维记录" />
  if (state.status === 'empty') return <PageState kind="empty" title="记录不存在" description="没有可打开的记录。" />
  if (state.status === 'revoked') return <PageState kind="empty" title="记录访问已撤销" description="当前内容已收起。" />
  if (state.status === 'error') return <PageState kind="error" title="记录工作区暂不可用" description={state.message} />

  const showsPublishedRevision = mode === 'read' || mode === 'revision'
  const previewModel = showsPublishedRevision ? state.revision?.render_model : undefined
  // While editing, the body differs from the published revision, so a stale status
  // from that revision must not be reported against the draft being written.
  const previewModelStatus = showsPublishedRevision
    ? decodeRenderModelStatusV1(state.revision?.render_model_status)
    : undefined
  const hasCollaboration = Boolean(recordId) && mode !== 'new'
  const ownerLabel = state.payload.owner_id
    ? members.find((member) => member.id === state.payload.owner_id)?.label ?? state.payload.owner_id
    : ''

  return (
    <div className="page record-page">
      <RecordWorkspaceHeader
        state={state}
        recordId={recordId}
        revisionId={revisionId}
        recordHref={(path) => withSubjectReturnQuery(path, subjectReturn)}
        subjectReturnHref={subjectReturn
          ? withReturnVPSQuery(`${subjectReturn.basePath}/${subjectReturn.view}`, returnVPSIdFromNavigationState(location.state))
          : null}
        subjectReturnState={subjectReturn?.kind === 'target' ? undefined : location.state}
        ownerLabel={ownerLabel}
        uploading={uploads.active}
        onSave={() => void commands.saveDraft()}
        onPublish={() => {
          // 附件还在上传或安全检查时不发布：否则发布会先删掉草稿，未完成的附件随之被释放。
          if (uploads.isBusy()) return
          void commands.publish()
        }}
        onExport={() => setTool('export')}
        onImport={() => setTool('import')}
      />

      <div className="record-layout">
        <div className="record-layout__main">
          {editable ? (
            <RecordEditorPanel
              title={state.payload.title}
              body={state.payload.body_markdown}
              layout={layout}
              references={materials}
              onTitle={(title) => commands.patchPayload({ title })}
              onBody={commands.setBody}
              onLayout={setLayout}
              onSave={() => { void commands.saveDraft() }}
              onInsertTemplate={() => commands.setBody(insertMarkdownSnippet(
                state.payload.body_markdown,
                templateMarkdownForType(state.payload.record_type),
              ))}
              onPromote={recordId && mode !== 'new' ? () => setPromoteOpen(true) : undefined}
            />
          ) : (
            <article className="record-section record-reading" aria-label="记录正文">
              <Suspense fallback={<p className="record-muted">正在加载正文</p>}>
                <MarkdownPreview
                  source={state.payload.body_markdown}
                  model={previewModel}
                  modelStatus={previewModelStatus}
                  references={materials}
                />
              </Suspense>
            </article>
          )}

          {mode === 'revision' && state.revision && state.record ? (
            <RevisionDiff base={state.revision} local={state.record.current} />
          ) : null}

          {hasCollaboration ? (
            <div className="record-layout__collab">
              <RecordActionPanel
                state={collaboration.state}
                actions={collaboration.actions}
                members={members}
                busy={collaboration.busy}
                onCreate={(values) => collaboration.createAction(values)}
                onUpdate={collaboration.updateAction}
                onTransition={collaboration.transitionAction}
              />
              <RecordCommentThread
                state={collaboration.state}
                comments={collaboration.comments}
                currentUserId={userId}
                members={members}
                busy={collaboration.busy}
                onSubmit={collaboration.submitComment}
                onRedact={collaboration.redactComment}
              />
            </div>
          ) : null}
        </div>

        <aside className="record-layout__aside" aria-label={editable ? '记录属性' : '记录信息'}>
          {editable ? (
            <RecordEditorAside
              payload={state.payload}
              baseline={state.record?.current ?? null}
              members={members}
              materials={materials}
              onPatch={commands.patchPayload}
              onOpenMaterials={() => setMaterialsOpen(true)}
            />
          ) : null}
          {editable && hasCollaboration ? (
            <RecordWatchControl
              state={collaboration.state}
              watch={collaboration.watch}
              busy={collaboration.busy}
              onChange={collaboration.setWatchPreference}
            />
          ) : null}
          {editable ? null : (
            <RecordReadingAside
              source={state.payload.body_markdown}
              model={previewModel}
              materials={materials}
              onPreview={setPreviewing}
              collaboration={hasCollaboration ? collaboration : null}
              restore={mode === 'revision' ? {
                reason: restoreReason,
                busy: state.publishing,
                onReason: setRestoreReason,
                onRestore: () => void commands.restore(restoreReason),
              } : null}
            />
          )}
        </aside>
      </div>

      <RecordToolDialogs
        tool={tool}
        recordId={recordId}
        revisionId={revisionId}
        snapshotIds={evidenceIds}
        onClose={() => setTool(null)}
      />
      <RecordMaterialDrawer
        open={materialsOpen}
        onClose={() => setMaterialsOpen(false)}
        items={materials}
        readOnly={!editable}
        uploads={editable ? {
          rows: uploads.rows,
          notice: uploads.notice,
          disabled: state.publishing,
          onFiles: (files) => { void uploads.addFiles(files) },
          onRetry: uploads.retry,
          onCancel: uploads.cancel,
          onRemove: uploads.remove,
        } : undefined}
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
        busy={collaboration.busy}
        onClose={() => setPromoteOpen(false)}
        onConfirm={(values) => {
          if (!recordId) return
          collaboration.createAction({
            title: values.title,
            details: values.details,
            assignee_id: userId,
            due_at: null,
            subject_revision_id: state.record?.current_revision_id ?? state.revision?.revision_id ?? '',
          }, () => setPromoteOpen(false))
        }}
      />
      <AttachmentPreviewDialog attachment={previewing?.attachment ?? null} onClose={() => setPreviewing(null)} />
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
