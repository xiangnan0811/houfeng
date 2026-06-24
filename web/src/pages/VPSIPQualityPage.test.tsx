import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSIPQualityPage } from './VPSIPQualityPage'

const ipQualitySummaryBody = {
  report_id: 'ipq_001',
  vps_id: 'vps_001',
  observed_at: '2026-06-08T12:00:00Z',
  ip_address: '192.0.2.1',
  ip_version: 4,
  status: 'success',
  risk_level: 'high',
  use_region_code: 'JP',
  use_region_name: 'Japan',
  asn: 'AS64500',
  organization: 'Example Transit',
  stale: false,
  ambiguous: false,
  assignment_mode: 'link',
  provider_count: 2,
  unlockable_count: 3,
  coverage: {
    expected_provider_count: 4,
    successful_provider_count: 2,
    failed_provider_count: 1,
    skipped_provider_count: 0,
    not_configured_provider_count: 1,
    expected_service_count: 4,
    successful_service_count: 2,
    failed_service_count: 0,
    skipped_service_count: 1,
    not_configured_service_count: 1,
  },
}

const ipQualityReportBody = {
  summary: ipQualitySummaryBody,
  latest_report: {
    report_id: 'ipq_001',
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-06-08T12:00:00Z',
    received_at: '2026-06-08T12:00:05Z',
    agent_version: 'dev',
    fingerprint: 'fp-001',
    sync_batch_id: 'sync_001',
    ip_address: '192.0.2.1',
    ip_version: 4,
    status: 'success',
    asn: 'AS64500',
    organization: 'Example Transit',
    latitude: 35.68,
    longitude: 139.76,
    use_region_code: 'JP',
    use_region_name: 'Japan',
    registered_region_code: 'US',
    registered_region_name: 'United States',
    risk_level: 'high',
    is_backfilled: false,
    created_at: '2026-06-08T12:00:06Z',
    coverage: ipQualitySummaryBody.coverage,
    diagnostics_json: { source_version: 'v2', ip_candidates: { 'ipapi.is': '192.0.2.1' } },
  },
  provider_results: [
    {
      provider: 'ipinfo',
      status: 'success',
      source_type: 'default',
      latency_ms: 73,
      usage_type: 'hosting',
      company_type: 'business',
      risk_level: 'high',
      risk_score: '80',
      region_code: 'JP',
      region_name: 'Japan',
      is_proxy: false,
      is_tor: false,
      is_vpn: true,
      is_server: true,
      is_abuser: false,
      is_robot: false,
      extra_json: { privacy: { vpn: true } },
    },
    {
      provider: 'fraud-check',
      status: 'failure',
      source_type: 'default',
      latency_ms: 1500,
      usage_type: 'business',
      company_type: 'hosting',
      risk_level: 'medium',
      risk_score: '52',
      region_code: 'US',
      region_name: 'United States',
      is_proxy: true,
      is_tor: false,
      is_vpn: false,
      is_server: true,
      is_abuser: true,
      is_robot: false,
      error_code: 'http_status',
      error_summary: 'http status 429',
      extra_json: { rate_limit: true },
    },
    {
      provider: 'clean-db',
      status: 'success',
      source_type: 'default',
      latency_ms: 64,
      usage_type: 'residential',
      company_type: 'isp',
      risk_level: 'low',
      risk_score: '8',
      region_code: 'JP',
      region_name: 'Japan',
      is_proxy: false,
      is_tor: false,
      is_vpn: false,
      is_server: false,
      is_abuser: false,
      is_robot: false,
    },
    {
      provider: 'score-only-db',
      status: 'success',
      source_type: 'default',
      latency_ms: 72,
      risk_score: '31',
      region_code: 'JP',
      is_proxy: false,
      is_tor: false,
      is_vpn: false,
      is_server: false,
      is_abuser: false,
      is_robot: false,
    },
    {
      provider: 'maxmind',
      status: 'not_configured',
      source_type: 'optional',
      error_code: 'not_configured',
      error_summary: 'optional IP quality source requires configuration',
    },
  ],
  service_unlocks: [
    { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', probe_status: 'success', latency_ms: 211, region: 'JP', unlock_type: 'native', extra_json: { cf_country: 'JP' } },
    { service: 'netflix', source: 'netflix_title_probe', status: 'partial', probe_status: 'success', latency_ms: 320, region: 'US', unlock_type: 'originals' },
    { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unknown', probe_status: 'skipped', unlock_type: 'default_probe', error_code: 'unsupported_default_probe' },
    {
      service: 'disney-plus',
      source: 'disney_default_probe',
      status: 'unknown',
      probe_status: 'skipped',
      region: 'US',
      error_code: 'unsupported_default_probe',
      error_summary: 'safe default probe is not available without optional service configuration',
    },
    { service: 'ipqs', source: 'optional_service_probe', status: 'unknown', probe_status: 'not_configured', error_code: 'not_configured' },
  ],
  history: [
    ipQualitySummaryBody,
    { ...ipQualitySummaryBody, report_id: 'ipq_000', observed_at: '2026-06-07T12:00:00Z', risk_level: 'medium' },
  ],
}

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function renderPage(initialEntry = '/vps/vps_001/ip-quality') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/vps/:vpsId/ip-quality" element={<VPSIPQualityPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('VPSIPQualityPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the full IP quality cockpit from the VPS report', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse(ipQualityReportBody))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: 'IP 质量驾驶舱' })).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/ip-quality', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(screen.getByRole('link', { name: '返回 VPS 详情' })).toHaveAttribute('href', '/vps/vps_001')
    const headerActions = screen.getByRole('link', { name: '返回 VPS 详情' }).closest('.section-heading__actions')
    expect(headerActions).not.toBeNull()
    expect(within(headerActions as HTMLElement).queryByText('高风险')).not.toBeInTheDocument()
    expect(screen.getAllByText('风险信号').length).toBeGreaterThan(0)
    expect(screen.getByText('解锁可用')).toBeInTheDocument()
    expect(screen.getAllByText('数据库一致性').length).toBeGreaterThan(0)
    expect(screen.getByText('采集完整性')).toBeInTheDocument()
    expect(screen.queryByText(/完整展示低频 IP 质量报告/)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '风险信号矩阵' })).toBeInTheDocument()
    expect(screen.queryByText(/Server \/ Datacenter 本身只作为上下文/)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '各 IP 数据库判断' })).toBeInTheDocument()
    expect(screen.queryByText(/逐 provider 展示/)).not.toBeInTheDocument()
    expect(screen.getByText('ipinfo')).toBeInTheDocument()
    expect(screen.getByText('fraud-check')).toBeInTheDocument()
    expect(screen.queryByRole('cell', { name: 'maxmind' })).not.toBeInTheDocument()
    const sourceGaps = screen.getByLabelText('未配置 IP 数据库来源')
    expect(within(sourceGaps).getByText('未配置来源：')).toBeInTheDocument()
    expect(within(sourceGaps).getByText('maxmind')).toBeInTheDocument()
    expect(screen.getAllByText('查看').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Server').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Proxy').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Abuse').length).toBeGreaterThan(0)
    expect(screen.queryByText('无用户证据')).not.toBeInTheDocument()
    expect(screen.getByText('采集失败')).toBeInTheDocument()
    expect(screen.getAllByText('未发现风险信号').length).toBeGreaterThan(0)
    expect(screen.getByText('52')).toBeInTheDocument()
    expect(screen.getByText('31')).toBeInTheDocument()
    expect(screen.getByText('风险分')).toBeInTheDocument()
    expect(screen.queryByText(/optional IP quality source requires configuration/)).not.toBeInTheDocument()
    expect(screen.queryByText(/http status 429/)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '服务解锁矩阵' })).toBeInTheDocument()
    expect(screen.getByLabelText('服务解锁状态统计')).toHaveClass('vps-ip-quality-dashboard__service-stats')
    expect(screen.getByText('ChatGPT')).toBeInTheDocument()
    expect(screen.getByText('Netflix')).toBeInTheDocument()
    expect(screen.getByText('Disney+')).toBeInTheDocument()
    expect(screen.getByText('YouTube Premium')).toBeInTheDocument()
    expect(screen.getAllByText('默认探测暂不支持该服务').length).toBeGreaterThan(0)
    expect(screen.getByText('需要可选配置后才能检测')).toBeInTheDocument()
    expect(screen.queryByText(/safe default probe is not available/)).not.toBeInTheDocument()
    expect(screen.queryByText('disney_default_probe · —')).not.toBeInTheDocument()
    expect(screen.queryByText(/disney_default_probe/)).not.toBeInTheDocument()
    expect(screen.queryByText(/default_probe/)).not.toBeInTheDocument()
    expect(screen.queryByText(/openai_status_probe/)).not.toBeInTheDocument()
    expect(screen.queryByText('跳过')).not.toBeInTheDocument()
    expect(screen.queryByText('not_configured')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '证据上下文与采集完整性' })).toBeInTheDocument()
    expect(screen.getByText('AS64500')).toBeInTheDocument()
    expect(screen.getByText('Example Transit')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '质量变化历史' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: '查看详情' })[0]).toHaveAttribute('href', '/vps/vps_001/ip-quality?report_id=ipq_001')
    expect(screen.getByRole('heading', { name: '诊断与异常' })).toBeInTheDocument()
    expect(screen.getByText(/source_version/)).toBeInTheDocument()
  })

  it('loads a historical report detail when report_id is present', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse({
      ...ipQualityReportBody,
      latest_report: { ...ipQualityReportBody.latest_report, report_id: 'ipq_000' },
      history: [],
    }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?report_id=ipq_000')

    await waitFor(() => expect(screen.getByRole('heading', { name: 'IP 质量驾驶舱' })).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/ip-quality/reports/ipq_000', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(screen.getByText('ipq_000')).toBeInTheDocument()
  })

  it('renders raw JSON details as text rather than HTML', async () => {
    const maliciousJSON = { html: '<img src=x onerror=alert(1)>', script: '<script>alert(1)</script>' }
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse({
      ...ipQualityReportBody,
      provider_results: [
        {
          ...ipQualityReportBody.provider_results[0],
          extra_json: maliciousJSON,
        },
      ],
      service_unlocks: [],
      history: [],
    }))
    vi.stubGlobal('fetch', fetchMock)

    const { container } = renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: 'IP 质量驾驶舱' })).toBeInTheDocument())
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText(/<img src=x onerror=alert\(1\)>/)).toBeInTheDocument()
    expect(screen.getByText(/<script>alert\(1\)<\/script>/)).toBeInTheDocument()
  })

  it('shows an empty state when there is no user-visible summary', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse({
      summary: null,
      latest_report: {
        ...ipQualityReportBody.latest_report,
        ip_address: '0.0.0.0',
        status: 'failure',
        error_code: 'lookup_failed',
        error_summary: 'non_json_response',
      },
      provider_results: [],
      service_unlocks: [],
      history: [],
    }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '尚无可展示的 IP 质量事实' })).toBeInTheDocument())
    expect(screen.queryByText('0.0.0.0')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回 VPS 详情' })).toHaveAttribute('href', '/vps/vps_001')
  })

  it('keeps matrix sections readable when provider, service, and history rows are empty', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse({
      summary: {
        ...ipQualitySummaryBody,
        provider_count: 0,
        unlockable_count: 0,
        coverage: null,
      },
      latest_report: { ...ipQualityReportBody.latest_report, coverage: null },
      provider_results: [],
      service_unlocks: [],
      history: [],
    }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: 'IP 质量驾驶舱' })).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '风险信号矩阵' })).toBeInTheDocument()
    expect(screen.getByText('暂无 provider 结果。')).toBeInTheDocument()
    expect(screen.getByText('暂无服务解锁结果。')).toBeInTheDocument()
    expect(screen.getByText('暂无历史变化。')).toBeInTheDocument()
    expect(screen.getAllByText('未采集').length).toBeGreaterThan(0)
  })
})
