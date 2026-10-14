import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'

import type { IPQualityIPVersion } from '../src/lib/types'
import { expect, test } from './fixtures'
import type { ApiFixtureProfile } from './fixtures/contracts'
import {
  collectPostRoute,
  collectRequest,
  COLLECT_AVAILABLE,
  emptyScenario,
  IPQ_COLLECT_PATH,
  IPQ_PAGE_PATH,
  IPV4_IDENTITY,
  IPV6_IDENTITY,
  ipqDetailPath,
  ipqPageURL,
  ipqReportPath,
  ipQualityScenario,
  ipQualityScenarioProfile,
  legacyAgentProfile,
  scenarioAfterCollect,
  type CollectOutcome,
  type IPQualityScenario,
} from './fixtures/ipQualityProfiles'
import { expectNoDocumentOverflow } from './support/geometry'

// 全部数据来自 e2e/fixtures/ipQualityProfiles.ts 的浏览器夹具：只验证页面行为契约，不代表 center/PG/agent 证据。

// 页面可见合同（docs/spec/contracts/ip-quality-web.md）集中在此，文案调整只改一处。
const UI = {
  familyGroup: 'IP 地址族',
  collectHint: '分别检测 IPv4 / IPv6',
  updated: '报告已更新',
  invalidFamily: '地址族参数无效',
  showDefault: '查看默认报告',
  lastValid: '上次有效结果',
  stale: '已过期',
  legacyRound: '本次仅收到单地址族报告，未验证另一地址族',
  refreshFailed: '刷新失败',
  unavailable: (ipVersion: IPQualityIPVersion) => `本轮未检测到 IPv${ipVersion} 可用本地地址`,
  lookupFailed: (ipVersion: IPQualityIPVersion) => `IPv${ipVersion} 出口探测失败`,
  noResult: (ipVersion: IPQualityIPVersion) => `IPv${ipVersion} 尚无采集结果`,
} as const

// 夹具时间按 Asia/Shanghai 渲染（playwright.config timezoneId）。
const ROUND1_TIME = '2026/10/03 20:00'
const ROUND2_TIME = '2026/10/04 20:00'
const IPV6_PREVIOUS_TIME = '2026/09/28 16:30'
const IPV4_ROUND2_IP = '198.51.100.20'
const IPV6_ROUND2_IP = '2001:db8:ffff:ffff:ffff:ffff:ffff:5678'

const VIEWPORTS = [
  { width: 1440, height: 1000 },
  { width: 1024, height: 768 },
  { width: 390, height: 900 },
] as const

const THEMES = [
  { preset: 'houfeng', mode: 'dark' },
  { preset: 'houfeng', mode: 'light' },
  { preset: 'precision', mode: 'dark' },
  { preset: 'precision', mode: 'light' },
  { preset: 'observatory', mode: 'dark' },
] as const

const ANONYMOUS_PROBE_NOTICE = '免登录服务探测，仅反映当前入口和地区可用性，不代表账号或付费内容权限。'

function familyButton(page: Page, ipVersion: IPQualityIPVersion): Locator {
  return page.getByRole('group', { name: UI.familyGroup }).getByRole('button', { name: `IPv${ipVersion}`, exact: true })
}

/** URL、族选择器与历史身份三者一致；reportId 缺省表示必须没有 report_id。 */
async function expectSelection(page: Page, ipVersion: IPQualityIPVersion, reportId?: string) {
  await expect(page).toHaveURL((url) => (
    url.pathname === IPQ_PAGE_PATH
    && url.searchParams.getAll('ip_version').join(',') === String(ipVersion)
    && (url.searchParams.get('report_id') ?? undefined) === reportId
  ))
  await expect(familyButton(page, ipVersion)).toHaveAttribute('aria-pressed', 'true')
  await expect(familyButton(page, ipVersion === 4 ? 6 : 4)).toHaveAttribute('aria-pressed', 'false')
}

function reportGETCount(api: { requestCount: (method: 'GET', path: string) => number }): number {
  return api.requestCount('GET', ipqReportPath()) + api.requestCount('GET', ipqReportPath(4)) + api.requestCount('GET', ipqReportPath(6))
}

async function expectIdentityFits(page: Page) {
  const geometry = await page.getByLabel('报告身份').evaluate((element) => {
    const rect = element.getBoundingClientRect()
    return { right: rect.right, viewport: window.innerWidth, overflow: element.scrollWidth - element.clientWidth }
  })
  expect(geometry.right).toBeLessThanOrEqual(geometry.viewport + 1)
  expect(geometry.overflow).toBeLessThanOrEqual(1)
}

async function expectNoBlockingAxe(page: Page) {
  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({ id: violation.id, impact: violation.impact, targets: violation.nodes.map((node) => node.target) }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
}

function deferred(): { promise: Promise<void>, resolve: () => void } {
  let resolve!: () => void
  const promise = new Promise<void>((done) => { resolve = done })
  return { promise, resolve }
}

const collectStatus = (page: Page) => page.getByRole('status', { name: 'IP 质量采集状态' })

// ---------------------------------------------------------------------------
// 旧 agent 单族报告：revision1/legacy/revision2 行为与地址族 URL 合同
// ---------------------------------------------------------------------------

for (const viewport of VIEWPORTS) {
  test(`legacy single-family report keeps revision, risk and coverage semantics at ${viewport.width}px`, async ({ api, page }, testInfo) => {
    api.useProfile(legacyAgentProfile())
    await page.setViewportSize(viewport)
    await page.goto(IPQ_PAGE_PATH)

    await expect(page.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeVisible()
    await expect(page.getByLabel('报告身份')).toContainText('95.169.166.229')
    // 默认请求只发一次；规范化 URL 不再触发显式族请求。
    await expectSelection(page, 4)
    expect(api.requestCount('GET', ipqReportPath())).toBe(1)
    expect(api.requestCount('GET', ipqReportPath(4))).toBe(0)
    await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
    await expect(page.getByText(UI.collectHint)).toBeVisible()
    // 旧 agent 没有 IPv6 记录：切到 IPv6 只能说尚无结果，不能说缺族，也不借 IPv4 事实。
    await familyButton(page, 6).click()
    await expectSelection(page, 6)
    await expect(page.locator('.ipq-page')).toContainText(UI.noResult(6))
    await expect(page.locator('.ipq-page')).not.toContainText(UI.unavailable(6))
    await expect(page.getByLabel('报告身份')).toHaveCount(0)
    await familyButton(page, 4).click()
    await expectSelection(page, 4)
    await expect(page.getByLabel('报告身份')).toContainText('95.169.166.229')

    const notice = page.getByRole('note', { name: '服务探测可信度' })
    await expect(notice).toContainText(ANONYMOUS_PROBE_NOTICE)
    await expect(notice).not.toContainText('已停用')
    await expect(notice).not.toContainText('旧版或未识别')

    // abuse 缺失时大字是证据缺口，低风险只作为已返回来源的副文；失败来源残留的高风险不进入结论。
    const verdict = page.getByRole('region', { name: '数据库风险评级' })
    await expect(verdict.locator('.ipq-verdict__value strong')).toHaveText('证据不完整')
    await expect(verdict).toContainText('已返回来源：低风险')
    await expect(verdict.locator('.ipq-verdict__scope')).toHaveText('风险字段 4/5')
    await expect(verdict).not.toContainText('质量分')
    await expect(verdict).not.toContainText('高风险')

    const metrics = page.getByLabel('IP 质量摘要指标')
    const riskSignals = metrics.locator('.ipq-metric').filter({ hasText: '风险信号' })
    await expect(riskSignals.locator('strong')).toHaveText('未知')
    // 成功 4 / (18 - 13)：十三个未接入不是失败，也不进入摘要。
    const enabledRate = metrics.locator('.ipq-metric').filter({ hasText: '已启用来源响应成功率' })
    await expect(enabledRate.locator('strong')).toHaveText('80%')
    await expect(enabledRate).toContainText('响应成功 4/5')
    await expect(enabledRate).not.toContainText('13')
    // 服务分母是完整 7 项：ChatGPT 部分可用不进分子，Disney+ 未知不算受限。
    const serviceRate = metrics.locator('.ipq-metric').filter({ hasText: '服务可用性' })
    await expect(serviceRate.locator('strong')).toHaveText('5/7 可用')
    await expect(serviceRate).toContainText('1 部分可用 · 1 未知')

    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('legacy-report-top.png'), fullPage: true })

    // 失败行残留的 blocked/US/范围一律不展示。
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

    // 数据库明细：四个成功来源，未接入来源只在诊断里；零分保留，仅地理来源有说明。
    const providerSection = page.locator('section').filter({ has: page.getByRole('heading', { name: 'IP 数据库判断' }) })
    await expect(providerSection.getByRole('row')).toHaveCount(5)
    await expect(providerSection.getByRole('row').filter({ hasText: 'ipquery.io' }).locator('.ipq-risk-cell__score')).toHaveText('0')
    await expect(providerSection.getByRole('row').filter({ hasText: 'ipwho.is' })).toContainText('该来源本次仅返回地理信息')
    await expect(providerSection.getByLabel('风险信号命中')).not.toContainText('VPN')
    await expect(providerSection.getByLabel('未返回结果的数据库')).toContainText('ip2location.io · 失败')
    await expect(providerSection).not.toContainText('91')
    await expect(providerSection).not.toContainText('maxmind')
    await expect(providerSection).not.toContainText('已返回风险字段未命中')

    // 诊断：失败/未接入分开计数，原始 18 来源覆盖可见；raw JSON 限高可滚动且无 HTML。
    const diagnostics = page.getByLabel('采集诊断')
    await diagnostics.getByRole('heading', { name: '采集诊断' }).click()
    await expect(diagnostics).toContainText('18 个来源 · 7 项服务探测')
    await expect(diagnostics).toContainText('未接入 13')
    await expect(diagnostics).not.toContainText('未配置')
    await expect(diagnostics).toContainText('成功 4/18 · 失败 1 · 跳过 0 · 未接入 13')
    await expect(diagnostics).toContainText('成功 6/7 · 失败 1 · 跳过 0 · 未接入 0')
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

    // revision 1 历史：链接携带族与 ID，detail API 只带 ID；供应商 high 不被摘要 low 遮盖。
    const historySection = page.locator('section.ipq-history')
    await historySection.scrollIntoViewIfNeeded()
    await historySection.locator('a[href*="report_id=ipq_hist_rev1"]').click()
    await expectSelection(page, 4, 'ipq_hist_rev1')
    expect(api.requestCount('GET', ipqDetailPath('ipq_hist_rev1'))).toBe(1)
    await expect(page.locator('.badge').filter({ hasText: '历史报告' })).toBeVisible()
    await expect(page.getByRole('button', { name: '立即采集' })).toHaveCount(0)
    await expect(notice).toContainText('已停用')
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务解锁' })).toBeVisible()
    await expect(verdict.locator('.ipq-verdict__value strong')).toHaveText('高风险')
    await expect(verdict).toContainText('风险字段 5/5 已返回')
    await expect(riskSignals.locator('strong')).toHaveText('2 项')
    await expect(riskSignals).toContainText('高风险来源：proxycheck.io')
    await expect(enabledRate).toContainText('响应成功 5/5')
    await expectNoDocumentOverflow(page)

    // 查看最新报告回到显式 IPv4 latest。
    await page.getByRole('link', { name: '查看最新报告' }).click()
    await expectSelection(page, 4)
    await expect(notice).toContainText(ANONYMOUS_PROBE_NOTICE)

    // 来源不明修订：保留旧规则警示；旧行没有 endpoint 诊断，不从 error_summary 反解析状态码。
    await historySection.scrollIntoViewIfNeeded()
    await historySection.locator('a[href*="report_id=ipq_hist_legacy"]').click()
    await expectSelection(page, 4, 'ipq_hist_legacy')
    await expect(notice).toContainText('旧版或未识别')
    await expect(servicesSection).toContainText('服务返回异常状态')
    await expect(servicesSection).not.toContainText('HTTP 403')
    await expect(servicesSection).not.toContainText('http status 403')
    await expect(enabledRate).toContainText('响应成功 15/16')
    await expectNoDocumentOverflow(page)

    await page.getByRole('link', { name: '查看最新报告' }).click()
    await expectSelection(page, 4)
    await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务可用性（免登录探测）' })).toBeVisible()
    await expect(enabledRate.locator('strong')).toHaveText('80%')
  })
}

// ---------------------------------------------------------------------------
// 双栈：默认 IPv4、手动切换 IPv6，事实/时间/历史整体切换
// ---------------------------------------------------------------------------

for (const viewport of VIEWPORTS) {
  test(`dual-stack report defaults to IPv4 and switches every fact to IPv6 at ${viewport.width}px`, async ({ api, page }, testInfo) => {
    api.useProfile(ipQualityScenarioProfile('dual_stack'))
    await page.setViewportSize(viewport)
    await page.goto(IPQ_PAGE_PATH)

    const pageRoot = page.locator('.ipq-page')
    const identity = page.getByLabel('报告身份')
    const verdict = page.getByRole('region', { name: '数据库风险评级' })
    const serviceRate = page.getByLabel('IP 质量摘要指标').locator('.ipq-metric').filter({ hasText: '服务可用性' })
    const servicesSection = page.locator('section.ipq-services')
    const historySection = page.locator('section.ipq-history')

    await expect(identity).toContainText(IPV4_IDENTITY.ipAddress)
    await expectSelection(page, 4)
    expect(api.requestCount('GET', ipqReportPath())).toBe(1)
    expect(api.requestCount('GET', ipqReportPath(4))).toBe(0)
    await expect(identity).toContainText(IPV4_IDENTITY.asn)
    await expect(identity).toContainText(ROUND1_TIME)
    await expect(verdict.locator('.ipq-verdict__value strong')).toHaveText('低风险')
    await expect(verdict).toContainText('风险字段 5/5 已返回')
    await expect(serviceRate.locator('strong')).toHaveText('6/7 可用')
    await expect(historySection).toContainText('共 2 份')
    await expect(pageRoot).not.toContainText(IPV6_IDENTITY.ipAddress)
    await expect(pageRoot).not.toContainText(IPV6_PREVIOUS_TIME)
    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('dual-stack-ipv4.png'), fullPage: true })

    await familyButton(page, 6).click()
    await expectSelection(page, 6)
    expect(api.requestCount('GET', ipqReportPath(6))).toBe(1)
    await expect(identity).toContainText(IPV6_IDENTITY.ipAddress)
    await expect(identity).toContainText(IPV6_IDENTITY.asn)
    await expect(pageRoot).not.toContainText(IPV4_IDENTITY.ipAddress)
    await expect(pageRoot).not.toContainText(IPV4_IDENTITY.asn)
    // IPv6 自己的风险：medium 已发现但 abuse 缺失，仍展示等级并标不完整。
    await expect(verdict.locator('.ipq-verdict__value strong')).toHaveText('中风险')
    await expect(verdict).toContainText('4/5')
    await expect(serviceRate.locator('strong')).toHaveText('2/7 可用')
    await expect(serviceRate).toContainText('1 部分可用 · 4 未知')
    // YouTube 只有可信页面地区 CN：未知 + 页面地区，残留 US 与 CN 都不能推出受限。
    const youtube = servicesSection.locator('.ipq-service').filter({ hasText: 'YouTube Premium' })
    await expect(youtube).toContainText('未知')
    await expect(youtube).toContainText('页面地区：CN；Premium 可用性未知')
    await expect(youtube).not.toContainText('US')
    await expect(youtube).not.toContainText('受限')
    // 普通 403 不是业务拒绝。
    for (const name of ['Prime Video', 'Reddit']) {
      await expect(servicesSection.locator('.ipq-service').filter({ hasText: name })).not.toContainText('受限')
    }
    await expect(servicesSection.locator('.ipq-service').filter({ hasText: 'Netflix' })).toContainText('连接方式未检测')
    await expect(historySection).toContainText('共 2 份')
    await expect(historySection).toContainText(IPV6_PREVIOUS_TIME)
    await expectIdentityFits(page)
    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('dual-stack-ipv6.png'), fullPage: true })

    // IPv6 历史：页面 URL 带族与 ID，detail API 只带 ID，选择器保持 IPv6。
    await historySection.scrollIntoViewIfNeeded()
    await historySection.locator('a[href*="report_id=ipq_v6_prev"]').click()
    await expectSelection(page, 6, 'ipq_v6_prev')
    expect(api.requestCount('GET', ipqDetailPath('ipq_v6_prev'))).toBe(1)
    await expect(page.locator('.badge').filter({ hasText: '历史报告' })).toBeVisible()
    await expect(identity).toContainText(IPV6_PREVIOUS_TIME)
    await expect(pageRoot).not.toContainText(IPV4_IDENTITY.ipAddress)

    // 从历史切族：清除 report_id，显示 IPv4 最新。
    await familyButton(page, 4).click()
    await expectSelection(page, 4)
    await expect(identity).toContainText(IPV4_IDENTITY.ipAddress)
    await expect(page.locator('.badge').filter({ hasText: '历史报告' })).toHaveCount(0)
    await expectNoDocumentOverflow(page)
  })
}

test('family selector is keyboard operable and keeps focus across a switch', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack'))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)

  await familyButton(page, 4).focus()
  await page.keyboard.press('Tab')
  await expect(familyButton(page, 6)).toBeFocused()
  await page.keyboard.press('Enter')
  await expectSelection(page, 6)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
  await expect(familyButton(page, 6)).toBeFocused()

  await page.keyboard.press('Shift+Tab')
  await expect(familyButton(page, 4)).toBeFocused()
  await page.keyboard.press('Space')
  await expectSelection(page, 4)
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
})

for (const theme of THEMES) {
  test(`dual-stack IPv6 report and missing-family state have no serious axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile(ipQualityScenarioProfile('dual_stack'))
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.goto(ipqPageURL({ ipVersion: 6 }))
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    await expect(page.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
    await page.evaluate(() => document.fonts.ready)
    await expectNoBlockingAxe(page)

    // 缺族空态：族选择与尝试状态仍可达。
    api.useProfile(ipQualityScenarioProfile('ipv4_only'))
    await page.goto(ipqPageURL({ ipVersion: 6 }))
    await expectSelection(page, 6)
    await expect(page.locator('.ipq-page')).toContainText(UI.unavailable(6))
    await expectNoBlockingAxe(page)
  })
}

// ---------------------------------------------------------------------------
// 单栈与单侧失败
// ---------------------------------------------------------------------------

test('IPv4-only server keeps IPv4 default and explains the missing IPv6 family without failure risk', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('ipv4_only'))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
  await expect(page.getByRole('region', { name: '数据库风险评级' }).locator('.ipq-verdict__value strong')).toHaveText('低风险')

  await familyButton(page, 6).click()
  await expectSelection(page, 6)
  const pageRoot = page.locator('.ipq-page')
  await expect(pageRoot).toContainText(UI.unavailable(6))
  await expect(page.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeVisible()
  // 失败占位不是出口、不是风险、不借 IPv4 事实。
  await expect(page.getByLabel('报告身份')).toHaveCount(0)
  await expect(page.getByRole('region', { name: '数据库风险评级' })).toHaveCount(0)
  await expect(pageRoot).not.toContainText('0.0.0.0')
  await expect(pageRoot).not.toContainText(IPV4_IDENTITY.ipAddress)
  await expect(pageRoot).not.toContainText(UI.lookupFailed(6))
  await expectNoDocumentOverflow(page)
})

test('IPv6-only server defaults to IPv6 and still lets the user inspect the missing IPv4 family', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('ipv6_only'))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 6)
  expect(api.requestCount('GET', ipqReportPath())).toBe(1)
  expect(api.requestCount('GET', ipqReportPath(6))).toBe(0)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
  // IPv4 缺族不让 IPv6 报告变成整体失败。
  await expect(page.locator('.ipq-page')).not.toContainText(UI.lookupFailed(4))

  await familyButton(page, 4).click()
  await expectSelection(page, 4)
  const pageRoot = page.locator('.ipq-page')
  await expect(pageRoot).toContainText(UI.unavailable(4))
  await expect(familyButton(page, 6)).toBeVisible()
  await expect(pageRoot).not.toContainText('0.0.0.0')
  await expect(pageRoot).not.toContainText(IPV6_IDENTITY.ipAddress)
})

test('IPv4 lookup failure keeps IPv4 selected with the last valid result while IPv6 shows its own round', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('ipv4_lookup_failure'))
  await page.goto(IPQ_PAGE_PATH)
  // IPv4 失败不能静默切到 IPv6。
  await expectSelection(page, 4)
  const pageRoot = page.locator('.ipq-page')
  await expect(pageRoot).toContainText(UI.lookupFailed(4))
  await expect(pageRoot).toContainText(UI.lastValid)
  await expect(pageRoot).not.toContainText(UI.unavailable(4))
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
  await expect(page.getByLabel('报告身份')).toContainText(ROUND1_TIME)

  await familyButton(page, 6).click()
  await expectSelection(page, 6)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_ROUND2_IP)
  await expect(page.getByLabel('报告身份')).toContainText(ROUND2_TIME)
  await expect(page.locator('section.ipq-history')).toContainText('共 3 份')
})

test('fresh unavailable keeps the old same-family report reachable; stale unavailable is not called current', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('ipv6_unavailable_with_history'))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)

  await page.goto(ipqPageURL({ ipVersion: 6 }))
  await expectSelection(page, 6)
  const pageRoot = page.locator('.ipq-page')
  await expect(pageRoot).toContainText(UI.unavailable(6))
  await expect(pageRoot).toContainText(UI.lastValid)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_PREVIOUS_TIME)

  api.useProfile(ipQualityScenarioProfile('ipv6_unavailable_stale'))
  await page.goto(ipqPageURL({ ipVersion: 6 }))
  await expectSelection(page, 6)
  await expect(pageRoot).toContainText(UI.stale)
  await expect(pageRoot).not.toContainText(UI.unavailable(6))
  await expect(pageRoot).not.toContainText('0.0.0.0')
})

// ---------------------------------------------------------------------------
// URL 规范化与非法参数恢复
// ---------------------------------------------------------------------------

for (const search of ['?ip_version=5', '?ip_version=', '?ip_version=4&ip_version=6']) {
  test(`invalid family parameter ${search} shows a recoverable error without an illegal API request`, async ({ api, page }) => {
    api.useProfile(ipQualityScenarioProfile('dual_stack'))
    await page.goto(`${IPQ_PAGE_PATH}${search}`)
    await expect(page.getByRole('heading', { name: UI.invalidFamily })).toBeVisible()
    expect(reportGETCount(api)).toBe(0)

    const recover = page.getByRole('link', { name: UI.showDefault })
    await recover.click()
    await expectSelection(page, 4)
    await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
    expect(api.requestCount('GET', ipqReportPath())).toBe(1)
    expect(api.requestCount('GET', ipqReportPath(4))).toBe(0)
  })
}

for (const [label, search] of [
  ['legacy report_id-only link', '?report_id=ipq_v6_prev'],
  ['conflicting family', '?ip_version=4&report_id=ipq_v6_prev'],
  ['invalid family with valid history id', '?ip_version=5&report_id=ipq_v6_prev'],
] as const) {
  test(`history id decides the family for a ${label}`, async ({ api, page }) => {
    api.useProfile(ipQualityScenarioProfile('dual_stack'))
    await page.goto(`${IPQ_PAGE_PATH}${search}`)
    await expectSelection(page, 6, 'ipq_v6_prev')
    await expect(page.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
    await expect(page.locator('.ipq-page')).not.toContainText(IPV4_IDENTITY.ipAddress)
    expect(api.requestCount('GET', ipqDetailPath('ipq_v6_prev'))).toBe(1)
    expect(reportGETCount(api)).toBe(0)
  })
}

// ---------------------------------------------------------------------------
// 迟到响应与浏览器前进后退
// ---------------------------------------------------------------------------

test('a late IPv6 response cannot overwrite IPv4 after back navigation, and forward restores IPv6', async ({ api, page }) => {
  const release = deferred()
  const scenario = ipQualityScenario('dual_stack')
  api.useProfile(ipQualityScenarioProfile(scenario, {
    overrides: { 6: { status: 200, body: scenario.views[6], waitFor: release.promise } },
  }))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)

  await familyButton(page, 6).click()
  await expect.poll(() => api.requestCount('GET', ipqReportPath(6))).toBe(1)
  await page.goBack()
  await expect(page).toHaveURL((url) => url.searchParams.get('ip_version') === '4')
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)

  const lateResponse = page.waitForResponse((response) => response.url().includes('ip_version=6'))
  release.resolve()
  await lateResponse
  // 迟到的 IPv6 响应被丢弃：URL、选择器与事实仍是 IPv4。
  await expectSelection(page, 4)
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
  await expect(page.locator('.ipq-page')).not.toContainText(IPV6_IDENTITY.ipAddress)

  await page.goForward()
  await expectSelection(page, 6)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
  await page.goBack()
  await expectSelection(page, 4)
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
})

// ---------------------------------------------------------------------------
// 立即采集：一轮覆盖两族；完成 ≠ 当前报告已更新
// ---------------------------------------------------------------------------

// POST 之后 center 的 GET /collect 会返回这条进行中请求；页面在切族/进入历史后重新读取状态时必须看到它，
// 而不是 POST 之前的空闲状态（真实 center 不会在请求进行中返回空闲）。
async function startCollect(page: Page, api: { useProfile: (profile: ApiFixtureProfile) => void }, scenario: IPQualityScenario, outcome: CollectOutcome) {
  await expect(page.getByText(UI.collectHint)).toBeVisible()
  await page.getByRole('button', { name: '立即采集' }).click()
  await expect(collectStatus(page)).toContainText('正在采集最新 IP 质量')
  api.useProfile(ipQualityScenarioProfile(scenario, { collect: { ...COLLECT_AVAILABLE, request: collectRequest('dispatched', outcome) } }))
  await expect(page.getByRole('button', { name: /采集中/ })).toBeDisabled()
}

function completedProfile(outcome: CollectOutcome) {
  return ipQualityScenarioProfile(scenarioAfterCollect(outcome), { collect: { ...COLLECT_AVAILABLE, request: collectRequest('completed', outcome) } })
}

test('collection started on IPv4 survives a switch to IPv6 and updates the IPv6 report it finishes on', async ({ api, page }, testInfo) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack', { extra: collectPostRoute('both_success') }))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)
  await startCollect(page, api, ipQualityScenario('dual_stack'), 'both_success')

  // 族选择不是调度维度：切族不重复 POST，也不重置进行中请求。
  await familyButton(page, 6).click()
  await expectSelection(page, 6)
  await expect(page.getByRole('button', { name: /采集中/ })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath('collect-progress-ipv6.png') })

  api.useProfile(completedProfile('both_success'))
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_ROUND2_IP, { timeout: 10_000 })
  await expect(collectStatus(page)).toContainText(UI.updated)
  // 完成后仍在 IPv6，不跳回默认族。
  await expectSelection(page, 6)
  await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(1)
})

test('IPv4 failure in a partial round keeps the last IPv4 result and never claims the current report updated', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack', { extra: collectPostRoute('ipv4_failure_ipv6_success') }))
  await page.goto(IPQ_PAGE_PATH)
  await startCollect(page, api, ipQualityScenario('dual_stack'), 'ipv4_failure_ipv6_success')

  api.useProfile(completedProfile('ipv4_failure_ipv6_success'))
  const status = collectStatus(page)
  await expect(status).toContainText('IPv4', { timeout: 10_000 })
  await expect(status).toContainText('失败')
  await expect(status).not.toContainText(UI.updated)
  await expectSelection(page, 4)
  const pageRoot = page.locator('.ipq-page')
  await expect(pageRoot).toContainText(UI.lookupFailed(4))
  await expect(pageRoot).toContainText(UI.lastValid)
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
  await expect(page.getByLabel('报告身份')).toContainText(ROUND1_TIME)
  await expect(pageRoot).not.toContainText(IPV4_ROUND2_IP)

  // 手动切 IPv6：成功族的本轮事实，不混入 IPv4。
  await familyButton(page, 6).click()
  await expectSelection(page, 6)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_ROUND2_IP)
  await expect(pageRoot).not.toContainText(IPV4_IDENTITY.ipAddress)
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(1)
})

test('an unavailable IPv6 family in the finished round is reported as missing, not as an updated report', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack', { extra: collectPostRoute('ipv6_unavailable') }))
  await page.goto(ipqPageURL({ ipVersion: 6 }))
  await expectSelection(page, 6)
  await startCollect(page, api, ipQualityScenario('dual_stack'), 'ipv6_unavailable')

  api.useProfile(completedProfile('ipv6_unavailable'))
  const status = collectStatus(page)
  await expect(status).toContainText(UI.unavailable(6), { timeout: 10_000 })
  await expect(status).not.toContainText(UI.updated)
  await expectSelection(page, 6)
  await expect(page.locator('.ipq-page')).toContainText(UI.lastValid)
  await expect(page.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
})

test('a collection finishing while a history report is open stays on that history report', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack', { extra: collectPostRoute('both_success') }))
  await page.goto(IPQ_PAGE_PATH)
  await startCollect(page, api, ipQualityScenario('dual_stack'), 'both_success')

  const historySection = page.locator('section.ipq-history')
  await historySection.scrollIntoViewIfNeeded()
  await historySection.locator('a[href*="report_id=ipq_v4_prev"]').click()
  await expectSelection(page, 4, 'ipq_v4_prev')

  api.useProfile(completedProfile('both_success'))
  const status = collectStatus(page)
  await expect(status).toContainText('历史', { timeout: 10_000 })
  await expect(status).not.toContainText(UI.updated)
  await expectSelection(page, 4, 'ipq_v4_prev')
  await expect(page.locator('.badge').filter({ hasText: '历史报告' })).toBeVisible()
  await expect(page.locator('.ipq-page')).not.toContainText(IPV4_ROUND2_IP)
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(1)
})

test('a legacy single-family completion says the other family was not verified', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack', { extra: collectPostRoute('legacy_single_family') }))
  await page.goto(IPQ_PAGE_PATH)
  await startCollect(page, api, ipQualityScenario('dual_stack'), 'legacy_single_family')

  api.useProfile(completedProfile('legacy_single_family'))
  await expect(collectStatus(page)).toContainText(UI.legacyRound, { timeout: 10_000 })
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_ROUND2_IP)
  await expectSelection(page, 4)
})

test('a failed background refresh keeps the same-family report and does not pretend it updated', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack', { extra: collectPostRoute('both_success') }))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)
  await startCollect(page, api, ipQualityScenario('dual_stack'), 'both_success')

  api.useProfile(ipQualityScenarioProfile(scenarioAfterCollect('both_success'), {
    collect: { ...COLLECT_AVAILABLE, request: collectRequest('completed', 'both_success') },
    overrides: { 4: { status: 500, body: { error: 'fixture refresh failure', code: 'internal_error' } } },
  }))
  const status = collectStatus(page)
  await expect(status).toContainText(UI.refreshFailed, { timeout: 10_000 })
  await expect(status).not.toContainText(UI.updated)
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
  await expect(page.locator('.ipq-page')).not.toContainText(IPV4_ROUND2_IP)
  expect(api.requestCount('GET', ipqReportPath(4))).toBeGreaterThanOrEqual(1)
})

test('immediate collection from the empty state refreshes into the new dual-stack report', async ({ api, page }, testInfo) => {
  api.useProfile(ipQualityScenarioProfile(emptyScenario(), { extra: collectPostRoute('both_success') }))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)
  await expect(page.locator('.ipq-page')).toContainText(UI.noResult(4))
  await startCollect(page, api, emptyScenario(), 'both_success')
  await page.screenshot({ path: testInfo.outputPath('collect-progress.png') })

  api.useProfile(completedProfile('both_success'))
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_ROUND2_IP, { timeout: 10_000 })
  await expect(collectStatus(page)).toContainText(UI.updated)
  await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(1)
})
