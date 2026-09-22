import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import type { IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import { IPQualityDashboard } from './IPQualityDashboard'

const summary: IPQualitySummary = {
  report_id: 'ipq_001',
  vps_id: 'vps_001',
  observed_at: '2026-06-08T12:00:00Z',
  ip_address: '192.0.2.1',
  ip_version: 4,
  status: 'success',
  risk_level: 'high',
  use_region_code: 'JP',
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

function report(overrides: Partial<VPSIPQualityReport> = {}): VPSIPQualityReport {
  return {
    summary,
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
      is_backfilled: false,
      created_at: '2026-06-08T12:00:06Z',
      ...(summary.coverage != null ? { coverage: summary.coverage } : {}),
      diagnostics_json: { source_version: 'v2' },
    },
    provider_results: [
      {
        provider: 'ipinfo',
        status: 'success',
        source_type: 'default',
        latency_ms: 73,
        is_proxy: false,
        is_vpn: true,
        extra_json: { privacy: { vpn: true } },
      },
    ],
    service_unlocks: [
      { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', region: 'JP' },
    ],
    history: [summary],
    ...overrides,
  }
}

function renderDashboard(body: VPSIPQualityReport, initialEntry = '/vps/vps_001/ip-quality') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route
          path="/vps/:vpsId/ip-quality"
          element={<IPQualityDashboard report={body} summary={body.summary!} detailPath="/vps/vps_001" />}
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('IPQualityDashboard', () => {
  it('keeps every history report reachable instead of silently dropping later rows', () => {
    const history = Array.from({ length: 7 }, (_, index) => ({
      ...summary,
      report_id: `ipq_00${index}`,
      observed_at: `2026-06-${String(8 - index).padStart(2, '0')}T12:00:00Z`,
      risk_level: index === 0 ? 'high' : 'medium',
    }))

    renderDashboard(report({ history }))

    const historyLinks = screen.getAllByRole('link', { name: '查看详情' })
    expect(historyLinks).toHaveLength(7)
    expect(historyLinks.map((link) => link.getAttribute('href'))).toEqual([
      '/vps/vps_001/ip-quality?report_id=ipq_000',
      '/vps/vps_001/ip-quality?report_id=ipq_001',
      '/vps/vps_001/ip-quality?report_id=ipq_002',
      '/vps/vps_001/ip-quality?report_id=ipq_003',
      '/vps/vps_001/ip-quality?report_id=ipq_004',
      '/vps/vps_001/ip-quality?report_id=ipq_005',
      '/vps/vps_001/ip-quality?report_id=ipq_006',
    ])
  })

  it('renders identity as a compact definition list and keeps assignment_mode out of ordinary copy', () => {
    renderDashboard(report())

    const identity = screen.getByLabelText('报告身份')
    expect(identity.tagName).toBe('DL')
    expect(within(identity).queryByText('link')).not.toBeInTheDocument()
    expect(screen.getByText('最新报告')).toBeInTheDocument()
    expect(screen.getByText('最新报告')).not.toHaveTextContent('192.0.2.1')
    expect(within(screen.getByLabelText('采集诊断')).getByText('监控关联')).toBeInTheDocument()
    expect(screen.getAllByText('风险信号').length).toBeGreaterThan(0)
  })
  it('exposes a named focusable scroll region for the provider table', () => {
    renderDashboard(report())

    const heading = screen.getByRole('heading', { name: '各 IP 数据库判断' })
    const region = screen.getByRole('region', { name: '各 IP 数据库判断' })
    expect(heading.closest('section')).not.toHaveClass('page-panel--scroll-x')
    expect(region).toHaveAttribute('tabindex', '0')
    expect(region).toHaveAttribute('aria-labelledby', heading.id)
    expect(region).toHaveAttribute('aria-describedby', 'ip-quality-provider-table-hint')
  })

  it('keeps duplicate service rows distinct by service and source', () => {
    renderDashboard(report({
      service_unlocks: [
        { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', region: 'JP' },
        { service: 'chatgpt', source: 'backup_probe', status: 'blocked', region: 'US' },
      ],
    }))

    expect(screen.getAllByRole('heading', { name: 'ChatGPT' })).toHaveLength(2)
    expect(screen.getByText('解锁 · JP')).toBeInTheDocument()
    expect(screen.getByText('受阻 · US')).toBeInTheDocument()
  })

  it('keeps source, latency and extra JSON in collapsed diagnostics', () => {
    renderDashboard(report())

    const providerPanel = screen.getByRole('heading', { name: '各 IP 数据库判断' }).closest('section') as HTMLElement
    const diagnostics = screen.getByLabelText('采集诊断')
    expect(screen.getByRole('heading', { name: '诊断与异常' }).closest('details')).not.toHaveAttribute('open')
    expect(within(providerPanel).queryByText(/openai_status_probe/)).not.toBeInTheDocument()
    expect(within(providerPanel).queryByText('73 ms')).not.toBeInTheDocument()
    expect(within(diagnostics).getByText(/openai_status_probe/)).toBeInTheDocument()
    expect(within(diagnostics).getByText(/73 ms/)).toBeInTheDocument()
    expect(within(diagnostics).getByText(/"vpn":true/)).toBeInTheDocument()
  })

  it('does not treat unknown service rows as blocked', () => {
    renderDashboard(report({
      summary: { ...summary, status: 'partial' },
      service_unlocks: [
        { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'failure' },
        { service: 'disney-plus', source: 'disney_default_probe', status: 'unknown', probe_status: 'skipped' },
      ],
    }))

    const stats = screen.getByLabelText('服务解锁状态统计')
    expect(stats).toHaveTextContent('0 受阻')
    expect(stats).toHaveTextContent('2 未知')
    expect(screen.queryByRole('button', { name: /保存|编辑|删除/ })).not.toBeInTheDocument()
  })
})
