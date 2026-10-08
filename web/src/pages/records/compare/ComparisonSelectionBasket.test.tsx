import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type { ComparisonCandidateItem, EvidenceSnapshotRead } from '../../../lib/types'
import { recordRevisionFixture } from '../testFixtures'
import { ComparisonSelectionBasket } from './ComparisonSelectionBasket'
import { COMPARISON_SELECTION_LIMIT_ERROR, COMPARISON_URL_VERSION, type ComparisonURLState } from './comparisonQueryState'

const api = vi.hoisted(() => ({
  snapshot: vi.fn(),
  revision: vi.fn(),
}))

vi.mock('../../../lib/recordsApi', () => ({
  getEvidenceSnapshot: (...args: unknown[]) => api.snapshot(...args),
  getRecordRevision: (...args: unknown[]) => api.revision(...args),
}))

const WINDOW = {
  requested_from: '2026-07-01T00:00:00Z',
  requested_to: '2026-07-02T00:00:00Z',
}

function fixed(items: NonNullable<ComparisonURLState['items']>): ComparisonURLState {
  return {
    version: COMPARISON_URL_VERSION,
    mode: 'fixed',
    items,
    baseline: 0,
    alignment: 'actual_coverage',
    ...WINDOW,
  }
}

function candidate(index: number, snapshotId = `evs_cand${index}`): ComparisonCandidateItem {
  return {
    subject: { kind: 'vps', id: 'vps_same' },
    snapshot_id: snapshotId,
    record_id: 'rec_same',
    revision_ids: ['rev_should_not_use'],
    kind: 'monitoring.host',
    schema_version: 1,
    canonical_hash: 'aa'.repeat(32),
    requested_window: { start: WINDOW.requested_from, end: WINDOW.requested_to },
    actual_window: { start: WINDOW.requested_from, end: WINDOW.requested_to },
    quality_status: 'complete',
    captured_at: '2026-07-01T03:00:00Z',
    recommendation: 'nearest_window',
  }
}

function renderBasket(props: Partial<Parameters<typeof ComparisonSelectionBasket>[0]> = {}) {
  const onConfirm = vi.fn()
  const onRemove = vi.fn()
  const onClear = vi.fn()
  const onAdd = vi.fn()
  const onReviseSnapshots = vi.fn()
  render(
    <ComparisonSelectionBasket
      query={fixed([{ snapshot_id: 'evs_a' }])}
      linkProblem={null}
      candidates={null}
      selectionError={null}
      onConfirm={onConfirm}
      onRemove={onRemove}
      onClear={onClear}
      onAdd={onAdd}
      onReviseSnapshots={onReviseSnapshots}
      {...props}
    />,
  )
  return { onConfirm, onRemove, onClear, onAdd, onReviseSnapshots }
}

describe('ComparisonSelectionBasket', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.snapshot.mockImplementation(async (id: string) => {
      if (id === 'evs_missing') throw new ApiError(404, 'hidden', { code: 'resource_not_found' })
      return {
        snapshot_id: id,
        title: id === 'evs_a' ? '左侧观测' : '右侧观测',
        kind: 'monitoring.host',
        observed_at: '2026-07-01T03:00:00Z',
        quality: { status: 'complete' },
        subject: { type: 'vps', id: 'vps_same', display_name: '边缘甲' },
      } as EvidenceSnapshotRead
    })
    api.revision.mockImplementation(async (_recordId: string, revisionId: string) => recordRevisionFixture({
      record_id: 'rec_1',
      revision_id: revisionId,
      revision_no: revisionId === 'rev_a' ? 2 : 3,
      title: revisionId === 'rev_a' ? '第一次修订' : '第二次修订',
      created_at: '2026-07-01T04:00:00Z',
      subjects: [{
        registry_version: 1,
        kind: 'vps',
        role: 'affected',
        source_id: 'vps_same',
        primary: true,
        identity: { display_name: '边缘甲' },
      }],
    }))
  })

  it('blocks compare copy when fewer than two items are selected', () => {
    renderBasket()
    expect(screen.getByText(/至少选择 2 项/)).toBeInTheDocument()
    expect(screen.getByText(/当前 1 项/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认候选并比较' })).not.toBeInTheDocument()
  })

  it('shows readable identity and removes one snapshot without using the technical id as the title', async () => {
    const { onRemove, onClear } = renderBasket({
      query: fixed([{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }]),
      selectionError: '最多比较 6 项，超出的选择已拒绝。',
    })
    expect(screen.getByRole('alert')).toHaveTextContent('最多比较 6 项')
    expect(await screen.findByText('左侧观测')).toHaveClass('record-compare-items__title')
    expect(screen.getByText('右侧观测')).toHaveClass('record-compare-items__title')
    expect(screen.getByText('evs_a').closest('details')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '移出第 1 项' }))
    expect(onRemove).toHaveBeenCalledWith('snapshot:evs_a')
    fireEvent.click(screen.getByRole('button', { name: '清空比较篮' }))
    expect(onClear).toHaveBeenCalledOnce()
  })

  it('keeps two revisions of one record distinct', async () => {
    const { onRemove } = renderBasket({
      query: fixed([
        { record_id: 'rec_1', revision_id: 'rev_a' },
        { record_id: 'rec_1', revision_id: 'rev_b' },
      ]),
    })
    expect(await screen.findByText('第一次修订')).toBeInTheDocument()
    expect(screen.getByText('第二次修订')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '选择证据' })).toHaveLength(2)
    fireEvent.click(screen.getByRole('button', { name: '移出第 2 项' }))
    expect(onRemove).toHaveBeenCalledWith('revision:rec_1:rev_b')
  })

  it('marks an unreadable snapshot without inventing a title', async () => {
    renderBasket({ query: fixed([{ snapshot_id: 'evs_missing' }]) })
    expect(await screen.findByText('不可读')).toHaveClass('record-compare-items__title')
    expect(screen.getByText('evs_missing').closest('details')).toBeTruthy()
  })

  it('lets the user check at most six deduped snapshots and confirms only those ids', () => {
    const onConfirm = vi.fn()
    const duplicates = [
      candidate(0, 'evs_dup'),
      candidate(1, 'evs_dup'),
    ]
    const many = Array.from({ length: 8 }, (_, index) => candidate(index))
    const { rerender } = render(
      <ComparisonSelectionBasket
        query={{
          version: COMPARISON_URL_VERSION,
          mode: 'candidate',
          subjects: [{ kind: 'vps', id: 'vps_same' }, { kind: 'vps', id: 'vps_other' }],
          ...WINDOW,
        }}
        linkProblem={null}
        candidates={duplicates}
        selectionError={null}
        onConfirm={onConfirm}
        onRemove={vi.fn()}
        onClear={vi.fn()}
        onReviseSnapshots={vi.fn()}
      />,
    )
    expect(screen.getAllByRole('checkbox')).toHaveLength(1)
    expect(screen.getByText(/已按快照合并重复候选/)).toBeInTheDocument()

    rerender(
      <ComparisonSelectionBasket
        query={{
          version: COMPARISON_URL_VERSION,
          mode: 'candidate',
          subjects: [{ kind: 'vps', id: 'vps_same' }, { kind: 'vps', id: 'vps_other' }],
          ...WINDOW,
        }}
        linkProblem={null}
        candidates={many}
        selectionError={null}
        onConfirm={onConfirm}
        onRemove={vi.fn()}
        onClear={vi.fn()}
        onReviseSnapshots={vi.fn()}
      />,
    )
    const boxes = screen.getAllByRole('checkbox')
    expect(boxes).toHaveLength(8)
    expect(screen.getByRole('heading', { name: '比较对象 0' })).toBeInTheDocument()
    expect(screen.getByText(/不会自动比较前 6 项/)).toBeInTheDocument()
    expect(screen.queryByText('共 8 个候选，只比较前 6 个。')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认候选并比较' })).toBeDisabled()
    for (const box of boxes.slice(2)) fireEvent.click(box)
    fireEvent.click(boxes[0]!)
    expect(boxes[0]).not.toBeChecked()
    expect(screen.getByRole('alert')).toHaveTextContent(COMPARISON_SELECTION_LIMIT_ERROR)
    expect(screen.getByRole('heading', { name: '比较对象 6' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认候选并比较' }))
    const confirmed = onConfirm.mock.calls[0]?.[0] as { snapshot_id?: string; record_id?: string }[]
    expect(confirmed.map((item) => item.snapshot_id)).toEqual(['evs_cand2', 'evs_cand3', 'evs_cand4', 'evs_cand5', 'evs_cand6', 'evs_cand7'])
    expect(confirmed.every((item) => item.record_id == null)).toBe(true)
  })

  it('updates snapshot ids on the same revision item', async () => {
    api.revision.mockResolvedValue(recordRevisionFixture({
      record_id: 'rec_1',
      revision_id: 'rev_a',
      revision_no: 2,
      title: '第一次修订',
      created_at: '2026-07-01T04:00:00Z',
      evidence_snapshot_ids: ['evs_a', 'evs_b'],
      subjects: [],
    }))
    const { onReviseSnapshots } = renderBasket({
      query: fixed([{ record_id: 'rec_1', revision_id: 'rev_a' }]),
    })
    fireEvent.click(await screen.findByRole('button', { name: '选择证据' }))
    const first = await screen.findByRole('checkbox', { name: '选用 左侧观测' })
    const second = await screen.findByRole('checkbox', { name: '选用 右侧观测' })
    fireEvent.click(first)
    fireEvent.click(second)
    fireEvent.click(screen.getByRole('button', { name: '保存证据选择' }))
    await waitFor(() => expect(onReviseSnapshots).toHaveBeenCalledWith(
      { record_id: 'rec_1', revision_id: 'rev_a' },
      ['evs_a', 'evs_b'],
    ))
  })
})
