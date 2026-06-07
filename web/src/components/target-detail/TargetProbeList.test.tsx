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

    expect(screen.getByRole('heading', { name: '目标尚未配置 ProbeItem' })).toBeInTheDocument()
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
    expect(screen.getByText('mi_001')).toBeInTheDocument()
    expect(screen.getByText('83 ms')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
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
      screen.getByRole('button', { name: /^删除 ProbeItem pb_001\b/ }),
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

    const dialog = screen.getByRole('alertdialog', { name: '确认删除 ProbeItem' })
    expect(dialog).toBeInTheDocument()
    expect(within(dialog).getByText('path: /healthz · method: GET')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认删除 ProbeItem' })).toBeInTheDocument()
  })

  it('renders observations as a DataTable with status glyph, hostname, latency and http columns', () => {
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

    // DataTable rendered as a real <table>
    const table = screen.getByRole('table')
    expect(table).toBeInTheDocument()
    expect(table).toHaveClass('probe-observations')

    // Column headers
    expect(screen.getByRole('columnheader', { name: '执行监控实例' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '观测时间' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '延迟' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'HTTP / TLS' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '错误摘要' })).toBeInTheDocument()

    // Row content
    expect(screen.getByText('mi_alpha')).toBeInTheDocument()
    expect(screen.getByText('mi_beta')).toBeInTheDocument()
    expect(screen.getByText('42 ms')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
    expect(screen.getByText('connect: timeout')).toBeInTheDocument()

    // Status glyph: success → 成功 ; failure → 失败
    expect(screen.getByLabelText('成功')).toBeInTheDocument()
    expect(screen.getByLabelText('失败')).toBeInTheDocument()
  })

  it('shows the per-card empty state when a probe item has no observations yet', () => {
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

    // No DataTable rendered
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    // Inline empty placeholder
    expect(screen.getByText('尚未收到观测')).toBeInTheDocument()
    // dl meta also reflects "尚无观测结果"
    expect(screen.getByText('尚无观测结果')).toBeInTheDocument()
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

    const button = screen.getByRole('button', { name: '添加 Probe' })
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
