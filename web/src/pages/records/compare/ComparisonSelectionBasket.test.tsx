import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { ComparisonCandidateItem } from '../../../lib/types'
import { ComparisonSelectionBasket } from './ComparisonSelectionBasket'
import { COMPARISON_URL_VERSION } from './comparisonQueryState'

describe('ComparisonSelectionBasket', () => {
  it('blocks compare copy when fewer than two items are selected', () => {
    render(
      <ComparisonSelectionBasket
        query={{
          version: COMPARISON_URL_VERSION,
          mode: 'fixed',
          items: [{ snapshot_id: 'evs_a' }],
          baseline: 0,
          alignment: 'actual_coverage',
          requested_from: '2026-07-01T00:00:00Z',
          requested_to: '2026-07-02T00:00:00Z',
        }}
        candidates={null}
        onConfirm={vi.fn()}
      />,
    )
    expect(screen.getByText(/至少选择 2 项/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认候选并比较' })).not.toBeInTheDocument()
  })
})

describe('ComparisonSelectionBasket candidates', () => {
  it('counts, lists and confirms the same first six candidates', () => {
    const onConfirm = vi.fn()
    const candidates = Array.from({ length: 8 }, (_, index) => ({
      subject: { kind: 'vps' as const, id: `vps_${index}` },
      snapshot_id: `evs_cand${index}`,
      record_id: '',
      revision_ids: [],
      kind: 'monitoring.host',
      schema_version: 1,
      observed_at: '2026-07-01T00:00:00Z',
    }))
    render(
      <ComparisonSelectionBasket
        query={{
          version: COMPARISON_URL_VERSION,
          mode: 'candidate',
          subjects: [{ kind: 'vps', id: 'vps_0' }, { kind: 'vps', id: 'vps_1' }],
          requested_from: '2026-07-01T00:00:00Z',
          requested_to: '2026-07-02T00:00:00Z',
        }}
        candidates={candidates as unknown as ComparisonCandidateItem[]}
        onConfirm={onConfirm}
      />,
    )
    expect(screen.getByRole('heading', { name: '比较对象 6' })).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(6)
    expect(screen.getByText('共 8 个候选，只比较前 6 个。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认候选并比较' }))
    expect(onConfirm.mock.calls[0]?.[0]).toHaveLength(6)
  })
})
