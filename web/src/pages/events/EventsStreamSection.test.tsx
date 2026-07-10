import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { EventsStreamSection } from './EventsStreamSection'

describe('EventsStreamSection', () => {
  it('renders the filtered empty state without inline styles', () => {
    const { container } = render(
      <EventsStreamSection
        events={[]}
        exhausted
        loadingMore={false}
        hasActiveFilters
        page={1}
        nameMap={new Map()}
        onPageChange={vi.fn()}
        onLoadMore={vi.fn()}
        onClearFilters={vi.fn()}
      />,
    )

    expect(screen.getByText('当前筛选没有匹配的事件')).toHaveClass('events-stream-empty__message')
    expect(screen.getByRole('button', { name: '重置筛选' })).toHaveClass('events-stream-empty__reset')
    expect(container.querySelector('[style]')).not.toBeInTheDocument()
  })
})
