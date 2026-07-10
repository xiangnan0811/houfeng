import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { AssetDecisionEvidenceAssessment } from '../../lib/types'
import { renderEvidenceAssessment } from './renderHelpers'

const assessment: AssetDecisionEvidenceAssessment = {
  confidence_score: 82,
  pressure_score: 38,
  readiness_score: 76,
  quality_tier: 'strong',
  decision_bias: 'keep',
  support_signal_count: 5,
  risk_signal_count: 1,
  gap_signal_count: 0,
  summary: '证据完整：可保存组合判断',
}

describe('renderEvidenceAssessment', () => {
  it('uses native progress semantics without inline score styles', () => {
    const { container } = render(<>{renderEvidenceAssessment(assessment, 'detail')}</>)

    expect(screen.getByRole('progressbar', { name: '可信' })).toHaveAttribute('value', '82')
    expect(screen.getByRole('progressbar', { name: '压力' })).toHaveAttribute('value', '38')
    expect(screen.getByRole('progressbar', { name: '准备' })).toHaveAttribute('value', '76')
    expect(container.querySelector('[style]')).not.toBeInTheDocument()
  })
})
