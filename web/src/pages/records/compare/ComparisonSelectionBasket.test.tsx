import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

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
