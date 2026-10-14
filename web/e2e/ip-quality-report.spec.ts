import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import type {
  IPQualityCollectStatus,
  IPQualityCoverage,
  IPQualityProviderResult,
  IPQualityServiceUnlock,
  IPQualitySummary,
  VPSIPQualityReport,
} from '../src/lib/types'
import { expect, test } from './fixtures'
import { apiRouteKey, type ApiFixtureProfile } from './fixtures/contracts'
import { authenticatedProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

const REPORT_PATH = '/api/vps/vps_001/ip-quality'
const COLLECT_PATH = '/api/vps/vps_001/ip-quality/collect'

// 默认采集固定启用的五个数据库；其余十三个只作为未配置诊断行存在，不是失败请求。
const NOT_CONFIGURED_PROVIDERS: readonly IPQualityProviderResult[] = [
  'maxmind', 'ipinfo', 'ipregistry', 'ipdata', 'ipqualityscore', 'abuseipdb', 'scamalytics',
  'ipapi.co', 'ip-api.com', 'db-ip', 'ippure', 'meowvps', 'ip.net.coffee',
].map((provider) => ({
  provider,
  status: 'not_configured',
  source_type: 'optional',
  error_code: 'not_configured',
  error_summary: 'optional IP quality source requires configuration or is not enabled for default agent collection',
}))

// 原始覆盖保持 18 个来源（13 个未配置）与 7 项服务的计数合同。
const RAW_COVERAGE_BASE = {
  expected_provider_count: 18,
  skipped_provider_count: 0,
  not_configured_provider_count: 13,
  expected_service_count: 7,
  not_configured_service_count: 0,
} as const

// 与 agent 当前实际输出一致的五个启用来源：ip2location.io 本轮失败，其余四个成功。
// 失败行故意残留 VPN/高风险字段，页面不得把它们算进风险结论。
function currentProviders(): IPQualityProviderResult[] {
  return [
    {
      provider: 'ipapi.is', status: 'success', source_type: 'default', latency_ms: 412,
      company_type: 'hosting', region_code: 'DE', region_name: 'Germany',
      extra_json: { company: { name: 'BAGE CLOUD LLC', type: 'hosting' }, asn: { asn: 63150, org: 'BAGE CLOUD LLC', type: 'hosting' }, capability: 'geo_only' },
    },
    {
      provider: 'ipquery.io', status: 'success', source_type: 'default', latency_ms: 268,
      risk_level: 'low', risk_score: '0', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: true,
      extra_json: { isp: { asn: 'AS63150', org: 'BAGE CLOUD LLC' }, capability: 'risk' },
    },
    {
      provider: 'proxycheck.io', status: 'success', source_type: 'default', latency_ms: 531,
      usage_type: 'Hosting', risk_level: 'low', risk_score: '33', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: true, is_robot: false,
      extra_json: { api_version: '24-June-2026', compromised: false, anonymous: false, confidence: 0, capability: 'risk' },
    },
    {
      provider: 'ipwho.is', status: 'success', source_type: 'default', latency_ms: 377,
      region_code: 'DE', region_name: 'Germany',
      extra_json: { connection: { asn: 63150, org: 'BAGE CLOUD LLC', isp: 'BAGE CLOUD LLC' }, capability: 'geo_only' },
    },
    {
      provider: 'ip2location.io', status: 'failure', source_type: 'default', latency_ms: 5000,
      error_code: 'http_status', error_summary: 'http status 429',
      is_vpn: true, risk_level: 'high', risk_score: '91',
    },
    ...NOT_CONFIGURED_PROVIDERS,
  ]
}

function serviceEvidence(matchedRule: string, region: string, endpoints: Array<[string, number]>) {
  return {
    probe_revision: 2,
    evidence_kind: 'anonymous_service_probe',
    ...(matchedRule ? { matched_rule: matchedRule } : {}),
    http_status: endpoints[endpoints.length - 1]?.[1],
    ...(region ? { region } : {}),
    ip_version: 4,
    endpoints: endpoints.map(([endpoint, httpStatus]) => ({ endpoint, http_status: httpStatus })),
  }
}

// 免登录服务探测 revision 2：五项可用、ChatGPT 仅 Web 已验证、Disney+ 遇到人机验证。
// Disney+ 失败行故意残留 blocked/US/范围，页面只能显示未知。
function currentServices(): IPQualityServiceUnlock[] {
  return [
    { service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1840, region: 'DE', unlock_type: 'catalog_sample', extra_json: serviceEvidence('netflix_title_catalog', 'DE', [['title_81280792', 200], ['title_70143836', 200]]) },
    { service: 'chatgpt', source: 'openai_status_probe', status: 'partial', probe_status: 'success', latency_ms: 960, unlock_type: 'web_verified_app_unknown', extra_json: serviceEvidence('openai_web_positive_app_unknown', '', [['web_cookie_requirements', 200], ['app_home', 403]]) },
    { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1210, region: 'DE', unlock_type: 'premium_offer', extra_json: serviceEvidence('youtube_premium_region_offer', 'DE', [['premium', 200]]) },
    { service: 'amazon-prime-video', source: 'prime_video_page_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1490, region: 'DE', unlock_type: 'territory_offer', extra_json: serviceEvidence('prime_current_territory', 'DE', [['home', 200]]) },
    { service: 'disney-plus', source: 'disney_default_probe', status: 'blocked', probe_status: 'failure', latency_ms: 2210, region: 'US', unlock_type: 'supported_location', error_code: 'challenge_detected', error_summary: 'service challenge detected', extra_json: serviceEvidence('', '', [['devices', 403]]) },
    { service: 'tiktok', source: 'tiktok_home_probe', status: 'unlocked', probe_status: 'success', latency_ms: 1720, region: 'DE', unlock_type: 'web_region', extra_json: serviceEvidence('tiktok_script_region', 'DE', [['home', 200]]) },
    { service: 'reddit', source: 'reddit_home_probe', status: 'unlocked', probe_status: 'success', latency_ms: 880, region: 'DE', unlock_type: 'anonymous_web', extra_json: serviceEvidence('reddit_shreddit_country', 'DE', [['anonymous_chat', 200]]) },
  ]
}

// 原始报告与 agent rawEnvelopeV2 同形：只有脱敏状态与安全证据，没有 HTML 采样。
function rawEnvelope(providers: IPQualityProviderResult[], services: IPQualityServiceUnlock[], diagnostics: Record<string, unknown>) {
  return {
    providers: Object.fromEntries(providers.map((row) => [row.provider, {
      status: row.status,
      ...(row.error_code ? { error_code: row.error_code } : {}),
      ...(row.error_summary ? { error_summary: row.error_summary } : {}),
      ...(row.status === 'success' ? { raw: { ip: '95.169.166.229', ...(row.extra_json as Record<string, unknown>) } } : {}),
    }])),
    services: Object.fromEntries(services.map((row) => [row.service, {
      status: row.probe_status,
      ...(row.error_code ? { error_code: row.error_code } : {}),
      ...(row.error_summary ? { error_summary: row.error_summary } : {}),
      raw: row.extra_json,
    }])),
    diagnostics,
  }
}

// 历史报告与当前报告的身份字段相同，只差报告编号、时间、状态和覆盖。
function summaryFor(reportId: string, observedAt: string, status: string, riskLevel: string, reportCoverage: IPQualityCoverage, providerCount: number): IPQualitySummary {
  return {
    report_id: reportId,
    vps_id: 'vps_001',
    observed_at: observedAt,
    ip_address: '95.169.166.229',
    ip_version: 4,
    status,
    risk_level: riskLevel,
    use_region_code: 'DE',
    use_region_name: 'Germany',
    asn: 'AS63150',
    organization: 'BAGE CLOUD LLC',
    stale: false,
    ambiguous: false,
    assignment_mode: 'link',
    provider_count: providerCount,
    unlockable_count: 7,
    coverage: reportCoverage,
  }
}

function latestFor(summary: IPQualitySummary, receivedAt: string, diagnostics: Record<string, unknown>, raw: unknown): NonNullable<VPSIPQualityReport['latest_report']> {
  return {
    report_id: summary.report_id!,
    monitoring_instance_id: 'mi_001',
    observed_at: summary.observed_at,
    received_at: receivedAt,
    agent_version: 'v1.1.0',
    fingerprint: 'fp-001',
    sync_batch_id: 'sync_001',
    ip_address: summary.ip_address,
    ip_version: 4,
    status: summary.status,
    latitude: 50.11552,
    longitude: 8.68417,
    is_backfilled: false,
    created_at: receivedAt,
    ...(summary.coverage !== undefined ? { coverage: summary.coverage } : {}),
    diagnostics_json: diagnostics,
    raw_json: raw,
  }
}

// 来源不明修订的旧报告：多数数据库只给地区或什么都不给，原始报告里带大段 HTML 采样。
function legacyProviders(): IPQualityProviderResult[] {
  const empty = ['abuseipdb', 'db-ip', 'ipapi.co', 'ip-api.com', 'ipdata', 'ipgeolocation', 'ipregistry', 'iplocation', 'ipwho.is', 'ipquery.io', 'ip2proxy', 'ipwhois']
  return [
    { provider: 'ip2location.io', status: 'success', source_type: 'default', region_code: 'DE', is_proxy: false },
    { provider: 'ipapi.is', status: 'success', source_type: 'default', company_type: 'BAGE CLOUD LLC', region_name: 'Germany', is_proxy: true, is_vpn: true, is_server: true, is_tor: false, is_abuser: false },
    { provider: 'proxycheck.io', status: 'success', source_type: 'default', usage_type: 'hosting', risk_level: 'low', risk_score: '12', is_proxy: false, is_vpn: false, is_server: true },
    { provider: 'scamalytics', status: 'failure', source_type: 'default', error_code: 'http_status', error_summary: 'http status 429' },
    { provider: 'maxmind', status: 'not_configured', source_type: 'optional', error_code: 'not_configured' },
    ...empty.map((provider) => ({ provider, status: 'success', source_type: 'default' }) satisfies IPQualityProviderResult),
  ]
}

const HTML_SAMPLE = `<!DOCTYPE html><html lang="en"><head>${'<meta name="viewport" content="width=device-width">'.repeat(200)}</head></html>`

export function legacyReportFixture(): VPSIPQualityReport {
  const summary = summaryFor('ipq_hist_legacy', '2026-09-30T06:56:00Z', 'success', 'low', {
    expected_provider_count: 17,
    successful_provider_count: 15,
    failed_provider_count: 1,
    skipped_provider_count: 0,
    not_configured_provider_count: 1,
    expected_service_count: 7,
    successful_service_count: 5,
    failed_service_count: 1,
    skipped_service_count: 1,
    not_configured_service_count: 0,
  }, 17)
  return {
    summary,
    latest_report: latestFor(summary, '2026-09-30T06:56:05Z', { source_version: 'v2' }, {
      services: { reddit: { raw: { body_sample: HTML_SAMPLE, http_status: 200 } }, tiktok: { raw: { body_sample: HTML_SAMPLE } } },
    }),
    provider_results: legacyProviders(),
    service_unlocks: [
      { service: 'netflix', source: 'netflix_title_probe', status: 'unlocked', probe_status: 'success', region: 'DE', unlock_type: 'originals' },
      { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', probe_status: 'success', region: 'DE' },
      { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unlocked', probe_status: 'success', region: 'DE' },
      { service: 'amazon-prime-video', source: 'prime_video_page_probe', status: 'blocked', probe_status: 'success' },
      { service: 'disney-plus', source: 'disney_default_probe', status: 'blocked', probe_status: 'success' },
      { service: 'tiktok', source: 'tiktok_home_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' },
      { service: 'reddit', source: 'reddit_home_probe', status: 'unknown', probe_status: 'failure', error_code: 'http_status', error_summary: 'http status 403' },
    ],
    history: [],
  }
}

// revision 1 历史报告：服务探测停用（七项 skipped），数据库矩阵完整且有供应商高风险，
// 摘要却仍是旧规则留下的 low，页面必须以所选报告自己的来源事实为准。
function revision1Providers(): IPQualityProviderResult[] {
  return [
    {
      provider: 'ipapi.is', status: 'success', source_type: 'default', company_type: 'hosting', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: true, is_tor: false, is_server: true, is_abuser: false, is_robot: false,
    },
    {
      provider: 'proxycheck.io', status: 'success', source_type: 'default', usage_type: 'Business', risk_level: 'high', risk_score: '82', region_code: 'DE',
      is_proxy: true, is_vpn: true, is_tor: false, is_server: true,
    },
    {
      provider: 'ipquery.io', status: 'success', source_type: 'default', risk_level: 'low', risk_score: '0', region_code: 'DE', region_name: 'Germany',
      is_proxy: false, is_vpn: false, is_tor: false, is_server: true,
    },
    { provider: 'ipwho.is', status: 'success', source_type: 'default', region_code: 'DE', region_name: 'Germany' },
    { provider: 'ip2location.io', status: 'success', source_type: 'default', region_code: 'DE', region_name: 'Germany' },
    ...NOT_CONFIGURED_PROVIDERS,
  ]
}

const UNSUPPORTED_DEFAULT_PROBE = 'safe default probe is not available without verified business evidence'
const SERVICE_SOURCES: Array<[string, string]> = [
  ['netflix', 'netflix_title_probe'],
  ['chatgpt', 'openai_status_probe'],
  ['youtube-premium', 'youtube_premium_page_probe'],
  ['amazon-prime-video', 'prime_video_page_probe'],
  ['disney-plus', 'disney_default_probe'],
  ['tiktok', 'tiktok_home_probe'],
  ['reddit', 'reddit_home_probe'],
]

export function revision1ReportFixture(): VPSIPQualityReport {
  const providers = revision1Providers()
  const services = SERVICE_SOURCES.map(([service, source]) => ({
    service, source, status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: UNSUPPORTED_DEFAULT_PROBE,
  }) satisfies IPQualityServiceUnlock)
  const summary = summaryFor('ipq_hist_rev1', '2026-10-01T12:00:00Z', 'success', 'low', {
    ...RAW_COVERAGE_BASE,
    successful_provider_count: 5,
    failed_provider_count: 0,
    successful_service_count: 0,
    failed_service_count: 0,
    skipped_service_count: 7,
  }, 18)
  const diagnostics = { source_version: 'v2', service_probe_revision: 1 }
  return {
    summary,
    latest_report: latestFor(summary, '2026-10-01T12:00:05Z', diagnostics, rawEnvelope(providers, services, diagnostics)),
    provider_results: providers,
    service_unlocks: services,
    history: [],
  }
}

export function reportFixture(): VPSIPQualityReport {
  const providers = currentProviders()
  const services = currentServices()
  // 一个启用来源失败、一个服务探测失败 → agent 保留 partial。
  const summary = summaryFor('ipq_0b581f3fc74fc31f', '2026-10-03T12:00:00Z', 'partial', 'low', {
    ...RAW_COVERAGE_BASE,
    successful_provider_count: 4,
    failed_provider_count: 1,
    successful_service_count: 6,
    failed_service_count: 1,
    skipped_service_count: 0,
  }, 18)
  const diagnostics = {
    source_version: 'v2',
    collection_revision: 3,
    service_probe_revision: 2,
    elapsed_ms: 6840,
    ip_address_family: 4,
    discovery_ip_candidates: { 'ipapi.is': '95.169.166.229', 'ipquery.io': '2a0e:97c0:3e3:4bc::1' },
    ip_candidates: { 'ipapi.is': '95.169.166.229', 'ipquery.io': '95.169.166.229', 'proxycheck.io': '95.169.166.229', 'ipwho.is': '95.169.166.229' },
  }
  const revision1 = revision1ReportFixture().summary!
  const legacy = legacyReportFixture().summary!
  return {
    summary,
    latest_report: latestFor(summary, '2026-10-03T12:00:05Z', diagnostics, rawEnvelope(providers, services, diagnostics)),
    provider_results: providers,
    service_unlocks: services,
    history: [
      summary,
      revision1,
      legacy,
      ...Array.from({ length: 5 }, (_, index) => ({
        ...revision1,
        report_id: `ipq_hist_${index + 3}`,
        observed_at: new Date(Date.parse(legacy.observed_at) - (index + 1) * 86_400_000).toISOString(),
        risk_level: index % 2 === 0 ? 'low' : 'medium',
      })),
    ],
  }
}

const AVAILABLE: IPQualityCollectStatus = { enabled: true, available: true, monitoring_instance_id: 'mi_001' }
const EMPTY_REPORT: VPSIPQualityReport = { summary: null, latest_report: null, provider_results: [], service_unlocks: [], history: [] }

function collectRequest(status: 'pending' | 'dispatched' | 'completed') {
  const now = Date.now()
  return {
    request_id: 'ipqc_e2e',
    monitoring_instance_id: 'mi_001',
    status,
    requested_at: new Date(now - 5_000).toISOString(),
    expires_at: new Date(now + 600_000).toISOString(),
    ...(status === 'completed' ? { completed_at: new Date(now).toISOString(), report_status: 'success' } : {}),
  }
}

export function reportProfile(report: VPSIPQualityReport = reportFixture(), collect: IPQualityCollectStatus = AVAILABLE, extra: ApiFixtureProfile = {}): ApiFixtureProfile {
  return authenticatedProfile({
    [apiRouteKey('GET', REPORT_PATH)]: { status: 200, body: report },
    [apiRouteKey('GET', `${REPORT_PATH}/reports/ipq_hist_rev1`)]: { status: 200, body: revision1ReportFixture() },
    [apiRouteKey('GET', `${REPORT_PATH}/reports/ipq_hist_legacy`)]: { status: 200, body: legacyReportFixture() },
    [apiRouteKey('GET', COLLECT_PATH)]: { status: 200, body: collect },
    ...extra,
  })
}

function profile(report: VPSIPQualityReport, collect: IPQualityCollectStatus, extra: ApiFixtureProfile = {}): ApiFixtureProfile {
  return reportProfile(report, collect, extra)
}

async function expectNoBlockingAxe(page: Page) {
  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({ id: violation.id, impact: violation.impact, targets: violation.nodes.map((node) => node.target) }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
}

const VIEWPORTS = [
  { width: 1440, height: 1000 },
  { width: 1024, height: 768 },
  { width: 390, height: 844 },
] as const

const ANONYMOUS_PROBE_NOTICE = '免登录服务探测，仅反映当前入口和地区可用性，不代表账号或付费内容权限。'

for (const viewport of VIEWPORTS) {
  test(`IP quality report separates database risk, anonymous services and coverage at ${viewport.width}px`, async ({ api, page }, testInfo) => {
    api.useProfile(profile(reportFixture(), AVAILABLE))
    await page.setViewportSize(viewport)
    await page.goto('/vps/vps_001/ip-quality')

    await expect(page.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeVisible()
    await expect(page.getByLabel('报告身份')).toContainText('95.169.166.229')
    await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()

    // revision 2：中性范围说明，不再出现停用警示。
    const notice = page.getByRole('note', { name: '服务探测可信度' })
    await expect(notice).toContainText(ANONYMOUS_PROBE_NOTICE)
    await expect(notice).not.toContainText('已停用')
    await expect(notice).not.toContainText('旧版或未识别')

    // 数据库风险评级取代综合质量分；abuse 缺失时只能是不完整的低风险，失败来源残留的高风险不进入结论。
    const verdict = page.getByRole('region', { name: '数据库风险评级' })
    await expect(verdict.locator('.ipq-verdict__value strong')).toHaveText('低风险')
    await expect(verdict).toContainText('已返回来源：低风险')
    await expect(verdict).toContainText('风险字段 4/5，结论不完整')
    await expect(verdict).not.toContainText('质量分')
    await expect(verdict).not.toContainText('主力节点')
    await expect(verdict).not.toContainText('高风险')

    const metrics = page.getByLabel('IP 质量摘要指标')
    const riskSignals = metrics.locator('.ipq-metric').filter({ hasText: '风险信号' })
    await expect(riskSignals.locator('strong')).toHaveText('未知')
    // 已启用来源成功率只用 API coverage：成功 4 / (18 - 13)；十三个未配置不是十四个失败。
    const enabledRate = metrics.locator('.ipq-metric').filter({ hasText: '已启用来源成功率' })
    await expect(enabledRate.locator('strong')).toHaveText('80%')
    await expect(enabledRate).toContainText('成功 4/5 · 未配置 13')
    await expect(enabledRate).toContainText('服务探测 86%')

    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('report-top.png'), fullPage: true })

    // 服务卡片：统一的业务状态门；失败行残留的 blocked/US/范围一律不展示。
    const servicesSection = page.locator('section.ipq-services')
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务可用性（免登录探测）' })).toBeVisible()
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('5 可用')
    const netflix = servicesSection.locator('.ipq-service').filter({ hasText: 'Netflix' })
    await expect(netflix).toContainText('可用 · DE')
    await expect(netflix).toContainText('抽样目录可访问')
    await expect(servicesSection.locator('.ipq-service').filter({ hasText: 'ChatGPT' })).toContainText('部分可用')
    const disney = servicesSection.locator('.ipq-service').filter({ hasText: 'Disney+' })
    await expect(disney).toHaveClass(/ipq-service--unknown/)
    await expect(disney).toContainText('未知')
    await expect(disney).toContainText('服务要求人机验证')
    await expect(disney).not.toContainText('US')
    await expect(disney).not.toContainText('受限')
    await expect(disney).not.toContainText('地区支持检测')
    await expect(servicesSection).not.toContainText('catalog_sample')
    await expect(servicesSection).not.toContainText('默认探测暂不支持该服务')

    // 数据库明细：四个成功来源，未配置来源只在诊断里；零分保留，仅地理来源有说明。
    const providerSection = page.locator('section').filter({ has: page.getByRole('heading', { name: 'IP 数据库判断' }) })
    await expect(providerSection.getByRole('row')).toHaveCount(5)
    await expect(providerSection.getByRole('row').filter({ hasText: 'ipquery.io' }).locator('.ipq-risk-cell__score')).toHaveText('0')
    await expect(providerSection.getByRole('row').filter({ hasText: 'ipwho.is' })).toContainText('该来源本次仅返回地理信息')
    await expect(providerSection.getByRole('row').filter({ hasText: 'ipquery.io' })).not.toContainText('该来源本次仅返回地理信息')
    await expect(providerSection.getByLabel('风险信号命中')).not.toContainText('VPN')
    await expect(providerSection.getByLabel('未返回结果的数据库')).toContainText('ip2location.io · 失败')
    await expect(providerSection).not.toContainText('91')
    await expect(providerSection).not.toContainText('maxmind')
    await expect(providerSection).not.toContainText('未发现代理、VPN、Tor、滥用信号')
    await expect(providerSection).not.toContainText('已返回风险字段未命中')

    // 诊断：失败/未配置分开计数，原始 18 来源覆盖与启用来源成功率分别可见。
    const diagnostics = page.getByLabel('采集诊断')
    await diagnostics.getByRole('heading', { name: '采集诊断' }).click()
    await expect(diagnostics).toContainText('18 个来源 · 7 项服务探测')
    await expect(diagnostics).toContainText('失败 1')
    await expect(diagnostics).toContainText('未配置 13')
    await expect(diagnostics).not.toContainText('个来源未成功')
    await expect(diagnostics).toContainText('来源覆盖（全部）')
    await expect(diagnostics).toContainText('成功 4/18 · 失败 1 · 跳过 0 · 未配置 13')
    await expect(diagnostics).toContainText('成功 6/7 · 失败 1 · 跳过 0 · 未配置 0')
    const rawSummary = diagnostics.getByText('原始报告 JSON', { exact: true })
    await rawSummary.scrollIntoViewIfNeeded()
    await rawSummary.click()
    const code = diagnostics.getByLabel('原始报告 JSON内容')
    await expect(code).toBeVisible()
    await expect(code).toContainText('anonymous_service_probe')
    await expect(code).not.toContainText('<html')
    const box = await code.boundingBox()
    expect(box?.height ?? Infinity).toBeLessThanOrEqual(20 * 16 + 1)
    expect(await code.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
    await expectNoDocumentOverflow(page)
    await code.scrollIntoViewIfNeeded()
    await page.screenshot({ path: testInfo.outputPath('report-diagnostics.png'), fullPage: true })

    // revision 1 历史报告：保留停用警示与旧服务标题，风险/覆盖来自所选报告自己的来源，
    // 供应商 high 不被摘要 low 遮盖。
    const historySection = page.locator('section.ipq-history')
    await historySection.scrollIntoViewIfNeeded()
    await historySection.locator('a[href*="report_id=ipq_hist_rev1"]').click()
    await expect(page).toHaveURL(/\/vps\/vps_001\/ip-quality\?report_id=ipq_hist_rev1$/)
    await expect(page.locator('.badge').filter({ hasText: '历史报告' })).toBeVisible()
    await expect(page.getByRole('button', { name: '立即采集' })).toHaveCount(0)
    await expect(notice).toContainText('已停用')
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务解锁' })).toBeVisible()
    await expect(servicesSection).not.toContainText('服务可用性（免登录探测）')
    await expect(verdict.locator('.ipq-verdict__value strong')).toHaveText('高风险')
    await expect(verdict).toContainText('风险字段 5/5 已返回')
    await expect(verdict).not.toContainText('结论不完整')
    await expect(riskSignals.locator('strong')).toHaveText('2 项')
    await expect(riskSignals).toContainText('Proxy · VPN')
    await expect(riskSignals).toContainText('高风险来源：proxycheck.io')
    await expect(enabledRate.locator('strong')).toHaveText('100%')
    await expect(enabledRate).toContainText('成功 5/5 · 未配置 13')
    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('report-history-revision1.png'), fullPage: true })

    await page.getByRole('link', { name: '查看最新报告' }).click()
    await expect(page).toHaveURL('/vps/vps_001/ip-quality')
    await expect(notice).toContainText(ANONYMOUS_PROBE_NOTICE)

    // 来源不明修订的历史报告：保留旧规则警示与原始失败说明。
    await historySection.scrollIntoViewIfNeeded()
    await historySection.locator('a[href*="report_id=ipq_hist_legacy"]').click()
    await expect(page).toHaveURL(/\/vps\/vps_001\/ip-quality\?report_id=ipq_hist_legacy$/)
    await expect(notice).toContainText('旧版或未识别')
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务解锁' })).toBeVisible()
    await expect(servicesSection).toContainText('服务拒绝了探测请求（HTTP 403）')
    await expect(servicesSection).not.toContainText('http status 403')
    await expect(enabledRate).toContainText('成功 15/16 · 未配置 1')
    await expectNoDocumentOverflow(page)

    // 切回最新报告：恢复 revision 2 说明、服务标题与立即采集按钮。
    await page.getByRole('link', { name: '查看最新报告' }).click()
    await expect(page).toHaveURL('/vps/vps_001/ip-quality')
    await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
    await expect(notice).not.toContainText('已停用')
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务可用性（免登录探测）' })).toBeVisible()
    await expect(enabledRate.locator('strong')).toHaveText('80%')
  })
}

for (const theme of [
  { preset: 'houfeng', mode: 'dark' },
  { preset: 'houfeng', mode: 'light' },
  { preset: 'precision', mode: 'dark' },
  { preset: 'precision', mode: 'light' },
  { preset: 'observatory', mode: 'dark' },
] as const) {
  test(`IP quality report has no serious or critical axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile(profile(reportFixture(), AVAILABLE))
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.goto('/vps/vps_001/ip-quality')
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    await expect(page.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeVisible()
    await page.evaluate(() => document.fonts.ready)
    await expectNoBlockingAxe(page)
  })
}

test('immediate collection from the empty state refreshes into the new report', async ({ api, page }, testInfo) => {
  api.useProfile(profile(EMPTY_REPORT, AVAILABLE, {
    [apiRouteKey('POST', COLLECT_PATH)]: { status: 202, body: { ...AVAILABLE, request: collectRequest('pending') }, expectNoBody: true as const },
  }))
  await page.goto('/vps/vps_001/ip-quality')

  await expect(page.getByRole('heading', { name: '暂无 IP 质量报告' })).toBeVisible()
  await page.getByRole('button', { name: '立即采集' }).click()
  await expect(page.getByRole('status', { name: 'IP 质量采集状态' })).toContainText('正在采集最新 IP 质量')
  await expect(page.getByRole('button', { name: /采集中/ })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath('collect-progress.png') })

  api.useProfile(profile(reportFixture(), { ...AVAILABLE, request: collectRequest('completed') }))
  await expect(page.getByLabel('报告身份')).toContainText('95.169.166.229', { timeout: 10_000 })
  await expect(page.getByRole('status', { name: 'IP 质量采集状态' })).toContainText('采集完成，报告已更新')
  await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
  expect(api.requestCount('POST', COLLECT_PATH)).toBe(1)
})
