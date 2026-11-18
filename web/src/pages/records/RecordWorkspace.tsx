import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { Button } from '../../components/atoms'
import { RecordActionPanel } from '../../components/RecordActionPanel'
import { RecordCommentThread } from '../../components/RecordCommentThread'
import { RecordWatchControl } from '../../components/RecordWatchControl'
import { PageState } from '../../components/PageState'
import { useAuth } from '../../lib/auth-context'
import { decodeRenderModelStatusV1, insertMaterialToken } from '../../lib/documentMarkdown'
import { AttachmentPreviewDialog } from './attachments/AttachmentPreview'
import { useAttachmentMetadata } from './attachments/useAttachmentMetadata'
import { useRecordAttachmentUploads } from './attachments/useRecordAttachmentUploads'
import type { EvidenceCaptureSubject, EvidenceReservationNotice, PendingEvidence } from './evidence/EvidenceCapturePicker'
import { captureEvidencePreview, createRecordAllocation, getEvidenceSnapshot } from '../../lib/recordsApi'
import { parseRecordAllocation, requestDigest } from '../../lib/recordAllocation'
import { registerRecordClientAllocation } from '../../lib/recordSecurity'
import type { EvidenceCapturePreview, EvidenceCapturePreviewInput, RecordAllocation } from '../../lib/types'
import { evidenceKindLabel, presentGeneratedEvidenceTitle } from './evidence/evidencePresentation'
import { useIdLookup } from './hooks/useIdLookup'
import { listVPSAssets, listVPSMonitoringInstances } from '../../lib/api'
import type { OtherEvidenceSourceLoaders } from './evidence/useOtherEvidenceSource'
import { PromoteChecklistActionDialog } from './editor/PromoteChecklistActionDialog'
import { RecordConflictResolver } from './editor/RecordConflictResolver'
import { RecordMaterialDrawer, type RecordMaterialItem } from './editor/RecordMaterialDrawer'
import { RevisionDiff } from './editor/RevisionDiff'
import { useRecordDraft, type RecordWorkspaceMode } from './hooks/useRecordDraft'
import { parseSubjectActivityRoute, type SubjectRouteRef } from './activity/activityQueryState'
import { returnVPSIdFromNavigationState, withReturnVPSQuery } from '../monitoring-detail/monitoringDetailHelpers'
import { insertMarkdownSnippet, templateMarkdownForType } from './recordWorkspaceModel'
import { recordSubjectPrefillFromSearchParams } from './searchFilterModel'
import { DraftWriteInvalidatedError, draftBufferKey, draftBufferRecordId, openIndexedDBDraftBuffer } from './draftBuffer'
import { emptyRecordDraftPayload } from './recordPayload'
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

// 采集证据时可选的“其他 VPS”来源；模块级常量保证引用稳定。
const OTHER_EVIDENCE_SOURCES: OtherEvidenceSourceLoaders = {
  listVPS: () => listVPSAssets(),
  listMonitoringInstances: (vpsId) => listVPSMonitoringInstances(vpsId),
}

// 证据清单只需要标题与类型；模块级函数保证查询缓存的 loader 引用稳定。
function loadEvidenceSummary(snapshotId: string, signal: AbortSignal): Promise<{ title: string; kind: string }> {
  return getEvidenceSnapshot(snapshotId, signal).then((snapshot) => ({ title: snapshot.title, kind: snapshot.kind }))
}

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
  const { user } = useAuth()
  const [searchParams] = useSearchParams()
  const seedKey = props.mode !== 'new'
    ? ''
    : (() => {
      const prefill = recordSubjectPrefillFromSearchParams(searchParams)
      return prefill ? `${prefill.kind}:${prefill.role}:${prefill.source_id}` : ''
    })()
  return (
    <RecordWorkspaceSession
      key={`${user?.user_id ?? ''}:${props.mode}:${props.recordId ?? ''}:${props.revisionId ?? ''}:${seedKey || draftBufferRecordId(props.recordId)}`}
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
  const [layout, setLayout] = useState<RecordEditorLayout>('split')
  const [materialsOpen, setMaterialsOpen] = useState(false)
  const [previewing, setPreviewing] = useState<RecordMaterialItem | null>(null)
  const [pendingEvidence, setPendingEvidence] = useState<readonly PendingEvidence[]>([])
  // 预留身份独立持久化：预览或分配响应丢失后，刷新仍使用原 key 和 digest。
  const [newTargetAllocation, setNewTargetAllocation] = useState<RecordAllocation | null>(null)
  const [previewReservationNotice, setPreviewReservationNotice] = useState<EvidenceReservationNotice | null>(null)
  const [previewReady, setPreviewReady] = useState(mode !== 'new')
  const [reservationBusy, setReservationBusy] = useState(false)
  const [discardingReservation, setDiscardingReservation] = useState(false)
  const [preparingPublish, setPreparingPublish] = useState(false)
  const previewStore = useMemo(() => openIndexedDBDraftBuffer(), [])
  const previewKey = draftBufferKey(userId, `${draftBufferRecordId(undefined, seedSubjects)}:evidence-reservation`)
  const reservationRef = useRef<Promise<RecordAllocation | undefined> | null>(null)
  const discardInFlightRef = useRef(false)
  const publishInFlightRef = useRef(false)
  const publishButtonRef = useRef<HTMLButtonElement>(null)
  const reservationRecoveryFocusRef = useRef(false)
  const previewSessionRef = useRef({ active: false })
  const onSecurityClose = useCallback(() => {
    previewSessionRef.current.active = false
    reservationRecoveryFocusRef.current = false
    reservationRef.current = null
    discardInFlightRef.current = false
    publishInFlightRef.current = false
    setPendingEvidence([])
    setNewTargetAllocation(null)
    setPreviewReservationNotice(null)
    setPreviewReady(false)
    setReservationBusy(false)
    setDiscardingReservation(false)
    setPreparingPublish(false)
    setMaterialsOpen(false)
    setPreviewing(null)
  }, [])
  const { state, commands } = useRecordDraft({
    mode,
    userId,
    onSecurityClose,
    loadEnabled: mode !== 'new' || previewReady,
    ...(recordId ? { recordId } : {}),
    ...(revisionId ? { revisionId } : {}),
    ...(seedSubjects ? { seedSubjects } : {}),
  })
  const { bindEvidenceDestination, releaseEvidenceDestination, closeForCleanup } = commands
  const hasCollaboration = Boolean(recordId) && mode !== 'new' && state.record?.lifecycle === 'active'
  const collaboration = useRecordCollaboration(hasCollaboration ? recordId : undefined, mode)
  useLayoutEffect(() => {
    const session = { active: true }
    previewSessionRef.current = session
    return () => {
      session.active = false
      reservationRef.current = null
    }
  }, [])
  useEffect(() => {
    if (mode !== 'new') return
    const session = previewSessionRef.current
    let active = true
    void (async () => {
      const scope = await previewStore.captureScope(userId)
      const stored = await previewStore.get(previewKey)
      if (stored && stored.userId !== userId) throw new Error('记录预留归属不匹配')
      const reservation = stored?.evidenceReservation
      const allocation = reservation?.allocation ? parseRecordAllocation(reservation.allocation, {
        purpose: 'evidence_preview', idempotencyKey: reservation.allocationKey, requestDigest: reservation.requestDigest,
      }) : undefined
      if (!active || !session.active) return
      if (allocation && !bindEvidenceDestination(allocation.record_id)) throw new DraftWriteInvalidatedError()
      const targetScope = allocation ? await previewStore.captureScope(userId, allocation.record_id) : null
      await previewStore.validateScopes(targetScope ? [scope, targetScope] : [scope])
      const latest = await previewStore.get(previewKey)
      if (JSON.stringify(latest?.evidenceReservation) !== JSON.stringify(reservation)) throw new Error('本地预留状态已变化')
      await previewStore.validateScopes(targetScope ? [scope, targetScope] : [scope])
      if (!active || !session.active) return
      if (allocation) {
        registerRecordClientAllocation(userId, { record_id: allocation.record_id, allocation_id: allocation.allocation_id })
        if (!active || !session.active) return
        setNewTargetAllocation(allocation)
      } else if (reservation) {
        setPreviewReservationNotice({ message: '上次证据记录预留尚未确认，可重试或放弃本地确认', canDiscard: true })
        return
      }
      setPreviewReady(true)
    })().catch((error: unknown) => {
      if (error instanceof DraftWriteInvalidatedError) closeForCleanup()
      else if (active && session.active) setPreviewReservationNotice({ message: '本地预留状态暂不可读取，请刷新后重试', canDiscard: false })
    })
    return () => { active = false }
  }, [bindEvidenceDestination, closeForCleanup, mode, previewKey, previewStore, userId])
  const ensurePreviewAllocation = useCallback((input?: EvidenceCapturePreviewInput): Promise<RecordAllocation | undefined> => {
    const session = previewSessionRef.current
    if (!session.active) return Promise.resolve(undefined)
    if (discardInFlightRef.current) return Promise.reject(new Error('本地预留状态正在清理'))
    if (reservationRef.current) return reservationRef.current
    setReservationBusy(true)
    const run = (async () => {
      const scope = await previewStore.captureScope(userId)
      let buffered = await previewStore.get(previewKey)
      let reservation = buffered?.evidenceReservation
      if (!reservation && (input?.allocation || newTargetAllocation)) throw new Error('本地预留状态已清理，请重新确认')
      if (!reservation && input) {
        reservation = {
          allocationKey: crypto.randomUUID(),
          requestDigest: await requestDigest({ purpose: 'evidence_preview', selection: input }),
        }
        const value = { key: previewKey, userId, payload: emptyRecordDraftPayload(userId), updatedAt: Date.now(), evidenceReservation: reservation }
        try {
          await previewStore.set(value, scope)
          buffered = value
        } catch (error) {
          // Another tab may have fixed this workspace's request first.
          buffered = await previewStore.get(previewKey)
          if (!buffered?.evidenceReservation) throw error
          reservation = buffered.evidenceReservation
        }
      }
      if (!reservation || !buffered) return undefined
      const expected = { purpose: 'evidence_preview' as const, idempotencyKey: reservation.allocationKey, requestDigest: reservation.requestDigest }
      const allocation = reservation.allocation
        ? parseRecordAllocation(reservation.allocation, expected)
        : await createRecordAllocation({ purpose: 'evidence_preview', request_digest: reservation.requestDigest }, reservation.allocationKey)
      if (!session.active) return undefined
      if (!bindEvidenceDestination(allocation.record_id)) throw new DraftWriteInvalidatedError()
      const targetScope = await previewStore.captureScope(userId, allocation.record_id)
      await previewStore.validateScopes([scope, targetScope])
      if (!session.active) return undefined
      if (!await previewStore.bindEvidenceReservation(previewKey, reservation.allocationKey, reservation.requestDigest, allocation, scope)) {
        throw new Error('本地预留状态已清理，请重新确认')
      }
      await previewStore.validateScopes([scope, targetScope])
      if (!session.active) return undefined
      registerRecordClientAllocation(userId, { record_id: allocation.record_id, allocation_id: allocation.allocation_id })
      if (!session.active) return undefined
      setNewTargetAllocation(allocation)
      setPreviewReservationNotice(null)
      return allocation
    })().catch((error: unknown) => {
      if (error instanceof DraftWriteInvalidatedError) closeForCleanup()
      throw error
    })
    reservationRef.current = run
    void run.finally(() => {
      if (reservationRef.current === run) {
        reservationRef.current = null
        if (session.active) setReservationBusy(false)
      }
    }).catch(() => undefined)
    return run
  }, [bindEvidenceDestination, closeForCleanup, newTargetAllocation, previewKey, previewStore, userId])
  const requestPreview = useCallback(async (input: EvidenceCapturePreviewInput, signal: AbortSignal): Promise<EvidenceCapturePreview> => {
    const session = previewSessionRef.current
    if (recordId) return captureEvidencePreview(input, signal)
    let allocation: RecordAllocation | undefined
    try {
      allocation = await ensurePreviewAllocation(input)
    } catch (error) {
      if (!signal.aborted && session.active) setPreviewReservationNotice({ message: '上次证据记录预留尚未确认，可重试或放弃本地确认', canDiscard: true })
      throw error
    }
    if (!session.active || signal.aborted) throw new DOMException('Aborted', 'AbortError')
    if (!allocation) throw new Error('记录预留不可用')
    return captureEvidencePreview({ ...input, record_id: allocation.record_id, allocation }, signal)
  }, [ensurePreviewAllocation, recordId])
  const discardPreviewReservation = useCallback(async () => {
    const session = previewSessionRef.current
    if (!session.active || discardInFlightRef.current || reservationRef.current || publishInFlightRef.current || commands.isPublishing()) return
    discardInFlightRef.current = true
    setDiscardingReservation(true)
    try {
      const scope = await previewStore.captureScope(userId)
      const stored = await previewStore.get(previewKey)
      await previewStore.validateScopes([scope])
      if (!session.active) return
      if (stored?.evidenceReservation) {
        await previewStore.discardEvidenceReservation(previewKey, stored.evidenceReservation.allocationKey)
        const remaining = await previewStore.get(previewKey)
        await previewStore.validateScopes([scope])
        if (remaining?.evidenceReservation) throw new Error('本地预留状态已变化')
      }
      if (!session.active) return
      if (stored?.evidenceReservation?.allocation) releaseEvidenceDestination(stored.evidenceReservation.allocation.record_id)
      if (!session.active) return
      setNewTargetAllocation(null)
      setPendingEvidence([])
      setPreviewReservationNotice(null)
      setPreviewReady(true)
    } catch {
      if (session.active) setPreviewReservationNotice({ message: '本地预留状态暂不可清理，请重试', canDiscard: true })
    } finally {
      discardInFlightRef.current = false
      if (session.active) setDiscardingReservation(false)
    }
  }, [commands, previewKey, previewStore, releaseEvidenceDestination, userId])
  const [promoteOpen, setPromoteOpen] = useState(false)
  const [tool, setTool] = useState<RecordTool>(null)
  const [restoreReason, setRestoreReason] = useState('恢复历史修订')
  // 未决发布期间正文就是已发出的请求：只读展示，确认或放弃后才能继续编辑。
  const editable = (mode === 'new' || mode === 'edit') && !state.publicationPending
  const attachmentMetadata = useAttachmentMetadata(state.payload.attachment_ids)
  const uploads = useRecordAttachmentUploads({
    ensureDraft: commands.ensureDraft,
    isPublishing: () => publishInFlightRef.current || commands.isPublishing(),
    onAvailable: commands.addAttachment,
  })
  const { reset: resetUploads } = uploads
  // 草稿被发布或丢弃后，它名下剩下的失败/取消项随之作废，不能再对已消费的草稿重试。
  const draftId = state.draft?.draft_id ?? null
  const uploadDraftIdRef = useRef(draftId)
  useEffect(() => {
    if (uploadDraftIdRef.current && uploadDraftIdRef.current !== draftId) {
      resetUploads()
    }
    uploadDraftIdRef.current = draftId
  }, [draftId, resetUploads])

  // 待保存证据只在当前页面：离开前提示，避免误丢。
  useEffect(() => {
    if (pendingEvidence.length === 0) return
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [pendingEvidence.length])

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
  const evidenceSummaries = useIdLookup(evidenceIds, loadEvidenceSummary)
  // 发布后新修订已带回的证据不再算待保存（放下待保存要等发布完全结束，中间可能已读到新修订）。
  const unsavedEvidence = pendingEvidence.filter((evidence) => !evidenceIds.includes(evidence.snapshot_id))
  // 证据来源取自正在编辑的记录主体（跳过还没选定来源的空白行）；显示名优先用已发布修订里的身份快照。
  const captureSubjects: EvidenceCaptureSubject[] = state.payload.subjects.filter((subject) => subject.source_id.trim() !== '').map((subject) => {
    const identity = state.record?.current.subjects.find(
      (published) => published.kind === subject.kind && published.source_id === subject.source_id,
    )?.identity
    return { kind: subject.kind, source_id: subject.source_id, label: identity?.display_name || subject.source_id }
  })
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
        ...(state.publicationPending && metadata?.state !== 'available' ? { confirmationPending: true } : {}),
        ...(metadata ? { attachment: metadata } : {}),
      }
    }),
    ...evidenceIds.map((id): RecordMaterialItem => {
      const entry = evidenceSummaries.get(id)
      const summary = entry?.status === 'ready' ? entry.metadata : undefined
      return {
        kind: 'evidence',
        id,
        // 不露出快照 ID：读到前显示“证据”，读到后显示“类型 · 标题”。
        label: summary ? `${evidenceKindLabel(summary.kind)} · ${presentGeneratedEvidenceTitle(summary.title)}` : '证据',
        available: entry?.status !== 'unavailable',
        pending: !entry || entry.status === 'loading',
      }
    }),
    ...(editable || state.publicationPending ? unsavedEvidence.map((evidence): RecordMaterialItem => ({
      kind: 'evidence',
      id: evidence.snapshot_id,
      label: `${evidence.kind_label} · ${evidence.source_label}`,
      available: true,
      unsaved: true,
    })) : []),
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
    if (mode !== 'new' || !state.publishedRecordId) return
    void previewStore.get(previewKey).then((stored) => {
      const reservation = stored?.evidenceReservation
      if (reservation?.allocation?.record_id === state.publishedRecordId) return previewStore.discardEvidenceReservation(previewKey, reservation.allocationKey)
    }).catch(() => undefined)
  }, [mode, previewKey, previewStore, state.publishedRecordId])

  useEffect(() => {
    if (mode === 'revision' && state.restoredToRecordId) {
      navigate(withSubjectReturnQuery(`/records/${state.restoredToRecordId}`, subjectReturn), {
        replace: true,
        state: location.state,
      })
    }
  }, [location.state, mode, navigate, state.restoredToRecordId, subjectReturn])

  useEffect(() => {
    if (!reservationRecoveryFocusRef.current || !previewReady || !previewSessionRef.current.active) return
    const button = publishButtonRef.current
    if (!button || button.disabled) return
    reservationRecoveryFocusRef.current = false
    button.focus()
  }, [discardingReservation, previewReady, reservationBusy, state.status])

  if (mode === 'new' && (state.status === 'empty' || state.status === 'revoked')) {
    return <PageState kind="empty" title="当前新建记录已收起" description="当前内容已收起，可返回记录列表重新新建。" action={(
      <Button variant="secondary" size="lg" onClick={() => navigate('/records')}>返回记录列表</Button>
    )} />
  }

  if (state.status === 'empty') return <PageState kind="empty" title="记录不存在" description="没有可打开的记录。" />
  if (state.status === 'revoked') return <PageState kind="empty" title="记录访问已撤销" description="当前内容已收起。" />
  if (state.status === 'error') return <PageState kind="error" title="记录工作区暂不可用" description={state.message} />
  if (mode === 'new' && !previewReady && previewReservationNotice) {
    if (!previewReservationNotice.canDiscard) return <PageState kind="error" title="记录预留暂不可用" description={previewReservationNotice.message} />
    return (
      <PageState kind="error" title="记录预留待确认" description={previewReservationNotice.message} action={(
        <>
          <Button variant="secondary" size="lg" disabled={reservationBusy || discardingReservation} onClick={() => {
            reservationRecoveryFocusRef.current = true
            const session = previewSessionRef.current
            void ensurePreviewAllocation().then((allocation) => {
              if (session.active && allocation) setPreviewReady(true)
            }).catch(() => {
              if (session.active) setPreviewReservationNotice({ message: '上次证据记录预留尚未确认，可重试或放弃本地确认', canDiscard: true })
            })
          }}>重试预留确认</Button>
          <Button variant="secondary" size="lg" disabled={reservationBusy || discardingReservation} onClick={() => {
            reservationRecoveryFocusRef.current = true
            void discardPreviewReservation()
          }}>放弃预留确认</Button>
        </>
      )} />
    )
  }
  if (state.status === 'loading') return <PageState kind="loading" title="正在读取运维记录" />

  const showsPublishedRevision = mode === 'read' || mode === 'revision'
  const previewModel = showsPublishedRevision ? state.revision?.render_model : undefined
  // While editing, the body differs from the published revision, so a stale status
  // from that revision must not be reported against the draft being written.
  const previewModelStatus = showsPublishedRevision
    ? decodeRenderModelStatusV1(state.revision?.render_model_status)
    : undefined
  const ownerLabel = state.payload.owner_id
    ? members.find((member) => member.id === state.payload.owner_id)?.label ?? state.payload.owner_id
    : ''
  const canDiscardReservation = previewReady && !reservationBusy && !discardingReservation && !preparingPublish && !state.publicationPending
  const reservationNotice = previewReservationNotice ? (
    <div className="evidence-capture__actions">
      <p className="record-upload__notice" role="alert">{previewReservationNotice.message}</p>
      {previewReservationNotice.canDiscard && canDiscardReservation ? (
        <Button variant="secondary" size="sm" title="只清除本地确认状态，不撤销已发出的请求" onClick={() => {
          publishButtonRef.current?.focus()
          void discardPreviewReservation()
        }}>放弃预留确认</Button>
      ) : null}
    </div>
  ) : null

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
        publicationUnavailable={!previewReady || reservationBusy || discardingReservation || preparingPublish}
        publicationBusy={reservationBusy || preparingPublish || (!previewReady && !previewReservationNotice)}
        publishButtonRef={publishButtonRef}
        onSave={() => void commands.saveDraft()}
        onDiscardPublication={() => {
          const session = previewSessionRef.current
          void (async () => {
            const stored = await previewStore.get(previewKey)
            if (!session.active) return
            const discarded = await commands.discardPendingPublication()
            if (!discarded || !session.active) return
            if (stored?.evidenceReservation) {
              await previewStore.discardEvidenceReservation(previewKey, stored.evidenceReservation.allocationKey)
              if ((await previewStore.get(previewKey))?.evidenceReservation) throw new Error('本地预留状态已变化')
              if (stored.evidenceReservation.allocation) releaseEvidenceDestination(stored.evidenceReservation.allocation.record_id)
            }
            if (!session.active) return
            setPendingEvidence([])
            setNewTargetAllocation(null)
            setPreviewReservationNotice(null)
          })().catch(() => { if (session.active) setPreviewReservationNotice({ message: '本地预留状态暂不可清理，请重试', canDiscard: true }) })
        }}
        onPublish={() => {
          // 附件还在上传或安全检查时不发布：否则发布会先删掉草稿，未完成的附件随之被释放。
          // 已在发布时不重入：否则会用另一个幂等键再发一次。
          if (!previewReady || discardInFlightRef.current || reservationRef.current || publishInFlightRef.current || uploads.isBusy() || commands.isPublishing()) return
          publishInFlightRef.current = true
          setPreparingPublish(true)
          const session = previewSessionRef.current
          const evidence = unsavedEvidence
          // 未决发布先重放；否则先恢复可能丢响应的证据预留，再发布同一目标。
          void (async () => {
            try {
              const allocation = mode === 'new' && !state.publicationPending ? await ensurePreviewAllocation() : undefined
              if (!session.active) return
              const published = await commands.publish(evidence, allocation)
              if (published && session.active) setPendingEvidence((current) => current.filter((item) => !evidence.includes(item)))
            } catch {
              if (session.active) setPreviewReservationNotice({ message: '上次证据记录预留尚未确认，请稍后重试发布', canDiscard: true })
            } finally {
              publishInFlightRef.current = false
              if (session.active) setPreparingPublish(false)
            }
          })()
        }}
        onExport={() => setTool('export')}
        onImport={() => setTool('import')}
      />
      {!materialsOpen ? reservationNotice : null}

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
                  references={state.publicationPending ? materials.filter((item) => item.available && !item.pending) : materials}
                  pendingReferences={state.publicationPending}
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
              onOpenMaterials={() => { if (previewSessionRef.current.active) setMaterialsOpen(true) }}
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
              onPreview={(item) => { if (previewSessionRef.current.active) setPreviewing(item) }}
              collaboration={hasCollaboration ? collaboration : null}
              restore={mode === 'revision' && state.record?.lifecycle === 'active' && state.record.capabilities.update ? {
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
          disabled: state.publishing || preparingPublish,
          onFiles: (files) => { void uploads.addFiles(files) },
          onRetry: uploads.retry,
          onCancel: uploads.cancel,
          onRemove: uploads.remove,
        } : undefined}
        onInsert={(item) => {
          if (!editable) return
          commands.setBody(insertMaterialToken(state.payload.body_markdown, item))
        }}
        capture={editable ? {
          recordId: recordId ?? newTargetAllocation?.record_id,
          allocation: recordId ? undefined : newTargetAllocation ?? undefined,
          subjects: captureSubjects,
          reservationNotice: previewReservationNotice,
          onDiscardReservation: canDiscardReservation ? discardPreviewReservation : undefined,
          previewBlocked: !previewReady || reservationBusy || discardingReservation || (mode === 'new' && preparingPublish),
          otherSources: OTHER_EVIDENCE_SOURCES,
          requestPreview,
          // 发布进行中加入的证据赶不上这次发布，新记录还会随发布成功跳走而丢失：此时不接受。
          onConfirm: (evidence) => {
            if (!previewSessionRef.current.active || publishInFlightRef.current || commands.isPublishing()) return
            setPendingEvidence((current) =>
              current.some((item) => item.capture_intent_id === evidence.capture_intent_id) ? current : [...current, evidence])
          },
          disabled: state.publishing || preparingPublish,
        } : undefined}
        onRemove={(item) => {
          if (!previewSessionRef.current.active || !editable) return
          if (item.kind === 'evidence') {
            // 发布已带上这份证据时移除不再生效，不能让界面与请求不一致。
            if (item.unsaved && !publishInFlightRef.current && !commands.isPublishing()) {
              setPendingEvidence((current) => current.filter((evidence) => evidence.snapshot_id !== item.id))
            }
            return
          }
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
