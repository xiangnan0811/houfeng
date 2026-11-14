import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { ApiError } from '../../../lib/apiRequest'
import {
  clearComparisonSaveAttempts,
  findLegacyComparisonSaveGap,
  findPendingComparisonSaveAttempt,
  readComparisonSaveAttempt,
  readyComparisonSaveAttempt,
  retireComparisonSaveAttempt,
  writeComparisonSaveAttempt,
  type ComparisonSaveAttempt,
  type ComparisonSaveAttemptV2,
} from '../../../lib/comparisonSaveAttempts'
import { requestDigest, unresolvedPublicationMessage } from '../../../lib/recordAllocation'
import { isRecordId } from '../../../lib/recordIdentity'
import { createRecordSecurityController, registerRecordClientTarget, type RecordSecurityController, type RecordSecurityReason } from '../../../lib/recordSecurity'
import { DraftWriteInvalidatedError, openIndexedDBDraftBuffer, type DraftBufferStore, type DraftWriteScope } from '../draftBuffer'
import {
  createRecordAllocation,
  createRecordDraft,
  evaluateFixedComparison,
  recoverLegacyComparisonRecord,
  resolveComparisonCandidates,
  saveComparisonRecord,
} from '../../../lib/recordsApi'
import type {
  ComparisonAlignment,
  ComparisonCandidateItem,
  ComparisonCandidateRequest,
  ComparisonEvaluateRequest,
  ComparisonEvaluateResponse,
  ComparisonSubjectKind,
  ComparisonSubjectRef,
  RecordDraftPayload,
  RecordSubjectReference,
} from '../../../lib/types'
import { emptyRecordDraftPayload } from '../recordPayload'
import {
  COMPARISON_QUERY_PARAM,
  addFixedComparisonItem,
  canonicalComparisonURLState,
  confirmComparisonSnapshotItems,
  comparisonKindFromURL,
  comparisonKindKey,
  comparisonSearchParams,
  defaultComparisonMetric,
  parseComparisonSearchParams,
  removeFixedComparisonItem,
  replaceFixedComparisonItems,
  type ComparisonBasketResult,
  type ComparisonQueryParse,
  type ComparisonURLFixedItem,
  type ComparisonURLState,
} from './comparisonQueryState'

export type ComparisonWorkbenchState = Readonly<{
  query: ComparisonQueryParse
  candidates: ComparisonCandidateItem[] | null
  comparison: ComparisonEvaluateResponse | null
  loading: boolean
  error: string | null
  errorCode: string | null
  saveFeedback: string | null
  saveErrorCode: string | null
  title: string
  conclusion: string
  saveSubjects: ComparisonSubjectRef[]
  saving: boolean
  savedRecordId: string | null
  /** A save request was sent and its result is not confirmed: retry replays it, edits wait. */
  saveUnresolved: boolean
  saveBlocked: boolean
  cancelled: boolean
  selectionError: string | null
}>

export type ComparisonWorkbenchCommands = Readonly<{
  replaceQuery: (state: ComparisonURLState) => void
  confirmCandidates: (items: ComparisonURLFixedItem[]) => void
  addFixedItem: (item: ComparisonURLFixedItem) => void
  removeFixedItem: (key: string) => void
  replaceFixedItems: (items: ComparisonURLFixedItem[]) => void
  clearFixedItems: () => void
  setBaseline: (index: number) => void
  setAlignment: (alignment: ComparisonAlignment) => void
  setWindow: (from: string, to: string) => void
  setToleranceSeconds: (seconds: number) => void
  setBucketSeconds: (seconds: number | null) => void
  selectKind: (kind: string, metric?: string) => void
  setTitle: (title: string) => void
  setConclusion: (conclusion: string) => void
  save: () => Promise<void>
  /** 用户明确放弃确认未决的另存：只清本地重试状态，不代表原请求未生效。 */
  discardUnresolvedSave: () => void
  cancel: () => void
}>

type UseComparisonWorkbenchOptions = {
  userId: string
  store?: DraftBufferStore
  /** Test seam: called for the reservation key, then the publication key. */
  newIdempotencyKey?: () => string
}

type ComparisonClientScope = {
  requestKey: string
  userScope: DraftWriteScope
  sourceScopes: readonly DraftWriteScope[]
  sourceRecordIds: readonly string[]
}

type ComparisonGuards = {
  key: string
  closed: boolean
  target: RecordSecurityController | null
  sources: Map<string, RecordSecurityController>
  close: (recordId: string, reason: RecordSecurityReason) => void
}

type SettledWorkbench = {
  requestKey: string
  candidates: ComparisonCandidateItem[] | null
  comparison: ComparisonEvaluateResponse | null
  error: string | null
  errorCode: string | null
}

export function useComparisonWorkbench(options: UseComparisonWorkbenchOptions): {
  state: ComparisonWorkbenchState
  commands: ComparisonWorkbenchCommands
} {
  const [searchParams, setSearchParams] = useSearchParams()
  const query = parseComparisonSearchParams(searchParams)
  const store = useMemo(() => options.store ?? openIndexedDBDraftBuffer(), [options.store])
  const [clientScope, setClientScope] = useState<ComparisonClientScope | null>(null)
  const [settled, setSettled] = useState<SettledWorkbench | null>(null)
  const [title, setTitle] = useState('')
  const [conclusion, setConclusion] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedRecordId, setSavedRecordId] = useState<string | null>(null)
  const [saveUnresolved, setSaveUnresolved] = useState(false)
  const [saveFeedback, setSaveFeedback] = useState<{ key: string; message: string; code: string | null } | null>(null)
  const [selectionError, setSelectionError] = useState<string | null>(null)
  const [cancelKey, setCancelKey] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const saveAbortRef = useRef<AbortController | null>(null)
  const saveGenerationRef = useRef(0)
  const cancelKeyRef = useRef<string | null>(null)
  const queryRef = useRef(query)
  const saveAttemptRef = useRef<ComparisonSaveAttempt | null>(null)
  const guardsRef = useRef<ComparisonGuards | null>(null)
  const requestKey = `${options.userId}\0${query.ok ? JSON.stringify(query.state) : query.reason}`
  const feedbackKey = `${options.userId}\0${query.ok ? comparisonStateWithoutPresentation(query.state) : query.reason}`
  const requestKeyRef = useRef(requestKey)
  const previousQueryRef = useRef(query)
  const previousUserRef = useRef(options.userId)
  // Completed results belong to the view that displayed them. Reload may
  // restore one, but navigating to another basket retires that visible owner.
  const initialViewRef = useRef(true)
  const metadataFailureRef = useRef<Error | null>(null)
  // History and external URL writes change requestKey without commitQuery.
  // Drop the visible save while rendering. The layout effect retires the
  // generation before passive effects, so the next draft microtask cannot publish.
  const [trackedQuery, setTrackedQuery] = useState({ requestKey, query, userId: options.userId })
  if (trackedQuery.requestKey !== requestKey) {
    const previousQuery = trackedQuery.query
    const userChanged = trackedQuery.userId !== options.userId
    setTrackedQuery({ requestKey, query, userId: options.userId })
    if (userChanged) {
      setTitle('')
      setConclusion('')
      setSelectionError(null)
    }
    if (userChanged || !isPresentationOnlyQueryChange(previousQuery, query)) {
      if (saveFeedback !== null) setSaveFeedback(null)
      if (savedRecordId !== null) setSavedRecordId(null)
      if (saving) setSaving(false)
      if (saveUnresolved) setSaveUnresolved(false)
    }
  }
  const tooFewItems = query.ok && query.state.mode === 'fixed' && (query.state.items?.length ?? 0) < 2
  const cancelled = cancelKey === requestKey
  const settledCurrent = settled?.requestKey === requestKey && !cancelled
  const loading = query.ok && !cancelled && !tooFewItems && !settledCurrent
  const candidates = settledCurrent ? settled.candidates : null
  const comparison = settledCurrent ? settled.comparison : null
  const error = settledCurrent ? settled.error : null
  const errorCode = settledCurrent ? settled.errorCode : null
  const saveSubjects = comparison
    ? subjectsFromComparison(comparison)
    : query.ok && query.state.mode === 'candidate'
      ? query.state.subjects ?? []
      : []

  useLayoutEffect(() => {
    const previousQuery = previousQueryRef.current
    const previousKey = requestKeyRef.current
    const previousUser = previousUserRef.current
    previousUserRef.current = options.userId
    queryRef.current = query
    requestKeyRef.current = requestKey
    previousQueryRef.current = query
    if (previousKey === requestKey) return
    if (previousUser === options.userId && isPresentationOnlyQueryChange(previousQuery, query)) return
    initialViewRef.current = false
    // Basket commands already retire in commitQuery. History and external URL
    // writes do not, so retire here before passive effects and microtasks.
    saveGenerationRef.current += 1
    saveAbortRef.current?.abort()
    saveAttemptRef.current = null
    if (previousUser && options.userId && previousUser !== options.userId) {
      try {
        clearComparisonSaveAttempts(previousUser)
      } catch {
        metadataFailureRef.current = new Error('本地比较重试状态清理失败，请恢复浏览器存储后重新打开')
      }
    }
  }, [options.userId, query, requestKey])

  useLayoutEffect(() => {
    const guards: ComparisonGuards = {
      key: feedbackKey,
      closed: false,
      target: null,
      sources: new Map(),
      close(recordId, reason) {
        if (guards.closed) return
        const attempt = saveAttemptRef.current
        const targetAffected = guards.target?.lease.recordId === recordId
        guards.closed = true
        saveGenerationRef.current += 1
        saveAbortRef.current?.abort()
        abortRef.current?.abort()
        saveAttemptRef.current = null
        guards.target?.dispose()
        for (const source of guards.sources.values()) source.dispose()
        guards.sources.clear()
        setClientScope(null)
        setSaving(false)
        setSaveUnresolved(false)
        setSaveFeedback(null)
        setSavedRecordId(null)
        setTitle('')
        setConclusion('')
        let message = targetAffected
          ? '另存目标已清理，当前比较已关闭，请重新打开比较'
          : '比较来源访问已撤销，请重新打开比较'
        try {
          clearComparisonSaveAttempts(options.userId, reason === 'logout' || reason === 'user-switch' ? undefined : recordId)
          if (targetAffected && attempt?.userId === options.userId) retireComparisonSaveAttempt(attempt.userId, attempt.digest)
        } catch {
          message = '比较已关闭，但本地重试元数据清理失败'
        }
        setSettled({ requestKey: requestKeyRef.current, candidates: null, comparison: null, error: message, errorCode: null })
      },
    }
    guardsRef.current = guards
    guards.target = createRecordSecurityController('new', options.userId, 0, (reason) => {
      guards.close(guards.target?.lease.recordId ?? 'new', reason)
    })
    return () => {
      if (guardsRef.current === guards) guardsRef.current = null
      guards.closed = true
      guards.target?.dispose()
      for (const source of guards.sources.values()) source.dispose()
      guards.sources.clear()
    }
  }, [feedbackKey, options.userId])

  const bindTarget = useCallback((recordId: string | null): boolean => {
    const guards = guardsRef.current
    return Boolean(guards && guards.key === feedbackKey && !guards.closed && guards.target?.bindRecordId(recordId ?? 'new'))
  }, [feedbackKey])

  const bindSources = useCallback((recordIds: readonly string[]): boolean => {
    const guards = guardsRef.current
    if (!guards || guards.key !== feedbackKey || guards.closed) return false
    const next = new Set(recordIds)
    for (const recordId of next) {
      if (guards.sources.has(recordId)) continue
      const controller = createRecordSecurityController(recordId, options.userId, 0, (reason) => guards.close(recordId, reason))
      if (guards.closed || !controller.bindRecordId(recordId)) {
        controller.dispose()
        return false
      }
      guards.sources.set(recordId, controller)
    }
    for (const [recordId, controller] of guards.sources) {
      if (!next.has(recordId)) {
        controller.dispose()
        guards.sources.delete(recordId)
      }
    }
    return true
  }, [feedbackKey, options.userId])

  useEffect(() => {
    const controller = new AbortController()
    abortRef.current = controller
    const current = queryRef.current
    if (!current.ok || cancelKeyRef.current === requestKey) {
      return () => controller.abort()
    }
    if (current.state.mode === 'fixed' && (current.state.items?.length ?? 0) < 2) {
      return () => controller.abort()
    }
    let pendingRecovery: ComparisonSaveAttemptV2 | null = null
    const run = (async () => {
      if (metadataFailureRef.current) throw metadataFailureRef.current
      if (current.state.mode === 'fixed') {
        let pending = saveAbortRef.current && !saveAbortRef.current.signal.aborted
          ? null : findPendingComparisonSaveAttempt(options.userId, comparisonStateWithoutPresentation(current.state))
        pendingRecovery = pending
        if (pending) {
          saveAttemptRef.current = pending
          if (!bindSources(pending.sourceRecordIds)) throw new DOMException('Aborted', 'AbortError')
          if (pending.allocation && !bindTarget(pending.allocation.record_id)) throw new DOMException('Aborted', 'AbortError')
          const userScope = await store.captureScope(options.userId)
          const sourceScopes = await Promise.all(pending.sourceRecordIds.map((id) => store.captureScope(options.userId, id)))
          await store.validateScopes([userScope, ...sourceScopes])
          if (userScope.generation !== pending.generation) throw new DraftWriteInvalidatedError()
          if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
          if (!pending.allocation) {
            const allocation = await createRecordAllocation({ purpose: 'comparison_save', request_digest: pending.requestDigest }, pending.allocationKey, controller.signal)
            if (controller.signal.aborted || !bindTarget(allocation.record_id)) throw new DOMException('Aborted', 'AbortError')
            const targetScope = await store.captureScope(options.userId, allocation.record_id)
            await store.validateScopes([userScope, ...sourceScopes, targetScope])
            if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
            const existing = readComparisonSaveAttempt({ userId: pending.userId, digest: pending.digest, generation: userScope.generation, sourceRecordIds: pending.sourceRecordIds })
            if (existing?.version !== 2 || existing.idempotencyKey !== pending.idempotencyKey
              || existing.allocation || existing.request) throw new DraftWriteInvalidatedError()
            pending = { ...pending, allocation }
            writeComparisonSaveAttempt(pending)
            if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
            pendingRecovery = pending
          }
          const targetId = pending.allocation?.record_id
          if (!targetId) throw new DraftWriteInvalidatedError()
          const targetScope = await store.captureScope(options.userId, targetId)
          await store.validateScopes([userScope, ...sourceScopes, targetScope])
          if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
          const existing = readComparisonSaveAttempt({ userId: pending.userId, digest: pending.digest, generation: userScope.generation, sourceRecordIds: pending.sourceRecordIds })
          if (existing?.version !== 2 || existing.idempotencyKey !== pending.idempotencyKey
            || JSON.stringify(existing.allocation) !== JSON.stringify(pending.allocation)
            || JSON.stringify(existing.request) !== JSON.stringify(pending.request)) throw new DraftWriteInvalidatedError()
          saveAttemptRef.current = pending
          setClientScope({ requestKey, userScope, sourceScopes, sourceRecordIds: pending.sourceRecordIds })
          setSaveUnresolved(true)
          if (pending.request) {
            setSaving(true)
            try {
              const ready = await readyComparisonSaveAttempt(pending)
              await store.validateScopes([userScope, ...sourceScopes, targetScope])
              if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
              const result = await saveComparisonRecord(ready.request!, ready.idempotencyKey, controller.signal)
              await store.validateScopes([userScope, ...sourceScopes, targetScope])
              if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
              if (result.record_id !== pending.request.record_id) throw new Error('另存结果与预留记录不一致')
              const existing = readComparisonSaveAttempt({ userId: pending.userId, digest: pending.digest, generation: userScope.generation, sourceRecordIds: pending.sourceRecordIds })
              if (existing?.version !== 2 || JSON.stringify(existing.request) !== JSON.stringify(pending.request) || existing.idempotencyKey !== pending.idempotencyKey) throw new DraftWriteInvalidatedError()
              const finished = { ...ready, savedRecordId: result.record_id }
              writeComparisonSaveAttempt(finished)
              registerRecordClientTarget(options.userId, result.record_id)
              if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
              saveAttemptRef.current = finished
              pendingRecovery = null
              setSaveFeedback((current) => current?.key === feedbackKey ? null : current)
              setSaveUnresolved(false)
              setSavedRecordId(result.record_id)
            } catch (cause) {
              if (controller.signal.aborted || cause instanceof DraftWriteInvalidatedError) throw cause
              setSaveFeedback({ key: feedbackKey, message: unresolvedPublicationMessage(cause, '另存'), code: cause instanceof ApiError ? cause.code ?? null : null })
            } finally {
              if (!controller.signal.aborted) setSaving(false)
            }
          } else if (pending.draftPending) {
            setSaveFeedback({ key: feedbackKey, message: '上次另存的草稿创建结果尚未确认，请核对后选择放弃确认', code: null })
          }
        }
      }
      return current.state.mode === 'candidate'
        ? loadCandidates(current.state, controller.signal)
        : loadComparison(current.state, controller.signal)
    })()
    const userScope = store.captureScope(options.userId)
    void Promise.all([run, userScope])
      .then(async ([result, capturedUserScope]) => {
        const sourceRecordIds = [...new Set(result.kind === 'candidates'
          ? result.candidates.map((candidate) => candidate.record_id)
          : result.comparison.items.flatMap((item) => item.record_id
            ? [item.source_record_id, item.record_id]
            : [item.source_record_id]))].sort()
        if (sourceRecordIds.some((id) => !isRecordId(id))) throw new Error('比较来源归属不可用，请重新评估')
        if (controller.signal.aborted || requestKeyRef.current !== requestKey) return
        const guardedSources = pendingRecovery ? [...new Set([...sourceRecordIds, ...pendingRecovery.sourceRecordIds])] : sourceRecordIds
        if (!bindSources(guardedSources)) return
        const sourceScopes = await Promise.all(sourceRecordIds.map((id) => store.captureScope(options.userId, id)))
        await store.validateScopes([capturedUserScope, ...sourceScopes])
        if (controller.signal.aborted || requestKeyRef.current !== requestKey) return
        if (result.kind === 'comparison') {
          let stored: ComparisonSaveAttempt | null = null
          let metadataMismatch = false
          const identity = { userId: options.userId, digest: result.comparison.digest, generation: capturedUserScope.generation, sourceRecordIds }
          try {
            stored = readComparisonSaveAttempt(identity)
          } catch {
            metadataMismatch = true
          }
          const legacyGap = !stored && !metadataMismatch ? findLegacyComparisonSaveGap(identity) : null
          const selected = pendingRecovery ?? stored ?? legacyGap
          const targetId = selected?.version === 1 ? selected.recordId : selected?.allocation?.record_id
          if (targetId) {
            if (!bindTarget(targetId)) return
            const targetScope = await store.captureScope(options.userId, targetId)
            await store.validateScopes([capturedUserScope, ...sourceScopes, targetScope])
            if (controller.signal.aborted || requestKeyRef.current !== requestKey) return
            const latest = pendingRecovery
              ? readComparisonSaveAttempt({ userId: pendingRecovery.userId, digest: pendingRecovery.digest, generation: capturedUserScope.generation, sourceRecordIds: pendingRecovery.sourceRecordIds })
              : stored ? readComparisonSaveAttempt(identity) : findLegacyComparisonSaveGap(identity)
            if (!latest || latest.idempotencyKey !== selected?.idempotencyKey) throw new DraftWriteInvalidatedError()
          }
          if (legacyGap || metadataMismatch) setSaveFeedback({ key: feedbackKey, message: '当前比较与待确认的旧版另存不一致，原状态已保留', code: null })
          if (pendingRecovery) {
            saveAttemptRef.current = pendingRecovery
            if (pendingRecovery.payload) {
              setTitle(pendingRecovery.payload.title)
              setConclusion(pendingRecovery.payload.body_markdown)
            }
            setSaveUnresolved(true)
          } else {
            const previous = saveAttemptRef.current
            const selected = stored ?? legacyGap
            const restoring = selected && (!previous || previous.digest !== selected.digest || previous.userId !== selected.userId)
            if (selected) saveAttemptRef.current = selected
            if (restoring && stored?.version === 2 && stored.payload) {
              setTitle(stored.payload.title)
              setConclusion(stored.payload.body_markdown)
              if (stored.savedRecordId && initialViewRef.current) setSavedRecordId(stored.savedRecordId)
            }
            setSaveUnresolved(metadataMismatch || selected !== null && (selected.version === 1 || !selected.savedRecordId))
            setClientScope({ requestKey, userScope: capturedUserScope, sourceScopes, sourceRecordIds })
          }
        } else {
          setClientScope({ requestKey, userScope: capturedUserScope, sourceScopes, sourceRecordIds })
        }
        if (result.kind === 'candidates') {
          setSettled({
            requestKey,
            candidates: result.candidates,
            comparison: null,
            error: null,
            errorCode: null,
          })
          return
        }
        setSettled({
          requestKey,
          candidates: null,
          comparison: result.comparison,
          error: null,
          errorCode: null,
        })
        const nextState = withDefaultKindAndMetric(current.state, result.comparison)
        if (nextState !== current.state) {
          setSearchParams(comparisonSearchParams(nextState), { replace: true })
        }
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted || requestKeyRef.current !== requestKey || isAbortError(cause)) return
        if (cause instanceof DraftWriteInvalidatedError) {
          const guards = guardsRef.current
          if (guards && !guards.closed) guards.close(guards.target?.lease.recordId ?? 'new', 'cleanup')
          return
        }
        if (cause instanceof ApiError && cause.status === 404) {
          setSettled({
            requestKey,
            candidates: null,
            comparison: null,
            error: cause.message,
            errorCode: cause.code ?? 'resource_not_found',
          })
          return
        }
        setSettled({
          requestKey,
          candidates: null,
          comparison: null,
          error: cause instanceof Error ? cause.message : '比较请求失败',
          errorCode: cause instanceof ApiError ? cause.code ?? null : null,
        })
      })
    return () => controller.abort()
  }, [bindSources, bindTarget, feedbackKey, options.userId, requestKey, setSearchParams, store])

  useEffect(() => () => {
    saveAbortRef.current?.abort()
    saveGenerationRef.current += 1
  }, [])


  const saveBlocked = !saveUnresolved && !savedRecordId && (!comparison?.save_eligibility.eligible
    || !comparison.comparison_intent
    || comparison.save_eligibility.blockers.includes('snapshot_unreadable')
    || clientScope?.requestKey !== requestKey)

  function commitQuery(state: ComparisonURLState | null): boolean {
    if (state == null) {
      if (!query.ok && query.reason === 'missing') return false
    } else if (
      query.ok
      && JSON.stringify(canonicalComparisonURLState(query.state)) === JSON.stringify(canonicalComparisonURLState(state))
    ) {
      return false
    }
    abortRef.current?.abort()
    saveAbortRef.current?.abort()
    saveGenerationRef.current += 1
    setSavedRecordId(null)
    setSaving(false)
    if (state == null) {
      const params = new URLSearchParams(searchParams)
      params.delete(COMPARISON_QUERY_PARAM)
      setSearchParams(params, { replace: true })
      return true
    }
    setSearchParams(comparisonSearchParams(state), { replace: true })
    return true
  }

  function replaceQuery(state: ComparisonURLState) {
    commitQuery(state)
  }

  function applyBasket(result: ComparisonBasketResult) {
    if (!result.ok) {
      setSelectionError(result.error)
      return
    }
    if (!result.changed) return
    setSelectionError(null)
    commitQuery(result.state)
  }

  function writeBareURL() {
    if (!commitQuery(null)) return
    setSelectionError(null)
  }

  function patchFixed(mutator: (current: ComparisonURLState) => ComparisonURLState) {
    if (!query.ok || query.state.mode !== 'fixed') return
    replaceQuery(mutator(query.state))
  }

  async function save() {
    if (!clientScope || saving || !guardsRef.current || guardsRef.current.closed) return
    const restored = saveAttemptRef.current
    if (restored?.version === 2 && restored.request && !restored.savedRecordId) {
      const generation = saveGenerationRef.current
      const controller = new AbortController()
      saveAbortRef.current = controller
      setSaving(true)
      try {
        if (!bindTarget(restored.request.record_id)) return
        const targetScope = await store.captureScope(options.userId, restored.request.record_id)
        await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
        const ready = await readyComparisonSaveAttempt(restored)
        await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
        if (controller.signal.aborted || generation !== saveGenerationRef.current) return
        const created = await saveComparisonRecord(ready.request!, ready.idempotencyKey, controller.signal)
        await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
        if (controller.signal.aborted || generation !== saveGenerationRef.current) return
        if (created.record_id !== restored.request.record_id) throw new Error('另存结果与预留记录不一致')
        const existing = readComparisonSaveAttempt({ userId: restored.userId, digest: restored.digest, generation: clientScope.userScope.generation, sourceRecordIds: restored.sourceRecordIds })
        if (existing?.version !== 2 || JSON.stringify(existing.request) !== JSON.stringify(restored.request) || existing.idempotencyKey !== restored.idempotencyKey) return
        const finished = { ...ready, savedRecordId: created.record_id }
        writeComparisonSaveAttempt(finished)
        registerRecordClientTarget(options.userId, created.record_id)
        if (controller.signal.aborted || generation !== saveGenerationRef.current) return
        saveAttemptRef.current = finished
        setSaveFeedback((current) => current?.key === feedbackKey ? null : current)
        setSaveUnresolved(false)
        setSavedRecordId(created.record_id)
      } catch (cause) {
        if (controller.signal.aborted || generation !== saveGenerationRef.current) return
        if (cause instanceof DraftWriteInvalidatedError) {
          guardsRef.current?.close(restored.request.record_id, 'cleanup')
          return
        }
        setSaveFeedback({ key: feedbackKey, message: unresolvedPublicationMessage(cause, '另存'), code: cause instanceof ApiError ? cause.code ?? null : null })
      } finally {
        if (saveAbortRef.current === controller) saveAbortRef.current = null
        if (generation === saveGenerationRef.current) setSaving(false)
      }
      return
    }
    if (saveBlocked || !comparison?.comparison_intent) return
    if (restored?.version === 1 && restored.digest !== comparison.digest) {
      setSaveFeedback({ key: feedbackKey, message: '当前比较与待确认的旧版另存不一致，原状态已保留', code: null })
      return
    }
    if (restored?.version === 2 && !restored.request && restored.digest !== comparison.digest) {
      setSaveFeedback({ key: feedbackKey, message: '当前比较与待确认的另存不一致，原状态已保留', code: null })
      return
    }
    const generation = saveGenerationRef.current
    const intentToken = comparison.comparison_intent.token
    const digest = comparison.digest
    const controller = new AbortController()
    saveAbortRef.current = controller
    const stale = () => generation !== saveGenerationRef.current || controller.signal.aborted
      || !guardsRef.current || guardsRef.current.closed || guardsRef.current.key !== feedbackKey
    const newKey = () => options.newIdempotencyKey?.() ?? crypto.randomUUID()
    setSaving(true)
    try {
      await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes])
      if (stale()) return
      const identity = { userId: options.userId, digest, generation: clientScope.userScope.generation, sourceRecordIds: clientScope.sourceRecordIds }
      let current = saveAttemptRef.current?.digest === digest && saveAttemptRef.current.userId === options.userId
        ? saveAttemptRef.current
        : readComparisonSaveAttempt(identity)
      saveAttemptRef.current = current
      if (!current) {
        const legacyGap = findLegacyComparisonSaveGap(identity)
        if (legacyGap) {
          if (!bindTarget(legacyGap.recordId)) return
          saveAttemptRef.current = legacyGap
          setSaveUnresolved(true)
          throw new Error('当前比较与待确认的旧版另存不一致，原状态已保留')
        }
      }
      if (current?.version === 1) {
        // Earlier clients never stored their draft. Ask only for the completed
        // result with the original key and record, authenticated by the current
        // intent for this same comparison; the legacy bytes are never rewritten.
        if (!bindTarget(current.recordId)) return
        const targetScope = await store.captureScope(options.userId, current.recordId)
        await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
        const recovered = await recoverLegacyComparisonRecord({ record_id: current.recordId, comparison_intent: intentToken }, current.idempotencyKey, controller.signal)
        await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
        if (recovered.record_id !== current.recordId) throw new Error('另存结果与原记录不一致')
        if (stale() || !readComparisonSaveAttempt(identity)) return
        registerRecordClientTarget(options.userId, recovered.record_id)
        if (stale()) return
        setSaveFeedback((previous) => previous?.key === feedbackKey ? null : previous)
        setSaveUnresolved(false)
        setSavedRecordId(recovered.record_id)
        return
      }
      const payload = current && !current.savedRecordId && current.payload
        ? current.payload : comparisonSavePayload(options.userId, title, conclusion, saveSubjects)
      const payloadDigest = await requestDigest(payload)
      if (stale()) return
      if (current?.savedRecordId && current.payloadDigest !== payloadDigest) {
        retireComparisonSaveAttempt(options.userId, digest)
        current = null
        saveAttemptRef.current = null
      }
      let attempt: ComparisonSaveAttemptV2
      if (current) {
        if (!current.request && (!current.payload || current.payloadDigest !== payloadDigest)) throw new Error('待确认的比较草稿无法恢复')
        attempt = current
      } else {
        attempt = {
          version: 2,
          ...identity,
          queryKey: query.ok ? comparisonStateWithoutPresentation(query.state) : '',
          payloadDigest,
          payload,
          requestDigest: await requestDigest({
            purpose: 'comparison_save',
            comparison_digest: digest,
            payload_sha256: payloadDigest,
            source_record_ids: identity.sourceRecordIds,
          }),
          allocationKey: newKey(),
          idempotencyKey: newKey(),
        }
        if (stale()) return
        writeComparisonSaveAttempt(attempt)
        if (stale()) return
        saveAttemptRef.current = attempt
        setSaveUnresolved(true)
      }
      if (!attempt.request) {
        let allocation = attempt.allocation
        if (!allocation) {
          allocation = await createRecordAllocation({ purpose: 'comparison_save', request_digest: attempt.requestDigest }, attempt.allocationKey, controller.signal)
        }
        if (stale() || !bindTarget(allocation.record_id)) return
        const targetScope = await store.captureScope(options.userId, allocation.record_id)
        await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
        if (stale()) return
        if (!attempt.allocation) {
          attempt = { ...attempt, allocation }
          writeComparisonSaveAttempt(attempt)
          if (stale()) return
          saveAttemptRef.current = attempt
        }
        if (attempt.draftPending) throw new Error('上次另存的草稿创建结果尚未确认，请核对后选择放弃确认')
        attempt = { ...attempt, draftPending: true }
        writeComparisonSaveAttempt(attempt)
        if (stale()) return
        saveAttemptRef.current = attempt
        const draft = await createRecordDraft({ payload })
        await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
        if (stale()) return
        // Durable before the first save request: a lost reply or reload must
        // replay exactly this body and key, even after the draft is consumed.
        const request = { record_id: allocation.record_id, draft_id: draft.draft_id, draft_etag: draft.etag, comparison_intent: intentToken, allocation }
        const publicationDigest = await requestDigest(request)
        if (stale()) return
        attempt = { ...attempt, request, publicationDigest }
        writeComparisonSaveAttempt(attempt)
        if (stale()) return
        saveAttemptRef.current = attempt
        setSaveUnresolved(true)
      }
      if (!bindTarget(attempt.request!.record_id)) return
      const beforeReadyScope = await store.captureScope(options.userId, attempt.request!.record_id)
      await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, beforeReadyScope])
      if (stale()) return
      attempt = await readyComparisonSaveAttempt(attempt)
      const request = attempt.request!
      const targetScope = await store.captureScope(options.userId, request.record_id)
      await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
      if (stale()) return
      const created = await saveComparisonRecord(request, attempt.idempotencyKey, controller.signal)
      await store.validateScopes([clientScope.userScope, ...clientScope.sourceScopes, targetScope])
      if (created.record_id !== request.record_id) throw new Error('另存结果与预留记录不一致')
      if (stale()) return
      const existing = readComparisonSaveAttempt(identity)
      if (existing?.version !== 2 || JSON.stringify(existing.request) !== JSON.stringify(request) || existing.idempotencyKey !== attempt.idempotencyKey) return
      attempt = { ...attempt, savedRecordId: created.record_id }
      writeComparisonSaveAttempt(attempt)
      registerRecordClientTarget(options.userId, created.record_id)
      if (stale()) return
      saveAttemptRef.current = attempt
      setSaveFeedback((current) => current?.key === feedbackKey ? null : current)
      setSaveUnresolved(false)
      setSavedRecordId(created.record_id)
    } catch (cause) {
      if (stale() || isAbortError(cause)) return
      if (cause instanceof DraftWriteInvalidatedError) {
        const guards = guardsRef.current
        if (guards && !guards.closed) guards.close(guards.target?.lease.recordId ?? 'new', 'cleanup')
        return
      }
      let message = cause instanceof Error ? cause.message : '另存失败'
      const errorCode = cause instanceof ApiError ? cause.code ?? null : null
      const attempt = saveAttemptRef.current
      const sent = attempt !== null
      if (sent) {
        message = attempt.version === 1
          ? `当前比较无法确认旧版另存的结果，本地重试状态已保留；${unresolvedPublicationMessage(cause, '另存')}`
          : attempt.draftPending && !attempt.request
            ? '上次另存的草稿创建结果尚未确认，请核对后选择放弃确认'
            : unresolvedPublicationMessage(cause, '另存')
      }
      const closes = cause instanceof DraftWriteInvalidatedError || (!sent && retiresComparisonSave(cause))
      // A sent request keeps its envelope: no error reply proves it did not
      // commit. Only an unsent attempt may be retired here.
      if (closes && !sent) {
        saveAttemptRef.current = null
        try {
          retireComparisonSaveAttempt(options.userId, digest)
        } catch {
          message = '比较已失效，但本地重试状态清理失败'
          metadataFailureRef.current = new Error(message)
        }
      }
      setSaveFeedback({ key: feedbackKey, message, code: errorCode })
      if (closes) {
        setSettled({ requestKey, candidates: null, comparison: null, error: message, errorCode })
      }
    } finally {
      if (saveAbortRef.current === controller) saveAbortRef.current = null
      if (generation === saveGenerationRef.current) setSaving(false)
    }
  }

  function beginEditingSaved(): boolean {
    if (!savedRecordId) return true
    const previous = saveAttemptRef.current
    if (previous?.version === 1) {
      try {
        retireComparisonSaveAttempt(previous.userId, previous.digest)
      } catch {
        metadataFailureRef.current = new Error('本地比较重试状态清理失败，请恢复浏览器存储后重新打开')
        return false
      }
      saveAttemptRef.current = null
    }
    if (!bindTarget(null)) return false
    setSavedRecordId(null)
    return true
  }

  return {
    state: {
      query,
      candidates,
      comparison,
      loading,
      error,
      errorCode,
      saveFeedback: saveFeedback?.key === feedbackKey ? saveFeedback.message : null,
      saveErrorCode: saveFeedback?.key === feedbackKey ? saveFeedback.code : null,
      title,
      conclusion,
      saveSubjects,
      saving,
      savedRecordId,
      saveUnresolved,
      saveBlocked,
      cancelled,
      selectionError,
    },
    commands: {
      replaceQuery,
      confirmCandidates(items) {
        applyBasket(confirmComparisonSnapshotItems(query.ok ? query.state : null, items))
      },
      addFixedItem(item) {
        applyBasket(addFixedComparisonItem(query.ok ? query.state : null, item))
      },
      removeFixedItem(key) {
        applyBasket(removeFixedComparisonItem(query.ok ? query.state : null, key))
      },
      replaceFixedItems(items) {
        const result = replaceFixedComparisonItems(query.ok ? query.state : null, items)
        if (result.ok && result.state == null) {
          writeBareURL()
          return
        }
        applyBasket(result)
      },
      clearFixedItems() {
        writeBareURL()
      },
      setBaseline(index) {
        patchFixed((current) => ({ ...current, baseline: index }))
      },
      setAlignment(alignment) {
        patchFixed((current) => ({ ...current, alignment }))
      },
      setWindow(from, to) {
        if (!query.ok) return
        replaceQuery({ ...query.state, requested_from: from, requested_to: to })
      },
      setToleranceSeconds(seconds) {
        patchFixed((current) => ({ ...current, tolerance_seconds: seconds }))
      },
      setBucketSeconds(seconds) {
        patchFixed((current) => {
          const next = { ...current }
          if (seconds == null) delete next.bucket_seconds
          else next.bucket_seconds = seconds
          return next
        })
      },
      selectKind(kind, metric) {
        patchFixed((current) => {
          const next = { ...current, kind }
          const selectedMetric = metric ?? defaultComparisonMetric(kind)
          if (selectedMetric) next.metric = selectedMetric
          else delete next.metric
          return next
        })
      },
      // An unresolved sent request keeps the conclusion it published; edits wait for its result.
      setTitle(value) {
        if (guardsRef.current && !guardsRef.current.closed && !saveUnresolved && (value === title || beginEditingSaved())) setTitle(value)
      },
      setConclusion(value) {
        if (guardsRef.current && !guardsRef.current.closed && !saveUnresolved && (value === conclusion || beginEditingSaved())) setConclusion(value)
      },
      save,
      discardUnresolvedSave() {
        const attempt = saveAttemptRef.current
        if (guardsRef.current?.closed !== false || saving || (!attempt && !comparison)) return
        try {
          retireComparisonSaveAttempt(options.userId, attempt?.digest ?? comparison!.digest)
        } catch {
          metadataFailureRef.current = new Error('本地比较重试状态清理失败，请恢复浏览器存储后重新打开')
          return
        }
        saveAttemptRef.current = null
        bindTarget(null)
        setSaveUnresolved(false)
        setSaveFeedback({ key: feedbackKey, message: '已放弃确认上次另存；它可能已经创建了记录，请在记录列表核对', code: null })
      },
      cancel() {
        abortRef.current?.abort()
        saveAbortRef.current?.abort()
        saveGenerationRef.current += 1
        bindTarget(null)
        setSaving(false)
        cancelKeyRef.current = requestKey
        setCancelKey(requestKey)
      },
    },
  }
}

async function loadCandidates(state: ComparisonURLState, signal: AbortSignal) {
  const request: ComparisonCandidateRequest = {
    subjects: state.subjects ?? [],
    requested_window: { start: state.requested_from, end: state.requested_to },
  }
  const kind = state.kind ? comparisonKindFromURL(state.kind) : null
  if (kind) request.kinds = [kind]
  const response = await resolveComparisonCandidates(request, signal)
  return { kind: 'candidates' as const, candidates: response.candidates }
}

async function loadComparison(state: ComparisonURLState, signal: AbortSignal) {
  const request: ComparisonEvaluateRequest = {
    items: (state.items ?? []).map((item): ComparisonEvaluateRequest['items'][number] => (
      'snapshot_id' in item
        ? { snapshot_id: item.snapshot_id }
        : item.snapshot_ids?.length
          ? {
              record_id: item.record_id,
              revision_id: item.revision_id,
              snapshot_ids: item.snapshot_ids,
            }
          : {
              record_id: item.record_id,
              revision_id: item.revision_id,
            }
    )),
    baseline_index: state.baseline ?? 0,
    alignment: state.alignment ?? 'actual_coverage',
    requested_window: { start: state.requested_from, end: state.requested_to },
    tolerance_seconds: state.tolerance_seconds ?? 60,
  }
  if (state.bucket_seconds != null) request.bucket_seconds = state.bucket_seconds
  const kind = comparisonKindFromURL(state.kind)
  if (kind) {
    request.detail = {
      kind: kind.kind,
      schema_version: kind.schema_version,
      ...(state.metric ? { metric: state.metric } : {}),
    }
  }
  const comparison = await evaluateFixedComparison(request, signal)
  return { kind: 'comparison' as const, comparison }
}

function isAbortError(cause: unknown): boolean {
  return cause instanceof Error && cause.name === 'AbortError'
}

function retiresComparisonSave(cause: unknown): boolean {
  if (!(cause instanceof ApiError)) return false
  if (cause.status === 403 || cause.status === 404) return true
  return cause.status === 422
    && (cause.code === 'comparison_intent_invalid' || cause.code === 'comparison_intent_stale' || cause.code === 'comparison_intent_expired')
}

function isPresentationOnlyQueryChange(previous: ComparisonQueryParse, next: ComparisonQueryParse): boolean {
  if (!previous.ok || !next.ok) return false
  if (previous.state.mode !== 'fixed' || next.state.mode !== 'fixed') return false
  const previousKind = previous.state.kind ?? ''
  const nextKind = next.state.kind ?? ''
  const previousMetric = previous.state.metric ?? ''
  const nextMetric = next.state.metric ?? ''
  if (previousKind === nextKind && previousMetric === nextMetric) return false
  if (previousKind && previousKind !== nextKind) return false
  if (previousMetric && previousMetric !== nextMetric) return false
  return comparisonStateWithoutPresentation(previous.state) === comparisonStateWithoutPresentation(next.state)
}

function comparisonStateWithoutPresentation(state: ComparisonURLState): string {
  const canonical = canonicalComparisonURLState(state)
  delete canonical.kind
  delete canonical.metric
  return JSON.stringify(canonical)
}

function withDefaultKindAndMetric(
  state: ComparisonURLState,
  comparison: ComparisonEvaluateResponse,
): ComparisonURLState {
  if (state.mode !== 'fixed') return state
  const firstKind = comparison.available_kinds[0]
  if (!firstKind) return state
  const kind = state.kind || comparisonKindKey(firstKind)
  const metric = state.metric || defaultComparisonMetric(kind)
  if (kind === state.kind && metric === state.metric) return state
  return metric ? { ...state, kind, metric } : { ...state, kind }
}

function subjectsFromComparison(comparison: ComparisonEvaluateResponse): ComparisonSubjectRef[] {
  const seen = new Set<string>()
  const subjects: ComparisonSubjectRef[] = []
  for (const item of comparison.items) {
    if (
      (item.subject_kind !== 'vps' && item.subject_kind !== 'monitoring_instance' && item.subject_kind !== 'target') ||
      !item.subject_id
    ) {
      continue
    }
    const key = `${item.subject_kind}:${item.subject_id}`
    if (seen.has(key)) continue
    seen.add(key)
    subjects.push({ kind: item.subject_kind as ComparisonSubjectKind, id: item.subject_id })
  }
  return subjects
}

function comparisonSavePayload(
  userId: string,
  title: string,
  conclusion: string,
  subjects: ComparisonSubjectRef[],
): RecordDraftPayload {
  const payload = emptyRecordDraftPayload(userId)
  return {
    ...payload,
    title,
    body_markdown: conclusion,
    subjects: subjects.map((subject, index): RecordSubjectReference => ({
      registry_version: 1,
      kind: subject.kind,
      role: 'affected',
      source_id: subject.id,
      primary: index === 0,
    })),
    save_reason: 'comparison save',
  }
}

