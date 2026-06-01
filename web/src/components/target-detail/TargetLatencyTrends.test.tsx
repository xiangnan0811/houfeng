import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TargetLatencyTrends } from './TargetLatencyTrends'
import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'

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

function observation(overrides: Partial<ProbeObservation> = {}): ProbeObservation {
  return {
    monitoring_instance_id: 'mi_001',
    target_id: 'tg_001',
    probe_item_id: 'pb_http',
    probe_kind: 'http',
    observed_at: '2026-04-24T09:05:00Z',
    received_at: '2026-04-24T09:05:01Z',
    agent_version: 'dev',
    fingerprint: 'fp-001',
    result_kind: 'success',
    latency_ms: 100,
    http_status: 200,
    tls_expiry_days: null,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 'sync-001',
    ...overrides,
  }
}

describe('TargetLatencyTrends', () => {
  it('renders one metric-card per enabled probe item with sparkline polyline', () => {
    const items = [
      probeItem({ probe_item_id: 'pb_http', probe_kind: 'http', config: { path: '/healthz' } }),
      probeItem({ probe_item_id: 'pb_tcp', probe_kind: 'tcp', config: { port: 443 } }),
    ]
    const observations = [
      observation({ probe_item_id: 'pb_http', latency_ms: 90, observed_at: '2026-04-24T08:00:00Z' }),
      observation({ probe_item_id: 'pb_http', latency_ms: 120, observed_at: '2026-04-24T09:00:00Z' }),
      observation({
        probe_item_id: 'pb_tcp',
        probe_kind: 'tcp',
        latency_ms: 30,
        observed_at: '2026-04-24T09:00:00Z',
      }),
      observation({
        probe_item_id: 'pb_tcp',
        probe_kind: 'tcp',
        monitoring_instance_id: 'mi_002',
        latency_ms: 45,
        observed_at: '2026-04-24T09:05:00Z',
      }),
    ]
    const { container } = render(
      <TargetLatencyTrends
        probeItems={items}
        recentObservations={observations}
      />,
    )

    expect(screen.getByText('近期延迟趋势')).toBeInTheDocument()
    // Two metric cards, one per enabled probe item
    expect(container.querySelectorAll('.metric-card').length).toBe(2)
    // Sparkline polyline rendered for multi-sample series
    expect(container.querySelectorAll('svg.sparkline polyline').length).toBe(2)
    // Card titles include the kind label
    expect(
      screen.getByRole('heading', { name: 'HTTP · path: /healthz' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'TCP · port: 443' }),
    ).toBeInTheDocument()
    // Latest latency value 120 appears in HTTP card head + max stats; 45 in TCP card
    expect(screen.getAllByText('120 ms').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('45 ms').length).toBeGreaterThanOrEqual(1)
  })

  it('renders an empty state when there are no enabled probe items with samples', () => {
    render(
      <TargetLatencyTrends probeItems={[]} recentObservations={[]} />,
    )

    expect(
      screen.getByRole('heading', { name: '近 24h 暂无可用延迟样本' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '该目标尚未收到带有 latency_ms 的成功观测，或所有 ProbeItem 当前均处于停用状态。',
      ),
    ).toBeInTheDocument()
  })

  it('shows aside meta with sample count, oldest, newest, and backfill count', () => {
    const observations = [
      observation({ observed_at: '2026-04-24T08:00:00Z', is_backfilled: true }),
      observation({ observed_at: '2026-04-24T09:05:00Z' }),
    ]
    const { container } = render(
      <TargetLatencyTrends
        probeItems={[probeItem()]}
        recentObservations={observations}
      />,
    )

    const meta = container.querySelector('.detail-section__aside-meta')
    expect(meta).not.toBeNull()
    expect(meta?.textContent ?? '').toContain('24h 2 样本')
    expect(meta?.textContent ?? '').toContain('backfill 1')
  })

  it('applies maintenance ribbon when isMaintenance is true', () => {
    const { container } = render(
      <TargetLatencyTrends
        probeItems={[probeItem()]}
        recentObservations={[observation()]}
        isMaintenance
      />,
    )

    const section = container.querySelector('.detail-section')
    expect(section?.className ?? '').toContain('detail-section--ribbon-maintenance')
  })

  it('skips disabled probe items even when observations exist', () => {
    const items = [
      probeItem({ probe_item_id: 'pb_disabled', enabled: false }),
    ]
    const observations = [observation({ probe_item_id: 'pb_disabled', latency_ms: 100 })]
    const { container } = render(
      <TargetLatencyTrends probeItems={items} recentObservations={observations} />,
    )
    expect(container.querySelectorAll('.metric-card').length).toBe(0)
    expect(
      screen.getByRole('heading', { name: '近 24h 暂无可用延迟样本' }),
    ).toBeInTheDocument()
  })
})
