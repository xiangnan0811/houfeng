import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

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
import type { RecordDetail, RecordDraft, RecordDraftPayload, RecordRevision } from '../../../lib/types'
import {
  draftBufferKey,
  memoryDraftBufferStore,
  openIndexedDBDraftBuffer,
  readUnsyncedDraft,
  writeUnsyncedDraft,
  type DraftBufferStore,
} from '../draftBuffer'
import { emptyRecordDraftPayload, payloadFromRevision } from '../recordPayload'

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
  publish: () => Promise<void>
  restore: (saveReason: string) => Promise<void>
  resolveConflict: (payload: RecordDraftPayload) => void
  dismissConflict: () => void
}

const AUTOSAVE_MS = 2000

function isClosedError(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 403 || error.status === 404 || error.status === 410)
}

function isDraftConflict(error: unknown): boolean {
  return Boolean(error instanceof ApiError && error.recovery && typeof error.recovery === 'object' && 'server_draft' in error.recovery)
}

function isRevisionConflict(error: unknown): boolean {
  return Boolean(error instanceof ApiError && error.recovery && typeof error.recovery === 'object' && 'server_revision_id' in error.recovery)
}

function newIdempotencyKey(): string {
  return crypto.randomUUID()
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

export function useRecordDraft(options: {
  mode: RecordWorkspaceMode
  recordId?: string
  revisionId?: string
  userId: string
  store?: DraftBufferStore
}): { state: RecordWorkspaceState; commands: RecordWorkspaceCommands } {
  const store = useMemo(() => options.store ?? (typeof indexedDB === 'undefined' ? memoryDraftBufferStore() : openIndexedDBDraftBuffer()), [options.store])
  const [status, setStatus] = useState<RecordWorkspaceStatus>(
    options.mode === 'new' ? 'ready' : options.recordId ? 'loading' : 'empty',
  )
  const [payload, setPayload] = useState<RecordDraftPayload>(() => emptyRecordDraftPayload(options.userId))
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
  const securityRef = useRef<RecordSecurityController | null>(null)

  useEffect(() => {
    payloadRef.current = payload
    draftRef.current = draft
    recordRef.current = record
    dirtyRef.current = dirty
  }, [dirty, draft, payload, record])

  const emptyShell = useCallback((nextStatus: Extract<RecordWorkspaceStatus, 'error' | 'revoked' | 'empty'>, nextMessage: string) => {
    if (!mountedRef.current) return
    generationRef.current += 1
    if (nextStatus === 'revoked' || nextStatus === 'empty') {
      closedRef.current = true
    }
    setRecord(null)
    setRevision(null)
    setDraft(null)
    draftRef.current = null
    recordRef.current = null
    const nextPayload = emptyRecordDraftPayload(options.userId)
    payloadRef.current = nextPayload
    setPayload(nextPayload)
    setDirty(false)
    dirtyRef.current = false
    setConflictPayload(null)
    setConflictServer(null)
    setPublishedRecordId(null)
    setRestoredToRecordId(null)
    setStatus(nextStatus)
    setMessage(nextMessage)
  }, [options.userId])

  const clearLocalBuffer = useCallback(async () => {
    await store.delete(draftBufferKey(options.userId, options.recordId))
  }, [options.recordId, options.userId, store])

  const closeAuthorized = useCallback(async (error: unknown) => {
    await clearLocalBuffer()
    const nextMessage = errorMessage(error, '记录访问已撤销')
    if (securityRef.current && !securityRef.current.lease.revoked) {
      securityRef.current.revoke('revoke')
    }
    emptyShell('revoked', nextMessage)
  }, [clearLocalBuffer, emptyShell])

  const reportSaveError = useCallback((error: unknown) => {
    if (!mountedRef.current) return
    setStatus((current) => (current === 'conflict' ? 'conflict' : 'ready'))
    setMessage(errorMessage(error, '草稿暂不可用'))
  }, [])

  useEffect(() => {
    mountedRef.current = true
    const controller = createRecordSecurityController(options.recordId ?? 'new', options.userId, record?.authorization_epoch ?? 0, (reason) => {
      void clearLocalBuffer()
      if (!mountedRef.current) return
      emptyShell(reason === 'visibility' || reason === 'revoke' ? 'revoked' : 'empty', '记录访问已撤销')
    })
    securityRef.current = controller
    return () => {
      mountedRef.current = false
      securityRef.current = null
      controller.dispose()
    }
  }, [clearLocalBuffer, emptyShell, options.recordId, options.userId, record?.authorization_epoch])

  // Opening another record reuses this hook, so the closed latch and the restore
  // idempotency key must not survive: otherwise a revoked record would keep every
  // save disabled, and a second restore would replay the first one's key.
  useEffect(() => {
    closedRef.current = false
    restoreKeyRef.current = null
  }, [options.mode, options.recordId, options.revisionId])

  useEffect(() => {
    let active = true
    const applyBuffered = (buffered: Awaited<ReturnType<typeof readUnsyncedDraft>>) => {
      if (!buffered) return false
      payloadRef.current = buffered.payload
      setPayload(buffered.payload)
      setDirty(true)
      dirtyRef.current = true
      return true
    }

    if (options.mode === 'new') {
      void readUnsyncedDraft(store, draftBufferKey(options.userId)).then((buffered) => {
        if (!active || !mountedRef.current) return
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
        if (!active || !mountedRef.current) return
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
      if (!active || !mountedRef.current) return
      setRecord(loaded)
      recordRef.current = loaded
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
      const buffered = await readUnsyncedDraft(store, draftBufferKey(options.userId, recordId))
      const listedDraft = drafts.items.find((item) => item.record_id === recordId) ?? null
      const fetchedDraft = !listedDraft && buffered?.draftId
        ? await getRecordDraft(buffered.draftId).catch((error: unknown) => {
          if (isClosedError(error)) throw error
          return null
        })
        : null
      const serverDraft = listedDraft ?? fetchedDraft
      if (!active || !mountedRef.current) return

      if (serverDraft) {
        draftRef.current = serverDraft
        setDraft(serverDraft)
      }
      const bufferIsNewer = Boolean(
        buffered && (!serverDraft || buffered.updatedAt > Date.parse(serverDraft.updated_at)),
      )
      if (bufferIsNewer && applyBuffered(buffered)) {
        setStatus('ready')
        return
      }
      if (buffered && !bufferIsNewer) {
        await store.delete(draftBufferKey(options.userId, recordId))
      }
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
  }, [closeAuthorized, emptyShell, options.mode, options.recordId, options.revisionId, options.userId, store])

  const patchPayload = useCallback((patch: Partial<RecordDraftPayload>) => {
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
    const key = draftBufferKey(options.userId, options.recordId)
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
      if (stale()) await store.delete(key)
    })
    saveChainRef.current = run.then(() => undefined, () => undefined)
    return run
  }, [options.recordId, options.userId, store])

  const applyDraftConflict = useCallback((error: unknown) => {
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

  const saveDraft = useCallback(async (): Promise<RecordDraft | undefined> => {
    if (options.mode === 'read' || options.mode === 'revision' || closedRef.current) return
    const run = saveChainRef.current.then(async (): Promise<RecordDraft | undefined> => {
      if (options.mode === 'read' || options.mode === 'revision' || closedRef.current) return
      const generation = generationRef.current
      setSaving(true)
      try {
        const current = payloadRef.current
        const next = draftRef.current
          ? await patchRecordDraft(draftRef.current.draft_id, { payload: current }, draftRef.current.etag)
          : await createRecordDraft(options.recordId && recordRef.current
            ? { record_id: options.recordId, base_revision_id: recordRef.current.current_revision_id, payload: current }
            : { payload: current })
        draftRef.current = next
        if (generation === generationRef.current) {
          await store.delete(draftBufferKey(options.userId, options.recordId))
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
        if (isDraftConflict(error)) {
          applyDraftConflict(error)
          return
        }
        if (isClosedError(error)) {
          await closeAuthorized(error)
          return
        }
        reportSaveError(error)
      } finally {
        if (mountedRef.current) setSaving(false)
      }
    })
    saveChainRef.current = run.then(() => undefined, () => undefined)
    return run
  }, [applyDraftConflict, closeAuthorized, options.mode, options.recordId, options.userId, reportSaveError, store])

  useEffect(() => {
    if (!dirty || options.mode === 'read' || options.mode === 'revision') return
    const generation = generationRef.current
    const timer = window.setTimeout(() => {
      void persistUnsynced(payloadRef.current, generation).then(() => saveDraft())
    }, AUTOSAVE_MS)
    return () => window.clearTimeout(timer)
  }, [dirty, options.mode, payload, persistUnsynced, saveDraft])

  const publish = useCallback(async () => {
    setPublishing(true)
    try {
      let currentDraft = await saveDraft()
      if (dirtyRef.current) currentDraft = await saveDraft()
      if (!currentDraft) return
      if (options.mode === 'new' || !options.recordId) {
        const created = await createRecord({ draft_id: currentDraft.draft_id, draft_etag: currentDraft.etag }, newIdempotencyKey())
        draftRef.current = null
        if (mountedRef.current) {
          setDraft(null)
          setPublishedRecordId(created.record_id)
          setDirty(false)
          dirtyRef.current = false
          setMessage('')
        }
      } else if (recordRef.current) {
        await createRecordRevision(options.recordId, {
          draft_id: currentDraft.draft_id,
          draft_etag: currentDraft.etag,
          base_revision_id: recordRef.current.current_revision_id,
          lock_version: recordRef.current.lock_version,
          authorization_epoch: recordRef.current.authorization_epoch,
        }, newIdempotencyKey())
        const latest = await getRecord(options.recordId)
        draftRef.current = null
        if (mountedRef.current) {
          setDraft(null)
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
      await store.delete(draftBufferKey(options.userId, options.recordId))
    } catch (error) {
      if (isRevisionConflict(error)) {
        if (options.recordId) {
          const latest = await getRecord(options.recordId).catch((loadError: unknown) => {
            if (isClosedError(loadError)) throw loadError
            return null
          })
          if (latest && mountedRef.current) {
            setRecord(latest)
            recordRef.current = latest
            setRevision(latest.current)
          }
        }
        if (mountedRef.current) {
          setConflictPayload(payloadRef.current)
          setStatus('conflict')
        }
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
    } finally {
      if (mountedRef.current) setPublishing(false)
    }
  }, [applyDraftConflict, closeAuthorized, options.mode, options.recordId, options.userId, reportSaveError, saveDraft, store])

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
    try {
      const latest = await getRecord(options.recordId)
      if (!mountedRef.current || closedRef.current) return
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
        const buffered = await readUnsyncedDraft(store, draftBufferKey(options.userId, options.recordId))
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
  }, [options.mode, options.recordId, options.userId, revalidateAccess, store])

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
        setPayload(next)
        payloadRef.current = next
        setConflictPayload(null)
        setConflictServer(null)
        setStatus('ready')
        setDirty(true)
        dirtyRef.current = true
        generationRef.current += 1
      },
      dismissConflict: () => {
        setConflictPayload(null)
        setConflictServer(null)
        setStatus('ready')
      },
    },
  }
}
