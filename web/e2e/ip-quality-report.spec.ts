import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import type {
  IPQualityCollectStatus,
  IPQualityProviderResult,
  VPSIPQualityReport,
} from '../src/lib/types'
import { expect, test } from './fixtures'
import { apiRouteKey, type ApiFixtureProfile } from './fixtures/contracts'
import { authenticatedProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

const REPORT_PATH = '/api/vps/vps_001/ip-quality'
const COLLECT_PATH = '/api/vps/vps_001/ip-quality/collect'

// 贴近真实数据：多数数据库只给地区或什么都不给，原始报告里带大段 HTML 采样。
function providers(): IPQualityProviderResult[] {
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
  const summary = {
    report_id: 'ipq_hist_legacy',
    vps_id: 'vps_001',
    observed_at: '2026-09-30T06:56:00Z',
    ip_address: '95.169.166.229',
    ip_version: 4,
    status: 'success',
    risk_level: 'low',
    use_region_code: 'DE',
    use_region_name: 'Germany',
    asn: 'AS63150',
    organization: 'BAGE CLOUD LLC',
    stale: false,
    ambiguous: false,
    assignment_mode: 'link',
    provider_count: 17,
    unlockable_count: 7,
    coverage: {
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
    },
  }
  return {
    summary,
    latest_report: {
      report_id: summary.report_id,
      monitoring_instance_id: 'mi_001',
      observed_at: summary.observed_at,
      received_at: '2026-09-30T06:56:05Z',
      agent_version: 'v1.1.0',
      fingerprint: 'fp-001',
      sync_batch_id: 'sync_001',
      ip_address: summary.ip_address,
      ip_version: 4,
      status: 'success',
      latitude: 50.11552,
      longitude: 8.68417,
      is_backfilled: false,
      created_at: '2026-09-30T06:56:06Z',
      coverage: summary.coverage,
      diagnostics_json: { source_version: 'v2' },
      raw_json: { services: { reddit: { raw: { body_sample: HTML_SAMPLE, http_status: 200 } }, tiktok: { raw: { body_sample: HTML_SAMPLE } } } },
    },
    provider_results: providers(),
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

export function reportFixture(): VPSIPQualityReport {
  const summary = {
    report_id: 'ipq_0b581f3fc74fc31f',
    vps_id: 'vps_001',
    observed_at: '2026-10-03T12:00:00Z',
    ip_address: '95.169.166.229',
    ip_version: 4,
    status: 'success',
    risk_level: 'low',
    use_region_code: 'DE',
    use_region_name: 'Germany',
    asn: 'AS63150',
    organization: 'BAGE CLOUD LLC',
    stale: false,
    ambiguous: false,
    assignment_mode: 'link',
    provider_count: 17,
    unlockable_count: 7,
    coverage: {
      expected_provider_count: 17,
      successful_provider_count: 15,
      failed_provider_count: 1,
      skipped_provider_count: 0,
      not_configured_provider_count: 1,
      expected_service_count: 7,
      successful_service_count: 0,
      failed_service_count: 0,
      skipped_service_count: 7,
      not_configured_service_count: 0,
    },
  }
  const legacy = legacyReportFixture()
  return {
    summary,
    latest_report: {
      report_id: summary.report_id,
      monitoring_instance_id: 'mi_001',
      observed_at: summary.observed_at,
      received_at: '2026-10-03T12:00:05Z',
      agent_version: 'v1.1.0',
      fingerprint: 'fp-001',
      sync_batch_id: 'sync_001',
      ip_address: summary.ip_address,
      ip_version: 4,
      status: 'success',
      latitude: 50.11552,
      longitude: 8.68417,
      is_backfilled: false,
      created_at: '2026-10-03T12:00:06Z',
      coverage: summary.coverage,
      diagnostics_json: { source_version: 'v2', service_probe_revision: 1 },
      raw_json: {
        services: Object.fromEntries(
          ['netflix', 'chatgpt', 'youtube-premium', 'amazon-prime-video', 'disney-plus', 'tiktok', 'reddit']
            .map((service) => [service, {
              status: 'skipped',
              error_code: 'unsupported_default_probe',
              error_summary: 'safe default probe is not available without verified business evidence',
            }]),
        ),
      },
    },
    provider_results: providers(),
    service_unlocks: [
      { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
      { service: 'chatgpt', source: 'openai_status_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
      { service: 'youtube-premium', source: 'youtube_premium_page_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
      { service: 'amazon-prime-video', source: 'prime_video_page_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
      { service: 'disney-plus', source: 'disney_default_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
      { service: 'tiktok', source: 'tiktok_home_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
      { service: 'reddit', source: 'reddit_home_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe', error_summary: 'safe default probe is not available without verified business evidence' },
    ],
    history: [
      summary,
      legacy.summary!,
      ...Array.from({ length: 6 }, (_, index) => ({
        ...summary,
        report_id: `ipq_hist_${index + 2}`,
        observed_at: new Date(Date.parse(summary.observed_at) - (index + 2) * 86_400_000).toISOString(),
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
  const legacy = legacyReportFixture()
  return authenticatedProfile({
    [apiRouteKey('GET', REPORT_PATH)]: { status: 200, body: report },
    [apiRouteKey('GET', `${REPORT_PATH}/reports/ipq_hist_legacy`)]: { status: 200, body: legacy },
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

for (const viewport of VIEWPORTS) {
  test(`IP quality report stays bounded with raw JSON open at ${viewport.width}px`, async ({ api, page }, testInfo) => {
    api.useProfile(profile(reportFixture(), AVAILABLE))
    await page.setViewportSize(viewport)
    await page.goto('/vps/vps_001/ip-quality')

    await expect(page.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeVisible()
    await expect(page.getByLabel('报告身份')).toContainText('95.169.166.229')
    await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()

    // 默认探针停用提示与能力收缩事实：质量分 —，服务覆盖 0%
    const notice = page.getByRole('note', { name: '服务探测可信度' })
    await expect(notice).toBeVisible()
    await expect(notice).toContainText('已停用')

    const verdict = page.getByLabel('质量结论')
    await expect(verdict.locator('.ipq-verdict__value strong')).toHaveText('—')
    await expect(verdict).toContainText('服务 0%')

    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('report-top.png'), fullPage: true })

    // 默认服务全部显示中文说明
    const servicesSection = page.locator('section.ipq-services')
    await expect(servicesSection).toContainText('默认探测暂不支持该服务')

    // 空字段数据库折叠成计数，表格只剩给出判断的数据库。
    const providerSection = page.locator('section').filter({ has: page.getByRole('heading', { name: 'IP 数据库判断' }) })
    await expect(providerSection.getByRole('row')).toHaveCount(4)
    await expect(providerSection).toContainText('另有 12 个数据库未给出风险判断')

    const diagnostics = page.getByLabel('采集诊断')
    await diagnostics.getByRole('heading', { name: '采集诊断' }).click()
    const rawSummary = diagnostics.getByText('原始报告 JSON', { exact: true })
    await rawSummary.scrollIntoViewIfNeeded()
    await rawSummary.click()
    const code = diagnostics.getByLabel('原始报告 JSON内容')
    await expect(code).toBeVisible()
    const box = await code.boundingBox()
    expect(box?.height ?? Infinity).toBeLessThanOrEqual(20 * 16 + 1)
    expect(await code.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
    await expectNoDocumentOverflow(page)
    await code.scrollIntoViewIfNeeded()
    await page.screenshot({ path: testInfo.outputPath('report-diagnostics.png'), fullPage: true })

    // 导航至历史报告：展示历史规则警示，保留原始服务结果与评分，且无立即采集按钮
    const historySection = page.locator('section.ipq-history')
    await historySection.scrollIntoViewIfNeeded()
    await historySection.getByRole('link', { name: /查看 .* 的报告/ }).first().click()
    await expect(page).toHaveURL(/\/vps\/vps_001\/ip-quality\?report_id=ipq_hist_legacy/)
    await expect(page.locator('.badge').filter({ hasText: '历史报告' })).toBeVisible()
    await expect(page.getByRole('button', { name: '立即采集' })).toHaveCount(0)

    const historicalNotice = page.getByRole('note', { name: '服务探测可信度' })
    await expect(historicalNotice).toBeVisible()
    await expect(historicalNotice).toContainText('旧版或未识别')

    const historicalServices = page.locator('section.ipq-services')
    await expect(historicalServices).toContainText('服务拒绝了探测请求（HTTP 403）')
    await expect(historicalServices).not.toContainText('http status 403')

    await expectNoDocumentOverflow(page)

    // 切回最新报告：恢复最新提示与立即采集按钮
    await page.getByRole('link', { name: '查看最新报告' }).click()
    await expect(page).toHaveURL('/vps/vps_001/ip-quality')
    await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
    await expect(notice).toContainText('已停用')
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
