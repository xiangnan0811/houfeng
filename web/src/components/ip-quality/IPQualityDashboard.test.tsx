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
    selected_ip_version: 4,
    default_ip_version: 4,
    families: [
      {
        ip_version: 4,
        availability: 'available',
        latest_attempt: { observed_at: '2026-06-08T12:00:00Z', stale: false, report_status: 'success', collection_scope: 'dual_stack', collection_id: 'col_001' },
        latest_report_summary: summary,
      },
      { ip_version: 6, availability: 'unknown', latest_attempt: null, latest_report_summary: null },
    ],
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
  {
    collect = controller(),
    initialEntry = '/vps/vps_001/ip-quality',
    refreshState = 'idle',
    onSelectFamily = vi.fn(),
  }: {
    collect?: IPQualityCollectController
    initialEntry?: string
    refreshState?: 'idle' | 'refreshing' | 'failed'
    onSelectFamily?: (next: 4 | 6) => void
  } = {},
) {
  const viewingHistory = new URL(initialEntry, 'http://local').searchParams.has('report_id')
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route
          path="/vps/:vpsId/ip-quality"
          element={(
            <IPQualityDashboard
              view={{ kind: 'report', report: body, summary: body.summary! }}
              detailPath="/vps/vps_001"
              collect={collect}
              viewingHistory={viewingHistory}
              refreshState={refreshState}
              onSelectFamily={onSelectFamily}
            />
          )}
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

  it('rates database risk from successful providers and dedupes risk signals by flag class', () => {
    renderDashboard(report())

    const verdict = screen.getByLabelText('数据库风险评级')
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('高风险')
    expect(within(verdict).getByText('风险字段 5/5 已返回')).toBeInTheDocument()
    expect(within(verdict).queryByText(/质量分|主力节点/)).not.toBeInTheDocument()
    const metrics = screen.getByLabelText('IP 质量摘要指标')
    const signals = within(metrics).getByText('风险信号').nextElementSibling as HTMLElement
    expect(signals.querySelector('strong')).toHaveTextContent('1 项')
    expect(signals).toHaveTextContent('VPN')
    expect(signals).toHaveTextContent('高风险来源：ipinfo')
    // 失败来源 fraud-check 的 proxy 字段不能进入结论。
    expect(signals).not.toHaveTextContent('Proxy')
    // 分母是 coverage 的完整 4 项服务；未返回行的服务计为未知，不算受阻。
    const serviceMetric = within(metrics).getByText('服务解锁').nextElementSibling as HTMLElement
    expect(serviceMetric.querySelector('strong')).toHaveTextContent('1/4 解锁')
    expect(serviceMetric).toHaveTextContent('1 受阻 · 2 未知')
  })

  it('shows services with status and region without internal probe fields', () => {
    renderDashboard(report())

    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(within(services).getByLabelText('服务状态统计')).toHaveTextContent('1 解锁1 受阻1 未知')
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

    const stats = screen.getByLabelText('服务状态统计')
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
      '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000',
      '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_002',
      '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_003',
      '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_004',
      '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_005',
      '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_006',
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
    // 回到该历史报告所属族的最新报告。
    expect(screen.getByRole('link', { name: '查看最新报告' })).toHaveAttribute('href', '/vps/vps_001/ip-quality?ip_version=4')
    expect(screen.queryByRole('button', { name: '立即采集' })).not.toBeInTheDocument()
  })

  it('reports incomplete low risk evidence as an evidence gap without a clean or green verdict', () => {
    renderDashboard(report({
      summary: { ...summary, risk_level: 'low' },
      provider_results: [{
        provider: 'clean-db',
        status: 'success',
        source_type: 'default',
        risk_level: 'low',
        is_proxy: false,
        is_vpn: false,
        is_abuser: false,
        is_robot: false,
      }],
    }))

    const verdict = screen.getByLabelText('数据库风险评级')
    expect(verdict).not.toHaveClass('ipq-verdict--normal')
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('证据不完整')
    expect(within(verdict).getByText('已返回来源：低风险')).toBeInTheDocument()
    expect(within(verdict).getByText('风险字段 4/5', { selector: '.ipq-verdict__scope' })).toBeInTheDocument()
    const metrics = screen.getByLabelText('IP 质量摘要指标')
    expect(within(metrics).getByText('风险信号').nextElementSibling?.querySelector('strong')).toHaveTextContent('未知')
    const providers = screen.getByRole('heading', { name: 'IP 数据库判断' }).closest('section') as HTMLElement
    expect(within(providers).getByLabelText('风险信号命中')).toHaveTextContent('风险字段不完整')
    expect(screen.queryByText(/已返回风险字段未命中|未发现代理/)).not.toBeInTheDocument()
  })

  it('keeps a found medium level visible while flagging missing risk fields', () => {
    renderDashboard(report({
      provider_results: [{ provider: 'mid-db', status: 'success', source_type: 'default', risk_level: 'medium', is_vpn: false }],
    }))

    const verdict = screen.getByLabelText('数据库风险评级')
    expect(verdict).toHaveClass('ipq-verdict--notice')
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('中风险')
    expect(within(verdict).getByText('风险字段 1/5，结论不完整', { selector: '.ipq-verdict__scope' })).toBeInTheDocument()
  })

  it('states that returned risk fields did not hit only when all five are present', () => {
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
    }))

    const verdict = screen.getByLabelText('数据库风险评级')
    // 没有任何来源给出等级时，不把 summary low 当作评级。
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('—')
    const metrics = screen.getByLabelText('IP 质量摘要指标')
    expect(within(metrics).getByText('风险信号').nextElementSibling?.querySelector('strong')).toHaveTextContent('未命中')
    expect(within(metrics).getByText('已返回风险字段未命中')).toBeInTheDocument()
    const providers = screen.getByRole('heading', { name: 'IP 数据库判断' }).closest('section') as HTMLElement
    expect(within(providers).getByLabelText('风险信号命中')).toHaveTextContent('已返回风险字段未命中')
  })

  it('lets a successful high-risk provider override a contradictory summary low', () => {
    renderDashboard(report({
      summary: { ...summary, risk_level: 'low' },
      provider_results: [
        { provider: 'clean-db', status: 'success', risk_level: 'low', is_proxy: false, is_tor: false, is_vpn: false, is_abuser: false, is_robot: false },
        { provider: 'proxycheck.io', status: 'success', risk_level: 'high', risk_score: '0' },
      ],
    }))

    const verdict = screen.getByLabelText('数据库风险评级')
    expect(verdict).toHaveClass('ipq-verdict--alert')
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('高风险')
    expect(within(verdict).queryByText('低风险')).not.toBeInTheDocument()
    // 原始供应商零分保留在来源行，不被当作缺失。
    const providers = screen.getByRole('heading', { name: 'IP 数据库判断' }).closest('section') as HTMLElement
    expect(providers.querySelector('.ipq-risk-cell__score')).toHaveTextContent('0')
    expect(within(providers).getByText('风险分：数值越高风险越高；不同数据库不可直接比较')).toBeInTheDocument()
  })

  it('keeps the same risk assessment whether services are unknown or known', () => {
    const unknownServices = renderDashboard(report({
      service_unlocks: [{ service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'failure', error_code: 'timeout' }],
    }))
    const unknownVerdict = screen.getByLabelText('数据库风险评级')
    const unknownText = unknownVerdict.querySelector('.ipq-verdict__score')?.textContent
    const unknownSignals = within(screen.getByLabelText('IP 质量摘要指标')).getByText('风险信号').nextElementSibling?.textContent
    const unknownClass = unknownVerdict.className
    unknownServices.unmount()

    renderDashboard(report({
      service_unlocks: [
        { service: 'netflix', source: 'netflix_title_probe', status: 'blocked', probe_status: 'success' },
        { service: 'reddit', source: 'reddit_home_probe', status: 'unlocked', probe_status: 'success' },
      ],
    }))
    const knownVerdict = screen.getByLabelText('数据库风险评级')
    expect(knownVerdict.querySelector('.ipq-verdict__score')?.textContent).toBe(unknownText)
    expect(within(screen.getByLabelText('IP 质量摘要指标')).getByText('风险信号').nextElementSibling?.textContent).toBe(unknownSignals)
    expect(knownVerdict.className).toBe(unknownClass)
  })

  it('withholds the aggregate rating for a source IP conflict but keeps provider rows', () => {
    renderDashboard(report({
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: { source_version: 'v2', collection_revision: 3, ip_conflict: true },
      },
    }))

    const verdict = screen.getByLabelText('数据库风险评级')
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('—')
    expect(within(verdict).getByText('来源返回了不同 IP，需复核来源')).toBeInTheDocument()
    expect(within(verdict).getByText('来源 IP 冲突')).toBeInTheDocument()
    expect(within(verdict).queryByText('高风险')).not.toBeInTheDocument()
    const region = screen.getByRole('heading', { name: 'IP 数据库判断' }).closest('section') as HTMLElement
    expect(within(region).getByText('ipinfo')).toBeInTheDocument()
  })

  it.each(['ambiguous', 'ip_conflict'] as const)('withholds the clean signal claim when %s prevents attribution', (blocker) => {
    renderDashboard(report({
      summary: { ...summary, ambiguous: blocker === 'ambiguous' },
      latest_report: {
        ...report().latest_report!,
        diagnostics_json: { ip_conflict: blocker === 'ip_conflict' },
      },
      provider_results: [{
        provider: 'all-false-db', status: 'success',
        is_proxy: false, is_vpn: false, is_tor: false, is_abuser: false, is_robot: false,
      }],
    }))

    const metrics = screen.getByLabelText('IP 质量摘要指标')
    const signals = within(metrics).getByText('风险信号').nextElementSibling as HTMLElement
    expect(signals.querySelector('strong')).toHaveTextContent('未知')
    expect(signals).not.toHaveTextContent('未命中')
    expect(signals).toHaveTextContent('复核')
    const providers = screen.getByRole('heading', { name: 'IP 数据库判断' }).closest('section') as HTMLElement
    expect(within(providers).getByText('all-false-db')).toBeInTheDocument()
  })

  it('marks stale partial reports as historical risk information', () => {
    renderDashboard(report({ summary: { ...summary, status: 'partial', stale: true } }))

    const verdict = screen.getByLabelText('数据库风险评级')
    expect(verdict).not.toHaveClass('ipq-verdict--alert')
    expect(verdict.querySelector('.ipq-verdict__value strong')).toHaveTextContent('高风险')
    expect(within(verdict).getByText('报告已过期，仅作历史风险信息')).toBeInTheDocument()
    expect(within(verdict).getByText('报告过期')).toBeInTheDocument()
    expect(within(verdict).getByText('部分采集')).toBeInTheDocument()
  })

  it('presents revision 2 anonymous probes with neutral scope, enabled-source coverage and gated service cards', () => {
    const coverage = {
      expected_provider_count: 18,
      successful_provider_count: 5,
      failed_provider_count: 0,
      skipped_provider_count: 0,
      not_configured_provider_count: 13,
      expected_service_count: 7,
      successful_service_count: 6,
      failed_service_count: 1,
      skipped_service_count: 0,
      not_configured_service_count: 0,
    }
    renderDashboard(report({
      summary: { ...summary, coverage },
      latest_report: {
        ...report().latest_report!,
        coverage,
        diagnostics_json: { source_version: 'v2', collection_revision: 3, service_probe_revision: 2 },
      },
      provider_results: [
        ...report().provider_results,
        { provider: 'ipwho.is', status: 'success', source_type: 'default', region_code: 'DE', extra_json: { capability: 'geo_only' } },
      ],
      service_unlocks: [
        { service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success', region: 'DE', unlock_type: 'catalog_sample', extra_json: { probe_revision: 2 } },
        { service: 'chatgpt', source: 'openai_status_probe', status: 'partial', probe_status: 'success', unlock_type: 'web_verified_app_unknown' },
        { service: 'reddit', source: 'reddit_home_probe', status: 'blocked', probe_status: 'success', unlock_type: 'anonymous_web' },
        { service: 'tiktok', source: 'tiktok_home_probe', status: 'blocked', probe_status: 'failure', region: 'US', unlock_type: 'web_region', error_code: 'challenge_detected', error_summary: 'service challenge detected' },
      ],
    }))

    const notice = screen.getByRole('note', { name: '服务探测可信度' })
    expect(notice).toHaveClass('ipq-notice--muted')
    expect(notice).not.toHaveClass('ipq-notice--warning')
    expect(notice).toHaveTextContent('免登录服务探测，仅反映当前入口和地区可用性，不代表账号或付费内容权限。')

    const services = screen.getByRole('heading', { name: '服务可用性（免登录探测）' }).closest('section') as HTMLElement
    expect(within(services).getByLabelText('服务状态统计')).toHaveTextContent('1 可用1 受限1 部分可用1 未知')
    expect(within(services).getByText('可用 · DE')).toBeInTheDocument()
    expect(within(services).getByText('抽样目录可访问')).toBeInTheDocument()
    expect(within(services).getByText('部分可用')).toBeInTheDocument()
    expect(within(services).getByText('Web 已验证，App 入口未知')).toBeInTheDocument()
    expect(within(services).getByText('受限')).toBeInTheDocument()
    const tiktok = within(services).getByText('TikTok').closest('li') as HTMLElement
    expect(tiktok).toHaveClass('ipq-service--unknown')
    expect(within(tiktok).getByText('未知')).toBeInTheDocument()
    expect(within(tiktok).getByText('服务要求人机验证')).toBeInTheDocument()
    // 失败探测残留的区域与范围不能进入卡片。
    expect(tiktok).not.toHaveTextContent(/US|网页地区检测|web_region/)
    expect(within(services).queryByText(/catalog_sample|anonymous_web/)).not.toBeInTheDocument()

    const metrics = screen.getByLabelText('IP 质量摘要指标')
    // 2 个分母外的未返回服务也计为未知：1/7 可用，partial 与 unknown 分列。
    const serviceMetric = within(metrics).getByText('服务可用性').nextElementSibling as HTMLElement
    expect(serviceMetric.querySelector('strong')).toHaveTextContent('1/7 可用')
    expect(serviceMetric).toHaveTextContent('1 部分可用 · 1 受限 · 4 未知')
    const providerCoverage = within(metrics).getByText('已启用来源响应成功率').nextElementSibling as HTMLElement
    expect(providerCoverage.querySelector('strong')).toHaveTextContent('100%')
    expect(providerCoverage).toHaveTextContent('响应成功 5/5')
    // 未接入数量只放诊断，不进入摘要指标。
    expect(providerCoverage).not.toHaveTextContent('13')
    expect(within(metrics).getByText('已知字段一致性')).toBeInTheDocument()

    const providers = screen.getByRole('heading', { name: 'IP 数据库判断' }).closest('section') as HTMLElement
    expect(within(providers).getByText('ipwho.is').closest('td')).toHaveTextContent('该来源本次仅返回地理信息')

    const diagnostics = screen.getByLabelText('采集诊断')
    expect(within(diagnostics).getByText(/个来源 · 4 项服务探测/)).toHaveTextContent('失败 1 · 未接入 1')
    expect(within(diagnostics).getByText('来源覆盖（全部）').nextElementSibling).toHaveTextContent('成功 5/18 · 失败 0 · 跳过 0 · 未接入 13')
    expect(within(diagnostics).getByText('服务覆盖').nextElementSibling).toHaveTextContent('成功 6/7 · 失败 1 · 跳过 0 · 未接入 0')
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
    // 保留原服务行，旧版报告继续使用“服务解锁”措辞。
    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(within(services).getByText('解锁 · JP')).toBeInTheDocument()
    expect(within(services).getByText('受阻')).toBeInTheDocument()
  })

  it('renders the disabled-probe notice and a full-denominator service count for a revision 1 default report with skipped probes', () => {
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

    const metrics = screen.getByLabelText('IP 质量摘要指标')
    const serviceMetric = within(metrics).getByText('服务解锁').nextElementSibling as HTMLElement
    expect(serviceMetric.querySelector('strong')).toHaveTextContent('0/7 解锁')
    expect(serviceMetric).toHaveTextContent('7 未知')
    expect(screen.getByRole('heading', { name: '服务解锁' })).toBeInTheDocument()
  })

  it.each([
    '1',
    '2',
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
    expect(notice).toHaveClass('ipq-notice--warning')
    // 所选历史报告没有 revision 2，不套用免登录探测措辞。
    expect(screen.getByRole('heading', { name: '服务解锁' })).toBeInTheDocument()
    expect(screen.queryByText(/免登录/)).not.toBeInTheDocument()
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

  it('keeps status counts without a fabricated ratio when coverage is missing even with multiple rows', () => {
    const reportWithoutCoverage = report({
      summary: { ...summary },
      latest_report: { ...report().latest_report! },
    })
    delete reportWithoutCoverage.summary!.coverage
    delete reportWithoutCoverage.latest_report!.coverage

    renderDashboard(reportWithoutCoverage)

    const metrics = screen.getByLabelText('IP 质量摘要指标')
    const integrity = within(metrics).getByText('已启用来源响应成功率').nextElementSibling as HTMLElement
    expect(integrity.querySelector('strong')).toHaveTextContent('—')
    expect(integrity).toHaveTextContent('暂无有效覆盖数据')
    const serviceMetric = within(metrics).getByText('服务解锁').nextElementSibling as HTMLElement
    expect(serviceMetric.querySelector('strong')).toHaveTextContent('1 解锁')
    expect(serviceMetric.querySelector('strong')).not.toHaveTextContent('/')
    expect(serviceMetric).toHaveTextContent('1 受阻 · 1 未知')
  })

  it('shows trusted revision 3 page geography and endpoint diagnostics without changing service status', () => {
    const youtubeExtra = {
      probe_revision: 3,
      evidence_kind: 'anonymous_service_probe',
      ip_version: 6,
      observed_region: 'CN',
      region_evidence: 'youtube_content_region',
      http_status: 200,
      endpoints: [{ endpoint: 'premium', http_status: 200, error_code: 'invalid_response', outcome: 'unknown' }],
    }
    const ipv6Report = (family: 4 | 6) => report({
      selected_ip_version: family,
      summary: { ...summary, ip_address: '2001:db8::10', ip_version: family },
      latest_report: {
        ...report().latest_report!,
        ip_address: '2001:db8::10',
        ip_version: family,
        diagnostics_json: { source_version: 'v2', service_probe_revision: 3 },
      },
      service_unlocks: [
        { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unknown', probe_status: 'failure', region: 'US', error_code: 'invalid_response', extra_json: youtubeExtra },
        {
          service: 'chatgpt', source: 'openai_status_probe', status: 'partial', probe_status: 'success', unlock_type: 'web_verified_app_unknown',
          extra_json: {
            probe_revision: 3, evidence_kind: 'anonymous_service_probe', ip_version: 6,
            endpoints: [
              { endpoint: 'web_cookie_requirements', http_status: 200, matched_rule: 'openai_cookie_requirements', outcome: 'available' },
              { endpoint: 'app_home', error_code: 'network_unreachable', outcome: 'unknown' },
            ],
          },
        },
        { service: 'reddit', source: 'reddit_home_probe', status: 'unknown', probe_status: 'failure', error_code: 'http_status', error_summary: 'service returned an unsuccessful HTTP status', extra_json: { probe_revision: 3, evidence_kind: 'anonymous_service_probe', ip_version: 6, http_status: 403, endpoints: [{ endpoint: 'anonymous_chat', http_status: 403, error_code: 'http_status', outcome: 'unknown' }] } },
      ],
    })

    const { unmount } = renderDashboard(ipv6Report(6))
    expect(screen.getByRole('note', { name: '服务探测可信度' })).toHaveClass('ipq-notice--muted')
    const services = screen.getByRole('heading', { name: '服务可用性（免登录探测）' }).closest('section') as HTMLElement
    const youtube = within(services).getByText('YouTube Premium').closest('li') as HTMLElement
    expect(youtube).toHaveClass('ipq-service--unknown')
    expect(within(youtube).getByText('未知')).toBeInTheDocument()
    expect(within(youtube).getByLabelText('YouTube Premium 附加证据')).toHaveTextContent('页面地区：CN；Premium 可用性未知')
    // 残留 normalized region 不进入卡片。
    expect(youtube).not.toHaveTextContent('US')
    expect(within(services).getByLabelText('服务状态统计')).toHaveTextContent('0 可用0 受限1 部分可用2 未知')
    const reddit = within(services).getByText('Reddit').closest('li') as HTMLElement
    expect(reddit).toHaveTextContent('服务拒绝了探测请求（HTTP 403）')
    expect(within(services).getByText('ChatGPT').closest('li')).toHaveTextContent('服务地区未知')

    const diagnostics = screen.getByLabelText('采集诊断')
    const chatgptRow = within(diagnostics).getByText('openai_status_probe').closest('tr') as HTMLElement
    expect(chatgptRow).toHaveTextContent('web_cookie_requirements · HTTP 200 · 可用 · openai_cookie_requirements')
    expect(chatgptRow).toHaveTextContent('app_home · 未知 · 网络不可达')
    unmount()

    // 证据族与所选报告族不一致时不展示页面地区。
    renderDashboard(ipv6Report(4))
    const mismatched = within(screen.getByRole('heading', { name: '服务可用性（免登录探测）' }).closest('section') as HTMLElement)
      .getByText('YouTube Premium').closest('li') as HTMLElement
    expect(mismatched).not.toHaveTextContent('页面地区')
  })

  it('labels the untested Netflix connection method without inventing native or DNS unlock', () => {
    renderDashboard(report())
    const netflix = screen.getByText('Netflix', { selector: '.ipq-service__name' }).closest('li') as HTMLElement
    expect(netflix).toHaveTextContent('连接方式未检测')
    expect(netflix).not.toHaveTextContent(/原生|DNS/)
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
    expect(notice).toHaveTextContent('此报告缺少服务探测来源信息，无法核验解锁结果；历史原始结果予以保留。')
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

  describe('dual-stack collection completion and family state', () => {
    type Request = NonNullable<IPQualityCollectStatus['request']>
    function completedCollect(familyResults: NonNullable<Request['family_results']> | null, extra: Partial<Request> = {}) {
      const request: Request = {
        request_id: 'ipqc_dual',
        monitoring_instance_id: 'mi_001',
        status: 'completed',
        requested_at: new Date(Date.now() - 30_000).toISOString(),
        expires_at: new Date(Date.now() + 500_000).toISOString(),
        report_status: 'partial',
        collection_scope: 'dual_stack',
        collection_id: 'col_new',
        ...(familyResults ? { family_results: familyResults } : {}),
        ...extra,
      }
      return controller({ watchedRequestId: 'ipqc_dual' }, { enabled: true, available: true, request })
    }
    const ipv4FailedIPv6Ok = [
      { ip_version: 4 as const, availability: 'available', report_status: 'failure', error_code: 'lookup_failed' },
      { ip_version: 6 as const, availability: 'available', report_status: 'success' },
    ]
    const withCollection = (collectionID: string, patch: Partial<VPSIPQualityReport> = {}) => {
      const base = report(patch)
      return { ...base, latest_report: { ...base.latest_report!, diagnostics_json: { source_version: 'v2', collection_id: collectionID } } }
    }

    it('keeps the previous IPv4 result and reports the IPv4 failure instead of an update', () => {
      renderDashboard(withCollection('col_old', {
        families: [
          { ip_version: 4, availability: 'available', latest_attempt: { observed_at: '2026-06-09T12:00:00Z', stale: false, report_status: 'failure', error_code: 'lookup_failed', collection_scope: 'dual_stack', collection_id: 'col_new' }, latest_report_summary: summary },
          { ip_version: 6, availability: 'available', latest_attempt: null, latest_report_summary: null },
        ],
      }), { collect: completedCollect(ipv4FailedIPv6Ok) })

      const status = screen.getByRole('status', { name: 'IP 质量采集状态' })
      expect(status).toHaveTextContent('本轮 IPv4 采集失败')
      expect(status).toHaveTextContent('IPv4 出口探测失败')
      expect(status).not.toHaveTextContent('报告已更新')
      const family = screen.getByRole('note', { name: '地址族采集状态' })
      expect(family).toHaveTextContent('IPv4 出口探测失败')
      expect(family).toHaveTextContent('以下为上次有效结果')
      // 失败尝试不进入事实：身份仍是上次有效 IPv4 出口。
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('192.0.2.1 · IPv4')
    })

    it('announces an update only for the selected family when the shown report belongs to the completed round', () => {
      const ipv6Summary = { ...summary, report_id: 'ipq_v6', ip_address: '2001:db8::10', ip_version: 6 }
      const ipv6 = withCollection('col_new', {
        selected_ip_version: 6,
        summary: ipv6Summary,
        history: [ipv6Summary],
      })
      ipv6.latest_report = { ...ipv6.latest_report!, report_id: 'ipq_v6', ip_address: '2001:db8::10', ip_version: 6 }
      const { unmount } = renderDashboard(ipv6, { collect: completedCollect(ipv4FailedIPv6Ok) })
      expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('采集完成，报告已更新')
      // IPv6 视图不出现 IPv4 出口。
      expect(document.querySelector('.ipq-page')).not.toHaveTextContent('192.0.2.1')
      unmount()

      // 刷新进行中或失败时不能宣称已更新；展示的报告不是本轮结果时也不能。
      for (const [refreshState, text] of [['refreshing', '正在刷新报告'], ['failed', '刷新失败']] as const) {
        const rendered = renderDashboard(ipv6, { collect: completedCollect(ipv4FailedIPv6Ok), refreshState })
        expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent(text)
        expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent('报告已更新')
        rendered.unmount()
      }
      renderDashboard({ ...ipv6, latest_report: { ...ipv6.latest_report!, diagnostics_json: { source_version: 'v2', collection_id: 'col_old' } } }, { collect: completedCollect(ipv4FailedIPv6Ok) })
      expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent('报告已更新')
    })

    it('keeps a history view on history when the round completes', () => {
      renderDashboard(withCollection('col_new'), {
        collect: completedCollect([{ ip_version: 4, availability: 'available', report_status: 'success' }, { ip_version: 6, availability: 'unavailable', report_status: 'failure', error_code: 'address_family_unavailable' }]),
        initialEntry: '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000',
      })
      const status = screen.getByRole('status', { name: 'IP 质量采集状态' })
      expect(status).toHaveTextContent('历史')
      expect(status).not.toHaveTextContent('报告已更新')
      expect(screen.queryByRole('note', { name: '地址族采集状态' })).not.toBeInTheDocument()
    })

    it('explains a missing family and legacy single-family rounds without claiming dual-stack verification', () => {
      const v6Unavailable = [
        { ip_version: 4 as const, availability: 'available', report_status: 'success' },
        { ip_version: 6 as const, availability: 'unavailable', report_status: 'failure', error_code: 'address_family_unavailable' },
      ]
      const ipv6Empty = { ...report({ selected_ip_version: 6, summary: null, latest_report: null, provider_results: [], service_unlocks: [], history: [] }) }
      const { unmount } = render(
        <MemoryRouter initialEntries={['/vps/vps_001/ip-quality?ip_version=6']}>
          <Routes>
            <Route
              path="/vps/:vpsId/ip-quality"
              element={<IPQualityDashboard view={{ kind: 'empty', report: { ...ipv6Empty, families: [ipv6Empty.families[0]!, { ip_version: 6, availability: 'unavailable', latest_attempt: { observed_at: '2026-06-09T12:00:00Z', stale: false, report_status: 'failure', error_code: 'address_family_unavailable', collection_scope: 'dual_stack', collection_id: 'col_new' }, latest_report_summary: null }] }, ipVersion: 6 }} detailPath="/vps/vps_001" collect={completedCollect(v6Unavailable)} viewingHistory={false} refreshState="idle" onSelectFamily={vi.fn()} />}
            />
          </Routes>
        </MemoryRouter>,
      )
      expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('本轮未检测到 IPv6 可用本地地址')
      expect(screen.getByRole('note', { name: '地址族采集状态' })).toHaveTextContent('本轮未检测到 IPv6 可用本地地址')
      expect(screen.queryByLabelText('报告身份')).not.toBeInTheDocument()
      expect(screen.queryByLabelText('数据库风险评级')).not.toBeInTheDocument()
      expect(document.querySelector('.ipq-page')).not.toHaveTextContent(/0\.0\.0\.0|192\.0\.2\.1/)
      // 缺族仍可切换。
      expect(screen.getByRole('button', { name: 'IPv4' })).toHaveAttribute('aria-pressed', 'false')
      expect(screen.getByRole('button', { name: 'IPv6' })).toHaveAttribute('aria-pressed', 'true')
      unmount()

      const legacyCollect = completedCollect(null, { collection_scope: 'legacy_single_family', report_status: 'success' })
      delete legacyCollect.status!.request!.collection_id
      renderDashboard(report({
        latest_report: { ...report().latest_report!, diagnostics_json: { source_version: 'v2', collect_request_id: 'ipqc_dual' } },
      }), { collect: legacyCollect })
      const status = screen.getByRole('status', { name: 'IP 质量采集状态' })
      expect(status).toHaveTextContent('采集完成，报告已更新')
      expect(status).toHaveTextContent('本次仅收到单地址族报告，未验证另一地址族')
    })

    it('marks a stale unavailable attempt as expired instead of a current missing family', () => {
      renderDashboard(report({
        families: [
          { ip_version: 4, availability: 'unavailable', latest_attempt: { observed_at: '2026-05-01T12:00:00Z', stale: true, report_status: 'failure', error_code: 'address_family_unavailable', collection_scope: 'dual_stack', collection_id: 'col_old' }, latest_report_summary: summary },
          { ip_version: 6, availability: 'unknown', latest_attempt: null, latest_report_summary: null },
        ],
      }))
      const family = screen.getByRole('note', { name: '地址族采集状态' })
      expect(family).toHaveTextContent('已过期')
      expect(family).not.toHaveTextContent('本轮未检测到')
    })

    it('switches family through the selector without starting a collection', () => {
      const onSelectFamily = vi.fn()
      const collect = controller()
      renderDashboard(report(), { onSelectFamily, collect })
      const group = screen.getByRole('group', { name: 'IP 地址族' })
      expect(within(group).getByRole('button', { name: 'IPv4' })).toHaveAttribute('aria-pressed', 'true')
      fireEvent.click(within(group).getByRole('button', { name: 'IPv6' }))
      expect(onSelectFamily).toHaveBeenCalledWith(6)
      expect(collect.start).not.toHaveBeenCalled()
      expect(screen.getByText('分别检测 IPv4 / IPv6')).toBeInTheDocument()
    })
  })
})
