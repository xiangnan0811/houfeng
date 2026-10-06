import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type { EvidenceCapturePreview, EvidenceCapturePreviewInput } from '../../../lib/types'
import { EvidenceCapturePicker, type EvidenceCaptureSubject } from './EvidenceCapturePicker'
import type { OtherEvidenceSourceLoaders } from './useOtherEvidenceSource'

const NOW = new Date('2026-08-16T02:03:00Z')
const now = () => NOW

const subjects: readonly EvidenceCaptureSubject[] = [
  { kind: 'vps', source_id: 'vps_edge', label: '边缘节点' },
  { kind: 'monitoring_instance', source_id: 'mi_primary', label: '主监控实例' },
  { kind: 'monitoring_instance', source_id: 'mi_backup', label: '备用监控实例' },
]

function previewFor(input: EvidenceCapturePreviewInput, overrides: Partial<EvidenceCapturePreview> = {}): EvidenceCapturePreview {
  return {
    record_id: input.record_id ?? 'rec_allocated',
    snapshot_id: 'evs_picker',
    capture_intent_id: 'eci_picker',
    kind: input.kind,
    schema_version: input.schema_version,
    subject: { type: 'vps', id: 'vps_edge', display_name: '边缘节点' },
    source: { type: input.source_type, id: input.source_id, display_name: '' },
    requested_window: input.requested_window,
    actual_window: input.requested_window,
    observed_at: '2026-08-16T02:00:00Z',
    source_revision: 'revision',
    source_watermark: 'watermark',
    producer_version: 'producer-v1',
    calculation_version: 'calculation-v1',
    units: { status: 'not_applicable', values: {} },
    quality: {
      status: 'complete', sample_count: 12, gap_count: 0, maintenance_count: 0, backfilled_count: 0,
      bucket_count: 12, data_point_count: 12, peak_count: 1, truncated: false, partial: false,
    },
    sensitivity: 'normal',
    actual_precision_seconds: 300,
    bucket_width_seconds: 300,
    quota: { status: 'allowed' },
    retention: { immutable: true, scope: 'record_revision', source_deletion: 'snapshot_retained_source_unavailable' },
    redaction: [],
    estimated_canonical_bytes: 4096,
    renderer_version: 'renderer-v1',
    previewed_at: '2026-08-16T02:03:00Z',
    valid_until: '2026-08-16T02:18:00Z',
    ...overrides,
  }
}

function echoPreview(overrides: Partial<EvidenceCapturePreview> = {}) {
  return vi.fn((input: EvidenceCapturePreviewInput) => Promise.resolve(previewFor(input, overrides)))
}

function lastInput(requestPreview: ReturnType<typeof echoPreview>): EvidenceCapturePreviewInput {
  const call = requestPreview.mock.calls.at(-1)
  if (!call) throw new Error('requestPreview was not called')
  return call[0]
}

async function generatePreview() {
  fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
  return screen.findByRole('region', { name: '证据预览' })
}

describe('EvidenceCapturePicker', () => {
  it('预选记录主体与常用指标，按字典序提交已结束的 24 小时窗口', async () => {
    const requestPreview = echoPreview()
    const onConfirm = vi.fn()
    render(<EvidenceCapturePicker recordId="rec_1" subjects={subjects} requestPreview={requestPreview} onConfirm={onConfirm} now={now} />)

    expect(screen.getByLabelText('证据类型')).toHaveValue('monitoring.host')
    expect(screen.getByLabelText('来源')).toHaveValue('monitoring_instance/mi_primary')
    // 没有提供其他来源时不出现“其他 VPS…”。
    expect(within(screen.getByLabelText('来源')).queryByRole('option', { name: '其他 VPS…' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '近 24 小时' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('checkbox', { name: 'CPU 使用率' })).toBeChecked()
    expect(screen.queryByRole('checkbox', { name: 'Swap 使用率' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '更多指标' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Swap 使用率' }))
    fireEvent.change(screen.getByLabelText('来源'), { target: { value: 'monitoring_instance/mi_backup' } })
    const region = await generatePreview()

    expect(lastInput(requestPreview)).toEqual({
      record_id: 'rec_1',
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mi_backup',
      // 02:03 往前至少 5 分钟并对齐 5 分钟：截止 01:55。
      requested_window: { start: '2026-08-15T01:55:00.000Z', end: '2026-08-16T01:55:00.000Z' },
      metrics: ['cpu_usage_pct', 'disk_used_pct', 'load_1', 'mem_used_pct', 'net_in_bytes_per_sec', 'net_out_bytes_per_sec', 'swap_used_pct'],
      precision_seconds: 0,
      sensitive_topology_fields: [],
    })
    expect(within(region).getByText('主机监控 · 备用监控实例')).toBeInTheDocument()
    expect(within(region).getByText('容量充足')).toBeInTheDocument()

    fireEvent.click(within(region).getByRole('button', { name: '加入记录' }))
    expect(onConfirm).toHaveBeenCalledWith(expect.objectContaining({
      record_id: 'rec_1',
      capture_intent_id: 'eci_picker',
      snapshot_id: 'evs_picker',
      kind_label: '主机监控',
      source_label: '备用监控实例',
      valid_until: '2026-08-16T02:18:00Z',
    }))
  })

  it('精度只提供不低于窗口默认精度的选项，且至少选一个指标才能预览', () => {
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={echoPreview()} onConfirm={vi.fn()} now={now} />)

    const options = () => within(screen.getByLabelText('精度')).getAllByRole('option').map((option) => option.textContent)
    expect(options()).toEqual(['自动', '5 分钟', '1 小时', '1 天'])
    fireEvent.click(screen.getByRole('button', { name: '近 1 小时' }))
    expect(options()).toEqual(['自动', '1 分钟', '5 分钟', '1 小时', '1 天'])
    fireEvent.click(screen.getByRole('button', { name: '近 7 天' }))
    expect(options()).toEqual(['自动', '1 小时', '1 天'])

    for (const name of ['CPU 使用率', '内存使用率', '磁盘使用率', '入站流量', '出站流量', '1 分钟负载']) {
      fireEvent.click(screen.getByRole('checkbox', { name }))
    }
    expect(screen.getByRole('button', { name: '生成预览' })).toBeDisabled()
  })

  it('窗口变长后不再允许的细精度回到“自动”，不提交服务端会拒绝的值', async () => {
    const requestPreview = echoPreview()
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={requestPreview} onConfirm={vi.fn()} now={now} />)
    fireEvent.click(screen.getByRole('button', { name: '近 1 小时' }))
    fireEvent.change(screen.getByLabelText('精度'), { target: { value: '60' } })
    expect(screen.getByLabelText('精度')).toHaveDisplayValue('1 分钟')

    fireEvent.click(screen.getByRole('button', { name: '近 7 天' }))
    expect(screen.getByLabelText('精度')).toHaveDisplayValue('自动')
    await generatePreview()
    expect(lastInput(requestPreview).precision_seconds).toBe(0)
  })

  it('自定义窗口按本地时间解释，开始不早于结束时不能预览', async () => {
    const requestPreview = echoPreview()
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={requestPreview} onConfirm={vi.fn()} now={now} />)

    fireEvent.click(screen.getByRole('button', { name: '自定义' }))
    fireEvent.change(screen.getByLabelText('开始'), { target: { value: '2026-08-15T10:00' } })
    fireEvent.change(screen.getByLabelText('结束'), { target: { value: '2026-08-15T09:00' } })
    expect(screen.getByRole('button', { name: '生成预览' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('结束'), { target: { value: '2026-08-15T12:00' } })
    await generatePreview()
    expect(lastInput(requestPreview).requested_window).toEqual({
      start: new Date('2026-08-15T10:00').toISOString(),
      end: new Date('2026-08-15T12:00').toISOString(),
    })
  })

  it('订阅成本取完整的 UTC 自然月，IP 质量可选敏感字段并排序', async () => {
    const requestPreview = echoPreview()
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={requestPreview} onConfirm={vi.fn()} now={now} />)

    fireEvent.change(screen.getByLabelText('证据类型'), { target: { value: 'subscription.cost' } })
    expect(screen.getByLabelText('来源')).toHaveValue('vps/vps_edge')
    expect(screen.queryByLabelText('精度')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '生成预览' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('账单月份（UTC）'), { target: { value: '2026-12' } })
    await generatePreview()
    expect(lastInput(requestPreview)).toMatchObject({
      kind: 'subscription.cost',
      source_type: 'vps',
      source_id: 'vps_edge',
      requested_window: { start: '2026-12-01T00:00:00.000Z', end: '2027-01-01T00:00:00.000Z' },
      metrics: [],
      precision_seconds: 0,
    })

    fireEvent.change(screen.getByLabelText('证据类型'), { target: { value: 'ip_quality.report' } })
    expect(screen.queryByRole('region', { name: '证据预览' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByText('包含敏感拓扑字段（默认不含）'))
    fireEvent.click(screen.getByRole('checkbox', { name: 'IP 地址' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'ASN' }))
    await generatePreview()
    expect(lastInput(requestPreview).sensitive_topology_fields).toEqual(['asn', 'ip_address'])
  })

  it('选择变化立即作废预览；过期预览不能加入，配额已满也不能加入', async () => {
    let current = NOW
    const onConfirm = vi.fn()
    const { rerender } = render(
      <EvidenceCapturePicker subjects={subjects} requestPreview={echoPreview()} onConfirm={onConfirm} now={() => current} />,
    )
    await generatePreview()
    fireEvent.click(screen.getByRole('checkbox', { name: 'CPU 使用率' }))
    expect(screen.queryByRole('region', { name: '证据预览' })).not.toBeInTheDocument()

    const region = await generatePreview()
    current = new Date('2026-08-16T02:18:00Z')
    rerender(<EvidenceCapturePicker subjects={subjects} requestPreview={echoPreview()} onConfirm={onConfirm} now={() => current} />)
    expect(within(region).getByText('预览已过期，请重新生成')).toBeInTheDocument()
    expect(within(region).getByRole('button', { name: '加入记录' })).toBeDisabled()

    current = NOW
    const exceeded = echoPreview({ quota: { status: 'exceeded', reason: 'project evidence quota exceeded' } })
    rerender(<EvidenceCapturePicker subjects={subjects} requestPreview={exceeded} onConfirm={onConfirm} now={() => current} />)
    const full = await generatePreview()
    expect(within(full).getByText('证据容量已满，无法加入')).toBeInTheDocument()
    expect(within(full).getByRole('button', { name: '加入记录' })).toBeDisabled()
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('配额接近上限仍可加入，容量不可知时不能加入', async () => {
    const onConfirm = vi.fn()
    const { rerender } = render(
      <EvidenceCapturePicker subjects={subjects} onConfirm={onConfirm} now={now}
        requestPreview={echoPreview({ quota: { status: 'warning', reason: 'project evidence quota warning threshold reached' } })} />,
    )
    const warning = await generatePreview()
    expect(within(warning).getByText('证据容量接近上限')).toBeInTheDocument()
    fireEvent.click(within(warning).getByRole('button', { name: '加入记录' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)

    rerender(
      <EvidenceCapturePicker subjects={subjects} onConfirm={onConfirm} now={now}
        requestPreview={echoPreview({ quota: { status: 'unavailable', reason: 'project evidence capacity unavailable' } })} />,
    )
    const unavailable = await generatePreview()
    expect(within(unavailable).getByText('证据容量暂不可知，无法加入')).toBeInTheDocument()
    expect(within(unavailable).getByRole('button', { name: '加入记录' })).toBeDisabled()
  })

  it.each([
    ['配额原因与状态不符', { quota: { status: 'warning', reason: 'disk nearly full on db-1' } }],
    ['缺少配额', { quota: undefined }],
    ['来源与请求不一致', { source: { type: 'monitoring_instance', id: 'mi_other', display_name: '' } }],
    ['请求窗口与请求不一致', { requested_window: { start: '2026-08-15T00:00:00Z', end: '2026-08-16T00:00:00Z' } }],
  ] as const)('拒绝%s的预览，不进入预览区', async (_name, overrides) => {
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={echoPreview(overrides as Partial<EvidenceCapturePreview>)} onConfirm={vi.fn()} now={now} />)
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('预览结果与当前选择不一致，请重新生成')
    expect(screen.queryByRole('region', { name: '证据预览' })).not.toBeInTheDocument()
    expect(screen.queryByText('disk nearly full on db-1')).not.toBeInTheDocument()
  })

  it('记录发布进行中可以预览但不能加入', async () => {
    const onConfirm = vi.fn()
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={echoPreview()} onConfirm={onConfirm} now={now} disabled />)
    const region = await generatePreview()
    expect(within(region).getByRole('button', { name: '加入记录' })).toBeDisabled()
    expect(within(region).getByText('正在发布，完成后再加入证据')).toBeInTheDocument()
  })

  it('拒绝与当前选择不一致或缺少快照 ID 的预览', async () => {
    const mismatched = vi.fn((input: EvidenceCapturePreviewInput) => Promise.resolve(previewFor(input, { record_id: 'rec_other' })))
    const { rerender } = render(<EvidenceCapturePicker recordId="rec_1" subjects={subjects} requestPreview={mismatched} onConfirm={vi.fn()} now={now} />)
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('预览结果与当前选择不一致，请重新生成')

    rerender(<EvidenceCapturePicker recordId="rec_1" subjects={subjects} requestPreview={echoPreview({ snapshot_id: '' })} onConfirm={vi.fn()} now={now} />)
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('预览结果与当前选择不一致，请重新生成'))
    expect(screen.queryByRole('region', { name: '证据预览' })).not.toBeInTheDocument()
  })

  it.each([
    ['evidence_source_empty', '所选窗口内没有数据，请调整时间窗口'],
    ['evidence_window_too_large', '时间窗口超出单份证据上限，请缩短窗口或调粗精度'],
    ['resource_not_found', '来源不可访问或已删除'],
    ['evidence_invalid', '当前选择无效，请检查后重试'],
    ['evidence_kind_unavailable', '该证据类型暂不可用'],
    ['evidence_service_unavailable', '无法生成证据预览，请稍后重试'],
  ])('预览错误 %s 显示可操作的提示', async (code, message) => {
    const requestPreview = vi.fn().mockRejectedValue(new ApiError(422, 'failed', { code }))
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={requestPreview} onConfirm={vi.fn()} now={now} />)
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
  })

  it('选择变化中止进行中的预览请求，迟到的结果不会显示', async () => {
    let resolveFirst: () => void = () => undefined
    const signals: AbortSignal[] = []
    const requestPreview = vi.fn((input: EvidenceCapturePreviewInput, signal: AbortSignal) => {
      signals.push(signal)
      return new Promise<EvidenceCapturePreview>((resolve) => { resolveFirst = () => resolve(previewFor(input)) })
    })
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={requestPreview} onConfirm={vi.fn()} now={now} />)
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    expect(screen.getByRole('button', { name: '正在生成预览…' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('来源'), { target: { value: 'monitoring_instance/mi_backup' } })
    expect(signals[0]?.aborted).toBe(true)
    await act(async () => resolveFirst())
    expect(screen.queryByRole('region', { name: '证据预览' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '生成预览' })).toBeEnabled()
  })

  it('可改选其他 VPS 的监控实例，用于跨主机对比', async () => {
    const otherSources: OtherEvidenceSourceLoaders = {
      listVPS: vi.fn().mockResolvedValue([
        { vps_id: 'vps_edge', display_name: '边缘节点' },
        { vps_id: 'vps_peer', display_name: '对照节点' },
      ]),
      listMonitoringInstances: vi.fn().mockResolvedValue([{ monitoring_instance_id: 'mi_peer', display_name: '对照监控' }]),
    }
    const requestPreview = echoPreview()
    const onConfirm = vi.fn()
    render(<EvidenceCapturePicker subjects={subjects} otherSources={otherSources} requestPreview={requestPreview} onConfirm={onConfirm} now={now} />)

    expect(otherSources.listVPS).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('来源'), { target: { value: 'other' } })
    expect(screen.getByRole('button', { name: '生成预览' })).toBeDisabled()
    await screen.findByRole('option', { name: '对照节点' })
    fireEvent.change(screen.getByLabelText('VPS'), { target: { value: 'vps_peer' } })
    await screen.findByRole('option', { name: '对照监控' })
    expect(otherSources.listMonitoringInstances).toHaveBeenCalledWith('vps_peer')
    fireEvent.change(screen.getByLabelText('监控实例'), { target: { value: 'mi_peer' } })

    const region = await generatePreview()
    expect(lastInput(requestPreview)).toMatchObject({ source_type: 'monitoring_instance', source_id: 'mi_peer' })
    fireEvent.click(within(region).getByRole('button', { name: '加入记录' }))
    expect(onConfirm).toHaveBeenCalledWith(expect.objectContaining({ source_label: '对照节点 · 对照监控' }))
  })

  it('主体只有 VPS 时，先列出该 VPS 名下的监控实例作为来源', async () => {
    let resolveInstances: () => void = () => undefined
    const otherSources: OtherEvidenceSourceLoaders = {
      listVPS: vi.fn().mockResolvedValue([]),
      listMonitoringInstances: vi.fn(() => new Promise<readonly { monitoring_instance_id: string; display_name: string }[]>((resolve) => {
        resolveInstances = () => resolve([{ monitoring_instance_id: 'mi_edge', display_name: '主监控' }])
      })),
    }
    const requestPreview = echoPreview()
    render(<EvidenceCapturePicker subjects={[subjects[0]!]} otherSources={otherSources} requestPreview={requestPreview} onConfirm={vi.fn()} now={now} />)

    // 读取中不跳到“其他 VPS”，也不能预览。
    expect(screen.getByLabelText('证据类型')).toHaveValue('monitoring.host')
    expect(screen.getByLabelText('来源')).toHaveDisplayValue('正在读取…')
    expect(screen.getByRole('button', { name: '生成预览' })).toBeDisabled()
    await waitFor(() => expect(otherSources.listMonitoringInstances).toHaveBeenCalledWith('vps_edge'))
    await act(async () => resolveInstances())

    expect(screen.getByLabelText('来源')).toHaveDisplayValue('边缘节点 · 主监控')
    expect(otherSources.listVPS).not.toHaveBeenCalled()
    await generatePreview()
    expect(lastInput(requestPreview)).toMatchObject({ source_type: 'monitoring_instance', source_id: 'mi_edge' })
  })

  it('加入记录后清掉预览，同一预览不能重复加入', async () => {
    const onConfirm = vi.fn()
    render(<EvidenceCapturePicker subjects={subjects} requestPreview={echoPreview()} onConfirm={onConfirm} now={now} />)
    const region = await generatePreview()
    fireEvent.click(within(region).getByRole('button', { name: '加入记录' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('region', { name: '证据预览' })).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('已加入“主机监控 · 主监控实例”，发布后保存')
  })

  it('没有匹配主体时直接进入其他 VPS，VPS 级证据不需要选监控实例', async () => {
    const otherSources: OtherEvidenceSourceLoaders = {
      listVPS: vi.fn().mockResolvedValue([{ vps_id: 'vps_peer', display_name: '对照节点' }]),
      listMonitoringInstances: vi.fn(),
    }
    const requestPreview = echoPreview()
    render(<EvidenceCapturePicker subjects={[]} otherSources={otherSources} requestPreview={requestPreview} onConfirm={vi.fn()} now={now} />)

    fireEvent.change(screen.getByLabelText('证据类型'), { target: { value: 'ip_quality.report' } })
    expect(screen.getByLabelText('来源')).toHaveValue('other')
    expect(screen.queryByLabelText('监控实例')).not.toBeInTheDocument()
    await screen.findByRole('option', { name: '对照节点' })
    fireEvent.change(screen.getByLabelText('VPS'), { target: { value: 'vps_peer' } })
    await generatePreview()
    expect(lastInput(requestPreview)).toMatchObject({ kind: 'ip_quality.report', source_type: 'vps', source_id: 'vps_peer' })
    expect(otherSources.listMonitoringInstances).not.toHaveBeenCalled()
  })

  it('其他 VPS 列表读取失败时提示，且不会预览', async () => {
    const otherSources: OtherEvidenceSourceLoaders = {
      listVPS: vi.fn().mockRejectedValue(new Error('offline')),
      listMonitoringInstances: vi.fn(),
    }
    render(<EvidenceCapturePicker subjects={subjects} otherSources={otherSources} requestPreview={echoPreview()} onConfirm={vi.fn()} now={now} />)
    fireEvent.change(screen.getByLabelText('来源'), { target: { value: 'other' } })
    expect(await screen.findByRole('alert')).toHaveTextContent('列表读取失败，请稍后重试')
    expect(screen.getByRole('button', { name: '生成预览' })).toBeDisabled()
  })

  it('没有可采集的主体且不能选其他来源时给出指引', () => {
    render(<EvidenceCapturePicker subjects={[]} requestPreview={echoPreview()} onConfirm={vi.fn()} now={now} />)
    expect(screen.getByText(/记录还没有可采集证据的主体/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '生成预览' })).not.toBeInTheDocument()
  })
})
