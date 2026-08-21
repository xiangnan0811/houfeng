import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { ComparisonEvaluateResponse } from '../../../lib/types'
import { ComparisonKindPanel } from './ComparisonKindPanel'

const comparison: ComparisonEvaluateResponse = {
  digest: 'aa'.repeat(32),
  items: [],
  review: [],
  available_kinds: [{ kind: 'monitoring.host', schema_version: 1 }],
  pairwise: [],
  series: [
    { item_index: 0, metric_id: 'cpu_usage_pct', segments: [], unit: '%' },
    { item_index: 0, metric_id: 'mem_used_pct', segments: [], unit: '%' },
  ],
  save_eligibility: { eligible: true, blockers: [] },
}

describe('ComparisonKindPanel', () => {
  it('lets the operator pick a series metric without changing kind', () => {
    const onSelect = vi.fn()
    render(
      <ComparisonKindPanel
        comparison={comparison}
        activeKind="monitoring.host/v1"
        metric="cpu_usage_pct"
        onSelect={onSelect}
      />,
    )
    expect(screen.getByRole('group', { name: '比较指标' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'mem_used_pct' }))
    expect(onSelect).toHaveBeenCalledWith('monitoring.host/v1', 'mem_used_pct')
  })
})
