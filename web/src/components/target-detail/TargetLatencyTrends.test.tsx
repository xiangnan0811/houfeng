import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import {
  TargetLatencyTrends,
  type TargetLatencyTrend,
} from './TargetLatencyTrends'
import type { ProbeItemRecord } from '../../lib/types'

function trend(overrides: Partial<TargetLatencyTrend> = {}): TargetLatencyTrend {
  return {
    probeItemId: 'pb_http',
    probeKind: 'http',
    count: 2,
    distinctNodeCount: 2,
    averageLatency: 100,
    maxLatency: 120,
    latestLatency: 120,
    newestObservedAt: '2026-04-24T09:05:00Z',
    oldestObservedAt: '2026-04-24T08:55:00Z',
    ...overrides,
  }
}

function probeItem(overrides: Partial<ProbeItemRecord> = {}): ProbeItemRecord {
  return {
    probe_item_id: 'pb_http',
    target_id: 'tg_001',
    probe_kind: 'http',
    enabled: true,
    frequency_tier: '5m',
    timeout_seconds: 5,
    config: { path: '/healthz' },
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

describe('TargetLatencyTrends', () => {
  it('renders trend cards using probe-kind from the probeItemsById map', () => {
    const probeItemsById = new Map([['pb_http', probeItem()]])
    render(<TargetLatencyTrends trends={[trend()]} probeItemsById={probeItemsById} />)

    expect(screen.getByText('近期延迟趋势')).toBeInTheDocument()
    expect(screen.getByText('HTTP')).toBeInTheDocument()
    expect(screen.getByText('pb_http')).toBeInTheDocument()
    expect(screen.getByText('100 ms')).toBeInTheDocument()
    expect(screen.getByText('2 次观测')).toBeInTheDocument()
  })

  it('renders an empty state when there are no trends', () => {
    render(<TargetLatencyTrends trends={[]} probeItemsById={new Map()} />)

    expect(screen.getByRole('heading', { name: '暂无可用延迟样本' })).toBeInTheDocument()
    expect(
      screen.getByText('近期延迟趋势仅统计已返回成功且带延迟值的近期探测观测。'),
    ).toBeInTheDocument()
  })
})
