import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FilterBar } from './FilterBar'

describe('FilterBar', () => {
  it('renders children controls', () => {
    render(
      <FilterBar>
        <button type="button">类型</button>
      </FilterBar>,
    )
    expect(screen.getByRole('button', { name: '类型' })).toBeInTheDocument()
  })

  it('hides clear-all button when no filters are active', () => {
    render(
      <FilterBar onClearAll={() => {}} hasActiveFilters={false}>
        <span>controls</span>
      </FilterBar>,
    )
    expect(screen.queryByRole('button', { name: '清空所有' })).not.toBeInTheDocument()
  })

  it('shows clear-all button when active and invokes onClearAll', () => {
    const onClearAll = vi.fn()
    render(
      <FilterBar
        onClearAll={onClearAll}
        hasActiveFilters
        activeChips={<span>chip</span>}
      >
        <span>controls</span>
      </FilterBar>,
    )

    const clearButton = screen.getByRole('button', { name: '清空所有' })
    expect(clearButton).toBeInTheDocument()
    expect(screen.getByText('chip')).toBeInTheDocument()

    fireEvent.click(clearButton)
    expect(onClearAll).toHaveBeenCalledTimes(1)
  })
})
