import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { TargetProbeList } from './TargetProbeList'
import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'

function probeItem(overrides: Partial<ProbeItemRecord> = {}): ProbeItemRecord {
  return {
    probe_item_id: 'pb_001',
    target_id: 'tg_001',
    probe_kind: 'http',
    enabled: true,
    frequency_tier: '5m',
    timeout_seconds: 5,
    config: { path: '/healthz', method: 'GET' },
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

function observation(overrides: Partial<ProbeObservation> = {}): ProbeObservation {
  return {
    monitoring_instance_id: 'mi_001',
    target_id: 'tg_001',
    probe_item_id: 'pb_001',
    probe_kind: 'http',
    observed_at: '2026-04-24T09:05:00Z',
    received_at: '2026-04-24T09:05:01Z',
    agent_version: 'dev',
    fingerprint: 'fp-001',
    result_kind: 'success',
    latency_ms: 83,
    http_status: 200,
    tls_expiry_days: null,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 'sync-001',
    ...overrides,
  }
}

const noopHandlers = {
  registerDeleteButtonRef: () => {},
  onEdit: () => {},
  onToggle: () => {},
  onDelete: () => {},
  onConfirmDelete: () => {},
  onCancelDeleteConfirmation: () => {},
}

describe('TargetProbeList', () => {
  it('renders the empty state when there are no probe items', () => {
    render(
      <TargetProbeList
        probeItems={[]}
        observationsByProbe={new Map()}
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('heading', { name: '目标尚未配置探测项' })).toBeInTheDocument()
  })

  it('renders probe item rows with the latest observations', () => {
    render(
      <TargetProbeList
        probeItems={[probeItem()]}
        observationsByProbe={new Map([['pb_001', [observation()]]])}
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('HTTP')).toBeInTheDocument()
    expect(screen.getByText('83 ms')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
    expect(screen.queryByText('mi_001')).not.toBeInTheDocument()
  })

  it('invokes onDelete when the delete button is clicked', () => {
    const onDelete = vi.fn()
    render(
      <TargetProbeList
        probeItems={[probeItem()]}
        observationsByProbe={new Map()}
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
        onDelete={onDelete}
      />,
    )

    fireEvent.click(
      screen.getByRole('button', { name: /^删除 探测项 pb_001\b/ }),
    )
    expect(onDelete).toHaveBeenCalledTimes(1)
  })

  it('renders the inline delete confirmation when pending matches the row', () => {
    render(
      <TargetProbeList
        probeItems={[probeItem()]}
        observationsByProbe={new Map()}
        actionsDisabled={false}
        pendingProbeConfirmation={{ probeItemId: 'pb_001', action: 'delete' }}
        confirmationCardDisabled={false}
        {...noopHandlers}
      />,
    )

    const dialog = screen.getByRole('alertdialog', { name: '确认删除探测项' })
    expect(dialog).toBeInTheDocument()
    expect(within(dialog).getByText('path: /healthz · method: GET')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认删除探测项' })).toBeInTheDocument()
  })

  it('puts latest HTTP, TLS, and error evidence in one compact table cell', () => {
    render(
      <TargetProbeList
        probeItems={[probeItem()]}
        observationsByProbe={
          new Map([
            [
              'pb_001',
              [
                observation({ monitoring_instance_id: 'mi_alpha', latency_ms: 42, http_status: 200 }),
                observation({
                  monitoring_instance_id: 'mi_beta',
                  observed_at: '2026-04-24T08:55:00Z',
                  result_kind: 'failure',
                  latency_ms: null,
                  http_status: null,
                  error_summary: 'connect: timeout',
                }),
              ],
            ],
          ])
        }
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
      />,
    )

    const table = screen.getByRole('table')
    expect(table).toBeInTheDocument()
    expect(table).toHaveClass('target-probe-table')
    expect(screen.getByRole('columnheader', { name: '方式' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '状态' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '频率' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '最近结果' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '最近观测' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '操作' })).toBeInTheDocument()

    expect(screen.getByText('成功')).toBeInTheDocument()
    expect(screen.getByText('42 ms')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
    expect(screen.queryByText('connect: timeout')).not.toBeInTheDocument()
    expect(screen.queryByText('mi_alpha')).not.toBeInTheDocument()
    expect(screen.queryByText('mi_beta')).not.toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: '执行监控实例' })).not.toBeInTheDocument()
  })

  it('keeps a failed result readable and folds the raw diagnostic', () => {
    render(
      <TargetProbeList
        probeItems={[probeItem()]}
        observationsByProbe={
          new Map([[
            'pb_001',
            [observation({
              result_kind: 'failure',
              latency_ms: 12,
              http_status: 503,
              error_summary: 'connect: timeout',
              error_code: 'probe_timeout',
            })],
          ]])
        }
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
      />,
    )

    const result = screen.getByText('失败').closest('.target-probe-table__result')
    expect(result).toHaveTextContent('失败')
    expect(result).toHaveTextContent('12 ms')
    expect(result).toHaveTextContent('503')
    const details = result?.querySelector('details')
    expect(details).not.toHaveAttribute('open')
    expect(details).toHaveTextContent('connect: timeout')
    expect(screen.getByText('失败').closest('details')).toBeNull()
  })

  it('shows quiet empty copy in the latest cells when a probe item has no observations yet', () => {
    render(
      <TargetProbeList
        probeItems={[probeItem({ probe_item_id: 'pb_quiet' })]}
        observationsByProbe={new Map()}
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getByText('尚无观测')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('renders an "添加 Probe" CTA button in the empty state when onAddProbe is provided', () => {
    const onAddProbe = vi.fn()
    render(
      <TargetProbeList
        probeItems={[]}
        observationsByProbe={new Map()}
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
        onAddProbe={onAddProbe}
      />,
    )

    const button = screen.getByRole('button', { name: '添加探测项' })
    expect(button).toBeInTheDocument()
    fireEvent.click(button)
    expect(onAddProbe).toHaveBeenCalledTimes(1)
  })

  it('renders TLS observation meta column with day suffix', () => {
    render(
      <TargetProbeList
        probeItems={[probeItem({ probe_kind: 'tls' })]}
        observationsByProbe={
          new Map([
            [
              'pb_001',
              [
                observation({
                  probe_kind: 'tls',
                  http_status: null,
                  tls_expiry_days: 13,
                }),
              ],
            ],
          ])
        }
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('13 天')).toBeInTheDocument()
  })
})
