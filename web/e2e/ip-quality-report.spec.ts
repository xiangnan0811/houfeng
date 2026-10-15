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
  enrichingView,
  scenarioAfterCollect,
  type CollectOutcome,
  type IPQualityScenario,
} from './fixtures/ipQualityProfiles'
import { expectNoDocumentOverflow } from './support/geometry'

// 全部数据来自 e2e/fixtures/ipQualityProfiles.ts 的浏览器夹具：只验证页面行为契约，不代表 center/PG/agent 证据。

// 页面可见合同（docs/spec/contracts/ip-quality-web.md）集中在此，文案调整只改一处。
const UI = {
  familyGroup: 'IP 地址族',
  collectDual: '采集 IPv4 / IPv6',
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

const basicInfo = (page: Page) => page.getByRole('region', { name: '基础信息' })
const sectionByHeading = (page: Page, name: string) => page.locator('section').filter({ has: page.getByRole('heading', { level: 2, name, exact: true }) })

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
    const trigger = page.getByRole('button', { name: '立即采集' })
    await expect(trigger).toBeEnabled()
    // 采集范围点击后才展开；按钮旁不常驻解释文字。
    await expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
    await expect(page.getByText('分别检测 IPv4 / IPv6')).toHaveCount(0)
    // 旧 agent 没有 IPv6 记录：切到 IPv6 只能说尚无结果，不能说缺族，也不借 IPv4 事实。
    await familyButton(page, 6).click()
    await expectSelection(page, 6)
    await expect(page.locator('.ipq-page')).toContainText(UI.noResult(6))
    await expect(page.locator('.ipq-page')).not.toContainText(UI.unavailable(6))
    await expect(page.getByLabel('报告身份')).toHaveCount(0)
    await familyButton(page, 4).click()
    await expectSelection(page, 4)
    await expect(page.getByLabel('报告身份')).toContainText('95.169.166.229')

    // 旧报告：只有短标记与旧版评级标注，没有可信度段落、完整性口号或百分比指标。
    await expect(page.locator('.badge').filter({ hasText: '旧版报告' })).toBeVisible()
    await expect(page.getByRole('note', { name: '服务探测可信度' })).toHaveCount(0)
    await expect(page.locator('.ipq-page')).not.toContainText('证据不完整')
    await expect(page.locator('.ipq-page')).not.toContainText('响应成功率')
    const riskHits = page.getByRole('group', { name: '风险命中' })
    await expect(riskHits).toContainText('旧版评级 · 低风险')

    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('legacy-report-top.png'), fullPage: true })

    // 失败行残留的 blocked/US/范围一律不展示。
    const servicesSection = page.locator('section.ipq-services')
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务解锁' })).toBeVisible()
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('5 可用')
    const netflix = servicesSection.locator('.ipq-service').filter({ hasText: 'Netflix' })
    await expect(netflix).toContainText('可用 · DE')
    await expect(servicesSection.locator('.ipq-service').filter({ hasText: 'ChatGPT' })).toContainText('部分可用')
    const disney = servicesSection.locator('.ipq-service').filter({ hasText: 'Disney+' })
    await expect(disney).toHaveClass(/ipq-service--unknown/)
    await expect(disney).toContainText('未知')
    await expect(disney).toContainText('服务要求人机验证')
    await expect(disney).not.toContainText('US')
    await expect(disney).not.toContainText('受限')
    await expect(servicesSection).not.toContainText('catalog_sample')
    await expect(servicesSection).not.toContainText('连接方式未检测')

    // 来源表：每个默认来源一行，失败来源只显示短失败态；未接入占位只在诊断里。
    const providerSection = sectionByHeading(page, '风险评分与因子')
    await expect(providerSection.getByRole('row')).toHaveCount(6)
    await expect(providerSection.getByRole('row').filter({ hasText: 'ip2location.io' })).toContainText('查询失败')
    await expect(riskHits).not.toContainText('VPN')
    await expect(providerSection).not.toContainText('91')
    await expect(providerSection).not.toContainText('maxmind')
    await expect(sectionByHeading(page, '邮件连通性')).toContainText('旧版报告无此项')
    await expect(sectionByHeading(page, '黑名单')).toContainText('旧版报告无此项')

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
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('探测已停用')
    // revision1 历史保留自己的来源事实：proxycheck 的 VPN 命中可见，摘要只作旧版评级标注。
    await expect(providerSection.getByRole('row').filter({ hasText: 'proxycheck.io' })).toContainText('VPN 是')
    await expect(riskHits).toContainText('旧版评级 · 低风险')
    await expectNoDocumentOverflow(page)

    // 查看最新报告回到显式 IPv4 latest。
    await page.getByRole('link', { name: '查看最新报告' }).click()
    await expectSelection(page, 4)
    await expect(servicesSection.getByLabel('服务状态统计')).not.toContainText('探测已停用')

    // 来源不明修订：保留旧规则警示；旧行没有 endpoint 诊断，不从 error_summary 反解析状态码。
    await historySection.scrollIntoViewIfNeeded()
    await historySection.locator('a[href*="report_id=ipq_hist_legacy"]').click()
    await expectSelection(page, 4, 'ipq_hist_legacy')
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('旧版规则')
    await expect(servicesSection).toContainText('服务返回异常状态')
    await expect(servicesSection).not.toContainText('HTTP 403')
    await expect(servicesSection).not.toContainText('http status 403')
    await expect(providerSection.getByRole('row').filter({ hasText: 'scamalytics' })).toContainText('查询失败')
    await expectNoDocumentOverflow(page)

    await page.getByRole('link', { name: '查看最新报告' }).click()
    await expectSelection(page, 4)
    await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()
    await expect(servicesSection.getByRole('heading', { level: 2, name: '服务解锁' })).toBeVisible()
    await expect(riskHits).toContainText('旧版评级 · 低风险')
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
    const servicesSection = page.locator('section.ipq-services')
    const historySection = page.locator('section.ipq-history')

    await expect(identity).toContainText(IPV4_IDENTITY.ipAddress)
    await expectSelection(page, 4)
    expect(api.requestCount('GET', ipqReportPath())).toBe(1)
    expect(api.requestCount('GET', ipqReportPath(4))).toBe(0)
    await expect(basicInfo(page)).toContainText(IPV4_IDENTITY.asn)
    await expect(identity).toContainText(ROUND1_TIME)
    // 结果优先：没有综合评级或证据完整性口号；风险只看服务端事实。
    await expect(page.getByRole('group', { name: '风险命中' })).toContainText('未见风险命中')
    await expect(pageRoot).not.toContainText('证据不完整')
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('6 可用')
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('1 受限')
    await expect(sectionByHeading(page, '邮件连通性')).toContainText('可连接 11/12')
    await expect(sectionByHeading(page, '黑名单')).toContainText('命中 0 / 已查 3')
    await expect(basicInfo(page)).toContainText('原生 IP')
    await expect(basicInfo(page).getByText('更多信息')).toBeVisible()
    await expect(basicInfo(page).locator('details.ipq-identity__more')).not.toHaveAttribute('open')
    await expect(sectionByHeading(page, '多源定位')).toContainText('Los Angeles')
    await expect(historySection).toContainText('共 2 份')
    await expect(pageRoot).not.toContainText(IPV6_IDENTITY.ipAddress)
    await expect(pageRoot).not.toContainText(IPV6_PREVIOUS_TIME)
    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('dual-stack-ipv4.png'), fullPage: true })

    await familyButton(page, 6).click()
    await expectSelection(page, 6)
    expect(api.requestCount('GET', ipqReportPath(6))).toBe(1)
    await expect(identity).toContainText(IPV6_IDENTITY.ipAddress)
    await expect(basicInfo(page)).toContainText(IPV6_IDENTITY.asn)
    await expect(pageRoot).not.toContainText(IPV4_IDENTITY.ipAddress)
    await expect(pageRoot).not.toContainText(IPV4_IDENTITY.asn)
    // IPv6 自己的服务端计数与模块：DNSBL 只在支持的地址族查询。
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('2 可用')
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('1 部分')
    await expect(servicesSection.getByLabel('服务状态统计')).toContainText('4 未知')
    await expect(sectionByHeading(page, '黑名单')).toContainText('命中 0 / 已查 1')
    await expect(sectionByHeading(page, '黑名单').getByRole('row').filter({ hasText: 'SpamCop' })).toContainText('不支持')
    await expect(sectionByHeading(page, '多源定位')).toContainText('Tokyo')
    await expect(sectionByHeading(page, '多源定位')).not.toContainText('Los Angeles')
    // YouTube 只有可信页面地区 CN：未知 + 页面地区，残留 US 与 CN 都不能推出受限。
    const youtube = servicesSection.locator('.ipq-service').filter({ hasText: 'YouTube Premium' })
    await expect(youtube).toContainText('未知')
    await expect(youtube).toContainText('页面地区 CN')
    await expect(youtube).not.toContainText('US')
    await expect(youtube).not.toContainText('受限')
    // 普通 403 不是业务拒绝。
    for (const name of ['Prime Video', 'Reddit']) {
      await expect(servicesSection.locator('.ipq-service').filter({ hasText: name })).not.toContainText('受限')
    }
    await expect(servicesSection.locator('.ipq-service').filter({ hasText: 'Netflix' })).not.toContainText('连接方式未检测')
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
  await expect(page.getByRole('group', { name: '风险命中' })).toContainText('未见风险命中')

  await familyButton(page, 6).click()
  await expectSelection(page, 6)
  const pageRoot = page.locator('.ipq-page')
  await expect(pageRoot).toContainText(UI.unavailable(6))
  await expect(page.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeVisible()
  // 失败占位不是出口、不是风险、不借 IPv4 事实。
  await expect(page.getByLabel('报告身份')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '风险评分与因子' })).toHaveCount(0)
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
  await page.getByRole('button', { name: '立即采集' }).click()
  await page.getByRole('menuitem', { name: UI.collectDual }).click()
  await expect(collectStatus(page)).toContainText('正在采集 IPv4 / IPv6')
  api.useProfile(ipQualityScenarioProfile(scenario, { collect: { ...COLLECT_AVAILABLE, request: collectRequest('dispatched', outcome) } }))
  await expect(page.getByRole('button', { name: /采集中/ })).toBeDisabled()
}

for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 900 }] as const) {
  test(`collect scope menu opens below the trigger, is keyboard operable and fits at ${viewport.width}px`, async ({ api, page }, testInfo) => {
    api.useProfile(ipQualityScenarioProfile('dual_stack', { extra: collectPostRoute('both_success') }))
    await page.setViewportSize(viewport)
    await page.goto(IPQ_PAGE_PATH)
    await expectSelection(page, 4)

    const trigger = page.getByRole('button', { name: '立即采集' })
    await trigger.focus()
    await page.keyboard.press('ArrowDown')
    const menu = page.getByRole('menu', { name: '立即采集' })
    await expect(menu).toBeVisible()
    const items = menu.getByRole('menuitem')
    await expect(items).toHaveText(['采集 IPv4', '采集 IPv6', UI.collectDual])
    await expect(items.first()).toBeFocused()
    // 菜单紧贴按钮下方，且不撑出视口。
    const [triggerBox, menuBox] = await Promise.all([trigger.boundingBox(), menu.boundingBox()])
    expect(menuBox!.y).toBeGreaterThanOrEqual(triggerBox!.y + triggerBox!.height - 1)
    expect(menuBox!.y - (triggerBox!.y + triggerBox!.height)).toBeLessThanOrEqual(12)
    expect(menuBox!.x + menuBox!.width).toBeLessThanOrEqual(viewport.width + 1)
    await expectNoDocumentOverflow(page)
    await page.screenshot({ path: testInfo.outputPath(`collect-menu-${viewport.width}.png`) })

    await page.keyboard.press('End')
    await expect(items.last()).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await expect(items.first()).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(menu).toHaveCount(0)
    await expect(trigger).toBeFocused()
    expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(0)
  })
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

// ---------------------------------------------------------------------------
// Center 补充资料：同一所选报告可见轮询，终态停止
// ---------------------------------------------------------------------------

test('running enrichment shows a single short status and fills the same report without navigation', async ({ api, page }) => {
  const scenario = ipQualityScenario('dual_stack')
  const running = enrichingView(scenario.views[4])
  api.useProfile(ipQualityScenarioProfile(scenario, { overrides: { default: { status: 200, body: running }, 4: { status: 200, body: running } } }))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)

  const enrichment = page.getByRole('status', { name: '补充资料状态' })
  await expect(enrichment).toContainText('查询中')
  // Agent 事实已可见；Center 资料未到只写短状态，不清空页面。
  await expect(sectionByHeading(page, '邮件连通性')).toContainText('可连接 11/12')
  await expect(sectionByHeading(page, '黑名单')).toContainText('查询中')

  // detail 路由返回同一报告的终态快照。
  await expect(sectionByHeading(page, '黑名单')).toContainText('命中 0 / 已查 3', { timeout: 10_000 })
  await expect(enrichment).toHaveCount(0)
  await expect(basicInfo(page)).toContainText('原生 IP')
  await expectSelection(page, 4)
  const polled = api.requestCount('GET', ipqDetailPath('ipq_v4_r1'))
  expect(polled).toBeGreaterThanOrEqual(1)
  await page.waitForTimeout(4_000)
  expect(api.requestCount('GET', ipqDetailPath('ipq_v4_r1'))).toBe(polled)
  await expectNoDocumentOverflow(page)
})
