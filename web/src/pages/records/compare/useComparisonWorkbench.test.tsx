import { act, renderHook, waitFor } from '@testing-library/react'
import { createContext, useContext, useEffect, type ReactNode } from 'react'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import * as recordsApi from '../../../lib/recordsApi'
import type {
  ComparisonCandidateResponse,
  ComparisonEvaluateResponse,
  RecordDraft,
  RecordMutationResult,
} from '../../../lib/types'
import {
  COMPARISON_SELECTION_LIMIT_ERROR,
  COMPARISON_URL_VERSION,
  comparisonFixedItemKey,
  encodeComparisonURLState,
  type ComparisonURLState,
} from './comparisonQueryState'
import { useComparisonWorkbench, type ComparisonWorkbenchCommands, type ComparisonWorkbenchState } from './useComparisonWorkbench'

const CANDIDATE: ComparisonURLState = {
  version: COMPARISON_URL_VERSION,
  mode: 'candidate',
  subjects: [
    { kind: 'vps', id: 'vps_0123456789abcdef' },
    { kind: 'vps', id: 'vps_0123456789abcde0' },
  ],
  requested_from: '2026-07-01T00:00:00Z',
  requested_to: '2026-07-02T00:00:00Z',
}

const FIXED: ComparisonURLState = {
  version: COMPARISON_URL_VERSION,
  mode: 'fixed',
  items: [
    { snapshot_id: 'evs_a' },
    { snapshot_id: 'evs_b' },
  ],
  baseline: 0,
  alignment: 'actual_coverage',
  requested_from: CANDIDATE.requested_from,
  requested_to: CANDIDATE.requested_to,
  tolerance_seconds: 60,
}

const CANDIDATE_LEFT = { kind: 'vps', id: 'vps_0123456789abcdef' } as const

const candidatesResponse: ComparisonCandidateResponse = {
  subjects: CANDIDATE.subjects ?? [],
  candidates: [{
    subject: CANDIDATE_LEFT,
    snapshot_id: 'evs_a',
    record_id: 'rec_a',
    revision_ids: ['rrv_a'],
    kind: 'monitoring.host',
    schema_version: 1,
    canonical_hash: 'aa'.repeat(32),
    requested_window: { start: CANDIDATE.requested_from, end: CANDIDATE.requested_to },
    actual_window: { start: CANDIDATE.requested_from, end: CANDIDATE.requested_to },
    quality_status: 'complete',
    captured_at: CANDIDATE.requested_to,
    recommendation: 'nearest_window',
  }],
}

const comparisonResponse: ComparisonEvaluateResponse = {
  digest: 'dd'.repeat(32),
  items: [
    {
      snapshot_id: 'evs_a',
      canonical_hash: '11'.repeat(32),
      kind: 'monitoring.host',
      schema_version: 1,
      revision_context: 'not_applicable',
      subject_kind: 'vps',
      subject_id: 'vps_0123456789abcdef',
    },
    {
      snapshot_id: 'evs_b',
      canonical_hash: '22'.repeat(32),
      kind: 'monitoring.host',
      schema_version: 1,
      revision_context: 'not_applicable',
      subject_kind: 'vps',
      subject_id: 'vps_0123456789abcde0',
    },
  ],
  review: [],
  available_kinds: [{ kind: 'monitoring.host', schema_version: 1 }],
  pairwise: [],
  series: [],
  save_eligibility: { eligible: true, blockers: [] },
  comparison_intent: {
    token: 'cmp1.valid.payload.mac',
    key_id: 'cmp_key',
    issued_at: '2026-08-20T10:00:00Z',
    expires_at: '2026-08-20T10:15:00Z',
  },
}

const saveDraft = {
  draft_id: 'rdf_compare',
  etag: 'rdt1_compare',
  record_id: 'rec_comparisonsave',
  payload: {} as RecordDraft['payload'],
  version: 1,
  created_at: '2026-08-20T10:00:00Z',
  updated_at: '2026-08-20T10:00:00Z',
  expires_at: '2026-11-01T10:00:00Z',
} as RecordDraft

const savedRecord: RecordMutationResult = {
  record_id: 'rec_comparisonsave',
  revision_id: 'rrv_comparisonsave',
  revision_no: 1,
  lock_version: 1,
  authorization_epoch: 1,
  lifecycle: 'active',
  created: true,
  replayed: false,
  committed_at: '2026-08-20T10:00:00Z',
}

function wrapper(initialURL: string) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={[initialURL]}>{children}</MemoryRouter>
  }
}

type HistoryControls = {
  back: () => void
  forward: () => void
  replace: (path: string) => void
}

type HistoryRegistry = {
  register: (controls: HistoryControls) => void
}

const HistoryControlsContext = createContext<HistoryRegistry | null>(null)

function historyWrapper(entries: string[], index = entries.length - 1) {
  const controls: HistoryControls = {
    back() {},
    forward() {},
    replace() {},
  }
  const registry: HistoryRegistry = {
    register(next) {
      controls.back = next.back
      controls.forward = next.forward
      controls.replace = next.replace
    },
  }
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <HistoryControlsContext.Provider value={registry}>
        <MemoryRouter initialEntries={entries} initialIndex={index}>
          <HistoryBridge />
          {children}
        </MemoryRouter>
      </HistoryControlsContext.Provider>
    )
  }
  return { Wrapper, controls }
}

function HistoryBridge() {
  const navigate = useNavigate()
  const registry = useContext(HistoryControlsContext)
  useEffect(() => {
    registry?.register({
      back: () => navigate(-1),
      forward: () => navigate(1),
      replace: (path: string) => navigate(path, { replace: true }),
    })
  }, [navigate, registry])
  return null
}

function compareURL(state: ComparisonURLState): string {
  return `/records/compare?state=${encodeComparisonURLState(state)}`
}

describe('useComparisonWorkbench', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads candidates without calling compare until IDs are confirmed', async () => {
    const resolve = vi.spyOn(recordsApi, 'resolveComparisonCandidates').mockResolvedValue(candidatesResponse)
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison')
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(CANDIDATE)) },
    )

    await waitFor(() => {
      expect(result.current.state.candidates).toEqual(candidatesResponse.candidates)
    })
    expect(resolve).toHaveBeenCalledTimes(1)
    expect(compare).not.toHaveBeenCalled()
    expect(resolve.mock.calls[0]?.[1]).toBeInstanceOf(AbortSignal)

    compare.mockResolvedValue(comparisonResponse)
    act(() => {
      result.current.commands.confirmCandidates([
        { snapshot_id: 'evs_a' },
        { snapshot_id: 'evs_b' },
      ])
    })
    await waitFor(() => {
      expect(compare).toHaveBeenCalled()
    })
    expect(compare.mock.calls[0]?.[0]).toMatchObject({
      items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
      baseline_index: 0,
    })
    await waitFor(() => {
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
      expect(result.current.state.query.ok && result.current.state.query.state.metric).toBe('cpu_usage_pct')
    })
  })

  it('aborts an in-flight compare when conditions change', async () => {
    let rejectFirst: ((reason: unknown) => void) | undefined
    const first = new Promise<ComparisonEvaluateResponse>((_, reject) => {
      rejectFirst = reject
    })
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison')
      .mockImplementationOnce((_input, signal) => {
        signal?.addEventListener('abort', () => rejectFirst?.(new DOMException('aborted', 'AbortError')))
        return first
      })
      .mockResolvedValue(comparisonResponse)
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )

    await waitFor(() => {
      expect(compare).toHaveBeenCalledTimes(1)
    })
    act(() => {
      result.current.commands.setAlignment('common_overlap')
    })
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.loading).toBe(true)
    await waitFor(() => {
      expect(compare).toHaveBeenCalledTimes(2)
    })
    expect(compare.mock.calls[0]?.[1]?.aborted).toBe(true)
    await waitFor(() => {
      expect(result.current.state.comparison?.digest).toBe(comparisonResponse.digest)
    })
    expect(result.current.state.error).toBeNull()
    await waitFor(() => {
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
      expect(result.current.state.query.ok && result.current.state.query.state.metric).toBe('cpu_usage_pct')
    })
  })

  it('cancels an in-flight compare and does not keep the late result', async () => {
    let rejectFirst: ((reason: unknown) => void) | undefined
    const first = new Promise<ComparisonEvaluateResponse>((_, reject) => {
      rejectFirst = reject
    })
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison')
      .mockImplementation((_input, signal) => {
        signal?.addEventListener('abort', () => rejectFirst?.(new DOMException('aborted', 'AbortError')))
        return first
      })
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )

    await waitFor(() => {
      expect(compare).toHaveBeenCalledTimes(1)
      expect(result.current.state.loading).toBe(true)
    })
    act(() => {
      result.current.commands.cancel()
    })
    expect(result.current.state.loading).toBe(false)
    expect(result.current.state.cancelled).toBe(true)
    expect(result.current.state.comparison).toBeNull()
    expect(compare.mock.calls[0]?.[1]?.aborted).toBe(true)
    await waitFor(() => {
      expect(result.current.state.comparison).toBeNull()
    })
    expect(result.current.state.error).toBeNull()
  })

  it('clears comparison identities after an opaque 404', async () => {
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockRejectedValue(
      new ApiError(404, 'resource not found', { code: 'resource_not_found' }),
    )
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )

    await waitFor(() => {
      expect(result.current.state.errorCode).toBe('resource_not_found')
    })
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.candidates).toBeNull()
  })

  it('saves through comparison intent and retries the same record id', async () => {
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const draft = {
      draft_id: 'rdf_compare',
      etag: 'rdt1_compare',
      record_id: 'rec_comparisonsave',
      payload: {} as RecordDraft['payload'],
      version: 1,
      created_at: '2026-08-20T10:00:00Z',
      updated_at: '2026-08-20T10:00:00Z',
      expires_at: '2026-11-01T10:00:00Z',
    } as RecordDraft
    const created: RecordMutationResult = {
      record_id: 'rec_comparisonsave',
      revision_id: 'rrv_comparisonsave',
      revision_no: 1,
      lock_version: 1,
      authorization_epoch: 1,
      lifecycle: 'active',
      created: true,
      replayed: false,
      committed_at: '2026-08-20T10:00:00Z',
    }
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(draft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(created)
    const publish = vi.spyOn(recordsApi, 'createRecord')
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_comparisonsave',
        newIdempotencyKey: () => 'comparison-save-key',
      }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )

    await waitFor(() => {
      expect(result.current.state.comparison).not.toBeNull()
      expect(result.current.state.saveSubjects).toEqual(CANDIDATE.subjects)
    })
    act(() => {
      result.current.commands.setTitle('横向比较')
      result.current.commands.setConclusion('人工结论只进修订')
    })
    await act(async () => {
      await result.current.commands.save()
    })
    await act(async () => {
      await result.current.commands.save()
    })

    expect(publish).not.toHaveBeenCalled()
    expect(createDraft).toHaveBeenCalled()
    expect(createDraft.mock.calls[0]?.[0]).toMatchObject({
      payload: expect.objectContaining({
        title: '横向比较',
        body_markdown: '人工结论只进修订',
      }),
    })
    expect(save).toHaveBeenCalledTimes(2)
    expect(save.mock.calls[0]?.[0]).toEqual({
      record_id: 'rec_comparisonsave',
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      comparison_intent: 'cmp1.valid.payload.mac',
    })
    expect(save.mock.calls[0]?.[1]).toBe('comparison-save-key')
    expect(save.mock.calls[1]?.[0].record_id).toBe('rec_comparisonsave')
    expect(save.mock.calls[1]?.[1]).toBe('comparison-save-key')
    expect(result.current.state.savedRecordId).toBe('rec_comparisonsave')
  })

  it('reuses the digest-scoped save attempt after remount', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue({
      draft_id: 'rdf_compare',
      etag: 'rdt1_compare',
      record_id: 'rec_comparisonsave',
      payload: {} as RecordDraft['payload'],
      version: 1,
      created_at: '2026-08-20T10:00:00Z',
      updated_at: '2026-08-20T10:00:00Z',
      expires_at: '2026-11-01T10:00:00Z',
    } as RecordDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue({
      record_id: 'rec_comparisonsave',
      revision_id: 'rrv_comparisonsave',
      revision_no: 1,
      lock_version: 1,
      authorization_epoch: 1,
      lifecycle: 'active',
      created: true,
      replayed: false,
      committed_at: '2026-08-20T10:00:00Z',
    })
    let keys = 0
    const options = {
      userId: 'usr_1',
      newRecordId: () => `rec_retry_${keys}`,
      newIdempotencyKey: () => `comparison-retry-${++keys}`,
    }
    const first = renderHook(() => useComparisonWorkbench(options), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => {
      expect(first.result.current.state.comparison).not.toBeNull()
    })
    await act(async () => {
      await first.result.current.commands.save()
    })
    first.unmount()
    const second = renderHook(() => useComparisonWorkbench(options), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => {
      expect(second.result.current.state.comparison).not.toBeNull()
    })
    await act(async () => {
      await second.result.current.commands.save()
    })
    expect(save).toHaveBeenCalledTimes(2)
    expect(save.mock.calls[0]?.[1]).toBe('comparison-retry-1')
    expect(save.mock.calls[1]?.[1]).toBe('comparison-retry-1')
    expect(keys).toBe(1)
  })

  it('does not render a save action when the intent is blocked', async () => {
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue({
      ...comparisonResponse,
      save_eligibility: { eligible: false, blockers: ['snapshot_unreadable'] },
    })
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )

    await waitFor(() => {
      expect(result.current.state.saveBlocked).toBe(true)
    })
    await act(async () => {
      await result.current.commands.save()
    })
    expect(save).not.toHaveBeenCalled()
  })

  it('does not evaluate zero or one fixed objects, then evaluates the fixed pair', async () => {
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper('/records/compare') },
    )
    expect(result.current.state.query).toEqual({ ok: false, reason: 'missing' })
    expect(result.current.state.loading).toBe(false)

    act(() => {
      result.current.commands.addFixedItem({ snapshot_id: 'evs_a' })
    })
    expect(compare).not.toHaveBeenCalled()
    expect(result.current.state.loading).toBe(false)
    expect(fixedItems(result.current.state.query)).toEqual([{ snapshot_id: 'evs_a' }])

    act(() => {
      result.current.commands.addFixedItem({ snapshot_id: 'evs_b' })
    })
    await waitFor(() => {
      expect(compare).toHaveBeenCalled()
    })
    expect(compare.mock.calls.at(-1)?.[0]).toMatchObject({
      items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
      baseline_index: 0,
    })
  })

  it('keeps two evidence objects and sends a replaced revision selection', async () => {
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const seeded: ComparisonURLState = {
      ...FIXED,
      items: [{ record_id: 'rec_same', revision_id: 'rrv_1', snapshot_ids: ['evs_a'] }],
    }
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(seeded)) },
    )
    expect(compare).not.toHaveBeenCalled()

    act(() => {
      result.current.commands.addFixedItem({
        record_id: 'rec_same',
        revision_id: 'rrv_2',
        snapshot_ids: ['evs_b'],
      })
    })
    await waitFor(() => {
      expect(compare).toHaveBeenCalled()
    })
    expect(compare.mock.calls.at(-1)?.[0].items).toEqual([
      { record_id: 'rec_same', revision_id: 'rrv_1', snapshot_ids: ['evs_a'] },
      { record_id: 'rec_same', revision_id: 'rrv_2', snapshot_ids: ['evs_b'] },
    ])

    act(() => {
      result.current.commands.addFixedItem({
        record_id: 'rec_same',
        revision_id: 'rrv_1',
        snapshot_ids: ['evs_c'],
      })
    })
    await waitFor(() => {
      expect(compare.mock.calls.at(-1)?.[0].items).toEqual([
        { record_id: 'rec_same', revision_id: 'rrv_1', snapshot_ids: ['evs_c'] },
        { record_id: 'rec_same', revision_id: 'rrv_2', snapshot_ids: ['evs_b'] },
      ])
    })
    expect(fixedItems(result.current.state.query)).toHaveLength(2)
  })

  it('rejects a seventh object in Chinese and leaves the basket unchanged', async () => {
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const six: ComparisonURLState = {
      ...FIXED,
      items: Array.from({ length: 6 }, (_, index) => ({ snapshot_id: `evs_${index}` })),
    }
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(six)) },
    )
    await waitFor(() => {
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    const calls = compare.mock.calls.length

    act(() => {
      result.current.commands.addFixedItem({ snapshot_id: 'evs_6' })
    })
    expect(result.current.state.selectionError).toBe(COMPARISON_SELECTION_LIMIT_ERROR)
    expect(fixedItems(result.current.state.query)).toHaveLength(6)
    expect(compare).toHaveBeenCalledTimes(calls)

    act(() => {
      result.current.commands.replaceFixedItems([
        ...fixedItems(result.current.state.query),
        { snapshot_id: 'evs_7' },
      ])
    })
    expect(result.current.state.selectionError).toBe(COMPARISON_SELECTION_LIMIT_ERROR)
    expect(fixedItems(result.current.state.query).map((item) => comparisonFixedItemKey(item))).toEqual([
      'snapshot:evs_0',
      'snapshot:evs_1',
      'snapshot:evs_2',
      'snapshot:evs_3',
      'snapshot:evs_4',
      'snapshot:evs_5',
    ])

    act(() => {
      result.current.commands.removeFixedItem(comparisonFixedItemKey({ snapshot_id: 'evs_5' }))
    })
    expect(result.current.state.selectionError).toBeNull()
    expect(fixedItems(result.current.state.query)).toHaveLength(5)
  })

  it('selects the next baseline, then the last, and clears to an empty URL', async () => {
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const three: ComparisonURLState = {
      ...FIXED,
      items: [
        { snapshot_id: 'evs_a' },
        { snapshot_id: 'evs_b' },
        { snapshot_id: 'evs_c' },
      ],
    }
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(three)) },
    )

    act(() => {
      result.current.commands.removeFixedItem(comparisonFixedItemKey({ snapshot_id: 'evs_a' }))
    })
    expect(fixedItems(result.current.state.query)).toEqual([
      { snapshot_id: 'evs_b' },
      { snapshot_id: 'evs_c' },
    ])
    expect(result.current.state.query.ok && result.current.state.query.state.baseline).toBe(0)
    await waitFor(() => {
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })

    act(() => {
      result.current.commands.setBaseline(1)
    })
    act(() => {
      result.current.commands.removeFixedItem(comparisonFixedItemKey({ snapshot_id: 'evs_c' }))
    })
    expect(fixedItems(result.current.state.query)).toEqual([{ snapshot_id: 'evs_b' }])
    expect(result.current.state.query.ok && result.current.state.query.state.baseline).toBe(0)
    expect(result.current.state.loading).toBe(false)

    act(() => {
      result.current.commands.clearFixedItems()
    })
    expect(result.current.state.query).toEqual({ ok: false, reason: 'missing' })
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.selectionError).toBeNull()
    expect(result.current.state.loading).toBe(false)
  })

  it('ignores a comparison that finishes after the basket changes', async () => {
    let resolveFirst: (value: ComparisonEvaluateResponse) => void = () => {}
    const first = new Promise<ComparisonEvaluateResponse>((resolve) => {
      resolveFirst = resolve
    })
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison')
      .mockImplementationOnce(() => first)
      .mockResolvedValue({ ...comparisonResponse, digest: 'ff'.repeat(32) })
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => {
      expect(compare).toHaveBeenCalledTimes(1)
    })

    act(() => {
      result.current.commands.addFixedItem({ snapshot_id: 'evs_c' })
    })
    expect(compare.mock.calls[0]?.[1]?.aborted).toBe(true)
    await act(async () => {
      resolveFirst({ ...comparisonResponse, digest: 'ee'.repeat(32) })
    })
    expect(result.current.state.comparison?.digest).not.toBe('ee'.repeat(32))
    expect(result.current.state.error).toBeNull()
    await waitFor(() => {
      expect(result.current.state.comparison?.digest).toBe('ff'.repeat(32))
    })
    expect(fixedItems(result.current.state.query).map((item) => (
      'snapshot_id' in item ? item.snapshot_id : item.revision_id
    ))).toEqual(['evs_a', 'evs_b', 'evs_c'])
  })

  it('ignores a save that finishes after the basket is cleared', async () => {
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    let resolveSave: (value: RecordMutationResult) => void = () => {}
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockImplementation(
      (_input, _key, signal) => new Promise((resolve) => {
        signal?.addEventListener('abort', () => {})
        resolveSave = resolve
      }),
    )
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => {
      expect(result.current.state.comparison).not.toBeNull()
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    let savePromise = Promise.resolve()
    act(() => {
      savePromise = result.current.commands.save()
    })
    await waitFor(() => {
      expect(save).toHaveBeenCalledTimes(1)
    })

    act(() => {
      result.current.commands.clearFixedItems()
    })
    expect(save.mock.calls[0]?.[2]?.aborted).toBe(true)
    await act(async () => {
      resolveSave(savedRecord)
      await savePromise
    })
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.error).toBeNull()
    expect(result.current.state.query).toEqual({ ok: false, reason: 'missing' })
    expect(result.current.state.saving).toBe(false)
    expect(save).toHaveBeenCalledTimes(1)
  })

  it('confirms snapshot ids only and does not evaluate an over-limit selection', async () => {
    const resolve = vi.spyOn(recordsApi, 'resolveComparisonCandidates').mockResolvedValue(candidatesResponse)
    const compare = vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1' }),
      { wrapper: wrapper(compareURL(CANDIDATE)) },
    )
    await waitFor(() => {
      expect(resolve).toHaveBeenCalledTimes(1)
    })

    act(() => {
      result.current.commands.confirmCandidates([
        { record_id: 'rec_a', revision_id: 'rrv_a' },
        { record_id: 'rec_b', revision_id: 'rrv_b' },
      ])
    })
    expect(compare).not.toHaveBeenCalled()
    expect(result.current.state.query.ok && result.current.state.query.state.mode).toBe('candidate')

    act(() => {
      result.current.commands.confirmCandidates([
        { record_id: 'rec_a', revision_id: 'rrv_ignored', snapshot_ids: ['evs_a'] },
        { record_id: 'rec_b', revision_id: 'rrv_other', snapshot_ids: ['evs_b', 'evs_b'] },
      ])
    })
    await waitFor(() => {
      expect(compare.mock.calls[0]?.[0].items).toEqual([
        { snapshot_id: 'evs_a' },
        { snapshot_id: 'evs_b' },
      ])
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    const calls = compare.mock.calls.length

    act(() => {
      result.current.commands.confirmCandidates(Array.from(
        { length: 7 },
        (_, index) => ({ snapshot_id: `evs_${index}` }),
      ))
    })
    expect(result.current.state.selectionError).toBe(COMPARISON_SELECTION_LIMIT_ERROR)
    expect(compare).toHaveBeenCalledTimes(calls)
    expect(fixedItems(result.current.state.query)).toEqual([
      { snapshot_id: 'evs_a' },
      { snapshot_id: 'evs_b' },
    ])
  })

  it.each([
    ['403', new ApiError(403, 'forbidden', { code: 'permission_denied' }), 'permission_denied'],
    ['404', new ApiError(404, 'hidden', { code: 'resource_not_found' }), 'resource_not_found'],
    ['422 comparison_intent_invalid', new ApiError(422, 'comparison intent is invalid', { code: 'comparison_intent_invalid' }), 'comparison_intent_invalid'],
    ['422 comparison_intent_stale', new ApiError(422, 'comparison intent is stale', { code: 'comparison_intent_stale' }), 'comparison_intent_stale'],
  ] as const)('retires comparison results and the signed intent when save returns %s', async (_label, failure, code) => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockRejectedValue(failure)
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_denied',
        newIdempotencyKey: () => 'comparison-denied',
      }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => {
      expect(result.current.state.comparison?.comparison_intent?.token).toBe('cmp1.valid.payload.mac')
      expect(result.current.state.candidates).toBeNull()
    })
    await act(async () => {
      await result.current.commands.save()
    })
    await act(async () => {
      await result.current.commands.save()
    })

    expect(result.current.state.errorCode).toBe(code)
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.candidates).toBeNull()
    expect(result.current.state.saveBlocked).toBe(true)
    expect(result.current.state.saving).toBe(false)
    expect(fixedItems(result.current.state.query)).toEqual(FIXED.items)
    expect(JSON.stringify(result.current.state)).not.toContain('cmp1.valid.payload.mac')
    expect(save).toHaveBeenCalledTimes(1)
  })

  it('keeps the signed intent and retries a transport failure with the same idempotency key', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockRejectedValue(
      new ApiError(503, 'unavailable'),
    )
    let deniedKeys = 0
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_denied',
        newIdempotencyKey: () => `comparison-denied-${++deniedKeys}`,
      }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => {
      expect(result.current.state.comparison?.comparison_intent?.token).toBe('cmp1.valid.payload.mac')
    })
    await act(async () => {
      await result.current.commands.save()
    })
    await act(async () => {
      await result.current.commands.save()
    })

    expect(result.current.state.errorCode).toBeNull()
    expect(result.current.state.comparison?.comparison_intent?.token).toBe('cmp1.valid.payload.mac')
    expect(result.current.state.saveBlocked).toBe(false)
    expect(fixedItems(result.current.state.query)).toEqual(FIXED.items)
    expect(save).toHaveBeenCalledTimes(2)
    expect(save.mock.calls[0]?.[0].comparison_intent).toBe('cmp1.valid.payload.mac')
    expect(deniedKeys).toBe(1)
    expect(save.mock.calls[0]?.[1]).toBe('comparison-denied-1')
    expect(save.mock.calls[1]?.[1]).toBe('comparison-denied-1')
  })

  it('does not publish a draft that resolves after browser back or forward', async () => {
    sessionStorage.clear()
    const other: ComparisonURLState = {
      ...FIXED,
      items: [{ snapshot_id: 'evs_c' }, { snapshot_id: 'evs_d' }],
    }
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    let resolveDraft: (value: RecordDraft) => void = () => {}
    vi.spyOn(recordsApi, 'createRecordDraft').mockImplementation(
      () => new Promise((resolve) => {
        resolveDraft = resolve
      }),
    )
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
    const { Wrapper, controls } = historyWrapper([compareURL(other), compareURL(FIXED)])
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_comparisonsave',
        newIdempotencyKey: () => 'comparison-history-draft',
      }),
      { wrapper: Wrapper },
    )
    await waitFor(() => {
      expect(result.current.state.comparison).not.toBeNull()
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    let savePromise = Promise.resolve()
    act(() => {
      savePromise = result.current.commands.save()
    })
    await waitFor(() => {
      expect(recordsApi.createRecordDraft).toHaveBeenCalledTimes(1)
    })

    act(() => {
      controls.back()
    })
    expect(result.current.state.saving).toBe(false)
    expect(result.current.state.savedRecordId).toBeNull()
    expect(fixedItems(result.current.state.query).map((item) => (
      'snapshot_id' in item ? item.snapshot_id : item.revision_id
    ))).toEqual(['evs_c', 'evs_d'])

    await act(async () => {
      resolveDraft(saveDraft)
      await savePromise
    })
    expect(save).not.toHaveBeenCalled()
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.saving).toBe(false)
    expect(result.current.state.error).toBeNull()

    act(() => {
      controls.forward()
    })
    expect(save).not.toHaveBeenCalled()
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.saving).toBe(false)
    expect(fixedItems(result.current.state.query).map((item) => (
      'snapshot_id' in item ? item.snapshot_id : item.revision_id
    ))).toEqual(['evs_a', 'evs_b'])
  })

  it('drops a published save id and a late save when an external URL leaves the basket', async () => {
    sessionStorage.clear()
    const other: ComparisonURLState = {
      ...FIXED,
      items: [{ snapshot_id: 'evs_c' }, { snapshot_id: 'evs_d' }],
    }
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const { Wrapper, controls } = historyWrapper([compareURL(FIXED)])
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_comparisonsave',
        newIdempotencyKey: () => 'comparison-external-published',
      }),
      { wrapper: Wrapper },
    )
    await waitFor(() => {
      expect(result.current.state.comparison?.comparison_intent?.token).toBe('cmp1.valid.payload.mac')
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    await act(async () => {
      await result.current.commands.save()
    })
    expect(result.current.state.savedRecordId).toBe('rec_comparisonsave')

    act(() => {
      controls.replace(compareURL(other))
    })
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.saving).toBe(false)
    expect(fixedItems(result.current.state.query).map((item) => (
      'snapshot_id' in item ? item.snapshot_id : item.revision_id
    ))).toEqual(['evs_c', 'evs_d'])

    let resolveSave: (value: RecordMutationResult) => void = () => {}
    save.mockImplementation((_input, _key, signal) => new Promise((resolve) => {
      signal?.addEventListener('abort', () => {})
      resolveSave = resolve
    }))
    await waitFor(() => {
      expect(result.current.state.comparison).not.toBeNull()
      expect(result.current.state.saveBlocked).toBe(false)
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    let savePromise = Promise.resolve()
    act(() => {
      savePromise = result.current.commands.save()
    })
    await waitFor(() => {
      expect(save).toHaveBeenCalledTimes(2)
    })
    expect(result.current.state.saving).toBe(true)

    act(() => {
      controls.replace(compareURL(FIXED))
    })
    expect(save.mock.calls[1]?.[2]?.aborted).toBe(true)
    expect(result.current.state.saving).toBe(false)
    expect(result.current.state.savedRecordId).toBeNull()
    await act(async () => {
      resolveSave(savedRecord)
      await savePromise
    })
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.saving).toBe(false)
    expect(result.current.state.error).toBeNull()
    expect(save).toHaveBeenCalledTimes(2)
  })

  it('does not publish a draft or save that resolves after unmount', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    let resolveDraft: (value: RecordDraft) => void = () => {}
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockImplementation(
      () => new Promise((resolve) => {
        resolveDraft = resolve
      }),
    )
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const draftHook = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_comparisonsave',
        newIdempotencyKey: () => 'comparison-unmount-draft',
      }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => {
      expect(draftHook.result.current.state.comparison).not.toBeNull()
      expect(draftHook.result.current.state.query.ok && draftHook.result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    let draftSave = Promise.resolve()
    act(() => {
      draftSave = draftHook.result.current.commands.save()
    })
    await waitFor(() => {
      expect(createDraft).toHaveBeenCalledTimes(1)
    })
    draftHook.unmount()
    await act(async () => {
      resolveDraft(saveDraft)
      await draftSave
    })
    expect(save).not.toHaveBeenCalled()

    createDraft.mockResolvedValue(saveDraft)
    let resolveSave: (value: RecordMutationResult) => void = () => {}
    save.mockImplementation((_input, _key, signal) => new Promise((resolve) => {
      signal?.addEventListener('abort', () => {})
      resolveSave = resolve
    }))
    const saveHook = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_comparisonsave',
        newIdempotencyKey: () => 'comparison-unmount-save',
      }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => {
      expect(saveHook.result.current.state.comparison).not.toBeNull()
      expect(saveHook.result.current.state.saveBlocked).toBe(false)
      expect(saveHook.result.current.state.query.ok && saveHook.result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    let savePromise = Promise.resolve()
    act(() => {
      savePromise = saveHook.result.current.commands.save()
    })
    await waitFor(() => {
      expect(save).toHaveBeenCalledTimes(1)
    })
    saveHook.unmount()
    expect(save.mock.calls[0]?.[2]?.aborted).toBe(true)
    await act(async () => {
      resolveSave(savedRecord)
      await savePromise
    })
    expect(save).toHaveBeenCalledTimes(1)
  })

  it('cancels an in-flight save without publishing it', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    let resolveSave: (value: RecordMutationResult) => void = () => {}
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockImplementation(
      (_input, _key, signal) => new Promise((resolve) => {
        signal?.addEventListener('abort', () => {})
        resolveSave = resolve
      }),
    )
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_comparisonsave',
        newIdempotencyKey: () => 'comparison-cancel-save',
      }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => {
      expect(result.current.state.comparison).not.toBeNull()
      expect(result.current.state.saveBlocked).toBe(false)
      expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    })
    let savePromise = Promise.resolve()
    act(() => {
      savePromise = result.current.commands.save()
    })
    await waitFor(() => {
      expect(save).toHaveBeenCalledTimes(1)
      expect(result.current.state.saving).toBe(true)
    })

    act(() => {
      result.current.commands.cancel()
    })
    expect(result.current.state.cancelled).toBe(true)
    expect(result.current.state.saving).toBe(false)
    expect(save.mock.calls[0]?.[2]?.aborted).toBe(true)
    await act(async () => {
      resolveSave(savedRecord)
      await savePromise
    })
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.saving).toBe(false)
    expect(result.current.state.error).toBeNull()
    expect(fixedItems(result.current.state.query)).toEqual(FIXED.items)
  })

  it('keeps an in-flight save when the URL only fills default kind and metric', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue({
      ...comparisonResponse,
      available_kinds: [],
    })
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    let resolveSave: (value: RecordMutationResult) => void = () => {}
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockImplementation(
      (_input, _key, signal) => new Promise((resolve) => {
        signal?.addEventListener('abort', () => {})
        resolveSave = resolve
      }),
    )
    const { Wrapper, controls } = historyWrapper([compareURL(FIXED)])
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
        newRecordId: () => 'rec_comparisonsave',
        newIdempotencyKey: () => 'comparison-presentation-save',
      }),
      { wrapper: Wrapper },
    )
    await waitFor(() => {
      expect(result.current.state.comparison).not.toBeNull()
      expect(result.current.state.saveBlocked).toBe(false)
    })
    expect(result.current.state.query.ok && result.current.state.query.state.kind).toBeUndefined()
    let savePromise = Promise.resolve()
    act(() => {
      savePromise = result.current.commands.save()
    })
    await waitFor(() => {
      expect(save).toHaveBeenCalledTimes(1)
      expect(result.current.state.saving).toBe(true)
    })

    act(() => {
      controls.replace(compareURL({
        ...FIXED,
        kind: 'monitoring.host/v1',
        metric: 'cpu_usage_pct',
      }))
    })
    expect(save.mock.calls[0]?.[2]?.aborted).toBe(false)
    expect(result.current.state.saving).toBe(true)
    expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    expect(result.current.state.query.ok && result.current.state.query.state.metric).toBe('cpu_usage_pct')

    await act(async () => {
      resolveSave(savedRecord)
      await savePromise
    })
    expect(result.current.state.savedRecordId).toBe('rec_comparisonsave')
    expect(result.current.state.saving).toBe(false)
    expect(result.current.state.error).toBeNull()
    expect(save).toHaveBeenCalledTimes(1)
  })

  it('derives save subjects from the current evaluation after clear then basket B', async () => {
    sessionStorage.clear()
    installBasketSubjectEvaluation()
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const { result } = renderHook(
      () => useComparisonWorkbench(comparisonSaveOptions('clear-b')),
      { wrapper: wrapper(compareURL(fixedBasket(['evs_a', 'evs_b']))) },
    )
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_a, BASKET_SUBJECTS.evs_b])

    act(() => {
      result.current.commands.clearFixedItems()
    })
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.query).toEqual({ ok: false, reason: 'missing' })

    act(() => {
      result.current.commands.addFixedItem({ snapshot_id: 'evs_c' })
    })
    expect(fixedItems(result.current.state.query)).toEqual([{ snapshot_id: 'evs_c' }])
    act(() => {
      result.current.commands.addFixedItem({ snapshot_id: 'evs_d' })
    })
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_c, BASKET_SUBJECTS.evs_d])
    await act(async () => {
      await result.current.commands.save()
    })

    expectSavedSubjects(createDraft, [BASKET_SUBJECTS.evs_c, BASKET_SUBJECTS.evs_d])
    expect(result.current.state.saveSubjects).toEqual([BASKET_SUBJECTS.evs_c, BASKET_SUBJECTS.evs_d])
  })

  it('derives save subjects from the current evaluation after adding a subject', async () => {
    sessionStorage.clear()
    installBasketSubjectEvaluation()
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const { result } = renderHook(
      () => useComparisonWorkbench(comparisonSaveOptions('add-subject')),
      { wrapper: wrapper(compareURL(fixedBasket(['evs_a', 'evs_b']))) },
    )
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_a, BASKET_SUBJECTS.evs_b])

    act(() => {
      result.current.commands.addFixedItem({ snapshot_id: 'evs_c' })
    })
    await settleEvaluatedSubjects(result, [
      BASKET_SUBJECTS.evs_a,
      BASKET_SUBJECTS.evs_b,
      BASKET_SUBJECTS.evs_c,
    ])
    await act(async () => {
      await result.current.commands.save()
    })

    expectSavedSubjects(createDraft, [
      BASKET_SUBJECTS.evs_a,
      BASKET_SUBJECTS.evs_b,
      BASKET_SUBJECTS.evs_c,
    ])
    expect(result.current.state.saveSubjects).toEqual([
      BASKET_SUBJECTS.evs_a,
      BASKET_SUBJECTS.evs_b,
      BASKET_SUBJECTS.evs_c,
    ])
  })

  it('derives save subjects from the current evaluation after removing a subject', async () => {
    sessionStorage.clear()
    installBasketSubjectEvaluation()
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const { result } = renderHook(
      () => useComparisonWorkbench(comparisonSaveOptions('remove-subject')),
      { wrapper: wrapper(compareURL(fixedBasket(['evs_a', 'evs_b', 'evs_c']))) },
    )
    await settleEvaluatedSubjects(result, [
      BASKET_SUBJECTS.evs_a,
      BASKET_SUBJECTS.evs_b,
      BASKET_SUBJECTS.evs_c,
    ])

    act(() => {
      result.current.commands.removeFixedItem(comparisonFixedItemKey({ snapshot_id: 'evs_a' }))
    })
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_b, BASKET_SUBJECTS.evs_c])
    await act(async () => {
      await result.current.commands.save()
    })

    expectSavedSubjects(createDraft, [BASKET_SUBJECTS.evs_b, BASKET_SUBJECTS.evs_c])
    expect(result.current.state.saveSubjects).toEqual([BASKET_SUBJECTS.evs_b, BASKET_SUBJECTS.evs_c])
  })

  it('derives save subjects from the current evaluation after a history URL change', async () => {
    sessionStorage.clear()
    installBasketSubjectEvaluation()
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const basketA = fixedBasket(['evs_a', 'evs_b'])
    const basketB = fixedBasket(['evs_c', 'evs_d'])
    const { Wrapper, controls } = historyWrapper([compareURL(basketB), compareURL(basketA)])
    const { result } = renderHook(
      () => useComparisonWorkbench(comparisonSaveOptions('history-subjects')),
      { wrapper: Wrapper },
    )
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_a, BASKET_SUBJECTS.evs_b])

    act(() => {
      controls.back()
    })
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_c, BASKET_SUBJECTS.evs_d])
    await act(async () => {
      await result.current.commands.save()
    })
    expectSavedSubjects(createDraft, [BASKET_SUBJECTS.evs_c, BASKET_SUBJECTS.evs_d])

    act(() => {
      controls.forward()
    })
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_a, BASKET_SUBJECTS.evs_b])
    await act(async () => {
      await result.current.commands.save()
    })
    expectSavedSubjects(createDraft, [BASKET_SUBJECTS.evs_a, BASKET_SUBJECTS.evs_b])
  })

  it('derives save subjects from the current evaluation after an external URL change', async () => {
    sessionStorage.clear()
    installBasketSubjectEvaluation()
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const { Wrapper, controls } = historyWrapper([compareURL(fixedBasket(['evs_a', 'evs_b']))])
    const { result } = renderHook(
      () => useComparisonWorkbench(comparisonSaveOptions('external-subjects')),
      { wrapper: Wrapper },
    )
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_a, BASKET_SUBJECTS.evs_b])

    act(() => {
      controls.replace(compareURL(fixedBasket(['evs_e', 'evs_f'])))
    })
    await settleEvaluatedSubjects(result, [BASKET_SUBJECTS.evs_e, BASKET_SUBJECTS.evs_f])
    await act(async () => {
      await result.current.commands.save()
    })

    expectSavedSubjects(createDraft, [BASKET_SUBJECTS.evs_e, BASKET_SUBJECTS.evs_f])
    expect(result.current.state.saveSubjects).toEqual([BASKET_SUBJECTS.evs_e, BASKET_SUBJECTS.evs_f])
  })
})

function fixedItems(query: ReturnType<typeof useComparisonWorkbench>['state']['query']) {
  return query.ok && query.state.mode === 'fixed' ? query.state.items ?? [] : []
}

const BASKET_SUBJECTS = {
  evs_a: { kind: 'vps' as const, id: 'vps_0123456789abcdef' },
  evs_b: { kind: 'vps' as const, id: 'vps_0123456789abcde0' },
  evs_c: { kind: 'monitoring_instance' as const, id: 'mi_0123456789abcdef' },
  evs_d: { kind: 'target' as const, id: 'tgt_0123456789abcdef' },
  evs_e: { kind: 'vps' as const, id: 'vps_0011223344556677' },
  evs_f: { kind: 'monitoring_instance' as const, id: 'mi_0011223344556677' },
}

function fixedBasket(snapshotIds: readonly string[]): ComparisonURLState {
  return {
    ...FIXED,
    items: snapshotIds.map((snapshot_id) => ({ snapshot_id })),
  }
}

function comparisonSaveOptions(prefix: string) {
  let keys = 0
  return {
    userId: 'usr_1',
    newRecordId: () => `rec_${prefix}`,
    newIdempotencyKey: () => `${prefix}-${++keys}`,
  }
}

function installBasketSubjectEvaluation() {
  vi.spyOn(recordsApi, 'evaluateFixedComparison').mockImplementation(async (request) => {
    const ids = request.items.map((item) => item.snapshot_id ?? '')
    const items = ids.map((snapshotId) => {
      const subject = BASKET_SUBJECTS[snapshotId as keyof typeof BASKET_SUBJECTS]
      if (!subject) throw new Error(`unexpected snapshot ${snapshotId}`)
      return {
        snapshot_id: snapshotId,
        canonical_hash: '11'.repeat(32),
        kind: 'monitoring.host' as const,
        schema_version: 1,
        revision_context: 'not_applicable' as const,
        subject_kind: subject.kind,
        subject_id: subject.id,
      }
    })
    return {
      ...comparisonResponse,
      digest: digestForSnapshots(ids),
      items,
      comparison_intent: {
        token: `cmp1.${ids.join('.')}`,
        key_id: 'cmp_key',
        issued_at: '2026-08-20T10:00:00Z',
        expires_at: '2026-08-20T10:15:00Z',
      },
    }
  })
}

function digestForSnapshots(ids: readonly string[]): string {
  const hex = ids.join('').replace(/[^0-9a-f]/g, '')
  return `${hex}${'0'.repeat(64)}`.slice(0, 64)
}

async function settleEvaluatedSubjects(
  result: { current: { state: ComparisonWorkbenchState; commands: ComparisonWorkbenchCommands } },
  subjects: readonly { id: string }[],
) {
  await waitFor(() => {
    expect(result.current.state.loading).toBe(false)
    expect(result.current.state.saveBlocked).toBe(false)
    expect(result.current.state.query.ok && result.current.state.query.state.kind).toBe('monitoring.host/v1')
    expect(result.current.state.comparison?.items.map((item) => item.subject_id)).toEqual(
      subjects.map((subject) => subject.id),
    )
  })
}

function expectSavedSubjects(
  createDraft: { mock: { calls: unknown[][] } },
  subjects: ReadonlyArray<{ kind: 'vps' | 'monitoring_instance' | 'target'; id: string }>,
) {
  const call = createDraft.mock.calls.at(-1)?.[0] as { payload?: { subjects?: unknown } } | undefined
  expect(call?.payload?.subjects).toEqual(subjects.map((subject, index) => ({
    registry_version: 1,
    kind: subject.kind,
    role: 'affected',
    source_id: subject.id,
    primary: index === 0,
  })))
}
