import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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
    expect(screen.getByRole('heading', { name: '结论与另存' })).toBeInTheDocument()
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

  it('confirms a save in Chinese and links without using the record id as the confirmation', () => {
    render(
      <MemoryRouter>
        <ComparisonSaveRecord
          blocked={false}
          title="比较"
          conclusion="结论"
          saving={false}
          savedRecordId="rec_saved"
          onTitle={vi.fn()}
          onConclusion={vi.fn()}
          onSave={vi.fn()}
        />
      </MemoryRouter>,
    )
    const status = screen.getByRole('status')
    expect(status).toHaveTextContent('比较记录已保存')
    expect(status).not.toHaveTextContent('rec_saved')
    expect(screen.getByRole('link', { name: '查看比较记录' })).toHaveAttribute('href', '/records/rec_saved')
  })
})
