import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { EvidenceCapturePreview } from '../../../lib/types'
import { EvidenceCapturePicker } from './EvidenceCapturePicker'

const captureOptions = [
  {
    kind: 'monitoring.host',
    schema_version: 1,
    label: '主机趋势',
    sources: [
      { source_type: 'monitoring_instance', source_id: 'mon_primary', label: '主监控实例' },
      { source_type: 'monitoring_instance', source_id: 'mon_backup', label: '备用监控实例' },
    ],
    metrics: [
      { value: 'cpu_usage_pct', label: 'CPU 使用率' },
      { value: 'memory_usage_pct', label: '内存使用率' },
    ],
    precision_options: [
      { seconds: 300, label: '5 分钟' },
      { seconds: 900, label: '15 分钟' },
    ],
    sensitive_topology_fields: [
      { value: 'source.private_address', label: '源私网地址' },
    ],
  },
  {
    kind: 'monitoring.probe',
    schema_version: 2,
    label: '探针趋势',
    sources: [
      { source_type: 'probe', source_id: 'probe_primary', label: '主探针' },
    ],
    metrics: [{ value: 'latency_ms', label: '延迟' }],
    precision_options: [{ seconds: 60, label: '1 分钟' }],
    sensitive_topology_fields: [],
  },
] as const

const preview: EvidenceCapturePreview = {
  record_id: 'rec_picker',
  snapshot_id: 'evs_picker',
  capture_intent_id: 'eci_picker',
  kind: 'monitoring.host',
  schema_version: 1,
  subject: { type: 'vps', id: 'vps_picker', display_name: '边缘节点' },
  source: { type: 'monitoring_instance', id: 'mon_primary', display_name: '主监控实例' },
  requested_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  actual_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  observed_at: '2026-08-16T02:00:00Z',
  source_revision: 'revision-safe',
  source_watermark: 'watermark-safe',
  producer_version: 'producer-v1',
  calculation_version: 'calculation-v1',
  units: { status: 'applicable', values: { cpu_usage_pct: 'percent' } },
  quality: {
    status: 'complete',
    sample_count: 12,
    gap_count: 0,
    maintenance_count: 0,
    backfilled_count: 0,
    bucket_count: 12,
    data_point_count: 12,
    peak_count: 1,
    truncated: false,
    partial: false,
  },
  sensitivity: 'normal',
  actual_precision_seconds: 300,
  bucket_width_seconds: 300,
  quota: { status: 'allowed' },
  retention: {
    immutable: true,
    scope: 'record_revision',
    source_deletion: 'snapshot_retained_source_unavailable',
  },
  redaction: [],
  estimated_canonical_bytes: 4096,
  renderer_version: 'monitoring_host_v1',
  previewed_at: '2026-08-16T02:00:00Z',
  valid_until: '2026-08-16T02:05:00Z',
}

function fillRequiredWorkflow() {
  fireEvent.change(screen.getByLabelText('证据类型'), { target: { value: 'monitoring.host/1' } })
  fireEvent.change(screen.getByLabelText('数据来源'), { target: { value: 'monitoring_instance/mon_primary' } })
  fireEvent.change(screen.getByLabelText('开始时间（UTC）'), { target: { value: '2026-08-16T01:00' } })
  fireEvent.change(screen.getByLabelText('结束时间（UTC）'), { target: { value: '2026-08-16T02:00' } })
  fireEvent.click(screen.getByRole('checkbox', { name: 'CPU 使用率' }))
  fireEvent.change(screen.getByLabelText('采样精度'), { target: { value: '300' } })
}

describe('EvidenceCapturePicker', () => {
  it('enforces the ordered workflow and resets every downstream choice after an upstream change', async () => {
    const requestPreview = vi.fn().mockResolvedValue(preview)
    const { container } = render(
      <EvidenceCapturePicker
        recordId="rec_picker"
        options={captureOptions}
        requestPreview={requestPreview}
        onConfirm={vi.fn()}
        now={() => new Date('2026-08-16T02:01:00Z')}
      />,
    )

    expect(Array.from(container.querySelectorAll('fieldset legend')).map((legend) => legend.textContent)).toEqual([
      '1. 证据类型',
      '2. 数据来源',
      '3. 绝对时间窗口',
      '4. 指标',
      '5. 精度',
      '6. 敏感字段',
      '7. 预览',
      '8. 确认',
    ])
    expect(screen.getByLabelText('数据来源')).toBeDisabled()
    expect(screen.getByLabelText('开始时间（UTC）')).toBeDisabled()
    expect(screen.getByRole('button', { name: '生成预览' })).toBeDisabled()

    fillRequiredWorkflow()
    expect(screen.getByRole('button', { name: '生成预览' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    await screen.findByText('预览有效至 2026-08-16T02:05:00Z')

    fireEvent.change(screen.getByLabelText('证据类型'), { target: { value: 'monitoring.probe/2' } })
    expect(screen.getByLabelText('数据来源')).toHaveValue('')
    expect(screen.getByLabelText('开始时间（UTC）')).toHaveValue('')
    expect(screen.queryByText('预览有效至 2026-08-16T02:05:00Z')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认引用' })).toBeDisabled()
  })

  it('sends a UTC allowlisted selection with sensitive fields empty by default', async () => {
    const requestPreview = vi.fn().mockResolvedValue(preview)
    render(
      <EvidenceCapturePicker
        recordId="rec_picker"
        options={captureOptions}
        requestPreview={requestPreview}
        onConfirm={vi.fn()}
        now={() => new Date('2026-08-16T02:01:00Z')}
      />,
    )

    fillRequiredWorkflow()
    expect(screen.getByRole('checkbox', { name: '源私网地址' })).not.toBeChecked()
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))

    await waitFor(() => expect(requestPreview).toHaveBeenCalledWith({
      record_id: 'rec_picker',
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mon_primary',
      requested_window: {
        start: '2026-08-16T01:00:00.000Z',
        end: '2026-08-16T02:00:00.000Z',
      },
      metrics: ['cpu_usage_pct'],
      precision_seconds: 300,
      sensitive_topology_fields: [],
    }, expect.any(AbortSignal)))
  })

  it('includes a sensitive field only after an explicit selection', async () => {
    const requestPreview = vi.fn().mockResolvedValue(preview)
    render(
      <EvidenceCapturePicker
        options={captureOptions}
        requestPreview={requestPreview}
        onConfirm={vi.fn()}
        now={() => new Date('2026-08-16T02:01:00Z')}
      />,
    )

    fillRequiredWorkflow()
    fireEvent.click(screen.getByRole('checkbox', { name: '源私网地址' }))
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))

    await waitFor(() => expect(requestPreview).toHaveBeenCalledWith(expect.objectContaining({
      sensitive_topology_fields: ['source.private_address'],
    }), expect.any(AbortSignal)))
  })

  it('never confirms a stale preview and does not silently replace it', async () => {
    const requestPreview = vi.fn().mockResolvedValue(preview)
    render(
      <EvidenceCapturePicker
        options={captureOptions}
        requestPreview={requestPreview}
        onConfirm={vi.fn()}
        now={() => new Date(preview.valid_until)}
      />,
    )

    fillRequiredWorkflow()
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    await screen.findByText('该预览已过期，请重新生成。')

    expect(screen.getByRole('button', { name: '确认引用' })).toBeDisabled()
    expect(requestPreview).toHaveBeenCalledTimes(1)
  })

  it('expires a preview against the stable default clock without another user event', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-16T02:01:00Z'))
    try {
      render(
        <EvidenceCapturePicker
          options={captureOptions}
          requestPreview={vi.fn().mockResolvedValue(preview)}
          onConfirm={vi.fn()}
        />,
      )

      fillRequiredWorkflow()
      fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
      await act(async () => Promise.resolve())
      expect(screen.getByRole('button', { name: '确认引用' })).toBeEnabled()

      act(() => vi.advanceTimersByTime(4 * 60 * 1000))
      expect(screen.getByText('该预览已过期，请重新生成。')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '确认引用' })).toBeDisabled()
    } finally {
      vi.useRealTimers()
    }
  })

  it.each([
    ['source type', { source: { ...preview.source, type: 'target' } }],
    ['source id', { source: { ...preview.source, id: 'mon_other' } }],
    ['requested start', {
      requested_window: { ...preview.requested_window, start: '2026-08-16T00:55:00Z' },
    }],
    ['requested end', {
      requested_window: { ...preview.requested_window, end: '2026-08-16T02:05:00Z' },
    }],
  ])('fails closed when the preview response changes the selected %s', async (_name, mutation) => {
    const onConfirm = vi.fn()
    render(
      <EvidenceCapturePicker
        recordId="rec_picker"
        options={captureOptions}
        requestPreview={vi.fn().mockResolvedValue({ ...preview, ...mutation })}
        onConfirm={onConfirm}
        now={() => new Date('2026-08-16T02:01:00Z')}
      />,
    )

    fillRequiredWorkflow()
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('预览响应与当前选择不一致。')
    expect(screen.getByRole('button', { name: '确认引用' })).toBeDisabled()
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('aborts an in-flight preview and ignores its late result after an upstream reset', async () => {
    let resolvePreview: ((value: typeof preview) => void) | undefined
    const requestPreview = vi.fn((_input, signal: AbortSignal) => new Promise<typeof preview>((resolve) => {
      resolvePreview = resolve
      expect(signal.aborted).toBe(false)
    }))
    render(
      <EvidenceCapturePicker
        options={captureOptions}
        requestPreview={requestPreview}
        onConfirm={vi.fn()}
        now={() => new Date('2026-08-16T02:01:00Z')}
      />,
    )

    fillRequiredWorkflow()
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    const signal = requestPreview.mock.calls[0]?.[1]
    fireEvent.change(screen.getByLabelText('数据来源'), { target: { value: 'monitoring_instance/mon_backup' } })

    expect(signal?.aborted).toBe(true)
    resolvePreview?.(preview)
    await Promise.resolve()
    expect(screen.queryByLabelText('证据预览')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认引用' })).toBeDisabled()
  })

  it('confirms only the record and capture-intent identifiers in server order', async () => {
    const onConfirm = vi.fn()
    render(
      <EvidenceCapturePicker
        options={captureOptions}
        requestPreview={vi.fn().mockResolvedValue(preview)}
        onConfirm={onConfirm}
        now={() => new Date('2026-08-16T02:01:00Z')}
      />,
    )

    fillRequiredWorkflow()
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    const confirmation = await screen.findByRole('group', { name: '8. 确认' })
    fireEvent.click(within(confirmation).getByRole('button', { name: '确认引用' }))

    expect(onConfirm).toHaveBeenCalledWith({
      record_id: 'rec_picker',
      capture_intent_id: 'eci_picker',
    })
    expect(Object.keys(onConfirm.mock.calls[0]?.[0] ?? {})).toEqual(['record_id', 'capture_intent_id'])
  })
})
