import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { ApiError } from '../../../lib/apiRequest'
import {
  createRecord,
  createRecordDraft,
  createRecordRevision,
  getRecord,
  getRecordDraft,
  getRecordRevision,
  listRecordDrafts,
  patchRecordDraft,
  restoreRecordRevision,
} from '../../../lib/recordsApi'
import { createRecordSecurityController, type RecordSecurityController } from '../../../lib/recordSecurity'
import type { RecordDetail, RecordDraft, RecordDraftPayload, RecordRevision, RecordSubjectReference } from '../../../lib/types'
import {
  draftBufferKey,
  draftBufferRecordId,
  memoryDraftBufferStore,
  openIndexedDBDraftBuffer,
  readUnsyncedDraft,
  writeUnsyncedDraft,
  type DraftBufferStore,
  type UnsyncedDraft,
} from '../draftBuffer'
import { emptyRecordDraftPayload, existingEvidenceItems, payloadFromRevision } from '../recordPayload'

export type RecordWorkspaceMode = 'new' | 'edit' | 'read' | 'revision'
export type RecordWorkspaceStatus = 'loading' | 'ready' | 'empty' | 'error' | 'revoked' | 'conflict'

export type RecordWorkspaceState = {
  status: RecordWorkspaceStatus
  mode: RecordWorkspaceMode
  payload: RecordDraftPayload
  record: RecordDetail | null
  revision: RecordRevision | null
  draft: RecordDraft | null
  dirty: boolean
  saving: boolean
  publishing: boolean
  message: string
  conflictPayload: RecordDraftPayload | null
  conflictServer: RecordDraftPayload | null
  publishedRecordId: string | null
  restoredToRecordId: string | null
}

export type RecordWorkspaceCommands = {
  patchPayload: (patch: Partial<RecordDraftPayload>) => void
  setBody: (body: string) => void
  saveDraft: () => Promise<void>
  /**
   * 发布当前编辑；evidence 是本次新采集、待保存的证据，按采集顺序追加在已有证据之后。
   * 返回记录或修订是否已写入：写入后即使随后的读取失败也返回 true，调用方据此放下已发布的证据。
   */
  publish: (evidence?: readonly PublishEvidence[]) => Promise<boolean>
  restore: (saveReason: string) => Promise<void>
  resolveConflict: (payload: RecordDraftPayload) => void
  dismissConflict: () => void
  /** 返回当前草稿；还没有草稿时先保存一次。冲突未解决、发布进行中或保存失败时为 undefined。 */
  ensureDraft: () => Promise<RecordDraft | undefined>
  /** 同步读取是否正在发布。 */
  isPublishing: () => boolean
  /** 把已可引用的附件加入草稿；不改变冲突状态。 */
  addAttachment: (attachmentId: string) => void
}

/** 待保存证据在发布时需要的部分：采集意图与其绑定的记录 ID、有效期。 */
export type PublishEvidence = Readonly<{
  record_id: string
  capture_intent_id: string
  valid_until: string
}>

const AUTOSAVE_MS = 2000
const EVIDENCE_EXPIRED_MESSAGE = '有证据预览已过期，请移除后重新采集'

function isClosedError(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 403 || error.status === 404 || error.status === 410)
}

function isDraftConflict(error: unknown): boolean {
  return Boolean(error instanceof ApiError && error.recovery && typeof error.recovery === 'object' && 'server_draft' in error.recovery)
}

// 锁版本或授权代次推进时后端只返回错误码、不带 recovery，同样要重新读取当前头让用户确认。
function isRevisionConflict(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 409) return false
  return error.code === 'record_revision_conflict'
    || Boolean(error.recovery && typeof error.recovery === 'object' && 'server_revision_id' in error.recovery)
}

function newIdempotencyKey(): string {
  return crypto.randomUUID()
}

function workspaceIdentity(mode: RecordWorkspaceMode, recordId?: string, revisionId?: string): string {
  return `${mode}\0${recordId ?? ''}\0${revisionId ?? ''}`
}

function errorMessage(error: unknown, fallback: string): string {
  // 草稿名下附件仍在安全检查时后端暂不清理草稿，稍后重试即可。
  if (error instanceof ApiError && error.code === 'draft_attachments_busy') return '附件仍在安全检查，请稍后再发布'
  // 发布时会重新采集：来源数据变化、预览过期或来源已不可用都会让预览失效。
  if (error instanceof ApiError && error.code === 'evidence_preview_stale') return '有证据预览已失效（过期或来源数据已变化），请移除后重新采集'
  return error instanceof Error ? error.message : fallback
}

export function useRecordDraft(options: {
  mode: RecordWorkspaceMode
  recordId?: string
  revisionId?: string
  userId: string
  store?: DraftBufferStore
  seedSubjects?: readonly RecordSubjectReference[]
}): { state: RecordWorkspaceState; commands: RecordWorkspaceCommands } {
  const store = useMemo(() => options.store ?? (typeof indexedDB === 'undefined' ? memoryDraftBufferStore() : openIndexedDBDraftBuffer()), [options.store])
  const bufferRecordId = draftBufferRecordId(options.recordId, options.seedSubjects)
  const [status, setStatus] = useState<RecordWorkspaceStatus>(
    options.mode === 'new' ? 'ready' : options.recordId ? 'loading' : 'empty',
  )
  const [payload, setPayload] = useState<RecordDraftPayload>(() => {
    const base = emptyRecordDraftPayload(options.userId)
    if (options.mode !== 'new' || !options.seedSubjects?.length) return base
    return { ...base, subjects: [...options.seedSubjects] }
  })
  const [record, setRecord] = useState<RecordDetail | null>(null)
  const [revision, setRevision] = useState<RecordRevision | null>(null)
  const [draft, setDraft] = useState<RecordDraft | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [publishing, setPublishing] = useState(false)
  const [message, setMessage] = useState('')
  const [conflictPayload, setConflictPayload] = useState<RecordDraftPayload | null>(null)
  const [conflictServer, setConflictServer] = useState<RecordDraftPayload | null>(null)
  const [publishedRecordId, setPublishedRecordId] = useState<string | null>(null)
  const [restoredToRecordId, setRestoredToRecordId] = useState<string | null>(null)
  const saveChainRef = useRef(Promise.resolve())
  const restoreKeyRef = useRef<string | null>(null)
  const closedRef = useRef(false)
  const mountedRef = useRef(true)
  const payloadRef = useRef(payload)
  const draftRef = useRef(draft)
  const recordRef = useRef(record)
  const dirtyRef = useRef(dirty)
  const generationRef = useRef(0)
  const bufferIdentityRef = useRef(bufferRecordId)
  const securityRef = useRef<RecordSecurityController | null>(null)
  // 本地编辑所依据的记录头：创建草稿与发布的默认基准。后台刷新与冲突读取只更新展示，不改它。
  const baseRef = useRef<RecordDetail | null>(null)
  // 记录读取序号：较早发出的读取晚到时不能覆盖更新的展示。
  const recordLoadRef = useRef(0)
  // 冲突解决器打开期间为真：服务端保存一律暂停（含定时器已触发、仍在保存链上排队的自动保存），
  // 本地缓冲照常写入。用户解决或关闭冲突后才恢复。
  const conflictOpenRef = useRef(false)
  // 发布进行中为真：上传入口据此拒绝，避免附件挂到正被发布消费的草稿上。
  const publishingRef = useRef(false)
  // 修订冲突时加载到、等待用户处理的服务端头；用户解决冲突后才成为确认头。
  const pendingHeadRef = useRef<RecordDetail | null>(null)
  // 用户在冲突解决器里确认过的头：下一次保存把草稿原子改到它上面，发布也按它的锁版本、
  // 授权代次和证据提交。后台刷新拿到的更新头不会悄悄替代它，头再变由服务端 409 重新打开解决器。
  const confirmedHeadRef = useRef<RecordDetail | null>(null)

  useEffect(() => {
    payloadRef.current = payload
    draftRef.current = draft
    recordRef.current = record
    dirtyRef.current = dirty
  }, [dirty, draft, payload, record])

  const emptyShell = useCallback((nextStatus: Extract<RecordWorkspaceStatus, 'error' | 'revoked' | 'empty'>, nextMessage: string) => {
    generationRef.current += 1
    if (nextStatus === 'revoked' || nextStatus === 'empty') {
      closedRef.current = true
    }
    draftRef.current = null
    recordRef.current = null
    baseRef.current = null
    conflictOpenRef.current = false
    pendingHeadRef.current = null
    confirmedHeadRef.current = null
    const nextPayload = emptyRecordDraftPayload(options.userId)
    payloadRef.current = nextPayload
    dirtyRef.current = false
    if (!mountedRef.current) return
    setRecord(null)
    setRevision(null)
    setDraft(null)
    setPayload(nextPayload)
    setDirty(false)
    setConflictPayload(null)
    setConflictServer(null)
    setPublishedRecordId(null)
    setRestoredToRecordId(null)
    setStatus(nextStatus)
    setMessage(nextMessage)
  }, [options.userId])

  const clearLocalBuffer = useCallback(async () => {
    await store.delete(draftBufferKey(options.userId, bufferRecordId))
  }, [bufferRecordId, options.userId, store])

  const closeAuthorized = useCallback(async (error: unknown) => {
    const nextMessage = errorMessage(error, '记录访问已撤销')
    if (securityRef.current && !securityRef.current.lease.revoked) {
      securityRef.current.revoke('revoke')
    }
    emptyShell('revoked', nextMessage)
    try {
      await clearLocalBuffer()
    } catch {
      // 本地库删除被拒绝或仍未结束时，不得把已撤销的正文写回。
    }
  }, [clearLocalBuffer, emptyShell])

  const reportSaveError = useCallback((error: unknown) => {
    // 撤销或清空之后的迟到失败只能被丢掉。再写 ready 会把已经收起的工作区重新打开。
    if (!mountedRef.current || closedRef.current) return
    setStatus((current) => {
      if (closedRef.current || current === 'revoked' || current === 'empty') return current
      return current === 'conflict' ? 'conflict' : 'ready'
    })
    setMessage(errorMessage(error, '草稿暂不可用'))
  }, [])

  useEffect(() => {
    mountedRef.current = true
    const controller = createRecordSecurityController(options.recordId ?? 'new', options.userId, record?.authorization_epoch ?? 0, (reason) => {
      emptyShell(reason === 'visibility' || reason === 'revoke' ? 'revoked' : 'empty', '记录访问已撤销')
      void clearLocalBuffer().catch(() => undefined)
    })
    securityRef.current = controller
    return () => {
      mountedRef.current = false
      securityRef.current = null
      controller.dispose()
    }
  }, [clearLocalBuffer, emptyShell, options.recordId, options.userId, record?.authorization_epoch])

  const workspaceIdentityRef = useRef(workspaceIdentity(options.mode, options.recordId, options.revisionId))
  useLayoutEffect(() => {
    workspaceIdentityRef.current = workspaceIdentity(options.mode, options.recordId, options.revisionId)
  }, [options.mode, options.recordId, options.revisionId])

  // Opening another record reuses this hook, so the closed latch and the restore
  // idempotency key must not survive: otherwise a revoked record would keep every
  // save disabled, and a second restore would replay the first one's key.
  useEffect(() => {
    closedRef.current = false
    restoreKeyRef.current = null
    baseRef.current = null
    conflictOpenRef.current = false
    pendingHeadRef.current = null
    confirmedHeadRef.current = null
  }, [options.mode, options.recordId, options.revisionId])

  useEffect(() => {
    const previousId = bufferIdentityRef.current
    if (previousId === bufferRecordId) return
    const previousPayload = payloadRef.current
    const previousDirty = dirtyRef.current
    const previousDraft = draftRef.current
    bufferIdentityRef.current = bufferRecordId
    if (previousDirty && options.mode !== 'read' && options.mode !== 'revision') {
      void writeUnsyncedDraft(store, {
        key: draftBufferKey(options.userId, previousId),
        userId: options.userId,
        payload: previousPayload,
        updatedAt: Date.now(),
        ...(previousId !== 'new' && !previousId.startsWith('new:') ? { recordId: previousId } : {}),
        ...(previousDraft ? { draftId: previousDraft.draft_id, etag: previousDraft.etag } : {}),
      })
    }
  }, [bufferRecordId, options.mode, options.userId, store])



  useEffect(() => {
    let active = true
    const applyBuffered = (buffered: UnsyncedDraft | undefined, overwriteLocal = false) => {
      if (!buffered || closedRef.current) return false
      if (!overwriteLocal && (dirtyRef.current || generationRef.current > 0)) return false
      payloadRef.current = buffered.payload
      setPayload(buffered.payload)
      setDirty(true)
      dirtyRef.current = true
      return true
    }

    if (options.mode === 'new') {
      void readUnsyncedDraft(store, draftBufferKey(options.userId, bufferRecordId)).then((buffered) => {
        if (!active || !mountedRef.current || closedRef.current) return
        applyBuffered(buffered)
      })
      return () => {
        active = false
      }
    }
    const recordId = options.recordId
    if (!recordId) {
      return
    }

    const load = async () => {
      if (options.mode === 'revision' && options.revisionId) {
        const [loaded, historical] = await Promise.all([
          getRecord(recordId),
          getRecordRevision(recordId, options.revisionId),
        ])
        if (!active || !mountedRef.current || closedRef.current) return
        setRecord(loaded)
        recordRef.current = loaded
        setRevision(historical)
        const nextPayload = payloadFromRevision(historical)
        payloadRef.current = nextPayload
        setPayload(nextPayload)
        setStatus('ready')
        return
      }

      const loaded = await getRecord(recordId)
      if (!active || !mountedRef.current || closedRef.current) return
      setRecord(loaded)
      recordRef.current = loaded
      baseRef.current = loaded
      setRevision(loaded.current)

      if (options.mode === 'read') {
        const nextPayload = payloadFromRevision(loaded.current)
        payloadRef.current = nextPayload
        setPayload(nextPayload)
        setDraft(null)
        draftRef.current = null
        setDirty(false)
        dirtyRef.current = false
        setStatus('ready')
        return
      }

      // The drafts endpoint has no per-record filter, so this reads the API maximum.
      // A record whose draft falls outside that page keeps its local buffer and
      // surfaces a draft conflict on save rather than losing unsynced work.
      const drafts = await listRecordDrafts({ limit: 100 }).catch((error: unknown) => {
        if (isClosedError(error)) throw error
        return { items: [] }
      })
      const buffered = await readUnsyncedDraft(store, draftBufferKey(options.userId, bufferRecordId))
      const listedDraft = drafts.items.find((item) => item.record_id === recordId) ?? null
      const fetchedDraft = !listedDraft && buffered?.draftId
        ? await getRecordDraft(buffered.draftId).catch((error: unknown) => {
          if (isClosedError(error)) throw error
          return null
        })
        : null
      const serverDraft = listedDraft ?? fetchedDraft
      if (!active || !mountedRef.current || closedRef.current) return

      if (serverDraft) {
        draftRef.current = serverDraft
        setDraft(serverDraft)
      }
      const bufferIsNewer = Boolean(
        buffered && (!serverDraft || buffered.updatedAt > Date.parse(serverDraft.updated_at)),
      )
      if (bufferIsNewer && applyBuffered(buffered, true)) {
        setStatus('ready')
        return
      }
      if (buffered && !bufferIsNewer) {
        await store.delete(draftBufferKey(options.userId, bufferRecordId))
      }
      if (!active || !mountedRef.current || closedRef.current) return
      const nextPayload = serverDraft ? serverDraft.payload : payloadFromRevision(loaded.current)
      payloadRef.current = nextPayload
      setPayload(nextPayload)
      setDirty(false)
      dirtyRef.current = false
      setStatus('ready')
    }

    load().catch((error: unknown) => {
      if (!active) return
      if (isClosedError(error)) {
        void closeAuthorized(error)
        return
      }
      emptyShell('error', errorMessage(error, '记录工作区暂不可用'))
    })
    return () => {
      active = false
    }
  }, [bufferRecordId, closeAuthorized, emptyShell, options.mode, options.recordId, options.revisionId, options.userId, store])

  const patchPayload = useCallback((patch: Partial<RecordDraftPayload>) => {
    // 编辑会把状态置回 ready、关掉解决器，等同关闭冲突。
    conflictOpenRef.current = false
    pendingHeadRef.current = null
    generationRef.current += 1
    setPayload((current) => {
      const next = { ...current, ...patch }
      payloadRef.current = next
      return next
    })
    setDirty(true)
    dirtyRef.current = true
    setStatus('ready')
    setMessage('')
  }, [])

  const persistUnsynced = useCallback(async (next: RecordDraftPayload, generation: number) => {
    const persistIdentity = bufferRecordId
    const key = draftBufferKey(options.userId, persistIdentity)
    const stale = () => closedRef.current || generation !== generationRef.current || !dirtyRef.current
    if (stale()) return
    const run = saveChainRef.current.then(async () => {
      if (stale()) return
      await writeUnsyncedDraft(store, {
        key,
        userId: options.userId,
        payload: next,
        updatedAt: Date.now(),
        ...(options.recordId ? { recordId: options.recordId } : {}),
        ...(draftRef.current ? { draftId: draftRef.current.draft_id, etag: draftRef.current.etag } : {}),
      })
      if (stale() && persistIdentity === bufferIdentityRef.current) await store.delete(key)
    })
    saveChainRef.current = run.then(() => undefined, () => undefined)
    return run
  }, [bufferRecordId, options.recordId, options.userId, store])

  const applyDraftConflict = useCallback((error: unknown) => {
    // 先于挂载判断置位：即使页面已卸载，排在保存链上的自动保存也不能拿服务端新 ETag 覆盖对方草稿。
    conflictOpenRef.current = true
    // 解决器改为展示服务端草稿：之前读到的待确认头不再可见，不能随草稿合并一起被确认。
    pendingHeadRef.current = null
    const recovery = error instanceof ApiError && error.recovery && typeof error.recovery === 'object' && 'server_draft' in error.recovery
      ? error.recovery as { server_draft: RecordDraft }
      : null
    if (recovery?.server_draft) {
      draftRef.current = recovery.server_draft
      if (mountedRef.current) {
        setDraft(recovery.server_draft)
        setConflictServer(recovery.server_draft.payload)
      }
    }
    if (mountedRef.current) {
      setConflictPayload(payloadRef.current)
      setStatus('conflict')
    }
  }, [])

  const applyRevisionConflict = useCallback(async () => {
    if (options.recordId) {
      const loadSeq = ++recordLoadRef.current
      let latest: RecordDetail
      try {
        latest = await getRecord(options.recordId)
      } catch (loadError) {
        if (isClosedError(loadError)) {
          await closeAuthorized(loadError)
          return
        }
        // 读不到新头就没有可确认的对象：不打开解决器，提示稍后重试，下次发布会重新进入冲突。
        if (!closedRef.current) reportSaveError(new Error('记录已有新修订，暂时无法读取，请稍后重试'))
        return
      }
      conflictOpenRef.current = true
      if (closedRef.current || !mountedRef.current) return
      pendingHeadRef.current = latest
      if (loadSeq === recordLoadRef.current) {
        setRecord(latest)
        recordRef.current = latest
        setRevision(latest.current)
      }
      // 解决器展示的服务端内容与待确认头是同一份快照，后台刷新不会把两者拆开。
      setConflictServer(payloadFromRevision(latest.current))
    }
    conflictOpenRef.current = true
    if (closedRef.current || !mountedRef.current) return
    setConflictPayload(payloadRef.current)
    setStatus('conflict')
  }, [closeAuthorized, options.recordId, reportSaveError])

  const saveDraft = useCallback(async (): Promise<RecordDraft | undefined> => {
    if (options.mode === 'read' || options.mode === 'revision' || closedRef.current || conflictOpenRef.current) return
    const run = saveChainRef.current.then(async (): Promise<RecordDraft | undefined> => {
      if (options.mode === 'read' || options.mode === 'revision' || closedRef.current || conflictOpenRef.current) return
      const generation = generationRef.current
      setSaving(true)
      try {
        const current = payloadRef.current
        const existing = draftRef.current
        const head = confirmedHeadRef.current ?? baseRef.current
        const rebaseTo = existing?.record_id && confirmedHeadRef.current
          && existing.base_revision_id !== confirmedHeadRef.current.current_revision_id
          ? confirmedHeadRef.current.current_revision_id
          : undefined
        const next = existing
          ? await patchRecordDraft(existing.draft_id, rebaseTo ? { payload: current, base_revision_id: rebaseTo } : { payload: current }, existing.etag)
          : await createRecordDraft(options.recordId && head
            ? { record_id: options.recordId, base_revision_id: head.current_revision_id, payload: current }
            : { payload: current })
        if (closedRef.current) return
        draftRef.current = next
        if (generation === generationRef.current) {
          await store.delete(draftBufferKey(options.userId, bufferRecordId))
        }
        if (closedRef.current) {
          draftRef.current = null
          return
        }
        if (mountedRef.current) {
          setDraft(next)
          if (generation === generationRef.current) {
            setDirty(false)
            dirtyRef.current = false
          }
        }
        return next
      } catch (error) {
        if (isRevisionConflict(error)) {
          await applyRevisionConflict()
          return
        }
        if (isDraftConflict(error)) {
          applyDraftConflict(error)
          return
        }
        if (isClosedError(error)) {
          await closeAuthorized(error)
          return
        }
        reportSaveError(error)
        await writeUnsyncedDraft(store, {
          key: draftBufferKey(options.userId, bufferRecordId),
          userId: options.userId,
          payload: payloadRef.current,
          updatedAt: Date.now(),
          ...(options.recordId ? { recordId: options.recordId } : {}),
          ...(draftRef.current ? { draftId: draftRef.current.draft_id, etag: draftRef.current.etag } : {}),
        })
      } finally {
        if (mountedRef.current) setSaving(false)
      }
    })
    saveChainRef.current = run.then(() => undefined, () => undefined)
    return run
  }, [applyDraftConflict, applyRevisionConflict, bufferRecordId, closeAuthorized, options.mode, options.recordId, options.userId, reportSaveError, store])

  useEffect(() => {
    return () => {
      if (closedRef.current) return
      if (options.mode === 'read' || options.mode === 'revision') return
      if (!dirtyRef.current) return
      void writeUnsyncedDraft(store, {
        key: draftBufferKey(options.userId, bufferIdentityRef.current),
        userId: options.userId,
        payload: payloadRef.current,
        updatedAt: Date.now(),
        ...(options.recordId ? { recordId: options.recordId } : {}),
        ...(draftRef.current ? { draftId: draftRef.current.draft_id, etag: draftRef.current.etag } : {}),
      })
    }
  }, [options.mode, options.recordId, options.userId, store])

  useEffect(() => {
    // 冲突期间仍写本地缓冲；服务端保存由 saveDraft 按 conflictOpenRef 暂停。status 列入依赖，
    // 使解决或关闭冲突后重新排定一次自动保存（冲突期间的定时器可能已经耗尽）。
    if (!dirty || options.mode === 'read' || options.mode === 'revision') return
    const generation = generationRef.current
    const timer = window.setTimeout(() => {
      void persistUnsynced(payloadRef.current, generation).then(() => saveDraft())
    }, AUTOSAVE_MS)
    return () => window.clearTimeout(timer)
  }, [dirty, options.mode, payload, persistUnsynced, saveDraft, status])

  const publish = useCallback(async (evidence: readonly PublishEvidence[] = []) => {
    if (publishingRef.current) return false
    // 采集意图 15 分钟内有效：过期的不发出去，免得整笔发布被拒。保存草稿可能耗时，正式提交前再查一次。
    const evidenceExpired = () => evidence.some((item) => !(Date.parse(item.valid_until) > Date.now()))
    if (evidenceExpired()) {
      reportSaveError(new Error(EVIDENCE_EXPIRED_MESSAGE))
      return false
    }
    publishingRef.current = true
    setPublishing(true)
    let published = false
    try {
      let currentDraft = await saveDraft()
      // 首次保存失败或进入冲突时不能再补存一次：那会绕过刚打开的冲突解决器继续发布。
      if (currentDraft && dirtyRef.current) currentDraft = await saveDraft()
      if (!currentDraft) return false
      if (evidenceExpired()) {
        reportSaveError(new Error(EVIDENCE_EXPIRED_MESSAGE))
        return false
      }
      const head = confirmedHeadRef.current ?? baseRef.current
      if (options.mode === 'new' || !options.recordId) {
        // 新记录的证据都绑定在首次预览预分配的同一个 record_id 上。
        const created = await createRecord(evidence.length > 0
          ? {
            draft_id: currentDraft.draft_id,
            draft_etag: currentDraft.etag,
            record_id: evidence[0]!.record_id,
            evidence_items: evidence.map((item) => ({ capture_intent_id: item.capture_intent_id })),
          }
          : { draft_id: currentDraft.draft_id, draft_etag: currentDraft.etag }, newIdempotencyKey())
        published = true
        draftRef.current = null
        if (mountedRef.current) {
          setDraft(null)
          setPublishedRecordId(created.record_id)
          setDirty(false)
          dirtyRef.current = false
          setMessage('')
        }
      } else if (head) {
        await createRecordRevision(options.recordId, {
          draft_id: currentDraft.draft_id,
          draft_etag: currentDraft.etag,
          base_revision_id: head.current_revision_id,
          lock_version: head.lock_version,
          authorization_epoch: head.authorization_epoch,
          // 证据不在草稿里：沿用基准修订的快照，否则新修订会丢掉全部证据。
          evidence_items: [
            ...existingEvidenceItems(head.current),
            ...evidence.map((item) => ({ capture_intent_id: item.capture_intent_id })),
          ],
        }, newIdempotencyKey())
        published = true
        // 发布成功时服务端已在同一事务删除草稿：立刻放下它，不依赖随后的读取成功，
        // 否则读取失败会让上传等入口继续拿到已消费的草稿。
        draftRef.current = null
        if (mountedRef.current) setDraft(null)
        // 发布后的读取最权威：作废此前仍在途的后台读取。
        // 读取返回前授权可能已经撤销。代次、关闭闩和当前身份都不符时，不得把正文写回。
        const generation = generationRef.current
        const loadSeq = ++recordLoadRef.current
        const recordId = options.recordId
        const identity = workspaceIdentityRef.current
        const latest = await getRecord(recordId)
        const currentPublication = !closedRef.current
          && generation === generationRef.current
          && loadSeq === recordLoadRef.current
          && identity === workspaceIdentityRef.current
        if (currentPublication) {
          baseRef.current = latest
          pendingHeadRef.current = null
          confirmedHeadRef.current = null
          if (mountedRef.current) {
            setRecord(latest)
            recordRef.current = latest
            setRevision(latest.current)
            const nextPayload = payloadFromRevision(latest.current)
            payloadRef.current = nextPayload
            setPayload(nextPayload)
            setDirty(false)
            dirtyRef.current = false
            setMessage('')
          }
        }
      }
      await store.delete(draftBufferKey(options.userId, bufferRecordId))
      return published
    } catch (error) {
      if (isRevisionConflict(error)) {
        await applyRevisionConflict()
        return published
      }
      if (isDraftConflict(error)) {
        applyDraftConflict(error)
        return published
      }
      if (isClosedError(error)) {
        await closeAuthorized(error)
        return published
      }
      reportSaveError(error)
      return published
    } finally {
      publishingRef.current = false
      if (mountedRef.current) setPublishing(false)
    }
  }, [applyDraftConflict, applyRevisionConflict, bufferRecordId, closeAuthorized, options.mode, options.recordId, options.userId, reportSaveError, saveDraft, store])

  const restore = useCallback(async (saveReason: string) => {
    if (!options.recordId || !options.revisionId) return
    restoreKeyRef.current ??= newIdempotencyKey()
    setPublishing(true)
    try {
      await restoreRecordRevision(options.recordId, options.revisionId, { save_reason: saveReason }, restoreKeyRef.current)
      const latest = await getRecord(options.recordId)
      if (mountedRef.current) {
        setRecord(latest)
        recordRef.current = latest
        setRestoredToRecordId(options.recordId)
        setMessage('已恢复为新修订')
      }
    } catch (error) {
      if (isClosedError(error)) {
        await closeAuthorized(error)
        return
      }
      reportSaveError(error)
    } finally {
      if (mountedRef.current) setPublishing(false)
    }
  }, [closeAuthorized, options.recordId, options.revisionId, reportSaveError])

  const revalidateAccess = useCallback(async () => {
    if (closedRef.current || !options.recordId || options.mode === 'new') return
    const loadSeq = ++recordLoadRef.current
    try {
      const latest = await getRecord(options.recordId)
      if (!mountedRef.current || closedRef.current || loadSeq !== recordLoadRef.current) return
      setRecord(latest)
      recordRef.current = latest
    } catch (error) {
      if (isClosedError(error)) {
        await closeAuthorized(error)
      }
    }
  }, [closeAuthorized, options.mode, options.recordId])

  useEffect(() => {
    function onPageShow(event: Event) {
      void (async () => {
        if (closedRef.current) return
        if ('persisted' in event && Boolean((event as { persisted?: boolean }).persisted)) {
          await revalidateAccess()
          if (closedRef.current) return
        }
        if (
          document.visibilityState === 'hidden'
          || dirtyRef.current
          || options.mode === 'read'
          || options.mode === 'revision'
        ) return
        const buffered = await readUnsyncedDraft(store, draftBufferKey(options.userId, bufferRecordId))
        if (!mountedRef.current || closedRef.current || !buffered || dirtyRef.current) return
        payloadRef.current = buffered.payload
        setPayload(buffered.payload)
        setDirty(true)
        dirtyRef.current = true
        setStatus('ready')
      })()
    }
    function onVisibilityChange() {
      if (document.visibilityState !== 'visible' || closedRef.current) return
      void revalidateAccess()
    }
    // Access can be withdrawn while this tab is offline, so regaining the network is
    // the third revalidation trigger next to pageshow and returning to the tab.
    function onOnline() {
      if (closedRef.current) return
      void revalidateAccess()
    }
    window.addEventListener('pageshow', onPageShow)
    document.addEventListener('visibilitychange', onVisibilityChange)
    window.addEventListener('online', onOnline)
    return () => {
      window.removeEventListener('pageshow', onPageShow)
      document.removeEventListener('visibilitychange', onVisibilityChange)
      window.removeEventListener('online', onOnline)
    }
  }, [bufferRecordId, options.mode, options.recordId, options.userId, revalidateAccess, store])

  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  return {
    state: {
      status,
      mode: options.mode,
      payload,
      record,
      revision,
      draft,
      dirty,
      saving,
      publishing,
      message,
      conflictPayload,
      conflictServer,
      publishedRecordId,
      restoredToRecordId,
    },
    commands: {
      patchPayload,
      setBody: (body) => patchPayload({ body_markdown: body }),
      saveDraft: async () => {
        await saveDraft()
      },
      publish,
      restore,
      resolveConflict: (next) => {
        // 只有已加载服务端新头的修订冲突，解决后才把草稿改到这个头上。
        if (pendingHeadRef.current) {
          confirmedHeadRef.current = pendingHeadRef.current
          pendingHeadRef.current = null
        }
        conflictOpenRef.current = false
        setPayload(next)
        payloadRef.current = next
        setConflictPayload(null)
        setConflictServer(null)
        setStatus('ready')
        setDirty(true)
        dirtyRef.current = true
        generationRef.current += 1
      },
      // 冲突未解决或工作区已关闭时不交出草稿：已有草稿也不行，否则上传会绕过解决器。
      ensureDraft: async () => {
        if (closedRef.current || conflictOpenRef.current || publishingRef.current) return undefined
        return draftRef.current ?? saveDraft()
      },
      isPublishing: () => publishingRef.current,
      addAttachment: (attachmentId) => {
        if (payloadRef.current.attachment_ids.includes(attachmentId)) return
        const next = { ...payloadRef.current, attachment_ids: [...payloadRef.current.attachment_ids, attachmentId] }
        payloadRef.current = next
        setPayload(next)
        // 冲突解决器打开时，本地一侧同样带上新附件，否则确认后会把刚上传的附件丢掉。
        setConflictPayload((current) => current && !current.attachment_ids.includes(attachmentId)
          ? { ...current, attachment_ids: [...current.attachment_ids, attachmentId] }
          : current)
        setDirty(true)
        dirtyRef.current = true
        generationRef.current += 1
      },
      dismissConflict: () => {
        pendingHeadRef.current = null
        conflictOpenRef.current = false
        setConflictPayload(null)
        setConflictServer(null)
        setStatus('ready')
      },
    },
  }
}
