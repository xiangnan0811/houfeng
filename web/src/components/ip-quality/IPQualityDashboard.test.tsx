import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { IPQualityCollectStatus, IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import { IPQualityDashboard } from './IPQualityDashboard'
import type { IPQualityCollectController } from './useIPQualityCollect'

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
      agent_version: 'v1.1.0',
      fingerprint: 'fp-001',
      sync_batch_id: 'sync_001',
      ip_address: '192.0.2.1',
      ip_version: 4,
      status: 'success',
      registered_region_code: 'US',
      is_backfilled: false,
      created_at: '2026-06-08T12:00:06Z',
      ...(summary.coverage != null ? { coverage: summary.coverage } : {}),
      diagnostics_json: { source_version: 'v2' },
      raw_json: { services: { reddit: { raw: { body_sample: '<!DOCTYPE html><script>alert(1)</script>' } } } },
    },
    provider_results: [
      { provider: 'clean-db', status: 'success', source_type: 'default', latency_ms: 64, usage_type: 'isp', is_proxy: false, is_vpn: false, is_tor: false, is_abuser: false, is_robot: false },
      {
        provider: 'ipinfo',
        status: 'success',
        source_type: 'default',
        latency_ms: 73,
        risk_level: 'high',
        is_proxy: false,
        is_vpn: true,
        is_server: true,
        extra_json: { privacy: { vpn: true } },
      },
      { provider: 'geo-only', status: 'success', source_type: 'default', latency_ms: 20 },
      { provider: 'fraud-check', status: 'failure', source_type: 'default', latency_ms: 1500, is_proxy: true, error_code: 'http_status', error_summary: 'http status 429' },
      { provider: 'maxmind', status: 'not_configured', source_type: 'optional', error_code: 'not_configured' },
    ],
    service_unlocks: [
      { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', probe_status: 'success', region: 'JP', latency_ms: 211 },
      { service: 'netflix', source: 'netflix_title_probe', status: 'blocked', probe_status: 'success' },
      { service: 'disney-plus', source: 'disney_default_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' },
    ],
    history: [summary],
    ...overrides,
  }
}

function controller(overrides: Partial<IPQualityCollectController> = {}, status: IPQualityCollectStatus | null = { enabled: true, available: true }): IPQualityCollectController {
  return {
    status,
    watchedRequestId: null,
    submitting: false,
    error: null,
    active: false,
    start: vi.fn(),
    ...overrides,
  }
}

function renderDashboard(
  body: VPSIPQualityReport,
  { collect = controller(), initialEntry = '/vps/vps_001/ip-quality' }: { collect?: IPQualityCollectController, initialEntry?: string } = {},
) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route
          path="/vps/:vpsId/ip-quality"
          element={<IPQualityDashboard report={body} summary={body.summary!} detailPath="/vps/vps_001" collect={collect} />}
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('IPQualityDashboard', () => {
  it('summarises identity in the header instead of a separate field grid', () => {
    renderDashboard(report())

    expect(screen.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeInTheDocument()
    const identity = screen.getByLabelText('报告身份')
    expect(identity.tagName).toBe('DL')
    expect(identity).toHaveTextContent('192.0.2.1 · IPv4')
    expect(identity).toHaveTextContent('AS64500 Example Transit')
    expect(identity).toHaveTextContent('注册地US')
    expect(within(identity).queryByText('link')).not.toBeInTheDocument()
    expect(within(identity).queryByText('v1.1.0')).not.toBeInTheDocument()
    expect(within(identity).queryByText('ipq_001')).not.toBeInTheDocument()
    expect(screen.queryByText(/本身不构成负面风险/)).not.toBeInTheDocument()
    expect(screen.queryByText(/仅用于排障/)).not.toBeInTheDocument()
  })

  it('shows the verdict with score, risk signals and service counts', () => {
    renderDashboard(report())

    const verdict = screen.getByLabelText('质量结论')
    expect(within(verdict).getByText('质量分')).toBeInTheDocument()
    expect(within(verdict).getByText('高风险')).toBeInTheDocument()
    const metrics = screen.getByLabelText('IP 质量摘要指标')
    expect(within(metrics).getByText('风险信号').nextElementSibling).toHaveTextContent('2 项')
    expect(within(metrics).getByText('风险信号').nextElementSibling).toHaveTextContent('VPN')
    // 失败来源 fraud-check 的 proxy 字段不能进入结论。
    expect(within(metrics).getByText('风险信号').nextElementSibling).not.toHaveTextContent('Proxy')
    expect(within(metrics).getByText('服务解锁').nextElementSibling).toHaveTextContent('1/2')
    expect(within(metrics).getByText('服务解锁').nextElementSibling).toHaveTextContent('1 受阻 · 1 未知')
  })

  it('shows services with status and region without internal probe fields', () => {
    renderDashboard(report())

    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(within(services).getByLabelText('服务解锁状态统计')).toHaveTextContent('1 可用1 受阻1 未知')
    expect(within(services).getByText('解锁 · JP')).toBeInTheDocument()
    expect(within(services).getByText('受阻')).toBeInTheDocument()
    expect(within(services).getByText('默认探测暂不支持该服务')).toBeInTheDocument()
    expect(within(services).queryByText(/openai_status_probe|211|skipped/)).not.toBeInTheDocument()
  })

  it('keeps duplicate service rows distinct by service and source', () => {
    renderDashboard(report({
      service_unlocks: [
        { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', region: 'JP' },
        { service: 'chatgpt', source: 'backup_probe', status: 'blocked', region: 'US' },
      ],
    }))

    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(within(services).getAllByText('ChatGPT')).toHaveLength(2)
    expect(within(services).getByText('解锁 · JP')).toBeInTheDocument()
    expect(within(services).getByText('受阻 · US')).toBeInTheDocument()
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
    expect(screen.getByText('部分采集')).toBeInTheDocument()
  })

  it('lists databases with facts first and folds empty or failed sources into a footnote', () => {
    renderDashboard(report())

    const heading = screen.getByRole('heading', { name: 'IP 数据库判断' })
    const section = heading.closest('section') as HTMLElement
    const region = within(section).getByRole('region', { name: 'IP 数据库判断' })
    expect(region).toHaveAttribute('tabindex', '0')
    expect(region).toHaveAttribute('aria-labelledby', heading.id)
    expect(region).not.toHaveAttribute('aria-describedby')

    const rows = within(region).getAllByRole('row').slice(1)
    expect(rows.map((row) => (row as HTMLTableRowElement).cells[0]?.textContent)).toEqual(['ipinfo', 'clean-db'])
    expect(within(section).getByLabelText('风险信号命中')).toHaveTextContent('VPN 1/2')
    expect(within(section).getByLabelText('风险信号命中')).toHaveTextContent('机房 1/1')
    expect(within(section).getByText('另有 1 个数据库未给出风险判断')).toBeInTheDocument()
    expect(within(section).getByLabelText('未返回结果的数据库')).toHaveTextContent('fraud-check · 失败')
    expect(within(section).queryByText(/maxmind|http status 429|73 ms/)).not.toBeInTheDocument()
  })

  it('collapses long history but keeps every report reachable', () => {
    const history = Array.from({ length: 7 }, (_, index) => ({
      ...summary,
      report_id: `ipq_00${index}`,
      observed_at: `2026-06-${String(8 - index).padStart(2, '0')}T12:00:00Z`,
      risk_level: index === 0 ? 'high' : 'medium',
    }))

    renderDashboard(report({ history }))

    const section = screen.getByRole('heading', { name: '历史报告' }).closest('section') as HTMLElement
    expect(within(section).getAllByRole('listitem')).toHaveLength(5)
    expect(within(section).getByText('当前查看').closest('li')).toHaveAttribute('aria-current', 'page')
    fireEvent.click(within(section).getByRole('button', { name: '显示全部 7 份' }))
    const links = within(section).getAllByRole('link', { name: /^查看 .* 的报告$/ })
    expect(links.map((link) => link.getAttribute('href'))).toEqual([
      '/vps/vps_001/ip-quality?report_id=ipq_000',
      '/vps/vps_001/ip-quality?report_id=ipq_002',
      '/vps/vps_001/ip-quality?report_id=ipq_003',
      '/vps/vps_001/ip-quality?report_id=ipq_004',
      '/vps/vps_001/ip-quality?report_id=ipq_005',
      '/vps/vps_001/ip-quality?report_id=ipq_006',
    ])
    expect(within(section).getByRole('button', { name: '收起' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('keeps diagnostics and raw JSON folded until requested', () => {
    const { container } = renderDashboard(report())

    const diagnostics = screen.getByLabelText('采集诊断')
    expect(within(diagnostics).getByRole('heading', { name: '采集诊断' }).closest('details')).not.toHaveAttribute('open')
    expect(within(diagnostics).getByText('监控关联')).toBeInTheDocument()
    expect(within(diagnostics).getByText('v1.1.0')).toBeInTheDocument()
    expect(within(diagnostics).getByText('openai_status_probe')).toBeInTheDocument()
    expect(within(diagnostics).getByText('73 ms')).toBeInTheDocument()
    expect(within(diagnostics).getByText('http_status · http status 429')).toBeInTheDocument()

    // 原始 JSON 默认只显示标题和大小，展开后才渲染内容，并且按文本输出。
    const raw = within(diagnostics).getByText('原始报告 JSON').closest('details') as HTMLDetailsElement
    expect(raw).not.toHaveAttribute('open')
    expect(within(raw).queryByLabelText('原始报告 JSON内容')).not.toBeInTheDocument()
    raw.open = true
    fireEvent(raw, new Event('toggle'))
    const code = within(raw).getByLabelText('原始报告 JSON内容')
    expect(code.tagName).toBe('PRE')
    expect(code).toHaveTextContent('"body_sample": "<!DOCTYPE html><script>alert(1)</script>"')
    expect(container.querySelector('script')).toBeNull()
    expect(within(raw).getByRole('button', { name: '复制原始报告 JSON' })).toBeInTheDocument()
    expect(within(diagnostics).getByText('来源附加数据')).toBeInTheDocument()
  })

  it('offers immediate collection from the header', () => {
    const collect = controller()
    renderDashboard(report(), { collect })

    const button = screen.getByRole('button', { name: '立即采集' })
    fireEvent.click(button)
    expect(collect.start).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('link', { name: '返回 VPS 详情' })).toHaveAttribute('href', '/vps/vps_001')
  })

  it('shows collection progress and disables the button while collecting', () => {
    const requestedAt = new Date(Date.now() - 12_000).toISOString()
    renderDashboard(report(), {
      collect: controller({ active: true, watchedRequestId: 'ipqc_001' }, {
        enabled: true,
        available: true,
        request: { request_id: 'ipqc_001', monitoring_instance_id: 'mi_001', status: 'dispatched', requested_at: requestedAt, expires_at: requestedAt },
      }),
    })

    expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('正在采集最新 IP 质量')
    expect(screen.getByRole('status')).toHaveTextContent(/agent 已接收，正在检测 · 已用 1\d 秒/)
  })

  it('explains why collection is unavailable and links to settings when disabled', () => {
    renderDashboard(report(), { collect: controller({}, { enabled: false, available: false, unavailable_reason: 'disabled' }) })

    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('IP 质量采集已在设置中关闭')
    expect(screen.getByRole('link', { name: '前往设置开启' })).toHaveAttribute('href', '/settings?tab=monitoring')
    // 保持历史列表可见可交互
    expect(screen.getByRole('heading', { name: '历史报告' })).toBeInTheDocument()
  })

  it('links unlinked or unbound agent to VPS monitoring workbench with explicit vpsId and keeps disabled button', () => {
    renderDashboard(report(), {
      collect: controller({}, { enabled: true, available: false, unavailable_reason: 'no_monitoring_instance' }),
    })

    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('该 VPS 尚未接入监控 agent')
    expect(screen.getByRole('link', { name: '前往监控工作台' })).toHaveAttribute('href', '/vps/vps_001?workbench=monitoring')
    expect(screen.getByRole('heading', { name: '历史报告' })).toBeInTheDocument()
  })

  it('links paused monitoring to actual MI with valid returnVPS and avoids automatic resume', () => {
    renderDashboard(report(), {
      collect: controller({}, {
        enabled: true,
        available: false,
        unavailable_reason: 'monitoring_paused',
        monitoring_instance_id: 'mi_001',
      }),
    })

    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('监控已暂停')
    expect(screen.getByRole('link', { name: '前往监控实例' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    // 不提供自动恢复或恢复监控按钮
    expect(screen.queryByRole('button', { name: /恢复/ })).not.toBeInTheDocument()
  })

  it('does not render guessed links when monitoring instance ID is missing for paused monitoring and refuses stale request MI', () => {
    // 缺少 monitoring_instance_id 时不生成猜测链接
    renderDashboard(report(), {
      collect: controller({}, {
        enabled: true,
        available: false,
        unavailable_reason: 'monitoring_paused',
      }),
    })

    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('监控已暂停')
    expect(screen.queryByRole('link', { name: '前往监控实例' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '前往监控工作台' })).not.toBeInTheDocument()

    // 仅历史 request 携带 monitoring_instance_id 时也绝不回退
    renderDashboard(report(), {
      collect: controller({}, {
        enabled: true,
        available: false,
        unavailable_reason: 'monitoring_paused',
        request: {
          request_id: 'ipqc_stale',
          monitoring_instance_id: 'mi_stale',
          status: 'completed',
          requested_at: new Date().toISOString(),
          expires_at: new Date().toISOString(),
        },
      }),
    })
    expect(screen.queryByRole('link', { name: '前往监控实例' })).not.toBeInTheDocument()
  })

  it('renders raw error_summary in closed details with safe Chinese primary summary for failed collection', () => {
    renderDashboard(report(), {
      collect: controller({
        active: false,
        watchedRequestId: 'ipqc_001',
      }, {
        enabled: true,
        available: true,
        request: {
          request_id: 'ipqc_001',
          monitoring_instance_id: 'mi_001',
          status: 'completed',
          report_status: 'failure',
          error_summary: 'dial tcp: connection refused',
          requested_at: new Date(Date.now() - 30_000).toISOString(),
          expires_at: new Date(Date.now() + 500_000).toISOString(),
        },
      }),
    })

    const statusRegion = screen.getByRole('status')
    expect(statusRegion).toHaveTextContent('本次采集失败，仍展示上一份有效报告')
    // 安全中文主摘要展示在 primary summary 中
    expect(statusRegion).toHaveTextContent('网络连接失败，未能连接采集源')

    // 原始 error_summary 放在默认折叠的 details 中
    const details = statusRegion.querySelector('details')
    expect(details).not.toBeNull()
    expect(details).not.toHaveAttribute('open')
    expect(within(details as HTMLElement).getByText('原始错误信息')).toBeInTheDocument()
    expect(within(details as HTMLElement).getByText('dial tcp: connection refused')).toBeInTheDocument()
  })

  it('never promotes mixed Chinese/URL raw content to primary summary and keeps it folded in details', () => {
    const mixedError = 'https://example.com/api/test 发生未授权错误'
    renderDashboard(report(), {
      collect: controller({
        active: false,
        watchedRequestId: 'ipqc_002',
      }, {
        enabled: true,
        available: true,
        request: {
          request_id: 'ipqc_002',
          monitoring_instance_id: 'mi_001',
          status: 'completed',
          report_status: 'failure',
          error_summary: mixedError,
          requested_at: new Date(Date.now() - 30_000).toISOString(),
          expires_at: new Date(Date.now() + 500_000).toISOString(),
        },
      }),
    })

    const statusRegion = screen.getByRole('status')
    // 主摘要必须是经过审查的安全中文语句，绝不能直接使用未经审查的混杂 URL/中文原文
    expect(statusRegion).toHaveTextContent('采集执行异常，未能获取有效报告')

    const details = statusRegion.querySelector('details')
    expect(details).not.toBeNull()
    expect(details).not.toHaveAttribute('open')
    expect(within(details as HTMLElement).getByText(mixedError)).toBeInTheDocument()
  })

  it('switches header actions when viewing a historical report', () => {
    renderDashboard(report(), { initialEntry: '/vps/vps_001/ip-quality?report_id=ipq_000' })

    expect(screen.getByText('历史报告', { selector: '.badge' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看最新报告' })).toHaveAttribute('href', '/vps/vps_001/ip-quality')
    expect(screen.queryByRole('button', { name: '立即采集' })).not.toBeInTheDocument()
  })

  it('shows an em dash and evidence insufficiency when one required risk field is absent', () => {
    renderDashboard(report({
      provider_results: [{
        provider: 'clean-db',
        status: 'success',
        source_type: 'default',
        is_proxy: false,
        is_vpn: false,
        is_abuser: false,
        is_robot: false,
      }],
      service_unlocks: [{ service: 'netflix', status: 'unlocked', probe_status: 'success' }],
    }))

    const verdict = screen.getByLabelText('质量结论')
    const scoreValue = verdict.querySelector('.ipq-verdict__value')
    expect(scoreValue).not.toBeNull()
    expect(scoreValue).toHaveTextContent('—')
    expect(within(verdict).getByText('证据不足，暂不评级')).toBeInTheDocument()
    expect(within(verdict).getByText('风险证据不足，未形成完整结论')).toBeInTheDocument()
  })

  it('states that returned risk fields did not hit when complete evidence is clean', () => {
    renderDashboard(report({
      summary: { ...summary, risk_level: 'low' },
      provider_results: [{
        provider: 'clean-db',
        status: 'success',
        source_type: 'default',
        is_proxy: false,
        is_tor: false,
        is_vpn: false,
        is_abuser: false,
        is_robot: false,
      }],
      service_unlocks: [{ service: 'netflix', status: 'unlocked', probe_status: 'success' }],
    }))

    const metrics = screen.getByLabelText('IP 质量摘要指标')
    expect(within(metrics).getByText('已返回的风险字段未命中')).toBeInTheDocument()
    expect(within(metrics).queryByText('风险证据不足，未形成完整结论')).not.toBeInTheDocument()
  })

  it.each(['unknown', '', 'challenge'] as const)('keeps service status %s unrated even with an explicit successful probe', (status) => {
    renderDashboard(report({
      summary: { ...summary, risk_level: 'low' },
      service_unlocks: [{ service: 'tiktok', status, probe_status: 'success' }],
    }))

    const verdict = screen.getByLabelText('质量结论')
    const scoreValue = verdict.querySelector('.ipq-verdict__value')
    expect(scoreValue).not.toBeNull()
    expect(scoreValue).toHaveTextContent('—')
    expect(within(verdict).getByText('证据不足，暂不评级')).toBeInTheDocument()
  })

  it('renders probe credibility notice for legacy default report without revision', () => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: { source_version: 'v2' },
      },
    }))

    const notice = screen.getByRole('note', { name: '服务探测可信度' })
    expect(notice).toHaveClass('ipq-notice', 'ipq-notice--warning')
    expect(within(notice).getByText('服务探测可信度')).toBeInTheDocument()
    expect(notice).toHaveTextContent('旧版或未识别')
    // 保留原服务行与评分
    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(within(services).getByText('解锁 · JP')).toBeInTheDocument()
    expect(within(services).getByText('受阻')).toBeInTheDocument()
    const verdict = screen.getByLabelText('质量结论')
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('59')
  })

  it('renders capability contraction notice, 0% service coverage, and em dash score for revision 1 default report with skipped probes', () => {
    const skippedReport = report({
      summary: {
        ...summary,
        coverage: {
          ...summary.coverage!,
          expected_service_count: 7,
          successful_service_count: 0,
          skipped_service_count: 7,
          failed_service_count: 0,
        },
      },
      latest_report: {
        ...report().latest_report!,
        coverage: {
          ...summary.coverage!,
          expected_service_count: 7,
          successful_service_count: 0,
          skipped_service_count: 7,
          failed_service_count: 0,
        },
        diagnostics_json: { source_version: 'v2', service_probe_revision: 1 },
      },
      service_unlocks: [
        { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
        { service: 'chatgpt', source: 'openai_status_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
        { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
        { service: 'amazon-prime-video', source: 'prime_video_page_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
        { service: 'disney-plus', source: 'disney_default_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
        { service: 'tiktok', source: 'tiktok_home_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
        { service: 'reddit', source: 'reddit_home_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
      ],
    })

    renderDashboard(skippedReport)

    const notice = screen.getByRole('note', { name: '服务探测可信度' })
    expect(notice).toHaveClass('ipq-notice', 'ipq-notice--warning')
    expect(notice).toHaveTextContent('已停用')

    const verdict = screen.getByLabelText('质量结论')
    expect(verdict.querySelector('.ipq-verdict__value')).toHaveTextContent('—')
    const metrics = screen.getByLabelText('IP 质量摘要指标')
    expect(within(metrics).getByText('采集完整性').nextElementSibling).toHaveTextContent('服务 0%')
  })

  it.each([
    '1',
    2,
    -1,
    0,
    null,
  ])('renders legacy notice when service_probe_revision is %s', (invalidRevision) => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: { source_version: 'v2', service_probe_revision: invalidRevision },
      },
    }))

    const notice = screen.getByRole('note', { name: '服务探测可信度' })
    expect(notice).toHaveTextContent('旧版或未识别')
  })

  it('does not render probe notice when report has no service rows', () => {
    renderDashboard(report({
      service_unlocks: [],
    }))

    expect(screen.queryByRole('note', { name: '服务探测可信度' })).not.toBeInTheDocument()
  })

  it('does not render probe notice for custom source report', () => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: { source_version: 'custom_v1' },
      },
      service_unlocks: [
        { service: 'internal-api', source: 'internal_custom_probe', status: 'unlocked', probe_status: 'success' },
      ],
    }))

    expect(screen.queryByRole('note', { name: '服务探测可信度' })).not.toBeInTheDocument()
  })

  it('does not render probe notice for revision 1 report without skipped default rows', () => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: { source_version: 'v2', service_probe_revision: 1 },
      },
      service_unlocks: [
        { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', probe_status: 'success', region: 'JP' },
        { service: 'netflix', source: 'netflix_title_probe', status: 'blocked', probe_status: 'success' },
      ],
    }))

    expect(screen.queryByRole('note', { name: '服务探测可信度' })).not.toBeInTheDocument()
  })

  it.each([
    { probe_status: 'skipped', error_code: 'unsupported_service' },
    { probe_status: 'failure', error_code: 'unsupported_default_probe' },
  ])('does not infer a disabled default probe from mismatched diagnostic %j', (diagnostic) => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: { source_version: 'v2', service_probe_revision: 1 },
      },
      service_unlocks: [
        { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', ...diagnostic },
      ],
    }))
    expect(screen.queryByRole('note', { name: '服务探测可信度' })).not.toBeInTheDocument()
  })

  it('uses selected report diagnostics when viewing historical report and retains actions boundary', () => {
    const historyReport = report({
      latest_report: {
        ...report().latest_report!,
        report_id: 'ipq_000',
        diagnostics_json: { source_version: 'v2' },
      },
    })

    renderDashboard(historyReport, { initialEntry: '/vps/vps_001/ip-quality?report_id=ipq_000' })

    expect(screen.getByText('历史报告', { selector: '.badge' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '立即采集' })).not.toBeInTheDocument()
    const notice = screen.getByRole('note', { name: '服务探测可信度' })
    expect(notice).toHaveTextContent('旧版或未识别')
  })

  it('translates invalid_response error code into Chinese in service card', () => {
    renderDashboard(report({
      service_unlocks: [
        { service: 'custom-service', source: 'custom_probe', status: 'unknown', probe_status: 'failure', error_code: 'invalid_response', error_summary: 'service response did not establish a business conclusion' },
      ],
    }))

    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(within(services).getByText('响应未形成可靠业务结论')).toBeInTheDocument()
    expect(within(services).queryByText('service response did not establish a business conclusion')).not.toBeInTheDocument()
  })

  it('renders em dashes for provider and service coverage when coverage is missing even with multiple rows', () => {
    const reportWithoutCoverage = report({
      summary: { ...summary },
      latest_report: { ...report().latest_report! },
    })
    delete reportWithoutCoverage.summary!.coverage
    delete reportWithoutCoverage.latest_report!.coverage

    renderDashboard(reportWithoutCoverage)

    const metrics = screen.getByLabelText('IP 质量摘要指标')
    const integrity = within(metrics).getByText('采集完整性').nextElementSibling as HTMLElement
    expect(integrity.querySelector('strong')).toHaveTextContent('—')
    expect(integrity.querySelector('span')).toHaveTextContent('服务 —')
  })

  it('renders generic notice when report lacks service probe source information', () => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: {},
      },
      service_unlocks: [
        { service: 'custom-one', source: '', status: 'unlocked', probe_status: 'success' },
        { service: 'custom-two', status: 'blocked', probe_status: 'success' },
      ],
    }))

    const notice = screen.getByRole('note', { name: '服务探测可信度' })
    expect(notice).toHaveClass('ipq-notice', 'ipq-notice--warning')
    expect(notice).toHaveTextContent('此报告缺少服务探测来源信息，无法核验解锁结果及依赖它的质量评分；历史原始结果予以保留。')
  })

  it('does not render probe notice for custom sources with unversioned diagnostics', () => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: {},
      },
      service_unlocks: [
        { service: 'internal-api', source: 'internal_custom_probe', status: 'unlocked', probe_status: 'success' },
      ],
    }))

    expect(screen.queryByRole('note', { name: '服务探测可信度' })).not.toBeInTheDocument()
  })

  it.each([
    {
      label: 'score >= 82 as primary node',
      risk_level: 'low',
      services: [{ service: 'chatgpt', status: 'unlocked', probe_status: 'success' }],
      expectedScore: '100',
      expectedVerdict: '适合作为主力节点',
      expectedTone: 'normal',
    },
    {
      label: 'score 68-81 as acceptable',
      risk_level: 'medium',
      services: [
        { service: 'chatgpt', status: 'unlocked', probe_status: 'success' },
        { service: 'netflix', status: 'blocked', probe_status: 'success' },
      ],
      expectedScore: '81',
      expectedVerdict: '可接受，建议持续观察',
      expectedTone: 'notice',
    },
    {
      label: 'score 50-67 as obvious risk',
      risk_level: 'high',
      services: [
        { service: 'chatgpt', status: 'unlocked', probe_status: 'success' },
        { service: 'netflix', status: 'blocked', probe_status: 'success' },
        { service: 'disney-plus', status: 'blocked', probe_status: 'success' },
        { service: 'youtube-premium', status: 'blocked', probe_status: 'success' },
      ],
      expectedScore: '59',
      expectedVerdict: '存在明显风险，谨慎使用',
      expectedTone: 'alert',
    },
    {
      label: 'score < 50 as not recommended',
      risk_level: 'critical',
      services: [
        { service: 'chatgpt', status: 'unlocked', probe_status: 'success' },
        { service: 'netflix', status: 'blocked', probe_status: 'success' },
        { service: 'disney-plus', status: 'blocked', probe_status: 'success' },
        { service: 'youtube-premium', status: 'blocked', probe_status: 'success' },
        { service: 'amazon-prime-video', status: 'blocked', probe_status: 'success' },
      ],
      expectedScore: '46',
      expectedVerdict: '不建议作为主力节点',
      expectedTone: 'alert',
    },
    {
      label: 'unknown service unlocks remain ungraded',
      risk_level: 'low',
      services: [{ service: 'chatgpt', status: 'unknown', probe_status: 'skipped' }],
      expectedScore: '—',
      expectedVerdict: '证据不足，暂不评级',
      expectedTone: 'neutral',
    },
  ])('reflects verdict and tone for $label', ({ risk_level, services, expectedScore, expectedVerdict, expectedTone }) => {
    renderDashboard(report({
      summary: { ...summary, risk_level },
      provider_results: [
        { provider: 'clean-db', status: 'success', source_type: 'default', is_proxy: false, is_vpn: false, is_tor: false, is_abuser: false, is_robot: false },
      ],
      service_unlocks: services,
    }))

    const verdict = screen.getByLabelText('质量结论')
    expect(verdict).toHaveClass(`ipq-verdict--${expectedTone}`)
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent(expectedScore)
    expect(within(verdict).getByText(expectedVerdict)).toBeInTheDocument()
  })
})
