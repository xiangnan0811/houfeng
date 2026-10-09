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

  it('adds a narrow-screen toggle that controls the collapsible controls row', () => {
    render(
      <FilterBar narrowCollapse={{ activeCount: 2 }}>
        <span>健康</span>
        <span className="filter-bar__keep">搜索</span>
      </FilterBar>,
    )
    const toggle = screen.getByRole('button', { name: '筛选 (2)' })
    const row = document.getElementById(toggle.getAttribute('aria-controls')!)!
    expect(row).toHaveClass('filter-bar__controls-row')
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(row).toHaveAttribute('data-narrow-collapsed')
    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(row).not.toHaveAttribute('data-narrow-collapsed')
  })

  it('has no narrow toggle unless the page opts in', () => {
    render(
      <FilterBar>
        <span>健康</span>
      </FilterBar>,
    )
    expect(screen.queryByRole('button', { name: /筛选/ })).not.toBeInTheDocument()
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
