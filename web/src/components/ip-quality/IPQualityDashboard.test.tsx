import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type {
  IPQualityCollectRequest,
  IPQualityCollectStatus,
  IPQualityProductSummary,
  IPQualityProductView,
  IPQualityProviderDetails,
  IPQualityProviderResult,
  IPQualitySummary,
  VPSIPQualityReport,
} from '../../lib/types'
import { IPQualityDashboard, type IPQualityDashboardView } from './IPQualityDashboard'
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

const OBSERVED = '2026-06-08T12:00:00Z'

function productSummary(): IPQualityProductSummary {
  return {
    risk_flags: ['vpn'],
    high_risk_sources: [],
    restricted_services: [],
    blacklist_listed_count: 1,
    blacklist_checked_count: 2,
    service_available_count: 5,
    service_partial_count: 1,
    service_restricted_count: 0,
    service_unknown_count: 1,
    geo_status: 'consistent',
  }
}

function details(patch: Partial<IPQualityProviderDetails>): IPQualityProviderDetails {
  return {
    queried_ip: '192.0.2.1', observed_at: OBSERVED, method: 'agent_public', api_tier: 'anonymous',
    country_code: 'US', region: 'California', city: '', postal_code: '', timezone: '',
    latitude: null, longitude: null, registered_country_code: '', asn: 'AS64500', as_name: '', as_domain: '',
    as_country_code: '', route: '', company_name: 'Example Transit', company_domain: '',
    is_mobile: null, is_anycast: null, is_bogon: null, scores: [], field_states: {},
    ...patch,
  }
}

function productView(): IPQualityProductView {
  return {
    revision: 1,
    report_id: 'ipq_001',
    ip_address: '192.0.2.1',
    ip_version: 4,
    state: 'complete',
    agent_observed_at: OBSERVED,
    completed_at: '2026-06-08T12:01:00Z',
    ptr_names: ['host.example.net'],
    providers: [
      {
        provider: 'ipapi.is', status: 'success', source_type: 'default', usage_type: 'hosting', company_type: 'hosting',
        details: details({ city: 'Los Angeles', field_states: { is_proxy: 'unsupported', is_vpn: 'unsupported' } }),
      },
      {
        provider: 'ipquery.io', status: 'success', source_type: 'default', usage_type: 'hosting', is_proxy: false, is_vpn: true,
        details: details({
          city: 'San Jose',
          field_states: { is_abuser: 'unknown', is_robot: 'error' },
          scores: [{ id: 'risk_score', label: '风险分', raw_value: '20', unit: 'score', scope: 'ip', direction: 'higher_risk', source_label: '', value: 20, min: 0, max: 100 }],
        }),
      },
      {
        provider: 'ipapi.is', status: 'success', source_type: 'default', usage_type: 'hosting', company_type: 'hosting',
        is_proxy: false, is_vpn: false, is_tor: false, is_abuser: false, is_server: true,
        details: details({
          method: 'center_api', api_tier: 'authenticated', city: 'Los Angeles', latitude: 0, longitude: 0,
          scores: [{ id: 'company_abuser_score', label: '公司滥用', raw_value: '0.0183 (Elevated)', unit: 'ratio', scope: 'company_network', direction: 'higher_risk', source_label: 'Elevated', value: 0.0183, min: 0, max: 1 }],
        }),
      },
      { provider: 'proxycheck.io', status: 'failure', source_type: 'default', error_code: 'timeout', is_proxy: true },
    ],
    mail: [
      { provider: 'gmail', domain: 'gmail.com', status: 'reachable', mx_host: 'gmail-smtp-in.l.google.com', target_ip: '192.0.2.25', error_code: '', mx_preference: 5, greeting_code: 220, latency_ms: 120, observed_at: OBSERVED },
      { provider: 'qq', domain: 'qq.com', status: 'timeout', mx_host: 'mx1.qq.com', target_ip: '192.0.2.26', error_code: 'timeout', mx_preference: 10, greeting_code: null, latency_ms: null, observed_at: OBSERVED },
    ],
    blacklists: [
      { source: 'spamcop', category: 'spam', status: 'not_listed', query_method: 'dnsbl', error_code: '', reference_url: 'https://www.spamcop.net/bl.shtml', matched_range: '', codes: [], observed_at: OBSERVED, dataset_at: null },
      { source: 'dronebl', category: 'abuse', status: 'listed', query_method: 'dnsbl', error_code: '', reference_url: 'https://dronebl.org/', matched_range: '', codes: ['8'], observed_at: OBSERVED, dataset_at: null },
      { source: 'spamhaus-drop', category: 'network', status: 'unknown', query_method: 'dataset', error_code: 'dataset_incomplete', reference_url: '', matched_range: '', codes: [], observed_at: OBSERVED, dataset_at: null },
    ],
    registration: {
      status: 'success', rir: 'arin', network_handle: 'NET-192-0-2-0-1', start_address: '192.0.2.0', end_address: '192.0.2.255',
      network_name: 'EXAMPLE-NET', network_country_code: 'US', registrant_country_code: 'US', registrant_name: 'Example', source_url: '',
      registered_at: null, updated_at: null,
    },
    asn: {
      status: 'success', number: 'AS64500', name: 'EXAMPLE-TRANSIT', domain: 'example.net', type: 'hosting', country_code: 'US', rir: 'arin',
      source_url: '', route: '192.0.2.0/24', registered_at: null, updated_at: null, observed_at: OBSERVED,
      ipv4_prefixes: '123', ipv4_addresses: '4096', ipv6_prefixes: null, ipv6_48s: null, origins: ['AS64500'],
    },
    summary: productSummary(),
  }
}

function productReport(overrides: Partial<IPQualityProductView> = {}): VPSIPQualityReport {
  const productSummaryValue = { ...summary, risk_level: '', product_revision: 1, intelligence_state: overrides.state ?? 'complete', product_summary: productSummary() }
  return report({
    summary: productSummaryValue,
    latest_report: { ...report().latest_report!, diagnostics_json: { source_version: 'v2', service_probe_revision: 4 } },
    service_unlocks: [
      {
        service: 'chatgpt', source: 'openai_status_probe', status: 'partial', probe_status: 'success',
        checks: [
          { id: 'web', label: 'Web', status: 'available', region: '', error_code: '', rule: 'openai_cookie_requirements', http_status: 200 },
          { id: 'app', label: 'App', status: 'unknown', region: '', error_code: 'timeout', rule: '', http_status: null },
        ],
      },
      {
        service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'catalog_sample',
        checks: [
          { id: 'title_81280792', label: '样片 81280792', status: 'available', region: 'US', error_code: '', rule: 'netflix_title_meta', http_status: 200 },
          { id: 'title_70143836', label: '样片 70143836', status: 'restricted', region: '', error_code: '', rule: 'netflix_title_missing', http_status: 404 },
        ],
      },
    ],
    history: [productSummaryValue],
    product: { ...productView(), ...overrides },
  })
}

function controller(overrides: Partial<IPQualityCollectController> = {}, status: IPQualityCollectStatus | null = { enabled: true, available: true }): IPQualityCollectController {
  return {
    status,
    checking: false,
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
    enrichment = null,
  }: {
    collect?: IPQualityCollectController
    initialEntry?: string
    refreshState?: 'idle' | 'refreshing' | 'failed'
    onSelectFamily?: (next: 4 | 6) => void
    enrichment?: { active: boolean, failed: boolean, retry: () => void } | null
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
              enrichment={enrichment}
            />
          )}
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('IPQualityDashboard', () => {
  it('keeps the identity strip to IP, family and time with a copy action', () => {
    renderDashboard(report())

    expect(screen.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeInTheDocument()
    const identity = screen.getByLabelText('报告身份')
    expect(identity.tagName).toBe('DL')
    expect(identity).toHaveTextContent('192.0.2.1')
    expect(identity).toHaveTextContent('IPv4')
    expect(within(identity).getByRole('button', { name: '复制出口 IP' })).toBeInTheDocument()
    expect(within(identity).queryByText(/link|v1\.1\.0|ipq_001|AS64500/)).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '历史' })).toHaveAttribute('href', '#ip-quality-history-title')
    // 旧报告没有 product：只给短标记，不铺解释段落。
    expect(screen.getByText('旧版报告')).toBeInTheDocument()
    expect(screen.queryByRole('note', { name: '服务探测可信度' })).not.toBeInTheDocument()
  })

  it('labels a legacy rating as legacy and keeps product-only modules truthful', () => {
    renderDashboard(report())

    const providers = screen.getByRole('heading', { name: '风险评分与因子' }).closest('section') as HTMLElement
    expect(within(providers).getByRole('group', { name: '风险命中' })).toHaveTextContent('旧版评级 · 高风险')
    // 旧行只有评级、没有真实原始分：不造评分行，旧版评级只留在头部标记。
    expect(within(providers).queryByRole('list', { name: '风险评分' })).not.toBeInTheDocument()
    const region = within(providers).getByRole('region', { name: '风险因子' })
    expect(region).toHaveAttribute('tabindex', '0')
    const rows = within(region).getAllByRole('row').slice(1)
    // 每个供应商一行；历史可选占位（maxmind）只在诊断出现。
    expect(rows.map((row) => (row as HTMLTableRowElement).cells[0]?.textContent)).toEqual(['clean-db', 'ipinfo', 'geo-only', 'fraud-check'])
    expect(rows[3]).toHaveTextContent('查询失败')
    // 失败来源残留的 proxy 不作为命中单元格展示。
    expect(within(rows[3] as HTMLElement).queryByText('Proxy 是')).not.toBeInTheDocument()
    expect(within(rows[1] as HTMLElement).getByText('VPN 是')).toBeInTheDocument()
    expect(within(rows[0] as HTMLElement).getByText('VPN 否')).toBeInTheDocument()
    expect(within(providers).queryByText(/maxmind|http status 429|73 ms/)).not.toBeInTheDocument()
    expect(within(providers).queryByRole('list', { name: '图例' })).not.toBeInTheDocument()
    expect(screen.queryByText(/证据不完整|响应成功率|一致性|N\/5/)).not.toBeInTheDocument()

    const mail = screen.getByRole('heading', { name: '邮件连通性' }).closest('section') as HTMLElement
    expect(mail).toHaveTextContent('旧版报告无此项')
    const blacklist = screen.getByRole('heading', { name: '黑名单' }).closest('section') as HTMLElement
    expect(blacklist).toHaveTextContent('旧版报告无此项')
    expect(screen.getByRole('region', { name: '基础信息' })).toHaveTextContent('AS64500')
  })

  it('keeps one legacy score row only when the source gave an actual score', () => {
    const base = report()
    renderDashboard(report({
      provider_results: base.provider_results.map((row) => (row.provider === 'ipinfo' ? { ...row, risk_score: '82' } : row)),
    }))
    const providers = screen.getByRole('heading', { name: '风险评分与因子' }).closest('section') as HTMLElement
    const items = within(within(providers).getByRole('list', { name: '风险评分' })).getAllByRole('listitem')
    expect(items).toHaveLength(1)
    expect(items[0]).toHaveTextContent('ipinfo')
    expect(items[0]).toHaveTextContent('82')
    expect(items[0]).toHaveTextContent('旧版评级')
  })

  it('shows services with status and region without internal probe fields', () => {
    renderDashboard(report())

    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(within(services).getByLabelText('服务状态统计')).toHaveTextContent('1 可用1 受限1 未知')
    expect(within(services).getByText('解锁 · JP')).toBeInTheDocument()
    expect(within(services).getByText('受阻')).toBeInTheDocument()
    // 未知原因只在折叠详情里，主卡片只有短状态。
    const disney = within(services).getByText('Disney+').closest('li') as HTMLElement
    expect(within(disney).getByText('未知')).toBeInTheDocument()
    expect(within(disney).getByText('原因：默认探测暂不支持该服务').closest('details')).not.toHaveAttribute('open')
    expect(within(services).queryByText(/openai_status_probe|211|skipped|连接方式未检测|服务地区未知/)).not.toBeInTheDocument()
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
    expect(stats).toHaveTextContent('0 受限')
    expect(stats).toHaveTextContent('2 未知')
    expect(screen.getByText('部分采集')).toBeInTheDocument()
  })

  it('collapses long history but keeps every report reachable with factual summaries', () => {
    const history: IPQualitySummary[] = Array.from({ length: 7 }, (_, index) => ({
      ...summary,
      report_id: `ipq_00${index}`,
      observed_at: `2026-06-${String(8 - index).padStart(2, '0')}T12:00:00Z`,
      risk_level: index === 0 ? 'high' : 'medium',
    }))
    history[0] = {
      ...history[0]!,
      risk_level: '',
      product_revision: 1,
      product_summary: { ...productSummary(), risk_flags: ['vpn'], blacklist_listed_count: 1 },
    }

    renderDashboard(report({ history }))

    const section = screen.getByRole('heading', { name: '历史报告' }).closest('section') as HTMLElement
    const items = within(section).getAllByRole('listitem')
    expect(items).toHaveLength(5)
    expect(within(section).getByText('当前查看').closest('li')).toHaveAttribute('aria-current', 'page')
    // revision1 历史用服务端事实，不再出现 unknown/低风险冲突；旧报告明确标为旧版评级。
    expect(items[0]).toHaveTextContent('VPN')
    expect(items[0]).toHaveTextContent('黑名单 1')
    expect(items[0]).toHaveTextContent('服务 5 可用')
    expect(items[0]).not.toHaveTextContent('评级')
    expect(items[1]).toHaveTextContent('旧版评级 · 中风险')
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

  describe('revision1 product report', () => {
    it('selects the authenticated sample per vendor, keeps scoped scores and four distinct cell states', () => {
      renderDashboard(productReport())

      expect(screen.queryByText('旧版报告')).not.toBeInTheDocument()
      const providers = screen.getByRole('heading', { name: '风险评分与因子' }).closest('section') as HTMLElement
      expect(within(providers).getByRole('group', { name: '风险命中' })).toHaveTextContent('VPN')

      // 评分逐源逐项一行，紧邻因子矩阵；只有带分数的来源占行。
      const scores = within(providers).getByRole('list', { name: '风险评分' })
      const scoreRows = within(scores).getAllByRole('listitem')
      expect(scoreRows).toHaveLength(2)
      // 0.0183 (Elevated) 是公司网段比例：显示 1.83%，中文等级“偏高”并保留来源原文与范围。
      expect(scoreRows[0]).toHaveTextContent('ipapi.is')
      expect(scoreRows[0]).toHaveTextContent('1.83%')
      expect(scoreRows[0]).toHaveTextContent('公司网段')
      expect(within(scoreRows[0]!).getByText('偏高')).toBeInTheDocument()
      expect(scoreRows[0]).toHaveTextContent('Elevated')
      expect(scoreRows[0]!.querySelector('.ipq-score__fill--notice')).not.toBeNull()
      // 只有数值没有等级：中性原尺度条，不捏造阈值等级。
      expect(scoreRows[1]).toHaveTextContent('ipquery.io')
      expect(scoreRows[1]).toHaveTextContent('20')
      expect(scoreRows[1]!.querySelector('.ipq-score__fill--neutral')).not.toBeNull()
      expect(scoreRows[1]!.querySelector('.badge')).toBeNull()

      const region = within(providers).getByRole('region', { name: '风险因子' })
      const rows = () => within(region).getAllByRole('row').slice(1) as HTMLTableRowElement[]
      expect(rows().map((row) => row.cells[0]?.textContent)).toEqual(['ipapi.is认证另 1 个样本', 'ipquery.io匿名', 'proxycheck.io'])

      const ipapi = rows()[0]!
      expect(within(ipapi).getByText('VPN 否')).toBeInTheDocument()
      expect(ipapi.querySelector('.ipq-score__bar')).toBeNull()

      const ipquery = rows()[1]!
      expect(within(ipquery).getByText('VPN 是')).toBeInTheDocument()
      expect(within(ipquery).getByText('Abuse 未知')).toBeInTheDocument()
      expect(within(ipquery).getByText('Robot 来源错误')).toBeInTheDocument()
      expect(rows()[2]).toHaveTextContent('查询失败')

      const toggle = within(ipapi).getByRole('button', { name: '展开 ipapi.is 的另外 1 个样本' })
      expect(toggle).toHaveAttribute('aria-expanded', 'false')
      expect(toggle).not.toHaveAttribute('aria-controls')
      toggle.focus()
      fireEvent.click(toggle)
      // 同一按钮切换名称与展开状态，焦点不丢失；样本行是合法的兄弟 tr。
      expect(toggle).toHaveAccessibleName('收起 ipapi.is 的样本')
      expect(toggle).toHaveAttribute('aria-expanded', 'true')
      expect(document.activeElement).toBe(toggle)
      const sample = rows()[1]!
      expect(sample).toHaveClass('ipq-provider-table__sample')
      expect(sample).toHaveTextContent('匿名')
      // 匿名层不提供的风险字段是“不支持”，不是“否”。
      expect(within(sample).getByText('Proxy 来源不支持')).toBeInTheDocument()
      expect(screen.queryByText(/证据不完整|综合|纯净度/)).not.toBeInTheDocument()
    })

    it('renders type and location cells from each source field state, not one missing dash', () => {
      renderDashboard(productReport({
        providers: [{
          provider: 'ipwho.is', status: 'success', source_type: 'default', usage_type: '',
          details: details({
            region: '', city: '',
            field_states: { usage_type: 'error', company_type: 'unsupported', region: 'unsupported', city: 'unknown', latitude: 'error', longitude: 'error' },
          }),
        }],
      }))

      const providers = screen.getByRole('heading', { name: '风险评分与因子' }).closest('section') as HTMLElement
      const row = within(within(providers).getByRole('region', { name: '风险因子' })).getAllByRole('row')[1] as HTMLElement
      expect(within(row).getByText('使用类型 来源错误')).toBeInTheDocument()
      expect(within(row).getByText('公司类型 来源不支持')).toBeInTheDocument()
      // 没有分数的来源不造空评分行。
      expect(within(providers).queryByRole('list', { name: '风险评分' })).not.toBeInTheDocument()
      expect(providers).toHaveTextContent('来源未返回风险评分。')

      const location = screen.getByRole('heading', { name: '多源定位' }).closest('section') as HTMLElement
      const locationRow = within(location).getAllByRole('row')[1] as HTMLElement
      expect(locationRow).toHaveTextContent('US')
      expect(within(locationRow).getByText('省 / 州 来源不支持')).toBeInTheDocument()
      expect(within(locationRow).getByText('城市 未知')).toBeInTheDocument()
      expect(within(locationRow).getByText('经纬度 来源错误')).toBeInTheDocument()
      // 三种缺失状态各有字形，不画成同一个占位。
      expect(locationRow.querySelector('.ipq-cell--unsupported')).toHaveTextContent('—')
      expect(locationRow.querySelector('.ipq-cell--unknown')).toHaveTextContent('?')
      expect(locationRow.querySelector('.ipq-cell--error')).toHaveTextContent('!')
    })

    it('renders basic, ASN, location, mail and blacklist facts from the product view', () => {
      renderDashboard(productReport())

      const identity = screen.getByRole('region', { name: '基础信息' })
      // 默认只有六项，按固定顺序；ASN 与组织来自同一份 ASN 资料。
      const primary = identity.querySelector('.ipq-identity__primary') as HTMLElement
      expect(Array.from(primary.querySelectorAll('dt')).map((dt) => dt.textContent)).toEqual(['自治系统号', '组织', '城市', '注册地', '使用地', 'IP 类型'])
      expect(primary).toHaveTextContent('AS64500')
      expect(primary).toHaveTextContent('EXAMPLE-TRANSIT')
      expect(primary).not.toHaveTextContent('Example Transit')
      expect(primary).toHaveTextContent('US')
      // 两个来源城市不同：不挑选其一，只给逐源对照入口。
      expect(within(primary).getByRole('link', { name: '城市来源有分歧，查看多源定位' })).toHaveAttribute('href', '#ip-quality-location-title')
      // 来源没有真实注册国家，不拿 RDAP/ASN 国家冒充注册地；旧报告无 ip_type 时 IP 类型是“未知”，不按 geo_status 重分类。
      expect(within(primary).getByText('注册地').nextElementSibling).toHaveTextContent('未知')
      expect(within(primary).getByText('IP 类型').nextElementSibling).toHaveTextContent('未知')
      expect(primary).not.toHaveTextContent(/原生|广播/)
      // 其余事实在默认关闭的“更多信息”里，且不重复六项。
      const more = within(identity).getByText('更多信息').closest('details') as HTMLElement
      expect(more).not.toHaveAttribute('open')
      expect(more).toHaveTextContent('host.example.net')
      expect(more).toHaveTextContent('123 前缀 · 4,096 地址')
      expect(more).toHaveTextContent('机房')
      expect(more).toHaveTextContent('注册与使用地一致')
      expect(Array.from(more.querySelectorAll('dt')).map((dt) => dt.textContent)).not.toContain('自治系统号')

      const location = screen.getByRole('heading', { name: '多源定位' }).closest('section') as HTMLElement
      // 0 是合法坐标。
      expect(location).toHaveTextContent('0, 0')
      expect(location).toHaveTextContent('San Jose')

      const mail = screen.getByRole('heading', { name: '邮件连通性' }).closest('section') as HTMLElement
      expect(mail).toHaveTextContent('可连接 1/2')
      expect(within(mail).getByText('可连接')).toBeInTheDocument()
      expect(within(mail).getByText('超时')).toBeInTheDocument()
      expect(within(mail).queryByText(/gmail-smtp-in\.l\.google\.com/)).not.toBeInTheDocument()
      fireEvent.click(within(mail).getByRole('button', { name: 'Gmail MX 详情' }))
      expect(within(mail).getByText(/gmail-smtp-in\.l\.google\.com/)).toBeInTheDocument()
      // 技术值等宽；已知原因是中文正文，不用等宽。
      expect(within(mail).getByText(/gmail-smtp-in\.l\.google\.com/)).toHaveClass('mono')
      fireEvent.click(within(mail).getByRole('button', { name: 'QQ 邮箱 MX 详情' }))
      const qqReason = within(mail).getAllByText('原因').map((dt) => dt.nextElementSibling as HTMLElement).at(-1)!
      expect(qqReason).not.toHaveClass('mono')
      expect(qqReason.textContent).not.toBe('timeout')
      // 所有列头都声明列作用域，隐藏的详情列头保留名称。
      for (const name of ['邮件连通性', '黑名单', '多源定位']) {
        const headers = (screen.getByRole('heading', { name }).closest('section') as HTMLElement).querySelectorAll('thead th')
        expect(Array.from(headers).every((th) => th.getAttribute('scope') === 'col')).toBe(true)
      }

      const blacklist = screen.getByRole('heading', { name: '黑名单' }).closest('section') as HTMLElement
      expect(blacklist).toHaveTextContent('命中 1 / 已查 2')
      const drone = within(blacklist).getByText('DroneBL').closest('tr') as HTMLElement
      expect(drone).toHaveTextContent('已列入')
      const drop = within(blacklist).getByText('Spamhaus DROP').closest('tr') as HTMLElement
      expect(drop).toHaveTextContent('未知')
      expect(drop).not.toHaveTextContent('未列入')

      const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
      expect(within(services).getByLabelText('服务状态统计')).toHaveTextContent('5 可用1 部分0 受限1 未知')
      // 卡片默认只给短结论与地区；Web/App、样片明细都在折叠详情里。
      const chatgpt = within(services).getByText('ChatGPT').closest('li') as HTMLElement
      expect(within(chatgpt).getByText('部分可用')).toBeInTheDocument()
      const chatgptDetails = within(chatgpt).getByText('详情').closest('details') as HTMLElement
      expect(chatgptDetails).not.toHaveAttribute('open')
      expect(chatgptDetails).toHaveTextContent('Web · 可用')
      expect(chatgptDetails).toHaveTextContent('App · 未知')
      const netflix = within(services).getByText('Netflix').closest('li') as HTMLElement
      expect(within(netflix).getByText('可用 · US')).toBeInTheDocument()
      expect(within(netflix).getByText('详情').closest('details')).toHaveTextContent('样片 70143836 · 受限')
    })

    it('compares typed country codes, not a source-local country name, in identity and location', () => {
      // 复现实网 Hostcram IPv4 报告形态：四个来源 typed country_code=US，ipapi.is 只给名称 United States。
      const located = (provider: string): IPQualityProviderResult => ({
        provider, status: 'success', source_type: 'default', region_code: 'US', region_name: 'United States',
        details: details({ region: 'Wyoming', city: 'Buffalo' }),
      })
      renderDashboard(productReport({
        providers: [
          located('ip2location.io'),
          // 真实报告中该行没有 region_code，只有 typed 空代码（状态 unknown）与名称。
          {
            provider: 'ipapi.is', status: 'success', source_type: 'default', region_name: 'United States',
            details: details({ region: 'Wyoming', city: 'Buffalo', country_code: '', field_states: { country_code: 'unknown' } }),
          },
          located('ipquery.io'),
          located('ipwho.is'),
          located('proxycheck.io'),
        ],
        summary: { ...productSummary(), geo_status: 'unknown' },
      }))

      const identity = screen.getByRole('region', { name: '基础信息' })
      expect(within(identity).queryAllByRole('link', { name: /来源有分歧/ })).toHaveLength(0)
      expect(identity).toHaveTextContent('US')
      expect(identity).toHaveTextContent('Wyoming')
      expect(identity).toHaveTextContent('Buffalo')
      // 服务端 geo_status 仍为 unknown：不因来源一致而贴原生/广播标签。
      expect(identity).not.toHaveTextContent(/原生|广播|地区分歧/)

      const location = screen.getByRole('heading', { name: '多源定位' }).closest('section') as HTMLElement
      expect(location).not.toHaveTextContent('来源有分歧')
      const ipapi = within(location).getByText('ipapi.is').closest('tr') as HTMLTableRowElement
      // 名称原样显示，不被换成其他来源的代码，也不查表补成 US。
      expect(ipapi.cells[1]?.textContent).toBe('United States')
    })

    it('still shows a genuine typed country disagreement in identity and location', () => {
      renderDashboard(productReport({
        providers: [
          { provider: 'ipwho.is', status: 'success', source_type: 'default', details: details({ country_code: 'US', city: 'Buffalo' }) },
          { provider: 'ipquery.io', status: 'success', source_type: 'default', details: details({ country_code: 'JP', city: 'Buffalo' }) },
        ],
      }))

      const identity = screen.getByRole('region', { name: '基础信息' })
      expect(within(identity).getByRole('link', { name: '使用地来源有分歧，查看多源定位' })).toHaveAttribute('href', '#ip-quality-location-title')
      expect(within(identity).queryByRole('link', { name: /城市来源有分歧/ })).not.toBeInTheDocument()
      const location = screen.getByRole('heading', { name: '多源定位' }).closest('section') as HTMLElement
      expect(location).toHaveTextContent('来源有分歧')
    })

    it.each([
      ['no positives', [], [], ''],
      ['flags only', ['vpn'], [], 'VPN'],
      ['high-risk sources only', [], ['ipquery.io'], '高风险来源 · ipquery.io'],
    ] as const)('keeps the risk headline to source positives (%s) while blacklist evidence stays separate', (_name, flags, sources, expected) => {
      const report = productReport()
      const summaryValue = { ...productSummary(), risk_flags: [...flags], high_risk_sources: [...sources] }
      renderDashboard({ ...report, summary: { ...report.summary!, product_summary: summaryValue }, product: { ...report.product!, summary: summaryValue } })
      const hits = screen.getByRole('group', { name: '风险命中' })
      if (expected) expect(hits).toHaveTextContent(expected)
      else expect(hits).toHaveTextContent(/^$/)
      // 无阳性时不给泛化的安全/阴性结论，也不退回旧版评级。
      expect(hits).not.toHaveTextContent(/未见|安全|干净|旧版评级/)
      // 黑名单独立区仍展示服务端 listed/checked 与证据。
      expect(screen.getByRole('heading', { name: '黑名单' }).closest('section')).toHaveTextContent('命中 1 / 已查 2')
    })

    it('gives every disagreeing field its own evidence from the selected primary samples only', () => {
      renderDashboard(productReport({
        providers: [
          // 同一供应商匿名 + 认证：只有认证主样本参与比较与展开，匿名样本的 FR/Anon Co 不出现。
          { provider: 'ipapi.is', status: 'success', source_type: 'default', usage_type: 'isp', company_type: 'isp', details: details({ method: 'agent_public', registered_country_code: 'fr', company_name: 'Anon Co', region: 'Anon', city: 'Anon City' }) },
          { provider: 'ipapi.is', status: 'success', source_type: 'default', usage_type: 'hosting', company_type: 'hosting', details: details({ method: 'center_api', api_tier: 'authenticated', registered_country_code: 'us', company_name: 'Alpha Hosting', region: 'Oregon', city: 'Portland' }) },
          { provider: 'ipquery.io', status: 'success', source_type: 'default', usage_type: 'isp', company_type: 'isp', details: details({ registered_country_code: 'DE', company_name: '  Beta Net  ', region: 'Hesse', city: 'Frankfurt' }) },
          // 失败与空值不参与。
          { provider: 'proxycheck.io', status: 'failure', source_type: 'default', error_code: 'timeout', details: details({ registered_country_code: 'CN', company_name: 'Gamma' }) },
          { provider: 'ipwho.is', status: 'success', source_type: 'default', details: details({ registered_country_code: ' ', company_name: '', region: 'Hesse', city: 'Frankfurt' }) },
        ],
      }))
      const identity = screen.getByRole('region', { name: '基础信息' })

      // 注册地：就近默认关闭的披露，列出供应商、方式与原值（代码统一大写）。
      const registration = identity.querySelector('summary[aria-label="查看注册地来源分歧"]')?.closest('details') as HTMLElement
      expect(registration).not.toHaveAttribute('open')
      const regRows = Array.from(registration.querySelectorAll('dl > div')).map((row) => row.textContent)
      expect(regRows).toEqual(['ipapi.is · 认证US', 'ipquery.io · 匿名DE'])
      expect(registration).not.toHaveTextContent(/FR|CN/)

      fireEvent.click(within(identity).getByText('更多信息'))
      const company = identity.querySelector('summary[aria-label="查看公司名称来源分歧"]')?.closest('details') as HTMLElement
      expect(Array.from(company.querySelectorAll('dl > div')).map((row) => row.textContent)).toEqual(['ipapi.is · 认证Alpha Hosting', 'ipquery.io · 匿名Beta Net'])
      expect(company).not.toHaveTextContent(/Anon Co|Gamma/)

      // 省州/城市指向多源定位；使用/公司类型指向风险因子；每个入口各自命名。
      expect(within(identity).getByRole('link', { name: '省 / 州来源有分歧，查看多源定位' })).toHaveAttribute('href', '#ip-quality-location-title')
      expect(within(identity).getByRole('link', { name: '城市来源有分歧，查看多源定位' })).toHaveAttribute('href', '#ip-quality-location-title')
      const usage = within(identity).getByRole('link', { name: '使用类型来源有分歧，查看风险因子' })
      expect(usage).toHaveAttribute('href', '#ip-quality-factors-title')
      expect(within(identity).getByRole('link', { name: '公司类型来源有分歧，查看风险因子' })).toHaveAttribute('href', '#ip-quality-factors-title')

      // 普通激活把焦点移到目标标题；修饰键点击（新窗口/标签）不劫持焦点。
      fireEvent.click(usage, { button: 0 })
      expect(document.activeElement).toBe(document.getElementById('ip-quality-factors-title'))
      const city = within(identity).getByRole('link', { name: '城市来源有分歧，查看多源定位' })
      city.focus()
      fireEvent.click(city, { button: 0, ctrlKey: true })
      expect(document.activeElement).toBe(city)
      fireEvent.click(city, { button: 0 })
      expect(document.activeElement).toBe(document.getElementById('ip-quality-location-title'))
    })

    it('offers no disagreement entry when values agree, are missing, or only names differ', () => {
      renderDashboard(productReport({
        providers: [
          { provider: 'ipwho.is', status: 'success', source_type: 'default', usage_type: 'hosting', company_type: 'hosting', details: details({ registered_country_code: 'us', company_name: 'Same Co' }) },
          { provider: 'ipquery.io', status: 'success', source_type: 'default', usage_type: 'hosting', company_type: 'hosting', details: details({ registered_country_code: 'US', company_name: ' Same Co ' }) },
          { provider: 'ipapi.is', status: 'success', source_type: 'default', details: details({ registered_country_code: '', company_name: '' }) },
        ],
      }))
      const identity = screen.getByRole('region', { name: '基础信息' })
      expect(identity.querySelectorAll('details.ipq-disagreement')).toHaveLength(0)
      expect(within(identity).queryAllByRole('link', { name: /来源有分歧/ })).toHaveLength(0)
      expect(within(identity).getByText('注册地').nextElementSibling).toHaveTextContent('US')
    })

    it('keeps Agent facts visible while Center enrichment is still running', () => {
      const running = productReport({
        state: 'running',
        completed_at: null,
        blacklists: [],
        registration: { ...productView().registration, status: 'unknown' },
        asn: { ...productView().asn, status: 'unknown' },
      })
      renderDashboard(running, { enrichment: { active: true, failed: false, retry: vi.fn() } })

      expect(screen.getByRole('status', { name: '补充资料状态' })).toHaveTextContent('查询中')
      const identity = screen.getByRole('region', { name: '基础信息' })
      // Agent 已知 ASN 仍展示；Center 资料未到只写短状态。
      expect(identity).toHaveTextContent('AS64500')
      expect(within(identity).getAllByText('查询中').length).toBeGreaterThan(0)
      expect(screen.getByRole('heading', { name: '黑名单' }).closest('section')).toHaveTextContent('查询中')
      expect(screen.getByRole('heading', { name: '邮件连通性' }).closest('section')).toHaveTextContent('可连接 1/2')
    })

    it('offers one retry when the enrichment refresh fails', () => {
      const retry = vi.fn()
      renderDashboard(productReport({ state: 'running', completed_at: null }), { enrichment: { active: true, failed: true, retry } })
      const status = screen.getByRole('status', { name: '补充资料状态' })
      expect(status).toHaveTextContent('资料刷新失败')
      fireEvent.click(within(status).getByRole('button', { name: '重试' }))
      expect(retry).toHaveBeenCalledTimes(1)
    })

    it('keeps the anonymous sample primary and marks a failed Center enhancement; failure-only vendors stay visible', () => {
      const failedCenter = (provider: string) => ({
        provider, status: 'failure', source_type: 'default', error_code: 'rate_limited',
        details: details({ method: 'center_api', api_tier: 'authenticated', country_code: '', region: '', asn: '', company_name: '' }),
      })
      const view = productView()
      renderDashboard(productReport({
        providers: [view.providers[0]!, view.providers[1]!, failedCenter('ipapi.is'), failedCenter('abuseipdb')],
      }))
      // 外层 section 与内部滚动区同名；与其它来源表用例一致，先限定到 section 再取滚动区。
      const providers = screen.getByRole('heading', { name: '风险评分与因子' }).closest('section') as HTMLElement
      const region = within(providers).getByRole('region', { name: '风险因子' })
      const rows = within(region).getAllByRole('row').slice(1) as HTMLTableRowElement[]
      expect(rows.map((row) => row.cells[0]?.textContent)).toEqual([
        'ipapi.is匿名增强查询失败另 1 个样本',
        'ipquery.io匿名',
        'abuseipdb认证',
      ])
      // 失败的认证样本不带事实，只显示短失败态。
      expect(rows[2]).toHaveTextContent('查询失败')
      expect(within(rows[1]!).queryByText('增强查询失败')).not.toBeInTheDocument()
    })

    it('treats a product belonging to another report as legacy', () => {
      renderDashboard(productReport({ report_id: 'ipq_other' }))
      expect(screen.getByText('旧版报告')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: '邮件连通性' }).closest('section')).toHaveTextContent('旧版报告无此项')
    })

    it('shows the server IP type and never turns an abuse contact country into the registered country', () => {
      const withIPType = (ipType: NonNullable<IPQualityProductSummary['ip_type']>) => productReport({ summary: { ...productSummary(), ip_type: ipType } })
      const typeFact = () => within(screen.getByRole('region', { name: '基础信息' })).getByText('IP 类型').nextElementSibling as HTMLElement
      const registeredFact = () => within(screen.getByRole('region', { name: '基础信息' })).getByText('注册地').nextElementSibling as HTMLElement

      // 服务端生产方向：country_code 是使用国，comparison_country_code 是注册国或滥用联系国家。
      // Hostcram 形态：MaxMind 失败，IPinfo 滥用联系国家 US 与使用国 US 一致 → 原生 IP。
      const first = renderDashboard(withIPType({
        status: 'native', basis: 'abuse_contact_country', provider: 'ipinfo', method: 'agent_public',
        country_code: 'US', comparison_country_code: 'US', upstream_commit: '2384a67c756eb35231f5982b34731e522be3653e', fork_revision: 1,
      }))
      // 滥用联系国家不进入“注册地”。
      expect(registeredFact()).toHaveTextContent('未知')
      first.unmount()

      // 不等国家、abuse 后备：注册地仍未知，不冒充联系国家 DE 或使用国 US。
      const abuse = renderDashboard(withIPType({ status: 'broadcast', basis: 'abuse_contact_country', provider: 'ipinfo', method: 'agent_public', country_code: 'US', comparison_country_code: 'DE' }))
      expect(typeFact()).toHaveTextContent('广播 IP')
      expect(registeredFact()).toHaveTextContent('未知')
      expect(registeredFact()).not.toHaveTextContent(/DE|US/)
      abuse.unmount()

      // 不等国家、真实注册国：注册地显示 comparison_country_code（JP），不是使用国 country_code（US）。
      const registered = renderDashboard(withIPType({ status: 'broadcast', basis: 'registered_country', provider: 'maxmind', method: 'center_api', country_code: 'US', comparison_country_code: 'JP' }))
      expect(typeFact()).toHaveTextContent('广播 IP')
      expect(registeredFact()).toHaveTextContent('JP')
      expect(registeredFact()).not.toHaveTextContent('US')
      registered.unmount()

      renderDashboard(withIPType({ status: 'conflict', basis: 'registered_country' }))
      expect(typeFact()).toHaveTextContent('来源冲突')
      expect(typeFact()).not.toHaveTextContent(/原生|广播/)
    })

    it('keeps scores on each source scale: zero is an empty bar and labels map to explicit semantic tones', () => {
      const score = (id: string, label: string, value: number) => ({ id, label: id, raw_value: String(value), unit: 'score', scope: 'ip' as const, direction: 'higher_risk' as const, source_label: label, value, min: 0, max: 100 })
      renderDashboard(productReport({
        providers: [{
          provider: 'scamalytics', status: 'success', source_type: 'default',
          details: details({ scores: [score('zero', 'Low', 0), score('high', 'High', 60), score('very', 'very high', 80), score('crit', 'CRITICAL', 95), score('odd', 'Suspicious', 50)] }),
        }],
      }))
      const rows = within(screen.getByRole('list', { name: '风险评分' })).getAllByRole('listitem')
      expect(rows).toHaveLength(5)
      // 0 分：保留轨道，不画最小宽度的填充。
      expect(rows[0]!.querySelector('.ipq-score__track')).not.toBeNull()
      expect(rows[0]!.querySelector('.ipq-score__fill')).toBeNull()
      expect(within(rows[0]!).getByText('低')).toBeInTheDocument()
      expect(within(rows[1]!).getByText('高')).toBeInTheDocument()
      expect(rows[1]!.querySelector('.ipq-score__fill--alert')).not.toBeNull()
      expect(within(rows[2]!).getByText('很高')).toBeInTheDocument()
      expect(within(rows[3]!).getByText('严重')).toBeInTheDocument()
      expect(rows[3]!.querySelector('.ipq-score__fill--critical')).not.toBeNull()
      // 未识别的来源等级保留原文、中性色，不猜高低。
      expect(within(rows[4]!).getByText('Suspicious')).toBeInTheDocument()
      expect(rows[4]!.querySelector('.ipq-score__fill--neutral')).not.toBeNull()
      // 不合成总分。
      expect(screen.queryByText(/综合|总分|平均/)).not.toBeInTheDocument()
    })

    it('shows fixed-detector service rows as short verdicts and keeps the other probe revisions intact', () => {
      const script = (service: string, rule: string, diagnostics: Array<Record<string, unknown>>) => ({
        evidence_kind: 'ipquality_script', probe_revision: 5, ip_version: 4, upstream_commit: '2384a67c756eb35231f5982b34731e522be3653e', fork_revision: 1, schema_revision: 1, rule, diagnostics,
        service,
      })
      const anonymous = { evidence_kind: 'anonymous_service_probe', probe_revision: 4, ip_version: 4, endpoints: [] }
      const body = productReport()
      body.latest_report = { ...body.latest_report!, diagnostics_json: { source_version: 'v2', service_probe_revision: 5 } }
      body.service_unlocks = [
        { service: 'youtube-premium', source: 'ipquality_script', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'premium_offer', extra_json: script('youtube-premium', 'youtube_premium_offer', [{ endpoint: 'youtube-premium', http_status: 200, outcome: 'positive' }]) },
        { service: 'amazon-prime-video', source: 'ipquality_script', status: 'unlocked', probe_status: 'success', region: 'US', unlock_type: 'territory_offer', extra_json: script('amazon-prime-video', 'prime_current_territory', [{ endpoint: 'amazon-prime-video', http_status: 200, outcome: 'positive' }]) },
        {
          service: 'netflix', source: 'ipquality_script', status: 'partial', probe_status: 'success', region: 'US', unlock_type: 'originals_only',
          extra_json: script('netflix', 'netflix_originals_only', [
            { endpoint: 'netflix-81280792', http_status: 200, outcome: 'restricted' },
            { endpoint: 'netflix-70143836', http_status: 200, outcome: 'restricted' },
            { endpoint: 'netflix-80057281', http_status: 200, outcome: 'positive' },
          ]),
        },
        {
          service: 'chatgpt', source: 'ipquality_script', status: 'unlocked', probe_status: 'success', region: 'US',
          extra_json: script('chatgpt', 'openai_cookie_region', [
            { endpoint: 'openai-cookie', http_status: 200, outcome: 'positive' },
            { endpoint: 'openai-ios', http_status: 403, outcome: 'unknown', error_code: 'client_access_denied' },
          ]),
        },
        { service: 'tiktok', source: 'tiktok_home_probe', status: 'unlocked', probe_status: 'success', region: 'US', extra_json: anonymous },
        { service: 'disney-plus', source: 'disney_default_probe', status: 'blocked', probe_status: 'success', extra_json: anonymous },
        { service: 'reddit', source: 'reddit_home_probe', status: 'blocked', probe_status: 'success', extra_json: anonymous },
      ]
      renderDashboard(body)

      const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
      const tile = (name: string) => within(services).getByText(name).closest('li') as HTMLElement
      expect(within(tile('YouTube Premium')).getByText('可用 · US')).toBeInTheDocument()
      expect(within(tile('Amazon Prime Video')).getByText('可用 · US')).toBeInTheDocument()
      expect(within(tile('Netflix')).getByText('仅自制剧 · US')).toBeInTheDocument()
      expect(within(tile('ChatGPT')).getByText('可用 · US')).toBeInTheDocument()
      expect(within(tile('TikTok')).getByText('可用 · US')).toBeInTheDocument()
      expect(within(tile('Disney+')).getByText('受限')).toBeInTheDocument()
      expect(within(tile('Reddit')).getByText('受限')).toBeInTheDocument()
      // 新规则不贴“旧版规则”等标记。
      expect(within(services).getByLabelText('服务状态统计')).not.toHaveTextContent(/旧版规则|探测已停用|来源未知/)

      // 默认卡片不展示规则、入口或“未知”长句；iOS 入口风控在详情里如实保留，不宣称 App 实测。
      const chatgpt = tile('ChatGPT')
      const chatgptDetails = within(chatgpt).getByText('详情').closest('details') as HTMLElement
      expect(chatgptDetails).not.toHaveAttribute('open')
      expect(chatgptDetails).toHaveTextContent('规则：openai_cookie_region')
      // HTTP 状态与原因合成一段，HTTP 403 只出现一次；入口、结果与规则不丢。
      expect(chatgptDetails).toHaveTextContent('openai-ios · HTTP 403 · 入口拒绝客户端访问 · 未知')
      expect(chatgptDetails.textContent?.match(/HTTP 403/g)).toHaveLength(1)
      expect(chatgpt).not.toHaveTextContent(/仅 Web|App 未知|未形成可靠业务结论/)
      const netflixDetails = within(tile('Netflix')).getByText('详情').closest('details') as HTMLElement
      expect(netflixDetails).toHaveTextContent('范围：仅自制剧可访问')
      expect(netflixDetails).toHaveTextContent('netflix-80057281 · HTTP 200 · 可用')
      expect(netflixDetails).toHaveTextContent('netflix-81280792 · HTTP 200 · 受限')
    })
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

  it('offers immediate collection from the header through the scope menu', () => {
    const collect = controller()
    renderDashboard(report(), { collect })

    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '采集 IPv4 / IPv6' }))
    expect(collect.start).toHaveBeenCalledWith([4, 6])
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
    expect(screen.getByRole('status')).toHaveTextContent('正在采集 IPv4 / IPv6')
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
    // 历史报告同样提供采集入口；完成后不切走历史（见采集完成用例）。
    expect(screen.getByRole('button', { name: '立即采集' })).toBeInTheDocument()
  })

  it.each([
    ['ambiguous', '归属需复核'],
    ['ip_conflict', '来源 IP 冲突'],
  ] as const)('marks %s attribution with a short header mark and keeps the source rows', (blocker, mark) => {
    renderDashboard(report(blocker === 'ambiguous'
      ? { summary: { ...summary, ambiguous: true } }
      : { latest_report: { ...report().latest_report!, diagnostics_json: { source_version: 'v2', ip_conflict: true } } }))
    expect(screen.getByText(mark)).toBeInTheDocument()
    const providers = screen.getByRole('heading', { name: '风险评分与因子' }).closest('section') as HTMLElement
    expect(within(within(providers).getByRole('region', { name: '风险因子' })).getByText('ipinfo')).toBeInTheDocument()
  })

  it('marks stale reports with a short expired mark', () => {
    renderDashboard(report({ summary: { ...summary, stale: true, status: 'partial' } }))
    expect(screen.getByText('已过期')).toBeInTheDocument()
    expect(screen.getByText('部分采集')).toBeInTheDocument()
  })

  it.each([
    ['missing revision on a default report', { source_version: 'v2' }, false, '旧版规则'],
    ['revision 1 with skipped default probes', { source_version: 'v2', service_probe_revision: 1 }, true, '探测已停用'],
    ['revision 3 anonymous probes', { source_version: 'v2', service_probe_revision: 3 }, false, null],
    ['revision 4 anonymous probes', { source_version: 'v2', service_probe_revision: 4 }, false, null],
  ] as const)('marks service rules for %s without a credibility paragraph', (_label, diagnostics, skipped, mark) => {
    renderDashboard(report({
      latest_report: { ...report().latest_report!, diagnostics_json: diagnostics },
      service_unlocks: [
        skipped
          ? { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' }
          : { service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success' },
      ],
    }))
    const stats = screen.getByLabelText('服务状态统计')
    if (mark) expect(stats).toHaveTextContent(mark)
    else expect(stats).not.toHaveTextContent(/旧版规则|探测已停用|来源未知/)
    expect(screen.queryByRole('note', { name: '服务探测可信度' })).not.toBeInTheDocument()
  })

  it('marks custom rows without any source as unverifiable but leaves named custom sources alone', () => {
    const { unmount } = renderDashboard(report({
      latest_report: { ...report().latest_report!, diagnostics_json: {} },
      service_unlocks: [
        { service: 'custom-one', source: '', status: 'unlocked', probe_status: 'success' },
        { service: 'custom-two', status: 'blocked', probe_status: 'success' },
      ],
    }))
    expect(screen.getByLabelText('服务状态统计')).toHaveTextContent('来源未知')
    unmount()

    renderDashboard(report({
      latest_report: { ...report().latest_report!, diagnostics_json: {} },
      service_unlocks: [{ service: 'internal-api', source: 'internal_custom_probe', status: 'unlocked', probe_status: 'success' }],
    }))
    expect(screen.getByLabelText('服务状态统计')).not.toHaveTextContent('来源未知')
  })

  it('translates invalid_response error code into Chinese in service card', () => {
    renderDashboard(report({
      service_unlocks: [
        { service: 'custom-service', source: 'custom_probe', status: 'unknown', probe_status: 'failure', error_code: 'invalid_response', error_summary: 'service response did not establish a business conclusion' },
      ],
    }))

    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    expect(services).toHaveTextContent('原因：响应未形成可靠业务结论')
    expect(within(services).queryByText('service response did not establish a business conclusion')).not.toBeInTheDocument()
  })

  it('shows trusted page geography and endpoint diagnostics without changing service status', () => {
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
    const services = screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement
    const youtube = within(services).getByText('YouTube Premium').closest('li') as HTMLElement
    expect(youtube).toHaveClass('ipq-service--unknown')
    expect(within(youtube).getByText('未知')).toBeInTheDocument()
    expect(youtube).toHaveTextContent('页面地区 CN')
    // 残留 normalized region 不进入卡片。
    expect(youtube).not.toHaveTextContent('US')
    expect(within(services).getByLabelText('服务状态统计')).toHaveTextContent('0 可用1 部分0 受限2 未知')
    const reddit = within(services).getByText('Reddit').closest('li') as HTMLElement
    expect(reddit).toHaveTextContent('服务拒绝了探测请求（HTTP 403）')
    // 入口证据在卡片详情里按需展开。
    const chatgpt = within(services).getByText('ChatGPT').closest('li') as HTMLElement
    expect(within(chatgpt).getByText('详情').closest('details')).not.toHaveAttribute('open')
    expect(chatgpt).toHaveTextContent('web_cookie_requirements · HTTP 200 · 可用 · openai_cookie_requirements')
    expect(chatgpt).not.toHaveTextContent('服务地区未知')

    const diagnostics = screen.getByLabelText('采集诊断')
    const chatgptRow = within(diagnostics).getByText('openai_status_probe').closest('tr') as HTMLElement
    expect(chatgptRow).toHaveTextContent('app_home · 网络不可达 · 未知')
    unmount()

    // 证据族与所选报告族不一致时不展示页面地区。
    renderDashboard(ipv6Report(4))
    const mismatched = within(screen.getByRole('heading', { name: '服务解锁' }).closest('section') as HTMLElement)
      .getByText('YouTube Premium').closest('li') as HTMLElement
    expect(mismatched).not.toHaveTextContent('页面地区')
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
      expect(family).toHaveTextContent('最近尝试')
      expect(screen.getByRole('group', { name: '报告状态' })).toHaveTextContent('上次有效结果')
      // 失败尝试不进入事实：身份仍是上次有效 IPv4 出口。
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('192.0.2.1')
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
      // 该族从未有过报告：不宣称保留了历史结果。
      expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent(/保留|上一份|仍展示/)
      // 空态的原因与最近尝试只在正文说明一次，上下文不重复；身份只写“暂无报告”。
      expect(screen.queryByRole('note', { name: '地址族采集状态' })).not.toBeInTheDocument()
      expect(document.querySelector('.page-state')).toHaveTextContent('最近一次采集未检测到 IPv6 可用本地地址')
      expect(document.querySelector('.page-state')).toHaveTextContent('最近尝试')
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('暂无报告')
      expect(screen.queryByRole('heading', { name: '风险评分与因子' })).not.toBeInTheDocument()
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
      expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    })
  })

  describe('report context and empty-state recovery', () => {
    function renderView(view: IPQualityDashboardView, { collect = controller(), initialEntry = '/vps/vps_001/ip-quality?ip_version=4' }: { collect?: IPQualityCollectController, initialEntry?: string } = {}) {
      const viewingHistory = new URL(initialEntry, 'http://local').searchParams.has('report_id')
      return render(
        <MemoryRouter initialEntries={[initialEntry]}>
          <Routes>
            <Route
              path="/vps/:vpsId/ip-quality"
              element={<IPQualityDashboard view={view} detailPath="/vps/vps_001" collect={collect} viewingHistory={viewingHistory} refreshState="idle" onSelectFamily={vi.fn()} />}
            />
          </Routes>
        </MemoryRouter>,
      )
    }
    type Family = VPSIPQualityReport['families'][number]
    type Attempt = NonNullable<Family['latest_attempt']>
    const attempt = (patch: Partial<Attempt> = {}): Attempt => ({
      observed_at: '2026-06-09T12:00:00Z', stale: false, report_status: 'failure', collection_scope: 'dual_stack', collection_id: 'col_new', ...patch,
    })
    const emptyReport = (availability: string, latest: Attempt | null): VPSIPQualityReport => report({
      summary: null, latest_report: null, provider_results: [], service_unlocks: [], history: [],
      families: [
        { ip_version: 4, availability, latest_attempt: latest, latest_report_summary: null },
        { ip_version: 6, availability: 'unknown', latest_attempt: null, latest_report_summary: null },
      ],
    })
    const description = () => document.querySelector('.page-state__description')
    const marks = () => screen.getByRole('group', { name: '报告状态' })

    it('keeps the selector first and the identity structure in every non-report state without other-family facts', () => {
      const { unmount } = renderView({ kind: 'loading', ipVersion: 6 })
      const context = screen.getByRole('region', { name: '报告上下文' })
      expect(context.firstElementChild).toContainElement(screen.getByRole('group', { name: 'IP 地址族' }))
      expect(screen.getByLabelText('报告身份')).toHaveAttribute('aria-busy', 'true')
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('IPv6')
      // 加载时采集入口只是非交互占位。
      expect(screen.queryByRole('button', { name: '立即采集' })).not.toBeInTheDocument()
      expect(screen.queryByRole('status', { name: 'IP 质量采集状态' })).not.toBeInTheDocument()
      unmount()

      const retry = vi.fn()
      const errored = renderView({ kind: 'error', ipVersion: 4, error: 'temporary failure', onRetry: retry })
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('加载失败，报告状态未知')
      expect(screen.queryByRole('button', { name: '立即采集' })).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: '重试' }))
      expect(retry).toHaveBeenCalledTimes(1)
      errored.unmount()

      // 历史报告未解析时不猜族，也不给猜出来的“查看最新报告”链接。
      const history = renderView({ kind: 'loading', ipVersion: 6 }, { initialEntry: '/vps/vps_001/ip-quality?ip_version=6&report_id=ipq_000' })
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('待确定')
      expect(screen.queryByRole('link', { name: '查看最新报告' })).not.toBeInTheDocument()
      history.unmount()

      renderView({ kind: 'invalid_family' })
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('地址族参数无效')
      expect(screen.getByLabelText('报告身份')).toHaveTextContent('待确定')
    })

    it('marks history, stale, last-valid, pre-refresh and non-round reports independently', () => {
      const staleSummary = { ...summary, stale: true }
      const historyView = renderDashboard(report({ summary: staleSummary, history: [staleSummary] }), { initialEntry: '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_001' })
      expect(marks()).toHaveTextContent('历史报告')
      expect(marks()).toHaveTextContent('已过期')
      // 历史视图不受当前族尝试影响。
      expect(screen.queryByRole('note', { name: '地址族采集状态' })).not.toBeInTheDocument()
      expect(within(screen.getByRole('region', { name: '报告上下文' })).getByRole('link', { name: '查看最新报告' })).toHaveAttribute('href', '/vps/vps_001/ip-quality?ip_version=4')
      historyView.unmount()

      const plain = renderDashboard(report())
      expect(marks()).toHaveTextContent('所选报告')
      expect(marks()).not.toHaveTextContent(/实时|上次有效|刷新前|非本轮/)
      plain.unmount()

      const refreshFailed = renderDashboard(report(), { refreshState: 'failed' })
      expect(marks()).toHaveTextContent('刷新前报告')
      expect(marks()).not.toHaveTextContent('所选报告')
      refreshFailed.unmount()

      // 刷新失败后又开始一轮采集：进度条不带 reportContext，外壳不再补“刷新前报告”。
      const progress = renderDashboard(report(), {
        refreshState: 'failed',
        collect: controller({ active: true }, { enabled: true, available: true, request: { request_id: 'ipqc_p', monitoring_instance_id: 'mi_001', status: 'dispatched', requested_at: new Date().toISOString(), expires_at: new Date(Date.now() + 600_000).toISOString() } }),
      })
      expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集')
      expect(marks()).not.toHaveTextContent('刷新前报告')
      expect(marks()).toHaveTextContent('所选报告')
      progress.unmount()

      // 采集结果条自己给出的 reportContext 仍原样保留。
      const completedFailed: IPQualityCollectRequest = { request_id: 'ipqc_f', monitoring_instance_id: 'mi_001', status: 'completed', requested_at: new Date().toISOString(), expires_at: new Date().toISOString(), report_status: 'success', collection_scope: 'dual_stack', collection_id: 'col_new', family_results: [{ ip_version: 4, availability: 'available', report_status: 'success' }] }
      const explicit = renderDashboard(report(), { refreshState: 'failed', collect: controller({ watchedRequestId: 'ipqc_f' }, { enabled: true, available: true, request: completedFailed }) })
      expect(marks()).toHaveTextContent('刷新前报告')
      explicit.unmount()

      const request: IPQualityCollectRequest = { request_id: 'ipqc_x', monitoring_instance_id: 'mi_001', status: 'completed', requested_at: new Date().toISOString(), expires_at: new Date().toISOString(), report_status: 'success', collection_scope: 'dual_stack', collection_id: 'col_new', family_results: [{ ip_version: 4, availability: 'available', report_status: 'success' }] }
      renderDashboard(report(), { collect: controller({ watchedRequestId: 'ipqc_x' }, { enabled: true, available: true, request }) })
      expect(marks()).toHaveTextContent('非本轮结果')
    })

    it.each([
      ['never', 'unknown', null, 'IPv4 尚无采集结果。', '可以首次采集。'],
      ['fresh unavailable', 'unavailable', attempt({ error_code: 'address_family_unavailable' }), '最近一次采集未检测到 IPv4 可用本地地址。', '检查网络配置后可以重新检查。'],
      ['stale unavailable', 'unavailable', attempt({ stale: true, error_code: 'address_family_unavailable' }), '不代表现在', '可以重新确认。'],
      ['lookup failure', 'available', attempt({ error_code: 'lookup_failed' }), 'IPv4 出口探测未形成报告。', '可以重试。'],
      ['stale failure', 'available', attempt({ stale: true }), '不代表当前故障', '可以重试。'],
    ] as const)('explains the %s empty state with one recovery path', (_name, availability, latest, fact, recovery) => {
      renderView({ kind: 'empty', report: emptyReport(availability, latest), ipVersion: 4 })
      expect(description()).toHaveTextContent(fact)
      expect(description()).toHaveTextContent(recovery)
      expect(description()).not.toHaveTextContent(/自动|保留|上一份/)
      if (latest) expect(description()).toHaveTextContent('最近尝试')
      // 原因只在正文说明，上下文不重复。
      expect(screen.queryByRole('note', { name: '地址族采集状态' })).not.toBeInTheDocument()
      expect(screen.getAllByRole('button', { name: '立即采集' })).toHaveLength(1)
    })

    it('defers recovery to the collect status when collection is unavailable, unknown or already running', () => {
      const body = emptyReport('unavailable', attempt({ error_code: 'address_family_unavailable' }))
      const unavailable = renderView({ kind: 'empty', report: body, ipVersion: 4 }, { collect: controller({}, { enabled: false, available: false, unavailable_reason: 'disabled' }) })
      expect(description()).toHaveTextContent('最近一次采集未检测到 IPv4 可用本地地址')
      expect(description()).not.toHaveTextContent(/重新检查|自动上报/)
      expect(within(screen.getByRole('status', { name: 'IP 质量采集状态' })).getByRole('link', { name: '前往设置开启' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
      unavailable.unmount()

      const checking = renderView({ kind: 'empty', report: body, ipVersion: 4 }, { collect: controller({ checking: true }, null) })
      expect(description()).toHaveTextContent('正在检查采集条件')
      expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
      checking.unmount()

      // 状态读取已失败：不再说“正在检查”，给出可直接尝试的恢复口径，按钮可用（由 POST 给出明确结果）。
      const readFailed = renderView({ kind: 'empty', report: body, ipVersion: 4 }, { collect: controller({}, null) })
      expect(description()).not.toHaveTextContent('正在检查')
      expect(description()).toHaveTextContent('检查网络配置后可以重新检查')
      expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled()
      readFailed.unmount()

      const running = (versions: number[]) => controller({ active: true }, {
        enabled: true, available: true,
        request: { request_id: 'ipqc_r', monitoring_instance_id: 'mi_001', status: 'dispatched', requested_at: new Date().toISOString(), expires_at: new Date(Date.now() + 600_000).toISOString(), requested_ip_versions: versions },
      })
      const covering = renderView({ kind: 'empty', report: body, ipVersion: 4 }, { collect: running([4, 6]) })
      expect(description()).toHaveTextContent('正在采集，完成后将刷新本族结果；是否形成报告以采集结果为准。')
      expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled()
      covering.unmount()

      renderView({ kind: 'empty', report: body, ipVersion: 4 }, { collect: running([6]) })
      expect(description()).toHaveTextContent('本轮未包含当前地址族，结束后可选择本族采集。')
      expect(description()).not.toHaveTextContent('完成后将刷新本族结果')
    })
  })
})
