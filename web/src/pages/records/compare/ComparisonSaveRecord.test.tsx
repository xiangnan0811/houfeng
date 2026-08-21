import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ComparisonSaveRecord } from './ComparisonSaveRecord'

describe('ComparisonSaveRecord', () => {
  it('omits the save action when blocked', () => {
    const { rerender } = render(
      <ComparisonSaveRecord
        blocked
        title=""
        conclusion=""
        saving={false}
        savedRecordId={null}
        onTitle={vi.fn()}
        onConclusion={vi.fn()}
        onSave={vi.fn()}
      />,
    )
    expect(screen.queryByRole('button', { name: '另存为记录' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '人工结论与另存' })).toBeInTheDocument()
    expect(screen.getByText(/不能另存/)).toBeInTheDocument()
    rerender(
      <ComparisonSaveRecord
        blocked={false}
        title="比较"
        conclusion="结论"
        saving={false}
        savedRecordId={null}
        onTitle={vi.fn()}
        onConclusion={vi.fn()}
        onSave={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: '另存为记录' })).toBeInTheDocument()
  })
})
