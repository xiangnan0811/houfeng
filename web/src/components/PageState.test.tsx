import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PageState } from './PageState'

describe('PageState', () => {
  it('renders a loading state with timestamp metadata', () => {
    render(
      <PageState
        kind="loading"
        title="正在加载事件…"
        timestamp="2026-05-13T08:00:00Z"
      />,
    )

    expect(screen.getByRole('status')).toHaveClass('page-state--loading')
    expect(screen.getByRole('heading', { name: '正在加载事件…' })).toBeInTheDocument()
    expect(screen.getByText('请求发起')).toBeInTheDocument()
  })

  it('renders an error state with truncated technical summary', () => {
    const summary = 'x'.repeat(140)

    render(
      <PageState
        kind="error"
        title="事件不可用"
        description="无法读取事件时间线。"
        technicalSummary={summary}
      />,
    )

    expect(screen.getByRole('alert')).toHaveClass('page-state--error')
    expect(screen.getByText('无法读取事件时间线。')).toBeInTheDocument()
    expect(screen.getByText(`${'x'.repeat(119)}…`)).toBeInTheDocument()
  })

  it('renders an empty state with action slot and compact surface', () => {
    render(
      <PageState
        kind="empty"
        surface="empty"
        compact
        title="没有匹配的事件"
        description="请重置筛选后重新查看。"
        action={<button type="button">重置筛选</button>}
      />,
    )

    const state = screen.getByRole('heading', { name: '没有匹配的事件' }).closest('section')
    expect(state).toHaveClass('empty-state', 'page-state--empty', 'page-state--compact')
    expect(screen.getByRole('button', { name: '重置筛选' })).toBeInTheDocument()
  })
})
