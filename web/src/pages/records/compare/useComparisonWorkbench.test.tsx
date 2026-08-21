import { act, renderHook, waitFor } from '@testing-library/react'
import { type ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
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
  COMPARISON_URL_VERSION,
  encodeComparisonURLState,
  type ComparisonURLState,
} from './comparisonQueryState'
import { useComparisonWorkbench } from './useComparisonWorkbench'

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

function wrapper(initialURL: string) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={[initialURL]}>{children}</MemoryRouter>
  }
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
})
