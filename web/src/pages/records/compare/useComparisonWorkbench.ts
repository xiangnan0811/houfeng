import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { ApiError } from '../../../lib/apiRequest'
import {
  createRecordDraft,
  evaluateFixedComparison,
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
  title: string
  conclusion: string
  saveSubjects: ComparisonSubjectRef[]
  saving: boolean
  savedRecordId: string | null
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
  cancel: () => void
}>

type UseComparisonWorkbenchOptions = {
  userId: string
  newRecordId?: () => string
  newIdempotencyKey?: () => string
}

type SaveAttempt = {
  digest: string
  recordId: string
  idempotencyKey: string
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
  const [settled, setSettled] = useState<SettledWorkbench | null>(null)
  const [title, setTitle] = useState('')
  const [conclusion, setConclusion] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedRecordId, setSavedRecordId] = useState<string | null>(null)
  const [selectionError, setSelectionError] = useState<string | null>(null)
  const [cancelKey, setCancelKey] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const saveAbortRef = useRef<AbortController | null>(null)
  const saveGenerationRef = useRef(0)
  const cancelKeyRef = useRef<string | null>(null)
  const queryRef = useRef(query)
  const saveAttemptRef = useRef<SaveAttempt | null>(null)
  const requestKey = query.ok ? JSON.stringify(query.state) : query.reason
  const requestKeyRef = useRef(requestKey)
  const previousQueryRef = useRef(query)
  // History and external URL writes change requestKey without commitQuery.
  // Drop the visible save while rendering. The layout effect retires the
  // generation before passive effects, so the next draft microtask cannot publish.
  const [trackedQuery, setTrackedQuery] = useState({ requestKey, query })
  if (trackedQuery.requestKey !== requestKey) {
    const previousQuery = trackedQuery.query
    setTrackedQuery({ requestKey, query })
    if (!isPresentationOnlyQueryChange(previousQuery, query)) {
      if (savedRecordId !== null) setSavedRecordId(null)
      if (saving) setSaving(false)
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
    queryRef.current = query
    requestKeyRef.current = requestKey
    previousQueryRef.current = query
    if (previousKey === requestKey) return
    if (isPresentationOnlyQueryChange(previousQuery, query)) return
    // Basket commands already retire in commitQuery. History and external URL
    // writes do not, so retire here before passive effects and microtasks.
    saveGenerationRef.current += 1
    saveAbortRef.current?.abort()
  }, [query, requestKey])

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
    const run = current.state.mode === 'candidate'
      ? loadCandidates(current.state, controller.signal)
      : loadComparison(current.state, controller.signal)
    void run
      .then((result) => {
        if (controller.signal.aborted || requestKeyRef.current !== requestKey) return
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
  }, [requestKey, setSearchParams])

  useEffect(() => () => {
    saveAbortRef.current?.abort()
    saveGenerationRef.current += 1
  }, [])

  const saveBlocked = !comparison?.save_eligibility.eligible
    || !comparison.comparison_intent
    || comparison.save_eligibility.blockers.includes('snapshot_unreadable')

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
    if (saveBlocked || !comparison?.comparison_intent || saving) return
    const generation = saveGenerationRef.current
    const intentToken = comparison.comparison_intent.token
    const digest = comparison.digest
    const controller = new AbortController()
    saveAbortRef.current = controller
    setSaving(true)
    try {
      if (!saveAttemptRef.current || saveAttemptRef.current.digest !== digest) {
        saveAttemptRef.current = readSaveAttempt(digest) ?? {
          digest,
          recordId: options.newRecordId?.() ?? newComparisonRecordId(),
          idempotencyKey: options.newIdempotencyKey?.() ?? crypto.randomUUID(),
        }
        writeSaveAttempt(saveAttemptRef.current)
      }
      const attempt = saveAttemptRef.current
      const draft = await createRecordDraft({
        payload: comparisonSavePayload(options.userId, title, conclusion, saveSubjects),
      })
      if (generation !== saveGenerationRef.current || controller.signal.aborted) return
      const created = await saveComparisonRecord({
        record_id: attempt.recordId,
        draft_id: draft.draft_id,
        draft_etag: draft.etag,
        comparison_intent: intentToken,
      }, attempt.idempotencyKey, controller.signal)
      if (generation !== saveGenerationRef.current || controller.signal.aborted) return
      setSavedRecordId(created.record_id)
    } catch (cause) {
      if (generation !== saveGenerationRef.current || controller.signal.aborted || isAbortError(cause)) return
      const message = cause instanceof Error ? cause.message : '另存失败'
      const errorCode = cause instanceof ApiError ? cause.code ?? null : null
      const retire = retiresComparisonSave(cause)
      setSettled((current) => {
        if (!retire && current && current.requestKey === requestKey) {
          return {
            requestKey,
            candidates: current.candidates,
            comparison: current.comparison,
            error: message,
            errorCode,
          }
        }
        return {
          requestKey,
          candidates: null,
          comparison: retire ? null : comparison,
          error: message,
          errorCode,
        }
      })
    } finally {
      if (generation === saveGenerationRef.current) setSaving(false)
    }
  }

  return {
    state: {
      query,
      candidates,
      comparison,
      loading,
      error,
      errorCode,
      title,
      conclusion,
      saveSubjects,
      saving,
      savedRecordId,
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
      setTitle,
      setConclusion,
      save,
      cancel() {
        abortRef.current?.abort()
        saveAbortRef.current?.abort()
        saveGenerationRef.current += 1
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
    && (cause.code === 'comparison_intent_invalid' || cause.code === 'comparison_intent_stale')
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

function newComparisonRecordId(): string {
  const bytes = new Uint8Array(8)
  crypto.getRandomValues(bytes)
  return `rec_${Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')}`
}

function saveAttemptStorageKey(digest: string): string {
  return `houfeng.comparison-save/${digest}`
}

function readSaveAttempt(digest: string): SaveAttempt | null {
  try {
    const raw = sessionStorage.getItem(saveAttemptStorageKey(digest))
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<SaveAttempt>
    if (
      parsed.digest !== digest
      || typeof parsed.recordId !== 'string'
      || typeof parsed.idempotencyKey !== 'string'
      || !parsed.recordId
      || !parsed.idempotencyKey
    ) {
      return null
    }
    return {
      digest,
      recordId: parsed.recordId,
      idempotencyKey: parsed.idempotencyKey,
    }
  } catch {
    return null
  }
}

function writeSaveAttempt(attempt: SaveAttempt): void {
  try {
    sessionStorage.setItem(saveAttemptStorageKey(attempt.digest), JSON.stringify(attempt))
  } catch {
    // sessionStorage may be unavailable; in-memory ref still covers the current page.
  }
}
