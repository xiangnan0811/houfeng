import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import * as recordsApi from '../../../lib/recordsApi'
import type { EvidenceSnapshotRead } from '../../../lib/types'
import { EvidenceSnapshotPage } from './EvidenceSnapshotPage'

const quality = {
  status: 'complete' as const,
  partial: false,
  truncated: false,
  sample_count: 1,
  maintenance_count: 0,
  backfilled_count: 0,
  bucket_count: 1,
  gap_count: 0,
  peak_count: 0,
  data_point_count: 3,
}

function ipQualitySnapshot(overrides: Partial<EvidenceSnapshotRead> = {}): EvidenceSnapshotRead {
  return {
    record_id: 'rec_renderer',
    snapshot_id: 'evs_9',
    kind: 'ip_quality.report',
    schema_version: 1,
    subject: { type: 'vps', id: 'vps_renderer', display_name: '边缘节点' },
    source: { type: 'ip_quality_report', id: 'ipq_renderer', display_name: '质量报告' },
    requested_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
    actual_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
    observed_at: '2026-08-16T02:00:00Z',
    captured_at: '2026-08-16T02:00:01Z',
    referenced_at: '2026-08-16T02:01:00Z',
    source_revision: 'source-revision',
    source_watermark: 'source-watermark',
    producer_version: 'producer-v1',
    calculation_version: 'calculation-v1',
    units: { status: 'not_applicable', values: {}, reason: 'not applicable' },
    quality,
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
    source_available: true,
    renderer_version: 'ip_quality_report_v1',
    title: 'IP quality report',
    read_model: {
      version: 'ip_quality_report_read_model/v1',
      report_id: 'ipq_safe_report',
      observed_at: '2026-08-16T01:59:00Z',
      received_at: '2026-08-16T01:59:01Z',
      ip_version: 4,
      status: 'success',
      stale: false,
      stale_after_seconds: 604800,
      risk_level: 'low',
      coverage: {
        expected_provider_count: 1,
        successful_provider_count: 1,
        failed_provider_count: 0,
        skipped_provider_count: 0,
        not_configured_provider_count: 0,
        expected_service_count: 1,
        successful_service_count: 1,
        failed_service_count: 0,
        skipped_service_count: 0,
        not_configured_service_count: 0,
      },
      providers: [{
        provider: 'provider-safe',
        status: 'success',
        source_type: 'default',
        latency_ms: 25,
        usage_type: 'isp',
        company_type: 'hosting',
        risk_level: 'low',
        risk_score: '1',
        is_proxy: false,
        is_tor: false,
        is_vpn: false,
        is_server: false,
        is_abuser: false,
        is_robot: false,
        error_code: '',
      }],
      services: [{
        service: 'service-safe',
        source: 'default',
        status: 'unlocked',
        probe_status: 'success',
        latency_ms: 31,
        unlock_type: 'full',
        error_code: '',
      }],
      quality: { ...quality, sample_count: 1, bucket_count: 1, data_point_count: 3 },
    },
    ...overrides,
  }
}

function renderPage(path = '/evidence/evs_9') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/evidence/:evidenceId" element={<EvidenceSnapshotPage />} />
        <Route path="/records/:recordId" element={<p>record home</p>} />
        <Route path="/vps/:vpsId/evidence" element={<p>subject evidence</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('EvidenceSnapshotPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads a registered snapshot without dumping payload or secrets', async () => {
    const snapshot = ipQualitySnapshot()
    const getSnapshot = vi.spyOn(recordsApi, 'getEvidenceSnapshot').mockResolvedValue({
      ...snapshot,
      read_model: {
        ...(typeof snapshot.read_model === 'object' && snapshot.read_model !== null ? snapshot.read_model : {}),
        payload: 'payload-secret',
        stdout: 'stdout-secret',
        token: 'token-secret',
      },
    })

    renderPage()

    expect(screen.getByText('正在加载证据快照')).toBeInTheDocument()
    expect(await screen.findByText('IP 质量报告')).toBeInTheDocument()
    expect(getSnapshot).toHaveBeenCalledWith('evs_9', expect.any(AbortSignal))
    expect(screen.getByText('观测时间')).toBeInTheDocument()
    expect(screen.getByText('捕获时间')).toBeInTheDocument()
    expect(screen.getByText('引用时间')).toBeInTheDocument()
    expect(screen.getAllByText(/完整/).length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: '打开记录' })).toHaveAttribute('href', '/records/rec_renderer')
    expect(screen.getByRole('link', { name: '返回主体证据' })).toHaveAttribute(
      'href',
      '/vps/vps_renderer/evidence',
    )
    expect(screen.queryByText('payload-secret')).not.toBeInTheDocument()
    expect(screen.queryByText('stdout-secret')).not.toBeInTheDocument()
    expect(screen.queryByText('token-secret')).not.toBeInTheDocument()
    expect(screen.queryByText('实时')).not.toBeInTheDocument()
  })

  it('shows not-found when the snapshot is missing', async () => {
    vi.spyOn(recordsApi, 'getEvidenceSnapshot').mockRejectedValue(
      new ApiError(404, 'resource not found', { code: 'resource_not_found' }),
    )

    renderPage()

    expect(await screen.findByText('未找到证据')).toBeInTheDocument()
    expect(screen.queryByText('IP 质量报告')).not.toBeInTheDocument()
  })

  it('retries a recoverable load error', async () => {
    const getSnapshot = vi.spyOn(recordsApi, 'getEvidenceSnapshot')
      .mockRejectedValueOnce(new ApiError(500, 'internal server error', { code: 'internal_error' }))
      .mockResolvedValueOnce(ipQualitySnapshot())

    renderPage()

    expect(await screen.findByText('无法加载证据')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('IP 质量报告')).toBeInTheDocument()
    expect(getSnapshot).toHaveBeenCalledTimes(2)
  })

  it('names source-unavailable snapshots as retained, not live', async () => {
    vi.spyOn(recordsApi, 'getEvidenceSnapshot').mockResolvedValue(ipQualitySnapshot({
      source_available: false,
      quality: { ...quality, backfilled_count: 2 },
    }))

    renderPage()

    expect(await screen.findByText('来源已不可用。以下为快照保留内容，不是实时数据。')).toBeInTheDocument()
    expect(screen.getByText('质量统计含回填样本，不能当作实时观测。')).toBeInTheDocument()
    expect(screen.getByText('IP 质量报告')).toBeInTheDocument()
    expect(screen.getByText('来源不可用')).toBeInTheDocument()
  })

  it('fails closed for an unknown renderer tuple', async () => {
    const registered = ipQualitySnapshot()
    const unsupportedResponse: unknown = {
      ...registered,
      renderer_version: 'unregistered_v1',
      read_model: { version: 'unregistered_read_model/v1', payload: 'unsupported-payload' },
    }
    vi.spyOn(recordsApi, 'getEvidenceSnapshot').mockImplementation(() => {
      if (typeof unsupportedResponse !== 'object' || unsupportedResponse === null) {
        return Promise.reject(new Error('unsupported fixture missing'))
      }
      const envelope = unsupportedResponse
      const rendererVersion = 'renderer_version' in envelope && typeof envelope.renderer_version === 'string'
        ? envelope.renderer_version
        : registered.renderer_version
      const readModel = 'read_model' in envelope ? envelope.read_model : registered.read_model
      return Promise.resolve({
        ...registered,
        renderer_version: rendererVersion,
        read_model: readModel,
      })
    })

    renderPage()

    expect(await screen.findByText('不支持的证据类型')).toBeInTheDocument()
    expect(screen.queryByText('unsupported-payload')).not.toBeInTheDocument()
    expect(screen.queryByText('IP 质量报告')).not.toBeInTheDocument()
  })
})
