import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ComparisonMatrix } from './ComparisonMatrix'
import type { ComparisonEvaluateResponse } from '../../../lib/types'

const comparison: ComparisonEvaluateResponse = {
  digest: 'aa'.repeat(32),
  items: [{
    snapshot_id: 'evs_a',
    canonical_hash: '11'.repeat(32),
    kind: 'monitoring.host',
    schema_version: 1,
    revision_context: 'not_applicable',
  }],
  review: [{ item_index: 0, reason: 'coverage_partial' }],
  available_kinds: [],
  pairwise: [{
    item_index: 0,
    kind: 'monitoring.host',
    schema_version: 1,
    compatible: true,
    reason: 'coverage_partial',
    values: { matched: 3, unmatched_baseline: 1, unmatched_item: 0, equal: false },
  }],
  series: [{
    item_index: 0,
    metric_id: 'cpu_usage_pct',
    unit: '%',
    segments: [[{ start: 'a', end: 'b', value: 1 }, { start: 'c', end: 'd', value: 2 }], [{ start: 'e', end: 'f', value: 3 }]],
  }],
  save_eligibility: { eligible: true, blockers: [] },
}

describe('ComparisonMatrix', () => {
  it('uses a named scroll region and readable reasons', () => {
    render(<ComparisonMatrix kind="monitoring.host/v1" comparison={comparison} />)
    const region = screen.getByRole('region', { name: '对齐矩阵' })
    expect(region).toHaveAttribute('tabIndex', '0')
    expect(screen.getByRole('columnheader', { name: '覆盖' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '桶数' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '质量' })).toBeInTheDocument()
    expect(screen.getByText('部分 3/4')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('有差值')).toBeInTheDocument()
    expect(screen.getByText('覆盖不完整')).toBeInTheDocument()
  })
})
