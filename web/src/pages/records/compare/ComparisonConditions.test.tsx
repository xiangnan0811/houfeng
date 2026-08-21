import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ComparisonConditions } from './ComparisonConditions'
import { COMPARISON_URL_VERSION } from './comparisonQueryState'

describe('ComparisonConditions', () => {
  it('keeps conditions inside a reviewable fold', () => {
    render(
      <ComparisonConditions
        query={{
          version: COMPARISON_URL_VERSION,
          mode: 'fixed',
          items: [{ snapshot_id: 'evs_a' }, { snapshot_id: 'evs_b' }],
          baseline: 0,
          alignment: 'actual_coverage',
          requested_from: '2026-07-01T00:00:00Z',
          requested_to: '2026-07-02T00:00:00Z',
          tolerance_seconds: 60,
        }}
        onBaseline={vi.fn()}
        onAlignment={vi.fn()}
        onWindow={vi.fn()}
        onTolerance={vi.fn()}
        onBucket={vi.fn()}
      />,
    )
    const fold = screen.getByRole('group')
    expect(fold).toHaveAttribute('open')
    expect(screen.getByRole('heading', { name: '比较条件' })).toBeInTheDocument()
    expect(screen.getByLabelText('对齐')).toBeInTheDocument()
  })
})
