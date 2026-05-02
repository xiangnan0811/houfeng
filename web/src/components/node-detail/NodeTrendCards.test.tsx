import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { NodeTrendCards, type NodeRecentTrend } from './NodeTrendCards'

function recentTrend(overrides: Partial<NodeRecentTrend> = {}): NodeRecentTrend {
  return {
    count: 2,
    newestObservedAt: '2026-04-24T09:30:00Z',
    oldestObservedAt: '2026-04-23T09:30:00Z',
    averageLoad5: 1.4,
    averageIowait: 5.0,
    averageSteal: 1.3,
    latestLoad5: 1.5,
    latestIowait: 4.8,
    latestSteal: 1.2,
    load5Series: [1.3, 1.5],
    iowaitSeries: [4.0, 6.0],
    stealSeries: [1.0, 1.6],
    ...overrides,
  }
}

describe('NodeTrendCards', () => {
  it('renders four trend cards with averages and sparklines when trend present', () => {
    const { container } = render(<NodeTrendCards recentTrend={recentTrend()} />)

    expect(screen.getAllByText('近期趋势')[0]).toBeInTheDocument()
    expect(screen.getByText('近 24h 样本')).toBeInTheDocument()
    expect(screen.getByText('Load5 平均')).toBeInTheDocument()
    expect(screen.getByText('1.4')).toBeInTheDocument()
    expect(screen.getByText('iowait 平均')).toBeInTheDocument()
    expect(screen.getByText('5.0%')).toBeInTheDocument()
    expect(screen.getByText('steal 平均')).toBeInTheDocument()
    expect(screen.getByText('1.3%')).toBeInTheDocument()
    // three sparklines (load5, iowait, steal)
    expect(container.querySelectorAll('polyline').length).toBe(3)
  })

  it('renders empty state when recentTrend is null', () => {
    render(<NodeTrendCards recentTrend={null} />)

    expect(screen.getByRole('heading', { name: '近 24h 暂无样本' })).toBeInTheDocument()
    expect(
      screen.getByText('近期趋势需要近 24h 主机采样数据，当前还没有可用样本。'),
    ).toBeInTheDocument()
  })
})
