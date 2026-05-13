import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Button } from './atoms'
import { ObservabilityEvidenceLead } from './ObservabilityEvidenceLead'

describe('ObservabilityEvidenceLead', () => {
  it('renders lead copy, filter context, and supplied actions', () => {
    const onClick = vi.fn()

    render(
      <ObservabilityEvidenceLead
        tone="alert"
        eyebrow="OBSERVABILITY SUPPORT"
        title="先处理异常证据"
        description="当前切片存在异常证据。"
        filterItems={['仅看异常', '严重']}
        emptyFilterLabel="完整库存"
        filterAriaLabel="当前证据筛选"
        action={
          <Button variant="secondary" size="md" onClick={onClick}>
            仅看异常
          </Button>
        }
        secondaryAction={<a href="/asset-decisions">资产决策队列</a>}
      />,
    )

    expect(screen.getByRole('heading', { name: '先处理异常证据' })).toBeInTheDocument()
    expect(screen.getByText('当前切片存在异常证据。')).toBeInTheDocument()
    expect(screen.getByLabelText('当前证据筛选')).toHaveTextContent('仅看异常')
    expect(screen.getByLabelText('当前证据筛选')).toHaveTextContent('严重')
    expect(screen.getByRole('link', { name: '资产决策队列' })).toHaveAttribute(
      'href',
      '/asset-decisions',
    )

    fireEvent.click(screen.getByRole('button', { name: '仅看异常' }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('renders the empty filter label when no filters are active', () => {
    render(
      <ObservabilityEvidenceLead
        tone="normal"
        eyebrow="ENTRY OBSERVABILITY"
        title="入口证据稳定"
        description="没有活动筛选。"
        filterItems={[]}
        emptyFilterLabel="完整 Target 库存"
        filterAriaLabel="当前入口证据筛选"
        action={<a href="/targets">查看入口</a>}
      />,
    )

    expect(screen.getByLabelText('当前入口证据筛选')).toHaveTextContent('完整 Target 库存')
  })
})
