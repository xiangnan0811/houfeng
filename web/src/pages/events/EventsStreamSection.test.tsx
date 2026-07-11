import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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

  it('keeps the wide event table inside a named keyboard-scroll region', () => {
    render(
      <MemoryRouter>
        <h1 id="events-page-title">事件流</h1>
        <EventsStreamSection
          events={[{
            event_id: 'event-1',
            object_type: 'target',
            object_id: 'target-1',
            event_type: 'target_paused',
            incident_id: '',
            incident_class: '',
            severity: '正常',
            summary: '目标创建完成',
            created_at: '2026-07-11T12:00:00Z',
          }]}
          exhausted
          loadingMore={false}
          hasActiveFilters={false}
          page={1}
          nameMap={new Map([['target-1', '目标一']])}
          onPageChange={vi.fn()}
          onLoadMore={vi.fn()}
          onClearFilters={vi.fn()}
        />
      </MemoryRouter>,
    )

    const region = screen.getByRole('region', { name: '事件流' })
    const hint = screen.getByText('表格可横向滚动查看完整事件字段')
    expect(region).toHaveClass('events-table-scroll')
    expect(region).toHaveAttribute('tabindex', '0')
    expect(region).toHaveAttribute('aria-labelledby', 'events-page-title')
    expect(region).toHaveAttribute('aria-describedby', hint.id)
    expect(within(region).getByRole('table')).toBeInTheDocument()
    expect(within(region).queryByRole('button', { name: '上一页' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '上一页' })).toBeInTheDocument()
  })
})
