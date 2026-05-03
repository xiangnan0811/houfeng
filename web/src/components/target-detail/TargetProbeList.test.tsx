import { createRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
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
    node_id: 'nd_001',
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
        pendingProbeConfirmationCardRef={createRef<HTMLDivElement>()}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('heading', { name: '当前还没有 ProbeItem' })).toBeInTheDocument()
  })

  it('renders probe item rows with the latest observations', () => {
    render(
      <TargetProbeList
        probeItems={[probeItem()]}
        observationsByProbe={new Map([['pb_001', [observation()]]])}
        actionsDisabled={false}
        pendingProbeConfirmation={null}
        confirmationCardDisabled={false}
        pendingProbeConfirmationCardRef={createRef<HTMLDivElement>()}
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('HTTP')).toBeInTheDocument()
    expect(screen.getByText('nd_001')).toBeInTheDocument()
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
        pendingProbeConfirmationCardRef={createRef<HTMLDivElement>()}
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
        pendingProbeConfirmationCardRef={createRef<HTMLDivElement>()}
        {...noopHandlers}
      />,
    )

    expect(screen.getByRole('heading', { name: '确认删除 ProbeItem' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认删除 ProbeItem' })).toBeInTheDocument()
  })
})
