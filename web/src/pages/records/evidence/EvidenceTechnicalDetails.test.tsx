import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { EvidenceSnapshotRead } from '../../../lib/types'
import { EvidenceTechnicalDetails } from './EvidenceTechnicalDetails'

const snapshot = {
  snapshot_id: 'evs_tech',
  kind: 'command.audit',
  schema_version: 1,
  renderer_version: 'command_audit_v1',
  observed_at: '2026-08-17T14:00:00Z',
  captured_at: '2026-08-17T14:00:05Z',
  referenced_at: '2026-08-17T14:10:00Z',
  requested_window: { start: '2026-08-17T13:00:00Z', end: '2026-08-17T14:00:00Z' },
  actual_window: { start: '2026-08-17T13:00:00Z', end: '2026-08-17T14:00:00Z' },
  actual_precision_seconds: 300,
  source_revision: 'rev-7',
  source_watermark: 'wm-7',
  producer_version: 'center/1.9.0',
  calculation_version: 'calc/v2',
  sensitivity: 'normal',
  retention: { immutable: true, scope: 'record_revision', source_deletion: 'snapshot_retained_source_unavailable' },
  redaction: [
    { path: 'payload.stdout', sensitivity: 'forbidden', action: 'stripped' },
    { path: 'payload.kept', sensitivity: 'normal', action: 'included' },
  ],
} as unknown as EvidenceSnapshotRead

describe('EvidenceTechnicalDetails', () => {
  it('keeps provenance and redaction folded and lists only processed fields', () => {
    render(<EvidenceTechnicalDetails snapshot={snapshot} />)
    const summary = screen.getByText('技术细节')
    expect(summary.closest('details')).not.toHaveAttribute('open')
    expect(screen.getByText('· 1 个字段已按策略处理')).toBeInTheDocument()
    fireEvent.click(summary)
    expect(screen.getByText('evs_tech')).toBeInTheDocument()
    expect(screen.getByText('5 分钟')).toBeInTheDocument()
    expect(screen.getByText('payload.stdout')).toBeInTheDocument()
    expect(screen.queryByText('payload.kept')).not.toBeInTheDocument()
    expect(screen.getByText('不可变快照 · 来源删除后仍保留')).toBeInTheDocument()
  })
})
