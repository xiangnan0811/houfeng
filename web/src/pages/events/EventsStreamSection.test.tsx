import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { StateChangeEventRecord } from '../../lib/types'
import { EventsStreamSection } from './EventsStreamSection'

function eventRecord(index: number): StateChangeEventRecord {
  return {
    event_id: `event-${index}`,
    object_type: 'target',
    object_id: 'target-1',
    event_type: 'target_paused',
    incident_id: '',
    incident_class: 'certificate',
    severity: '告警',
    summary: `事件摘要 ${index}`,
    created_at: '2026-07-11T12:00:00Z',
  }
}

function renderStream(
  events: StateChangeEventRecord[],
  overrides: Partial<Parameters<typeof EventsStreamSection>[0]> = {},
) {
  const onPageChange = vi.fn()
  const onLoadMore = vi.fn()
  render(
    <MemoryRouter>
      <h1 id="events-page-title">事件流</h1>
      <EventsStreamSection
        events={events}
        exhausted
        loadingMore={false}
        hasActiveFilters={false}
        page={1}
        nameMap={new Map([['target-1', '目标一']])}
        onPageChange={onPageChange}
        onLoadMore={onLoadMore}
        onClearFilters={vi.fn()}
        {...overrides}
      />
    </MemoryRouter>,
  )
  return { onPageChange, onLoadMore }
}

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

    expect(screen.getByRole('heading', { name: '当前筛选没有匹配的事件' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '清空筛选' })).toBeInTheDocument()
    expect(container.querySelector('[style]')).not.toBeInTheDocument()
  })

  it('renders notice-rail rows with Chinese type and class labels', () => {
    renderStream([eventRecord(1)])

    expect(screen.getByRole('list', { name: '事件流' })).toHaveClass('events-stream__list')
    expect(screen.getByText('告警')).toBeInTheDocument()
    expect(screen.getByText('目标已暂停')).toBeInTheDocument()
    expect(screen.getByText('事件摘要 1')).toBeInTheDocument()
    expect(screen.getByText('证书', { exact: false })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '目标 · 目标一' })).toHaveAttribute('href', '/targets/target-1')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('shows a count summary without pager buttons when a single exhausted page fits', () => {
    renderStream([eventRecord(1)])

    const pager = screen.getByRole('navigation', { name: '事件分页' })
    expect(pager).toHaveTextContent('第 1–1 条，共 1 条')
    expect(within(pager).queryByRole('button', { name: '上一页' })).not.toBeInTheDocument()
    expect(within(pager).queryByRole('button', { name: '下一页' })).not.toBeInTheDocument()
    expect(within(pager).queryByRole('button', { name: '加载后续' })).not.toBeInTheDocument()
  })

  it('pages locally and keeps load-more separate from next page', () => {
    const events = Array.from({ length: 45 }, (_, index) => eventRecord(index + 1))
    const { onPageChange, onLoadMore } = renderStream(events, { page: 2 })

    const pager = screen.getByRole('navigation', { name: '事件分页' })
    expect(pager).toHaveTextContent('第 21–40 条，共 45 条')
    expect(screen.getByText('事件摘要 21')).toBeInTheDocument()
    expect(screen.queryByText('事件摘要 1')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '第 2 页' })).toHaveAttribute('aria-current', 'page')

    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    expect(onPageChange).toHaveBeenCalledWith(3)
    expect(onLoadMore).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '加载后续' })).not.toBeInTheDocument()
  })

  it('offers load-more on the last loaded page when the window is not exhausted', () => {
    const events = Array.from({ length: 20 }, (_, index) => eventRecord(index + 1))
    const { onLoadMore } = renderStream(events, { exhausted: false })

    const pager = screen.getByRole('navigation', { name: '事件分页' })
    expect(pager).toHaveTextContent('第 1–20 条，已加载 20 条')
    expect(screen.queryByRole('button', { name: '下一页' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '加载后续' }))
    expect(onLoadMore).toHaveBeenCalledTimes(1)
  })

  it('keeps the current page and shows a local error when loading more failed', () => {
    const events = Array.from({ length: 20 }, (_, index) => eventRecord(index + 1))
    renderStream(events, { exhausted: false, loadMoreError: '网络中断' })

    expect(screen.getByText('事件摘要 1')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('网络中断')
    expect(screen.getByRole('button', { name: '加载后续' })).toBeEnabled()
  })
})
