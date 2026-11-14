import { act, renderHook, waitFor } from '@testing-library/react'
import { createContext, useContext, useEffect, type ReactNode } from 'react'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import * as recordsApi from '../../../lib/recordsApi'
import type {
  ComparisonCandidateResponse,
  ComparisonEvaluateResponse,
  RecordAllocation,
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
import type * as DraftBuffers from '../draftBuffer'
import { memoryDraftBufferStore } from '../draftBuffer'
import { recordAllocationFixture } from '../testFixtures'
import { applyRecordClientCleanup, type RecordCleanupAck } from '../../../lib/recordClientCleanup'

vi.mock('../draftBuffer', async (importOriginal) => {
  const actual = await importOriginal<typeof DraftBuffers>()
  return { ...actual, openIndexedDBDraftBuffer: () => actual.memoryDraftBufferStore() }
})

const withResolvers = <T,>() => (Promise as PromiseConstructor & {
  withResolvers: () => { promise: Promise<T>; resolve: (value: T) => void }
}).withResolvers()

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
      source_record_id: 'rec_a',
      canonical_hash: '11'.repeat(32),
      kind: 'monitoring.host',
      schema_version: 1,
      revision_context: 'not_applicable',
      subject_kind: 'vps',
      subject_id: 'vps_0123456789abcdef',
    },
    {
      snapshot_id: 'evs_b',
      source_record_id: 'rec_b',
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

// Every new comparison target comes from the reservation endpoint.
const SAVED_RECORD_ID = `rec2_8${'0'.repeat(30)}5`
const saveDraft = {
  draft_id: 'rdf_compare',
  etag: 'rdt1_compare',
  record_id: SAVED_RECORD_ID,
  payload: {} as RecordDraft['payload'],
  version: 1,
  created_at: '2026-08-20T10:00:00Z',
  updated_at: '2026-08-20T10:00:00Z',
  expires_at: '2026-11-01T10:00:00Z',
} as RecordDraft

const savedRecord: RecordMutationResult = {
  record_id: SAVED_RECORD_ID,
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
  beforeEach(() => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'createRecordAllocation').mockImplementation(async (input, key) => recordAllocationFixture('save', {
      purpose: input.purpose, request_digest: input.request_digest, idempotency_key: key, record_id: SAVED_RECORD_ID,
    }))
  })
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
      record_id: SAVED_RECORD_ID,
      payload: {} as RecordDraft['payload'],
      version: 1,
      created_at: '2026-08-20T10:00:00Z',
      updated_at: '2026-08-20T10:00:00Z',
      expires_at: '2026-11-01T10:00:00Z',
    } as RecordDraft
    const created: RecordMutationResult = {
      record_id: SAVED_RECORD_ID,
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
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockImplementation(async (body, key) => {
      const raw = sessionStorage.getItem(`houfeng.comparison-save/usr_1/${comparisonResponse.digest}`)
      expect(raw).not.toBeNull()
      expect(JSON.parse(raw!)).toMatchObject({ idempotencyKey: key, request: body })
      return created
    })
    const publish = vi.spyOn(recordsApi, 'createRecord')
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
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
      record_id: SAVED_RECORD_ID,
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      comparison_intent: 'cmp1.valid.payload.mac',
      allocation: expect.objectContaining({ record_id: SAVED_RECORD_ID, purpose: 'comparison_save' }),
    })
    expect(save.mock.calls[0]?.[1]).toBe('comparison-save-key')
    // The replay sends the stored body: no second reservation or draft.
    expect(save.mock.calls[1]?.[0]).toEqual(save.mock.calls[0]?.[0])
    expect(save.mock.calls[1]?.[1]).toBe('comparison-save-key')
    expect(createDraft).toHaveBeenCalledTimes(1)
    expect(recordsApi.createRecordAllocation).toHaveBeenCalledTimes(1)
    expect(result.current.state.savedRecordId).toBe(SAVED_RECORD_ID)
  })

  it('uses a fresh allocation and publication key for an edited conclusion after success', async () => {
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const allocate = vi.mocked(recordsApi.createRecordAllocation)
    allocate.mockImplementation(async (input, key) => recordAllocationFixture(key, {
      purpose: input.purpose, request_digest: input.request_digest, idempotency_key: key,
    }))
    const firstID = recordAllocationFixture('edited-1').record_id
    const secondID = recordAllocationFixture('edited-3').record_id
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
      .mockResolvedValueOnce({ ...savedRecord, record_id: firstID })
      .mockResolvedValueOnce({ ...savedRecord, record_id: secondID })
    let sequence = 0
    const { result } = renderHook(() => useComparisonWorkbench({ userId: 'usr_1', newIdempotencyKey: () => `edited-${++sequence}` }), {
      wrapper: wrapper(compareURL(FIXED)),
    })
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    act(() => result.current.commands.setConclusion('first conclusion'))
    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.savedRecordId).toBe(firstID)
    act(() => result.current.commands.setConclusion('revised conclusion'))
    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.savedRecordId).toBe(secondID)
    expect(allocate.mock.calls.map((call) => call[1])).toEqual(['edited-1', 'edited-3'])
    expect(save.mock.calls.map((call) => call[1])).toEqual(['edited-2', 'edited-4'])
    expect(save.mock.calls[0]?.[0].record_id).not.toBe(save.mock.calls[1]?.[0].record_id)
  })

  it('reuses the digest-scoped save attempt after remount', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue({
      draft_id: 'rdf_compare',
      etag: 'rdt1_compare',
      record_id: SAVED_RECORD_ID,
      payload: {} as RecordDraft['payload'],
      version: 1,
      created_at: '2026-08-20T10:00:00Z',
      updated_at: '2026-08-20T10:00:00Z',
      expires_at: '2026-11-01T10:00:00Z',
    } as RecordDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue({
      record_id: SAVED_RECORD_ID,
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
    expect(save.mock.calls[0]?.[1]).toBe('comparison-retry-2')
    expect(save.mock.calls[1]?.[1]).toBe('comparison-retry-2')
    expect(save.mock.calls.map(([input]) => input.record_id)).toEqual([SAVED_RECORD_ID, SAVED_RECORD_ID])
    // One reservation key and one publication key; the remount replays both.
    expect(keys).toBe(2)
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
    ['401', new ApiError(401, 'unauthenticated'), null],
    ['403', new ApiError(403, 'forbidden', { code: 'permission_denied' }), 'permission_denied'],
    ['404', new ApiError(404, 'hidden', { code: 'resource_not_found' }), 'resource_not_found'],
    ['409', new ApiError(409, 'conflict', { code: 'idempotency_conflict' }), 'idempotency_conflict'],
    ['503', new ApiError(503, 'pending', { code: 'publication_pending' }), 'publication_pending'],
    ['422 comparison_intent_invalid', new ApiError(422, 'comparison intent is invalid', { code: 'comparison_intent_invalid' }), 'comparison_intent_invalid'],
    ['422 comparison_intent_stale', new ApiError(422, 'comparison intent is stale', { code: 'comparison_intent_stale' }), 'comparison_intent_stale'],
    ['422 comparison_intent_expired', new ApiError(422, 'comparison intent expired', { code: 'comparison_intent_expired' }), 'comparison_intent_expired'],
  ] as const)('keeps the sent request available for exact retry after %s', async (_label, failure, code) => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockRejectedValue(failure)
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
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

    expect(result.current.state.saveErrorCode).toBe(code)
    expect(result.current.state.error).toBeNull()
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.comparison).not.toBeNull()
    expect(result.current.state.candidates).toBeNull()
    expect(result.current.state.saveBlocked).toBe(false)
    expect(result.current.state.saving).toBe(false)
    expect(fixedItems(result.current.state.query)).toEqual(FIXED.items)
    expect(save).toHaveBeenCalledTimes(2)
    // A refusal after the request was sent does not prove it did not commit.
    const stored = sessionStorage.getItem(`houfeng.comparison-save/usr_1/${comparisonResponse.digest}`)
    expect(stored).not.toBeNull()
    expect(JSON.parse(stored!)).toMatchObject({ idempotencyKey: 'comparison-denied', request: save.mock.calls[0]?.[0] })
  })

  it('keeps the signed intent and retries a transport failure with the same body and idempotency key', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockRejectedValue(
      new ApiError(503, 'unavailable'),
    )
    let deniedKeys = 0
    const { result } = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
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
    expect(deniedKeys).toBe(2)
    expect(save.mock.calls[0]?.[1]).toBe('comparison-denied-2')
    expect(save.mock.calls[1]?.[0]).toEqual(save.mock.calls[0]?.[0])
    expect(save.mock.calls[1]?.[1]).toBe('comparison-denied-2')
    expect(createDraft).toHaveBeenCalledTimes(1)
    expect(result.current.state.saveUnresolved).toBe(true)
    expect(result.current.state.saveFeedback).toContain('尚未确认')
  })

  it('replays the stored request after a lost reply and reload, even once its intent has expired', async () => {
    sessionStorage.clear()
    const evaluate = vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(savedRecord)
    let keys = 0
    const options = { userId: 'usr_1', newIdempotencyKey: () => `comparison-lost-${++keys}` }
    const first = renderHook(() => useComparisonWorkbench(options), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(first.result.current.state.saveBlocked).toBe(false))
    act(() => { first.result.current.commands.setTitle('原结论') })
    await act(async () => { await first.result.current.commands.save() })
    expect(first.result.current.state.saveUnresolved).toBe(true)
    first.unmount()

    // The old draft and intent may be gone; the result is checked before a new evaluation.
    evaluate.mockRejectedValue(new ApiError(404, 'comparison unavailable'))
    const second = renderHook(() => useComparisonWorkbench(options), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(second.result.current.state.savedRecordId).toBe(SAVED_RECORD_ID))

    expect(save).toHaveBeenCalledTimes(2)
    expect(save.mock.calls[1]?.[0]).toEqual(save.mock.calls[0]?.[0])
    expect(save.mock.calls[1]?.[0].comparison_intent).toBe('cmp1.valid.payload.mac')
    expect(save.mock.calls[1]?.[1]).toBe(save.mock.calls[0]?.[1])
    expect(createDraft).toHaveBeenCalledTimes(1)
    expect(recordsApi.createRecordAllocation).toHaveBeenCalledTimes(1)
    expect(keys).toBe(2)
    expect(second.result.current.state.savedRecordId).toBe(SAVED_RECORD_ID)
    expect(second.result.current.state.saveUnresolved).toBe(false)
  })

  it('clears confirmed save feedback without clearing a separate evaluation failure', async () => {
    const evaluate = vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
      .mockRejectedValueOnce(new TypeError('first reply lost'))
      .mockRejectedValueOnce(new TypeError('recovery unavailable'))
      .mockResolvedValueOnce(savedRecord)
    const options = { userId: 'usr_1', newIdempotencyKey: () => 'same-request-key' }
    const first = renderHook(() => useComparisonWorkbench(options), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(first.result.current.state.saveBlocked).toBe(false))
    await act(async () => { await first.result.current.commands.save() })
    first.unmount()

    evaluate.mockRejectedValue(new ApiError(404, 'comparison unavailable'))
    const second = renderHook(() => useComparisonWorkbench(options), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(second.result.current.state.errorCode).toBe('resource_not_found'))
    expect(second.result.current.state.saveFeedback).not.toBeNull()
    await act(async () => { await second.result.current.commands.save() })
    expect(second.result.current.state.savedRecordId).toBe(SAVED_RECORD_ID)
    expect(second.result.current.state.saveFeedback).toBeNull()
    expect(second.result.current.state.errorCode).toBe('resource_not_found')
    expect(save.mock.calls[2]?.slice(0, 2)).toEqual(save.mock.calls[0]?.slice(0, 2))
  })

  it('requires explicit discard before replacing an uncertain draft creation', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft')
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    let keys = 0
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1', newIdempotencyKey: () => `comparison-edit-${++keys}` }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    act(() => { result.current.commands.setTitle('first') })
    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.saveUnresolved).toBe(true)
    act(() => { result.current.commands.setTitle('second') })
    expect(result.current.state.title).toBe('first')
    await act(async () => { await result.current.commands.save() })
    expect(createDraft).toHaveBeenCalledTimes(1)
    act(() => { result.current.commands.discardUnresolvedSave() })
    act(() => { result.current.commands.setTitle('second') })
    await act(async () => { await result.current.commands.save() })

    const allocate = vi.mocked(recordsApi.createRecordAllocation)
    expect(allocate).toHaveBeenCalledTimes(2)
    expect(allocate.mock.calls[0]?.[1]).toBe('comparison-edit-1')
    expect(allocate.mock.calls[1]?.[1]).toBe('comparison-edit-3')
    expect(allocate.mock.calls[1]?.[0].request_digest).not.toBe(allocate.mock.calls[0]?.[0].request_digest)
    expect(createDraft).toHaveBeenCalledTimes(2)
    expect(save).toHaveBeenCalledTimes(1)
    expect(save.mock.calls[0]?.[1]).toBe('comparison-edit-4')
  })

  it('asks for a legacy save result with only its original key and record, never rewriting or reproducing it', async () => {
    sessionStorage.clear()
    const legacyKey = `houfeng.comparison-save/usr_1/${comparisonResponse.digest}`
    const legacyRaw = JSON.stringify({
      version: 1, userId: 'usr_1', digest: comparisonResponse.digest, recordId: 'rec_legacyresult',
      idempotencyKey: 'legacy-save-key', generation: '0', sourceRecordIds: ['rec_a', 'rec_b'],
    })
    sessionStorage.setItem(legacyKey, legacyRaw)
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const createDraft = vi.spyOn(recordsApi, 'createRecordDraft')
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
    const recover = vi.spyOn(recordsApi, 'recoverLegacyComparisonRecord')
      .mockRejectedValueOnce(new ApiError(409, 'claims differ', { code: 'idempotency_conflict' }))
      .mockResolvedValueOnce({ ...savedRecord, record_id: 'rec_legacyresult', replayed: true })
    const { result } = renderHook(() => useComparisonWorkbench({ userId: 'usr_1' }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(result.current.state.saveUnresolved).toBe(true))

    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.saveFeedback).toContain('无法确认旧版另存')
    expect(sessionStorage.getItem(legacyKey)).toBe(legacyRaw)
    await act(async () => { await result.current.commands.save() })

    expect(recover).toHaveBeenCalledTimes(2)
    expect(recover.mock.calls[1]?.slice(0, 2)).toEqual([{ record_id: 'rec_legacyresult', comparison_intent: 'cmp1.valid.payload.mac' }, 'legacy-save-key'])
    expect(result.current.state.savedRecordId).toBe('rec_legacyresult')
    expect(result.current.state.saveFeedback).toBeNull()
    expect(createDraft).not.toHaveBeenCalled()
    expect(save).not.toHaveBeenCalled()
    expect(recordsApi.createRecordAllocation).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(legacyKey)).toBe(legacyRaw)
  })

  it('retains a legacy envelope when the current evaluation has a different digest', async () => {
    const key = `houfeng.comparison-save/usr_1/${comparisonResponse.digest}`
    const raw = JSON.stringify({
      version: 1, userId: 'usr_1', digest: comparisonResponse.digest, recordId: 'rec_legacyresult',
      idempotencyKey: 'legacy-save-key', generation: '0', sourceRecordIds: ['rec_a', 'rec_b'],
    })
    sessionStorage.setItem(key, raw)
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue({ ...comparisonResponse, digest: 'ee'.repeat(32) })
    const recover = vi.spyOn(recordsApi, 'recoverLegacyComparisonRecord')
    const { result } = renderHook(() => useComparisonWorkbench({ userId: 'usr_1' }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(result.current.state.comparison?.digest).toBe('ee'.repeat(32)))
    expect(result.current.state.saveUnresolved).toBe(true)
    expect(result.current.state.saveFeedback).toContain('不一致')
    await act(async () => { await result.current.commands.save() })
    expect(recordsApi.createRecordAllocation).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(key)).toBe(raw)
    expect(recover).not.toHaveBeenCalled()
  })

  it('ends local confirmation only on explicit discard, then saves as a new request', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
      .mockRejectedValueOnce(new ApiError(503, 'publication pending', { code: 'publication_pending' }))
      .mockResolvedValueOnce(savedRecord)
    let keys = 0
    const { result } = renderHook(
      () => useComparisonWorkbench({ userId: 'usr_1', newIdempotencyKey: () => `comparison-discard-${++keys}` }),
      { wrapper: wrapper(compareURL(FIXED)) },
    )
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.saveUnresolved).toBe(true)
    expect(result.current.state.saveFeedback).toContain('确认中')
    act(() => { result.current.commands.discardUnresolvedSave() })
    expect(result.current.state.saveUnresolved).toBe(false)
    expect(result.current.state.saveFeedback).toContain('可能已经创建')
    expect(sessionStorage.getItem(`houfeng.comparison-save/usr_1/${comparisonResponse.digest}`)).toBeNull()
    act(() => { result.current.commands.setTitle('new conclusion') })
    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.saveFeedback).toBeNull()
    expect(save).toHaveBeenCalledTimes(2)
    expect(save.mock.calls[0]?.[1]).toBe('comparison-discard-2')
    expect(save.mock.calls[1]?.[1]).toBe('comparison-discard-4')
    expect(recordsApi.createRecordAllocation).toHaveBeenCalledTimes(2)
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
    expect(result.current.state.savedRecordId).toBe(SAVED_RECORD_ID)

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
    // Start a separate scenario after explicitly discarding the unknown draft attempt.
    sessionStorage.clear()

    createDraft.mockResolvedValue(saveDraft)
    let resolveSave: (value: RecordMutationResult) => void = () => {}
    save.mockImplementation((_input, _key, signal) => new Promise((resolve) => {
      signal?.addEventListener('abort', () => {})
      resolveSave = resolve
    }))
    const saveHook = renderHook(
      () => useComparisonWorkbench({
        userId: 'usr_1',
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
    expect(result.current.state.savedRecordId).toBe(SAVED_RECORD_ID)
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

  it('closes a pending comparison destination before cleanup acknowledgement and ignores its late success', async () => {
    const base = memoryDraftBufferStore()
    const cleanupGate = withResolvers<void>()
    const store = { ...base, applyCleanup: async (...args: Parameters<typeof base.applyCleanup>) => {
      await cleanupGate.promise
      return base.applyCleanup(...args)
    } }
    const userId = 'usr_comparecleanup'
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const response = withResolvers<RecordMutationResult>()
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord').mockReturnValue(response.promise)
    const { result } = renderHook(() => useComparisonWorkbench({ userId, store }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    act(() => {
      result.current.commands.setTitle('private comparison')
      result.current.commands.setConclusion('private conclusion')
    })
    const staleCommands = result.current.commands
    let saving!: Promise<void>
    act(() => { saving = result.current.commands.save() })
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
    let cleanup!: Promise<RecordCleanupAck>
    act(() => { cleanup = applyRecordClientCleanup(store, userId, 'browser1', {
      version: 1, cursor: '1', operation_id: 'rpo_comparedestination', record_id: SAVED_RECORD_ID,
      cleanup_epoch: '1', scopes: ['comparison_workbench', 'comparison_save_attempt'],
    }) })
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.title).toBe('')
    expect(result.current.state.conclusion).toBe('')
    expect(result.current.state.saveUnresolved).toBe(false)
    expect(result.current.state.saving).toBe(false)
    await act(async () => {
      staleCommands.setConclusion('late callback must not reopen content')
      await staleCommands.save()
    })
    expect(result.current.state.conclusion).toBe('')
    expect(save).toHaveBeenCalledTimes(1)
    await act(async () => { cleanupGate.resolve(); await cleanup })
    await act(async () => { response.resolve(savedRecord); await saving })
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.comparison).toBeNull()
    expect(sessionStorage.getItem(`houfeng.comparison-save/${userId}/${comparisonResponse.digest}`)).toBeNull()
  })

  it('does not expose an unresolved comparison allocation after cleanup', async () => {
    const store = memoryDraftBufferStore()
    const userId = 'usr_compareallocation'
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const allocate = vi.mocked(recordsApi.createRecordAllocation)
    const response = withResolvers<RecordAllocation>()
    allocate.mockRejectedValueOnce(new TypeError('allocation reply lost')).mockReturnValueOnce(response.promise)
    const draft = vi.spyOn(recordsApi, 'createRecordDraft')
    const first = renderHook(() => useComparisonWorkbench({ userId, store }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(first.result.current.state.saveBlocked).toBe(false))
    act(() => first.result.current.commands.setConclusion('hidden pending comparison'))
    await act(async () => { await first.result.current.commands.save() })
    expect(first.result.current.state.saveUnresolved).toBe(true)
    first.unmount()
    const second = renderHook(() => useComparisonWorkbench({ userId, store }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(allocate).toHaveBeenCalledTimes(2))
    expect(second.result.current.state.conclusion).toBe('')
    await act(async () => { await applyRecordClientCleanup(store, userId, 'browser4', {
      version: 1, cursor: '1', operation_id: 'rpo_compareallocation', record_id: SAVED_RECORD_ID,
      cleanup_epoch: '1', scopes: ['comparison_workbench', 'comparison_save_attempt'],
    }) })
    const call = allocate.mock.calls[1]
    if (!call) throw new Error('expected allocation replay')
    await act(async () => { response.resolve(recordAllocationFixture('save', {
      purpose: call[0].purpose, request_digest: call[0].request_digest, idempotency_key: call[1], record_id: SAVED_RECORD_ID,
    })) })
    await waitFor(() => expect(second.result.current.state.comparison).toBeNull())
    expect(second.result.current.state.saveBlocked).toBe(true)
    expect(second.result.current.state.errorCode).toBeNull()
    expect(second.result.current.state.conclusion).toBe('')
    expect(sessionStorage.getItem(`houfeng.comparison-save/${userId}/${comparisonResponse.digest}`)).toBeNull()
    expect(draft).not.toHaveBeenCalled()
  })


  it('does not expose a recovered comparison reservation when cleanup races its target scope', async () => {
    const base = memoryDraftBufferStore()
    const userId = 'usr_comparerestore'
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    const draft = vi.spyOn(recordsApi, 'createRecordDraft').mockRejectedValueOnce(new TypeError('draft reply lost'))
    const save = vi.spyOn(recordsApi, 'saveComparisonRecord')
    const first = renderHook(() => useComparisonWorkbench({ userId, store: base }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(first.result.current.state.saveBlocked).toBe(false))
    act(() => first.result.current.commands.setTitle('hidden recovered conclusion'))
    await act(async () => { await first.result.current.commands.save() })
    expect(first.result.current.state.saveUnresolved).toBe(true)
    first.unmount()
    const hold = withResolvers<void>()
    const reached = withResolvers<void>()
    const store = { ...base, captureScope: async (...args: Parameters<typeof base.captureScope>) => {
      if (args[1] === SAVED_RECORD_ID) { reached.resolve(); await hold.promise }
      return base.captureScope(...args)
    } }
    const second = renderHook(() => useComparisonWorkbench({ userId, store }), { wrapper: wrapper(compareURL(FIXED)) })
    await act(async () => { await reached.promise })
    expect(second.result.current.state.title).toBe('')
    await act(async () => { await applyRecordClientCleanup(base, userId, 'browser2', {
      version: 1, cursor: '1', operation_id: 'rpo_comparerestore', record_id: SAVED_RECORD_ID,
      cleanup_epoch: '1', scopes: ['comparison_workbench', 'comparison_save_attempt'],
    }) })
    expect(second.result.current.state.comparison).toBeNull()
    expect(second.result.current.state.title).toBe('')
    await act(async () => { hold.resolve() })
    expect(second.result.current.state.title).toBe('')
    expect(second.result.current.state.saveUnresolved).toBe(false)
    expect(draft).toHaveBeenCalledTimes(1)
    expect(save).not.toHaveBeenCalled()
  })

  it('closes a pending comparison destination on another tab cleanup broadcast', async () => {
    const store = memoryDraftBufferStore()
    const userId = 'usr_comparetab'
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    vi.spyOn(recordsApi, 'saveComparisonRecord').mockRejectedValue(new TypeError('response lost'))
    const { result } = renderHook(() => useComparisonWorkbench({ userId, store }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    act(() => result.current.commands.setConclusion('tab comparison secret'))
    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.saveUnresolved).toBe(true)
    const remote = new BroadcastChannel('houfeng-record-security')
    remote.postMessage({ type: 'cleanup', userId, event: {
      version: 1, cursor: '1', operation_id: 'rpo_comparetab', record_id: SAVED_RECORD_ID,
      cleanup_epoch: '1', scopes: ['comparison_workbench', 'comparison_save_attempt'],
    } })
    await waitFor(() => expect(result.current.state.comparison).toBeNull())
    expect(result.current.state.conclusion).toBe('')
    expect(result.current.state.saveUnresolved).toBe(false)
    expect(sessionStorage.getItem(`houfeng.comparison-save/${userId}/${comparisonResponse.digest}`)).toBeNull()
    remote.close()
  })

  it('detaches a discarded destination before a new comparison attempt', async () => {
    const store = memoryDraftBufferStore()
    const userId = 'usr_comparediscard'
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.mocked(recordsApi.createRecordAllocation).mockImplementation(async (input, key) => recordAllocationFixture(key, {
      purpose: input.purpose, request_digest: input.request_digest, idempotency_key: key,
    }))
    const draftReply = withResolvers<RecordDraft>()
    const draft = vi.spyOn(recordsApi, 'createRecordDraft').mockRejectedValueOnce(new TypeError('draft reply lost')).mockReturnValueOnce(draftReply.promise)
    const nextTarget = recordAllocationFixture('replace-3').record_id
    vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue({ ...savedRecord, record_id: nextTarget })
    let sequence = 0
    const { result } = renderHook(() => useComparisonWorkbench({ userId, store, newIdempotencyKey: () => `replace-${++sequence}` }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    act(() => result.current.commands.setTitle('first conclusion'))
    await act(async () => { await result.current.commands.save() })
    act(() => result.current.commands.discardUnresolvedSave())
    act(() => result.current.commands.setTitle('replacement conclusion'))
    let saving!: Promise<void>
    act(() => { saving = result.current.commands.save() })
    await waitFor(() => expect(draft).toHaveBeenCalledTimes(2))
    await act(async () => { await applyRecordClientCleanup(store, userId, 'browser3', {
      version: 1, cursor: '1', operation_id: 'rpo_comparediscard', record_id: recordAllocationFixture('replace-1').record_id,
      cleanup_epoch: '1', scopes: ['comparison_workbench', 'comparison_save_attempt'],
    }) })
    expect(result.current.state.comparison).not.toBeNull()
    expect(result.current.state.title).toBe('replacement conclusion')
    await act(async () => { draftReply.resolve(saveDraft); await saving })
    expect(result.current.state.savedRecordId).toBe(nextTarget)
  })

  it('closes source-dependent work and prevents a late draft response from publishing after cleanup', async () => {
    sessionStorage.clear()
    const store = memoryDraftBufferStore()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    let finishDraft!: (draft: RecordDraft) => void
    const create = vi.spyOn(recordsApi, 'createRecordDraft').mockImplementation(() => new Promise((resolve) => { finishDraft = resolve }))
    const publish = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const { result } = renderHook(() => useComparisonWorkbench({ userId: 'usr_cleanup', store }), { wrapper: wrapper(compareURL(FIXED)) })
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    act(() => { result.current.commands.setTitle('private source conclusion') })
    let saving!: Promise<void>
    act(() => { saving = result.current.commands.save() })
    await waitFor(() => expect(create).toHaveBeenCalled())
    await act(async () => {
      await applyRecordClientCleanup(store, 'usr_cleanup', 'browser_workbench', {
        version: 1, cursor: '9007199254740993', operation_id: 'rpo_workbench', record_id: 'rec_a',
        cleanup_epoch: '9007199254740995', scopes: ['comparison_workbench', 'comparison_save_attempt'],
      })
    })
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.title).toBe('')
    expect(result.current.state.saveBlocked).toBe(true)
    await act(async () => { finishDraft(saveDraft); await saving })
    expect(publish).not.toHaveBeenCalled()
    expect(result.current.state.savedRecordId).toBeNull()
  })

  it('does not carry conclusions or late successful save state into another user workspace', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    let finishSave!: (value: RecordMutationResult) => void
    const publish = vi.spyOn(recordsApi, 'saveComparisonRecord').mockImplementation(() => new Promise((resolve) => { finishSave = resolve }))
    const { result, rerender } = renderHook(({ userId }) => useComparisonWorkbench({ userId }), {
      initialProps: { userId: 'usr_first' }, wrapper: wrapper(compareURL(FIXED)),
    })
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    act(() => {
      result.current.commands.setTitle('first user private title')
      result.current.commands.setConclusion('first user private conclusion')
    })
    let saving!: Promise<void>
    act(() => { saving = result.current.commands.save() })
    await waitFor(() => expect(publish).toHaveBeenCalled())
    rerender({ userId: 'usr_second' })
    expect(result.current.state.title).toBe('')
    expect(result.current.state.conclusion).toBe('')
    await act(async () => { finishSave(savedRecord); await saving })
    expect(result.current.state.savedRecordId).toBeNull()
    expect(result.current.state.title).toBe('')
    expect(result.current.state.conclusion).toBe('')
  })

  it('blocks a replacement evaluation when prior-user retry metadata cannot be retired', async () => {
    sessionStorage.clear()
    vi.spyOn(recordsApi, 'evaluateFixedComparison').mockResolvedValue(comparisonResponse)
    vi.spyOn(recordsApi, 'createRecordDraft').mockResolvedValue(saveDraft)
    const publish = vi.spyOn(recordsApi, 'saveComparisonRecord').mockResolvedValue(savedRecord)
    const { result, rerender } = renderHook(({ userId }) => useComparisonWorkbench({ userId }), {
      initialProps: { userId: 'usr_first' }, wrapper: wrapper(compareURL(FIXED)),
    })
    await waitFor(() => expect(result.current.state.saveBlocked).toBe(false))
    await act(async () => { await result.current.commands.save() })
    expect(result.current.state.savedRecordId).toBe(savedRecord.record_id)
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementationOnce(() => { throw new Error('storage blocked') })
    rerender({ userId: 'usr_second' })
    await waitFor(() => expect(result.current.state.error).toContain('重试状态清理失败'))
    expect(result.current.state.comparison).toBeNull()
    expect(result.current.state.saveBlocked).toBe(true)
    await act(async () => { await result.current.commands.save() })
    expect(publish).toHaveBeenCalledTimes(1)
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
        source_record_id: `rec_${snapshotId.slice(4)}`,
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
