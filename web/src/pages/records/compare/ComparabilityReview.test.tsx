import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ComparabilityReview } from './ComparabilityReview'
import type { ComparisonEvaluateResponse } from '../../../lib/types'

const comparison: ComparisonEvaluateResponse = {
  digest: 'aa'.repeat(32),
  items: [],
  review: [{ item_index: 1, reason: 'metadata_only' }],
  available_kinds: [],
  pairwise: [],
  series: [],
  save_eligibility: { eligible: true, blockers: [] },
}

describe('ComparabilityReview', () => {
  it('does not claim there are no warnings before a comparison has loaded', () => {
    const { container } = render(<ComparabilityReview comparison={null} />)
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText(/没有额外可比性告警/)).not.toBeInTheDocument()
  })

  it('renders a review heading and readable reason text', () => {
    render(<ComparabilityReview comparison={comparison} />)
    expect(screen.getByRole('heading', { name: '可比性审查' })).toBeInTheDocument()
    expect(screen.getByText(/仅元数据，无数值比较/)).toBeInTheDocument()
  })
})
