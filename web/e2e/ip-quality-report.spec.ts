import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'

import type { IPQualityIPVersion } from '../src/lib/types'
import { expect, test } from './fixtures'
import { apiRouteKey, type ApiFixtureProfile, type ApiFixtureResponse } from './fixtures/contracts'
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
  // 报告页上下文（本轮未检测到…）与空态正文（最近一次采集未检测到…）共用同一事实短语。
  unavailable: (ipVersion: IPQualityIPVersion) => `未检测到 IPv${ipVersion} 可用本地地址`,
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
    await expect(page.getByLabel('报告身份')).toContainText('暂无报告')
    await expect(page.getByLabel('报告身份')).not.toContainText('95.169.166.229')
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
    // 历史报告也可发起新采集；采集后仍停留在该历史 ID（见历史采集场景）。
    await expect(page.getByRole('button', { name: '立即采集' })).toHaveCount(1)
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
    // 无阳性时风险区不给泛化的安全/阴性结论。
    await expect(page.getByRole('group', { name: '风险命中' })).toHaveText('')
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

// ---------------------------------------------------------------------------
// 吸顶报告上下文：滚动深处仍可辨识 IP/族/观察时间与历史/过期/上次有效状态
// ---------------------------------------------------------------------------

const reportContext = (page: Page) => page.getByRole('region', { name: '报告上下文' })

async function boxOf(locator: Locator) {
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  return box!
}

/** 吸顶后的上下文贴住滚动视口顶边（其上没有正文透出），完整位于视口内，且目标元素顶部不被它遮挡。 */
async function expectContextVisibleAbove(page: Page, target: Locator) {
  const viewport = page.viewportSize()!
  const scrollportTop = await page.locator('.main').evaluate((element) => element.getBoundingClientRect().top)
  const header = await boxOf(page.locator('.ipq-header'))
  const context = await boxOf(reportContext(page))
  // 页头已滚出视口时上下文必须吸顶并贴住视口顶边，不能留出正文透出的内边距带。
  if (header.y + header.height <= scrollportTop) expect(Math.abs(context.y - scrollportTop)).toBeLessThanOrEqual(1)
  expect(context.y + context.height).toBeLessThanOrEqual(viewport.height)
  const targetBox = await boxOf(target)
  expect(targetBox.y).toBeGreaterThanOrEqual(context.y + context.height - 1)
}

for (const viewport of [VIEWPORTS[0], VIEWPORTS[2]]) {
  test(`stale history context stays readable after scrolling every module at ${viewport.width}px`, async ({ api, page }, testInfo) => {
    const scenario = ipQualityScenario('dual_stack')
    const previous = scenario.details.ipq_v6_prev!
    scenario.details.ipq_v6_prev = { ...previous, summary: { ...previous.summary!, stale: true } }
    api.useProfile(ipQualityScenarioProfile(scenario))
    await page.setViewportSize(viewport)
    await page.goto(ipqPageURL({ ipVersion: 6, reportId: 'ipq_v6_prev' }))
    await expectSelection(page, 6, 'ipq_v6_prev')

    const context = reportContext(page)
    const marks = context.getByRole('group', { name: '报告状态' })
    // 历史与过期是两个独立标记；历史视图不展示当前族的最近尝试。
    await expect(marks).toContainText('历史报告')
    await expect(marks).toContainText(UI.stale)
    await expect(context.getByRole('note', { name: '地址族采集状态' })).toHaveCount(0)
    await expect(context.getByRole('link', { name: '查看最新报告' })).toHaveAttribute('href', `${IPQ_PAGE_PATH}?ip_version=6`)

    for (const heading of ['服务解锁', '风险评分与因子', '多源定位', '邮件连通性', '黑名单', '历史报告']) {
      const target = page.getByRole('heading', { level: 2, name: heading, exact: true })
      await target.scrollIntoViewIfNeeded()
      await expect(context.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
      await expect(context.getByLabel('报告身份')).toContainText(IPV6_PREVIOUS_TIME)
      await expect(context.getByLabel('报告身份')).toContainText('IPv6')
      const viewportBox = page.viewportSize()!
      const contextBox = await boxOf(context)
      expect(contextBox.y).toBeGreaterThanOrEqual(-1)
      expect(contextBox.y + contextBox.height).toBeLessThanOrEqual(viewportBox.height)
    }
    await page.screenshot({ path: testInfo.outputPath('stale-history-scrolled.png') })

    // 页头“历史”锚点：键盘激活后标题真实获得焦点、落在上下文下方不被遮挡，下一次 Tab 进入历史操作。
    await page.evaluate(() => document.querySelector('.main')?.scrollTo(0, 0))
    const historyAnchor = page.getByRole('link', { name: '历史', exact: true })
    const historyHeading = page.getByRole('heading', { level: 2, name: '历史报告', exact: true })
    await historyAnchor.focus()
    await page.keyboard.press('Enter')
    await expect(historyHeading).toBeFocused()
    await expect(page).toHaveURL(/#ip-quality-history-title$/)
    await expectContextVisibleAbove(page, historyHeading)
    await page.keyboard.press('Tab')
    expect(await page.evaluate(() => Boolean(document.activeElement?.closest('section.ipq-history')) && document.activeElement?.tagName !== 'H2')).toBe(true)
    await expectNoDocumentOverflow(page)

    // 修饰键点击属于新窗口/标签语义：不把焦点劫持到标题。文档级监听在页面处理之后才取消默认动作，只为不真的打开标签页。
    await page.evaluate(() => {
      document.querySelector('.main')?.scrollTo(0, 0)
      document.addEventListener('click', (event) => { if (event.ctrlKey || event.metaKey) event.preventDefault() })
    })
    await historyAnchor.focus()
    for (const modifier of ['ctrlKey', 'metaKey'] as const) {
      await historyAnchor.dispatchEvent('click', { button: 0, [modifier]: true })
      await expect(historyAnchor).toBeFocused()
    }
  })
}

test('latest report after a failed attempt marks the last valid result and the separate attempt time while scrolled', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('ipv4_lookup_failure'))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)
  const context = reportContext(page)
  await page.getByRole('heading', { level: 2, name: '历史报告', exact: true }).scrollIntoViewIfNeeded()
  await expect(context.getByRole('group', { name: '报告状态' })).toContainText(UI.lastValid)
  const attempt = context.getByRole('note', { name: '地址族采集状态' })
  await expect(attempt).toContainText(UI.lookupFailed(4))
  await expect(attempt).toContainText('最近尝试')
  await expect(attempt).toContainText(ROUND2_TIME)
  // 报告观察时间仍是上次有效报告自己的时间，不被最近尝试替换。
  await expect(context.getByLabel('报告身份')).toContainText(ROUND1_TIME)
  await expect(context.getByLabel('报告身份')).not.toContainText(ROUND2_TIME)
  await expectContextVisibleAbove(page, page.getByRole('heading', { level: 2, name: '历史报告', exact: true }))
})

test('a slow family switch keeps the selector in place and shows only the target family while loading', async ({ api, page }, testInfo) => {
  const release = deferred()
  const scenario = ipQualityScenario('dual_stack')
  api.useProfile(ipQualityScenarioProfile(scenario, {
    overrides: { 6: { status: 200, body: scenario.views[6], waitFor: release.promise } },
  }))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)
  const selectorBefore = await boxOf(page.getByRole('group', { name: UI.familyGroup }))

  await familyButton(page, 6).focus()
  await page.keyboard.press('Enter')
  await expect.poll(() => api.requestCount('GET', ipqReportPath(6))).toBe(1)
  const identity = page.getByLabel('报告身份')
  await expect(identity).toHaveAttribute('aria-busy', 'true')
  await expect(identity).toContainText('IPv6')
  await expect(page.locator('.ipq-page')).not.toContainText(IPV4_IDENTITY.ipAddress)
  await expect(page.locator('.ipq-page')).not.toContainText(ROUND1_TIME)
  await expect(page.getByRole('group', { name: '风险命中' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '立即采集' })).toHaveCount(0)
  await expect(familyButton(page, 6)).toBeFocused()
  const selectorLoading = await boxOf(page.getByRole('group', { name: UI.familyGroup }))

  release.resolve()
  await expect(identity).toContainText(IPV6_IDENTITY.ipAddress)
  await expect(familyButton(page, 6)).toBeFocused()
  const selectorAfter = await boxOf(page.getByRole('group', { name: UI.familyGroup }))
  // 记录实测几何而非预设跳动量；选择器位于上下文首行，数据高度变化不推动它。
  testInfo.annotations.push({ type: 'selector-boxes', description: JSON.stringify({ selectorBefore, selectorLoading, selectorAfter }) })
  expect(Math.abs(selectorLoading.y - selectorBefore.y)).toBeLessThanOrEqual(1)
  expect(Math.abs(selectorAfter.y - selectorBefore.y)).toBeLessThanOrEqual(1)
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(0)
})

test('a failed family load keeps the context, says the state is unknown and retries without collecting', async ({ api, page }) => {
  const scenario = ipQualityScenario('dual_stack')
  api.useProfile(ipQualityScenarioProfile(scenario, { overrides: { 6: { status: 500, body: { error: 'temporary failure' } } } }))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)

  await familyButton(page, 6).click()
  await expect(page.getByRole('heading', { name: 'IP 质量报告加载失败' })).toBeVisible()
  await expectSelection(page, 6)
  const identity = page.getByLabel('报告身份')
  await expect(identity).toContainText('加载失败，报告状态未知')
  await expect(identity).not.toContainText('暂无报告')
  await expect(page.locator('.ipq-page')).not.toContainText(IPV4_IDENTITY.ipAddress)
  await expect(page.getByRole('button', { name: '立即采集' })).toHaveCount(0)

  api.useProfile(ipQualityScenarioProfile(scenario))
  await page.getByRole('button', { name: '重试' }).click()
  await expect(identity).toContainText(IPV6_IDENTITY.ipAddress)
  await expectSelection(page, 6)
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(0)
})

test('an empty family explains why and keeps a single recovery path; unavailable collection defers to its status action', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('ipv4_only'))
  await page.goto(ipqPageURL({ ipVersion: 6 }))
  await expectSelection(page, 6)
  const empty = page.locator('.page-state')
  await expect(empty).toContainText(UI.unavailable(6))
  await expect(empty).toContainText('检查网络配置后可以重新检查')
  await expect(empty).toContainText('最近尝试')
  await expect(empty).not.toContainText('自动')
  await expect(page.getByRole('button', { name: '立即采集' })).toHaveCount(1)
  await expect(page.getByRole('button', { name: '立即采集' })).toBeEnabled()

  api.useProfile(ipQualityScenarioProfile('ipv4_only', { collect: { enabled: false, available: false, unavailable_reason: 'disabled' } }))
  await page.reload()
  await expect(empty).toContainText(UI.unavailable(6))
  await expect(empty).toContainText('当前无法发起采集')
  await expect(empty).not.toContainText('重新检查')
  await expect(page.getByRole('button', { name: '立即采集' })).toBeDisabled()
  // 恢复入口是可聚焦的真实链接，不依赖 hover/title。
  const action = collectStatus(page).getByRole('link', { name: '前往设置开启' })
  await action.focus()
  await expect(action).toBeFocused()
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
  await expect(page.getByRole('group', { name: '风险命中' })).toHaveText('')

  await familyButton(page, 6).click()
  await expectSelection(page, 6)
  const pageRoot = page.locator('.ipq-page')
  await expect(pageRoot).toContainText(UI.unavailable(6))
  await expect(page.getByRole('heading', { level: 1, name: 'IP 质量报告' })).toBeVisible()
  // 失败占位不是出口、不是风险、不借 IPv4 事实。
  await expect(page.getByLabel('报告身份')).toContainText('暂无报告')
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

test('a delayed history report with a conflicting family stays family-neutral until it resolves', async ({ api, page }) => {
  const release = deferred()
  const scenario = ipQualityScenario('dual_stack')
  api.useProfile(ipQualityScenarioProfile(scenario, {
    extra: { [apiRouteKey('GET', ipqDetailPath('ipq_v6_prev'))]: { status: 200, body: scenario.details.ipq_v6_prev, waitFor: release.promise } },
  }))
  const detailRequest = page.waitForRequest((request) => new URL(request.url()).pathname === ipqDetailPath('ipq_v6_prev'))
  await page.goto(ipqPageURL({ ipVersion: 4, reportId: 'ipq_v6_prev' }))
  await detailRequest

  // 解析前：query 的 IPv4 不冒充历史真实族；选择器、加载标题与身份都“待确定”，“查看最新报告”是非交互占位。
  const context = reportContext(page)
  await expect(familyButton(page, 4)).toHaveAttribute('aria-pressed', 'false')
  await expect(familyButton(page, 6)).toHaveAttribute('aria-pressed', 'false')
  await expect(context.getByLabel('报告身份')).toContainText('待确定')
  await expect(context.getByLabel('报告身份')).not.toContainText(/IPv[46]/)
  await expect(page.getByText('正在加载 IP 质量报告', { exact: true })).toBeVisible()
  await expect(page.getByText(/正在加载 IPv[46] 报告/)).toHaveCount(0)
  await expect(context.getByRole('link', { name: '查看最新报告' })).toHaveCount(0)
  await expect(context.getByText('查看最新报告', { exact: true })).toHaveAttribute('aria-disabled', 'true')

  release.resolve()
  await expectSelection(page, 6, 'ipq_v6_prev')
  await expect(context.getByLabel('报告身份')).toContainText(IPV6_IDENTITY.ipAddress)
  await expect(context.getByRole('link', { name: '查看最新报告' })).toHaveAttribute('href', `${IPQ_PAGE_PATH}?ip_version=6`)
  // detail API 只带 ID、只请求一次，不发族查询。
  expect(api.requestCount('GET', ipqDetailPath('ipq_v6_prev'))).toBe(1)
  expect(reportGETCount(api)).toBe(0)
})

test('history rows state each report’s own collection facts without a negative risk verdict', async ({ api, page }) => {
  // ipq_v4_prev 改为部分采集且名单一个都未查成；同一报告 ID 的 summary、latest 与 product 一起改，保持事实一致。
  const scenario = ipQualityScenario('dual_stack')
  const previous = scenario.details.ipq_v4_prev!
  previous.summary!.status = 'partial'
  previous.latest_report!.status = 'partial'
  previous.product!.blacklists = previous.product!.blacklists.map((row) => ({ ...row, status: 'unknown' as const, error_code: 'timeout' }))
  previous.product!.summary.blacklist_checked_count = 0
  expect(previous.summary!.product_summary).toBe(previous.product!.summary)
  api.useProfile(ipQualityScenarioProfile(scenario))
  await page.goto(ipqPageURL({ ipVersion: 4 }))
  await expectSelection(page, 4)

  const historySection = page.locator('section.ipq-history')
  const row = (reportId: string) => historySection.locator('li').filter({ has: page.locator(`a[href*="report_id=${reportId}"]`) })
  const currentRow = historySection.locator('li[aria-current="page"]')
  await historySection.scrollIntoViewIfNeeded()
  await expect(historySection).not.toContainText('未见风险命中')
  // 当前报告采集成功、无已知阳性：只陈述“采集成功”，服务数独立呈现。
  await expect(currentRow).toContainText('采集成功')
  await expect(currentRow).toContainText('服务 6 可用')
  await expect(row('ipq_v4_prev')).toContainText('部分采集')
  await expect(row('ipq_v4_prev')).toContainText('服务 6 可用')
  await expect(row('ipq_v4_prev')).not.toContainText('采集成功')

  // IPv6 本身是部分采集：同样只写过程事实。
  await familyButton(page, 6).click()
  await expectSelection(page, 6)
  await expect(historySection).not.toContainText('未见风险命中')
  await expect(currentRow).toContainText('部分采集')
  await expect(currentRow).toContainText('服务 2 可用')

  // 打开改过的旧报告：页头与历史行给出同一份事实，链接绑定原报告。
  await familyButton(page, 4).click()
  await expectSelection(page, 4)
  await row('ipq_v4_prev').getByRole('link').click()
  await expectSelection(page, 4, 'ipq_v4_prev')
  await expect(page.locator('.ipq-header__marks')).toContainText('部分采集')
  await expect(currentRow).toContainText('部分采集')
  expect(api.requestCount('GET', ipqDetailPath('ipq_v4_prev'))).toBe(1)
  await expectNoDocumentOverflow(page)
})

// ---------------------------------------------------------------------------
// POST 409 后补读状态失败：安全恢复提示；过时补读不能污染新一次操作
// ---------------------------------------------------------------------------

const CONFLICT_READ_FAILED = '本次采集未能发起，当前状态读取失败，请稍后重试'
const collectConflict = (): ApiFixtureResponse => ({ status: 409, body: { error: 'collect request conflict', code: 'collect_conflict' }, expectedBodyKeys: ['ip_versions'] })
const isCollectStatusRead = (request: { method: () => string, url: () => string }) => request.method() === 'GET' && new URL(request.url()).pathname === IPQ_COLLECT_PATH
const collectButton = (page: Page) => page.getByRole('button', { name: /^(立即采集|正在发起…|采集中…)$/ })

test('a 409 whose follow-up status read fails explains it safely and recovers on a manual retry', async ({ api, page }) => {
  api.useProfile(ipQualityScenarioProfile('dual_stack'))
  await page.goto(IPQ_PAGE_PATH)
  await expectSelection(page, 4)
  await expect(collectButton(page)).toBeEnabled()

  api.useProfile(ipQualityScenarioProfile('dual_stack', {
    extra: {
      [apiRouteKey('POST', IPQ_COLLECT_PATH)]: collectConflict(),
      [apiRouteKey('GET', IPQ_COLLECT_PATH)]: { status: 503, body: { error: 'collect status unavailable', code: 'service_unavailable' } },
    },
  }))
  const followUp = page.waitForResponse((response) => isCollectStatusRead(response.request()))
  await collectButton(page).click()
  await page.getByRole('menuitem', { name: UI.collectDual }).click()
  expect((await followUp).status()).toBe(503)
  await expect(collectStatus(page)).toContainText(CONFLICT_READ_FAILED)
  await expect(collectStatus(page)).not.toContainText('发起采集失败')
  await expect(collectButton(page)).toHaveAccessibleName('立即采集')
  await expect(collectButton(page)).toBeEnabled()
  // 原报告保留，不自动重发 POST。
  await expect(page.getByLabel('报告身份')).toContainText(IPV4_IDENTITY.ipAddress)
  await page.waitForTimeout(3_500)
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(1)

  api.useProfile(ipQualityScenarioProfile('dual_stack', { collect: { ...COLLECT_AVAILABLE, request: collectRequest('dispatched') }, extra: collectPostRoute('both_success') }))
  await collectButton(page).click()
  await page.getByRole('menuitem', { name: UI.collectDual }).click()
  await expect(collectStatus(page)).toContainText('正在采集 IPv4 / IPv6')
  await expect(page.getByText(CONFLICT_READ_FAILED)).toHaveCount(0)
  expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(2)
  await expectNoDocumentOverflow(page)
})

for (const [phase, staleRead] of [['pending', '503'], ['completed', 'old idle status']] as const) {
  test(`a stale 409 follow-up read (${staleRead}) cannot overwrite a newer collection whose POST is ${phase}`, async ({ api, page }) => {
    const releaseA = deferred()
    const releaseB = deferred()
    api.useProfile(ipQualityScenarioProfile('dual_stack'))
    await page.goto(IPQ_PAGE_PATH)
    await expectSelection(page, 4)
    await expect(collectButton(page)).toBeEnabled()

    // A：POST 409，随后的补读挂起。
    api.useProfile(ipQualityScenarioProfile('dual_stack', {
      extra: {
        [apiRouteKey('POST', IPQ_COLLECT_PATH)]: collectConflict(),
        [apiRouteKey('GET', IPQ_COLLECT_PATH)]: phase === 'pending'
          ? { status: 503, body: { error: 'collect status unavailable', code: 'service_unavailable' }, waitFor: releaseA.promise }
          : { status: 200, body: COLLECT_AVAILABLE, waitFor: releaseA.promise },
      },
    }))
    const followUpA = page.waitForRequest(isCollectStatusRead)
    await collectButton(page).click()
    await page.getByRole('menuitem', { name: UI.collectDual }).click()
    const requestA = await followUpA
    await expect(collectButton(page)).toHaveAccessibleName('立即采集')

    // B：同一会话内的新 POST，服务端返回不同的请求 ID。
    const requestB = { ...collectRequest('pending'), request_id: 'ipqc_e2e_b' }
    api.useProfile(ipQualityScenarioProfile('dual_stack', {
      collect: { ...COLLECT_AVAILABLE, request: { ...collectRequest('dispatched'), request_id: 'ipqc_e2e_b' } },
      extra: {
        [apiRouteKey('POST', IPQ_COLLECT_PATH)]: {
          status: 202,
          body: { ...COLLECT_AVAILABLE, request: requestB },
          expectedBodyKeys: ['ip_versions'],
          ...(phase === 'pending' ? { waitFor: releaseB.promise } : {}),
        },
      },
    }))
    const postB = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === IPQ_COLLECT_PATH)
    await collectButton(page).click()
    await page.getByRole('menuitem', { name: UI.collectDual }).click()
    await postB
    if (phase === 'pending') await expect(collectButton(page)).toHaveAccessibleName('正在发起…')
    else await expect(collectStatus(page)).toContainText('正在采集 IPv4 / IPv6')

    // 释放 A 的旧补读：既不给 B 套上冲突错误，也不用旧状态覆盖 B 的提交或进度。
    const responseA = page.waitForResponse((response) => response.request() === requestA)
    releaseA.resolve()
    await responseA
    await expect(page.getByText(CONFLICT_READ_FAILED)).toHaveCount(0)
    if (phase === 'pending') {
      await expect(collectButton(page)).toHaveAccessibleName('正在发起…')
      await expect(collectButton(page)).toBeDisabled()
      releaseB.resolve()
    }
    await expect(collectStatus(page)).toContainText('正在采集 IPv4 / IPv6')
    await expect(collectButton(page)).toHaveAccessibleName('采集中…')
    await expect(collectButton(page)).toBeDisabled()
    await expect(page.getByText(CONFLICT_READ_FAILED)).toHaveCount(0)
    expect(api.requestCount('POST', IPQ_COLLECT_PATH)).toBe(2)
    releaseB.resolve()
  })
}

// ---------------------------------------------------------------------------
// 来源分歧：每个字段可核对来源与值，入口定位与焦点正确，样本按钮读出供应商
// ---------------------------------------------------------------------------

test('every disagreeing identity field leads to its own source evidence with focus and vendor-named samples', async ({ api, page }) => {
  const scenario = ipQualityScenario('dual_stack')
  const product = scenario.views[4].product!
  const [first, second] = product.providers.filter((row) => row.status === 'success' && row.details)
  if (!first?.details || !second?.details) throw new Error('fixture needs two successful providers')
  // 同一供应商：匿名样本（FR/Anon Co）与认证主样本；只有认证主样本参与比较与展开。
  const anonymous = { ...first, usage_type: 'isp', company_type: 'isp', details: { ...first.details, registered_country_code: 'fr', company_name: 'Anon Co' } }
  const authenticated = { ...first, usage_type: 'hosting', company_type: 'hosting', details: { ...first.details, method: 'center_api' as const, api_tier: 'authenticated' as const, country_code: 'US', region: 'Oregon', city: 'Portland', registered_country_code: 'us', company_name: 'Alpha Hosting' } }
  const other = { ...second, usage_type: 'isp', company_type: 'isp', details: { ...second.details, country_code: 'DE', region: 'Hesse', city: 'Frankfurt', registered_country_code: 'DE', company_name: 'Beta Net' } }
  product.providers = product.providers.flatMap((row) => (row === first ? [anonymous, authenticated] : row === second ? [other] : [row]))
  api.useProfile(ipQualityScenarioProfile(scenario))
  await page.goto(ipqPageURL({ ipVersion: 4 }))
  await expectSelection(page, 4)

  const identity = basicInfo(page)
  await identity.getByText('更多信息').click()
  for (const [field, section, anchor] of [
    ['城市', '多源定位', 'ip-quality-location-title'],
    ['使用地', '多源定位', 'ip-quality-location-title'],
    ['省 / 州', '多源定位', 'ip-quality-location-title'],
    ['使用类型', '风险因子', 'ip-quality-factors-title'],
    ['公司类型', '风险因子', 'ip-quality-factors-title'],
  ] as const) {
    await expect(identity.getByRole('link', { name: `${field}来源有分歧，查看${section}` })).toHaveAttribute('href', `#${anchor}`)
  }

  // 注册地与公司名称：就近披露，列出主样本的供应商、方式与原值；方向标记关闭指右、打开指下，名称不含字形。
  const caretTransform = (details: Locator) => details.locator('summary .ipq-disagreement__caret').evaluate((element) => getComputedStyle(element).transform)
  const registration = identity.locator('details.ipq-disagreement').filter({ has: page.locator('summary[aria-label="查看注册地来源分歧"]') })
  const registrationSummary = registration.locator('summary')
  await expect(registration).not.toHaveAttribute('open')
  await expect(registrationSummary).toHaveAccessibleName('查看注册地来源分歧')
  await expect(registration.locator('.ipq-disagreement__caret')).toBeVisible()
  expect(await caretTransform(registration)).toBe('none')
  await registrationSummary.press('Enter')
  await expect(registration).toHaveAttribute('open')
  expect(await caretTransform(registration)).not.toBe('none')
  await expect(registrationSummary).toHaveCSS('border-top-style', 'solid')
  await expect(registration).toContainText(`${first.provider} · 认证US`)
  await expect(registration).toContainText(`${second.provider}`)
  await expect(registration).toContainText('DE')
  await expect(registration).not.toContainText('FR')
  await registrationSummary.press('Enter')
  await expect(registration).not.toHaveAttribute('open')
  expect(await caretTransform(registration)).toBe('none')
  await expect(registrationSummary).toHaveCSS('border-top-style', 'dashed')
  // 跳转链接保持原文字链接，不带展开标记。
  await expect(identity.locator('a .ipq-disagreement__caret')).toHaveCount(0)
  const company = identity.locator('details.ipq-disagreement').filter({ has: page.locator('summary[aria-label="查看公司名称来源分歧"]') })
  const companySummary = company.locator('summary')
  await expect(companySummary).toHaveAccessibleName('查看公司名称来源分歧')
  expect(await caretTransform(company)).toBe('none')
  await companySummary.focus()
  await page.keyboard.press('Space')
  await expect(company).toHaveAttribute('open')
  expect(await caretTransform(company)).not.toBe('none')
  await expect(company).toContainText('Alpha Hosting')
  await expect(company).toContainText('Beta Net')
  await expect(company).not.toContainText('Anon Co')
  await companySummary.click()
  await expect(company).not.toHaveAttribute('open')
  expect(await caretTransform(company)).toBe('none')
  await companySummary.click()
  await expect(company).toHaveAttribute('open')
  await expectNoDocumentOverflow(page)

  // 键盘激活：焦点移到目标标题，且标题不被吸顶上下文遮挡。
  await identity.getByRole('link', { name: '城市来源有分歧，查看多源定位' }).press('Enter')
  const locationHeading = page.getByRole('heading', { level: 2, name: '多源定位', exact: true })
  await expect(locationHeading).toBeFocused()
  await expectContextVisibleAbove(page, locationHeading)
  await identity.getByRole('link', { name: '使用类型来源有分歧，查看风险因子' }).click()
  const factorsHeading = page.getByRole('heading', { level: 3, name: '风险因子', exact: true })
  await expect(factorsHeading).toBeFocused()
  await expectContextVisibleAbove(page, factorsHeading)

  // 样本按钮：读出供应商与动作，同一按钮切换且焦点不丢。
  const toggle = page.getByRole('button', { name: `展开 ${first.provider} 的另外 1 个样本` })
  await toggle.focus()
  await page.keyboard.press('Enter')
  const collapse = page.getByRole('button', { name: `收起 ${first.provider} 的样本` })
  await expect(collapse).toBeFocused()
  await expect(collapse).toHaveAttribute('aria-expanded', 'true')
  await expectNoDocumentOverflow(page)
})

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
  const collectRequestSent = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === IPQ_COLLECT_PATH)
  await page.getByRole('menuitem', { name: UI.collectDual }).click()
  expect((await collectRequestSent).postDataJSON()).toEqual({ ip_versions: [4, 6] })
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
