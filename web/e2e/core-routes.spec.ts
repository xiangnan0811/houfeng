import { subscriptionOverviewFixture, vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile, dashboardPopulatedProfile } from './fixtures/profiles'
import { expect, test } from './fixtures'
import { expectMainDocumentCsp } from './support/diagnostics'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'tablet', width: 1024, height: 768 },
  { name: 'mobile', width: 390, height: 900 },
] as const

const CORE_ROUTES = [
  { name: 'Dashboard', path: '/', workflow: { role: 'link', name: '核对 VPS 库存' } },
  { name: 'VPS', path: '/vps', workflow: { role: 'link', name: '进入组合决策' } },
  { name: 'Asset Decisions', path: '/asset-decisions', workflow: { role: 'heading', name: '待决定的分组' } },
  { name: 'Monitoring', path: '/monitoring', workflow: { role: 'link', name: '从未关联 VPS 接入' } },
  { name: 'Targets', path: '/targets', workflow: { role: 'button', name: '新建目标' } },
  { name: 'Events', path: '/events', workflow: { role: 'button', name: '高级筛选' } },
  { name: 'Command Audit', path: '/command-audit', workflow: { role: 'button', name: '高级筛选' } },
  { name: 'Record Inbox', path: '/record-inbox', workflow: { role: 'button', name: '查看“评论提及”的对象' } },
  { name: 'Providers', path: '/providers', workflow: { role: 'button', name: '新建服务商' } },
  { name: 'Subscriptions', path: '/subscriptions', workflow: { role: 'button', name: '新建订阅' } },
  { name: 'Settings', path: '/settings', workflow: { role: 'tab', name: '监控策略' } },
] as const

for (const viewport of VIEWPORTS) {
  for (const route of CORE_ROUTES) {
    test(`${route.name} renders its primary workflow at ${viewport.width}x${viewport.height}`, async ({
      api,
      page,
    }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height })
      api.useProfile(coreRouteProfile(route.path))

      const response = await page.goto(route.path)

      expectMainDocumentCsp(response)
      await expect(page).toHaveURL((url) => url.pathname === route.path)
      const main = page.locator('main#main-content')
      await expect(main).toBeVisible()
      await expect(main).not.toBeEmpty()
      await expect(main.getByRole('heading', { level: 1 })).toBeVisible()

      const workflow = page.getByRole(route.workflow.role, {
        name: route.workflow.name,
        exact: true,
      })
      await expect(workflow).toBeVisible()
      await workflow.scrollIntoViewIfNeeded()
      await page.evaluate(() => document.fonts.ready)
      await expectLocatorNotClipped(workflow)
      await expectNoDocumentOverflow(page)
    })
  }
}

test('VPS preserves continuous search input and selection across workspace changes and history', async ({ api, page }) => {
  api.useProfile({
    ...coreRouteProfile('/vps'),
    'GET /api/vps': { status: 200, body: [{ ...vpsAssetFixture(), display_name: '东京 Tokyo Edge' }] },
  })
  await page.goto('/vps?workspace=ledger&selected=vps_001&source=review')
  const search = page.getByRole('searchbox', { name: '搜索 VPS' })
  await search.pressSequentially('东京 Tokyo')
  await expect(search).toHaveValue('东京 Tokyo')
  await page.getByRole('button', { name: '表格视图', exact: true }).click()
  await expect(page.getByRole('button', { name: '选择 东京 Tokyo Edge', exact: true })).toBeVisible()
  await expect(page).toHaveURL(url => url.searchParams.get('q') === '东京 Tokyo' && url.searchParams.get('selected') === 'vps_001' && url.searchParams.get('source') === 'review')
  await page.getByRole('button', { name: '目录视图', exact: true }).click()
  await expect(page.getByRole('region', { name: 'VPS 检查器' }).getByRole('heading', { name: '东京 Tokyo Edge' })).toBeVisible()
  await page.reload()
  await expect(search).toHaveValue('东京 Tokyo')
  await page.goto('/vps?workspace=workbench')
  await expect(page.getByRole('button', { name: '表格视图', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await page.goBack()
  await expect(page.getByRole('button', { name: '目录视图', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(search).toHaveValue('东京 Tokyo')
  await expect(page.getByRole('link', { name: '打开 VPS 详情' })).toHaveAttribute('href', '/vps/vps_001')
})

test('VPS inspector wraps long asset names and notes on both workspaces', async ({ api, page }) => {
  const longNote = 'reference:' + 'long_reference'.repeat(50)
  api.useProfile({
    ...coreRouteProfile('/vps'),
    'GET /api/vps': { status: 200, body: [{ ...vpsAssetFixture(), display_name: 'edge-long-hostname-'.repeat(15), note: longNote }] },
  })
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/vps?workspace=ledger&selected=vps_001')
    const ledgerInspector = page.getByRole('region', { name: 'VPS 检查器' })
    await expect(ledgerInspector).toContainText(longNote)
    expect(await ledgerInspector.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)

    await page.goto('/vps?workspace=workbench&selected=vps_001')
    await page.getByRole('button', { name: '选择 ' + 'edge-long-hostname-'.repeat(15), exact: true }).click()
    const quickPeek = page.getByRole('region', { name: 'VPS 快速查看' })
    await expect(quickPeek).toBeVisible()
    expect(await quickPeek.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  }
})

test('VPS retains the latest workspace preference through same-page sidebar navigation', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/vps'))
  await page.goto('/vps?workspace=ledger&q=Tokyo')
  await expect(page.getByRole('button', { name: '目录视图', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await page.reload()
  await page.getByRole('button', { name: '表格视图', exact: true }).click()
  await expect(page.getByRole('button', { name: '表格视图', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('link', { name: 'VPS', exact: true }).click()
  await expect(page).toHaveURL(url => url.pathname === '/vps' && url.search === '')
  await expect(page.getByRole('searchbox', { name: '搜索 VPS' })).toHaveValue('')
  await expect(page.getByRole('button', { name: '表格视图', exact: true })).toHaveAttribute('aria-pressed', 'true')
})




function subscriptionInsightsPopulatedProfile() {
  const costRows = Array.from({ length: 30 }, (_, index) => ({
    subscription_id: `sub_${index}`,
    vps_id: `vps_${index}`,
    vps_display_name: `Edge ${index}`,
    provider_id: `pv_${index % 25}`,
    provider_name: `Provider ${index % 25}`,
    display_name: `Edge ${index} plan`,
    cost_category: index % 3 === 0 ? 'compute' : 'network',
    labels: [],
    price: 10 + index,
    currency: 'CNY',
    monthly_price: 10 + index,
    monthly_price_base: 10 + index,
    yearly_price_base: (10 + index) * 12,
    base_currency: 'CNY',
    exchange_rate_status: 'identity' as const,
    renew_at: '2026-10-01T00:00:00Z',
    status: 'active',
    payment_method: 'card',
    country: 'JP',
    region: 'Kanto',
    lifecycle_status: 'active',
    renewal_decision: 'keep',
    budget_status: 'ok',
  }))
  const breakdown = (count: number, prefix: string) => Array.from({ length: count }, (_, index) => ({
    key: `${prefix}_${index}`,
    label: `${prefix} ${index}`,
    monthly_cost: 100 - index,
    yearly_cost: (100 - index) * 12,
    subscription_count: 1,
  }))
  const profile = coreRouteProfile('/subscriptions')
  return {
    ...profile,
    [apiRouteKey('GET', '/api/subscriptions/overview')]: {
      status: 200,
      body: subscriptionOverviewFixture({
        active_subscription_count: costRows.length,
        vps_costs: costRows,
        upcoming_renewals: costRows.slice(0, 12).map((row) => ({
          subscription_id: row.subscription_id,
          vps_id: row.vps_id,
          vps_display_name: row.vps_display_name,
          display_name: row.display_name,
          provider_name: row.provider_name,
          renew_at: row.renew_at,
          monthly_price_base: row.monthly_price_base,
          yearly_price_base: row.yearly_price_base,
          base_currency: 'CNY',
          currency: 'CNY',
          renewal_decision: 'keep',
          lifecycle_status: 'active',
          exchange_rate_status: 'identity' as const,
        })),
        archived_potential_costs: costRows.slice(0, 2),
        archived_potential_monthly_cost: 21,
      }),
    },
    [apiRouteKey('GET', '/api/subscriptions/statistics?window=year')]: {
      status: 200,
      body: {
        window: 'year',
        base_currency: 'CNY',
        total_monthly_cost: 735,
        total_yearly_cost: 8820,
        provider_breakdown: breakdown(25, 'Provider'),
        currency_breakdown: breakdown(1, 'CNY'),
        category_breakdown: breakdown(3, 'Category'),
        payment_breakdown: breakdown(2, 'Payment'),
        region_breakdown: breakdown(4, 'Region'),
        cost_month_buckets: Array.from({ length: 12 }, (_, index) => ({
          bucket: `2025-${String(index + 1).padStart(2, '0')}`,
          monthly_cost: 600 + index * 12,
          renewal_count: 2,
          budget_limit: 800,
          budget_currency: 'CNY',
          data_insufficient: false,
        })),
        renewal_month_buckets: [],
        budget_statuses: [],
      },
    },
  }
}

test('Subscription insights size panels to content and scroll only overlong lists', async ({ api, page }) => {
  api.useProfile(subscriptionInsightsPopulatedProfile())
  for (const viewport of [
    { width: 1440, height: 1000 },
    { width: 1024, height: 768 },
    { width: 861, height: 900 },
    { width: 1440, height: 1600 },
    // 窄屏两行排版下，超长列表同样只在上限内局部滚动。
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport)
    await page.goto('/subscriptions')
    const insights = page.getByRole('region', { name: '订阅成本洞察' })
    const archived = insights.getByRole('region', { name: '已归档资产潜在扣费' })
    const composition = insights.getByRole('region', { name: '成本构成' })
    await expect(archived).toBeVisible()
    await expect(composition).toContainText('Provider 24')
    await expect(insights.locator('.subscription-trend-chart-plot')).toBeVisible()
    await page.evaluate(() => document.fonts.ready)

    const measure = () => page.evaluate(() => {
      const box = (selector: string) => document.querySelector(selector)!.getBoundingClientRect()
      return {
        renewalBottom: box('.subscription-insight-panel--renewal').bottom,
        archivedTop: box('section[aria-label="已归档资产潜在扣费"]').top,
        plotHeight: box('.subscription-trend-chart-plot').height,
        trendHeight: Math.round(box('.subscription-insight-panel--trend').height),
        trendBottom: box('.subscription-insight-panel--trend').bottom,
        trendContentBottom: Math.max(box('.subscription-trend-readout').bottom, box('.subscription-trend-legend').bottom),
      }
    })
    const label = `${viewport.width}x${viewport.height}`
    const before = await measure()
    expect(before.renewalBottom, label).toBeLessThanOrEqual(before.archivedTop + 1)
    expect(before.plotHeight, label).toBeGreaterThanOrEqual(200)
    expect(before.trendContentBottom, label).toBeLessThanOrEqual(before.trendBottom)
    await archived.scrollIntoViewIfNeeded()
    await expect(archived).toBeInViewport()

    // 超出上限的长列表在面板内局部滚动，且可用键盘聚焦。
    const overflows = (element: HTMLElement | SVGElement) => element.scrollHeight > element.clientHeight
    expect(await composition.evaluate(overflows), label).toBe(true)
    await expect(composition).toHaveAttribute('tabindex', '0')
    const renewalQueue = insights.getByRole('region', { name: '续费队列' })
    expect(await renewalQueue.evaluate(overflows), label).toBe(true)
    await expect(renewalQueue).toHaveAttribute('tabindex', '0')

    await insights.getByRole('tab', { name: '排行', exact: true }).click()
    const ranking = insights.getByRole('region', { name: '月成本排行' })
    await expect(ranking).toBeVisible()
    expect(await ranking.evaluate(overflows), label).toBe(true)
    // 短列表完整展示，不出现局部滚动。
    await insights.getByLabel('构成维度').selectOption('category')
    await expect(composition).toContainText('Category 2')
    expect(await composition.evaluate(overflows), label).toBe(false)
    // 切换只影响下方面板，趋势面板不跳动。
    await expect.poll(async () => (await measure()).trendHeight).toBe(before.trendHeight)
  }
})

test('Shell search collapses to an icon on narrow screens and expands to a usable field on focus', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/'))
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  const input = page.getByRole('combobox', { name: '全局搜索' })
  await expect(input).toBeVisible()
  // 静止：收成触控尺寸的放大镜，把宽度让给标题。
  const rest = await input.boundingBox()
  expect(rest!.width).toBeLessThanOrEqual(48)
  expect(rest!.height).toBeGreaterThanOrEqual(44)
  const title = page.locator('.topbar .tp-page')
  await expect(title).toHaveText('工作台')
  // 标题允许超长时省略，但现行短标题必须完整显示，不被搜索框挤成“工…”。
  expect(await title.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true)

  // 聚焦：铺满顶栏，保留可用的文字宽度与触控高度。
  await input.focus()
  await expect.poll(async () => (await input.boundingBox())!.width).toBeGreaterThan(300)
  const metrics = await input.evaluate((element) => {
    const style = getComputedStyle(element)
    const kbd = element.parentElement?.querySelector('.global-search__kbd')
    return {
      paddingRight: parseFloat(style.paddingRight),
      height: element.getBoundingClientRect().height,
      kbdDisplay: kbd ? getComputedStyle(kbd).display : 'missing',
    }
  })
  expect(metrics.paddingRight).toBeLessThanOrEqual(12)
  expect(metrics.height).toBeGreaterThanOrEqual(44)
  expect(metrics.kbdDisplay).toBe('none')
  await expectNoDocumentOverflow(page)

  // 焦点离开：收回放大镜，已输入的查询保留。
  await input.fill('Tokyo')
  await page.locator('main#main-content').focus()
  await expect.poll(async () => (await input.boundingBox())!.width).toBeLessThanOrEqual(48)
  await expect(input).toHaveValue('Tokyo')
})

test('Sidebar scrolls on short viewports without squeezing any destination', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/'))
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 420 })
    await page.goto('/')
    const nav = page.getByRole('navigation', { name: '主导航' })
    await expect(nav.getByRole('link')).toHaveCount(12)
    await page.evaluate(() => document.fonts.ready)
    const heights = await nav.getByRole('link').evaluateAll((links) => links.map((link) => Math.round(link.getBoundingClientRect().height)))
    expect(Math.min(...heights), `${width}`).toBe(Math.max(...heights))
    expect(await nav.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
    const settings = nav.getByRole('link', { name: '设置' })
    await settings.focus()
    await expect(settings).toBeInViewport()
  }
})

test('Populated dashboard keeps bounded panels and one primary action on desktop widths', async ({ api, page }) => {
  api.useProfile(dashboardPopulatedProfile())
  // 1100px 是两栏布局的最窄视口（≤1100px 侧栏默认收起为图标栏，右栏随之变宽）；1024px 已切换为单栏。
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 1100, height: 800 }, { width: 1024, height: 768 }]) {
    await page.setViewportSize(viewport)
    await page.goto('/')
    await expect(page.getByRole('heading', { level: 1, name: '工作台' })).toBeVisible()
    await expect(page.getByRole('list', { name: '即将续费的订阅' }).getByRole('link')).toHaveCount(5)
    await expect(page.getByRole('list', { name: '最近状态变化' }).getByRole('listitem')).toHaveCount(5)
    await expect(page.getByRole('region', { name: '判断摘要' }).getByRole('link')).toHaveCount(3)
    await expect(page.getByRole('link', { name: '事件流', exact: true })).toHaveAttribute('href', '/events')
    const primary = page.getByRole('region', { name: '今日第一步' }).getByRole('link')
    await expect(primary).toHaveCount(1)
    await expect(primary).toHaveAttribute('href', '/events?severity=严重')
    await page.evaluate(() => document.fonts.ready)
    await expectLocatorNotClipped(primary)
    await expectNoDocumentOverflow(page)
    const label = `${viewport.width}x${viewport.height}`
    const panelOverflow = await page.locator('.dashboard-panel, .dashboard-evidence-lane, .dashboard-judgement').evaluateAll((elements) =>
      elements.filter((element) => element.scrollWidth > element.clientWidth + 1).map((element) => element.className))
    expect(panelOverflow, label).toEqual([])
    const clippedNames = await page.locator('.dashboard-renewal__name :is(strong, small)').evaluateAll((elements) =>
      elements.filter((element) => element.scrollWidth > element.clientWidth + 1).map((element) => element.textContent))
    expect(clippedNames, label).toEqual([])
    // 两栏时即将续费在右栏顶部；单栏时排在证据之前、最近动态在最后。
    const renewalsBox = await page.getByRole('region', { name: '即将续费' }).boundingBox()
    const observationBox = await page.getByRole('region', { name: '观测证据' }).boundingBox()
    const activityBox = await page.getByRole('region', { name: '最近动态' }).boundingBox()
    if (viewport.width > 1024) {
      expect(renewalsBox!.x, label).toBeGreaterThan(observationBox!.x + observationBox!.width - 1)
      expect(Math.abs(renewalsBox!.y - observationBox!.y), label).toBeLessThan(2)
    } else {
      expect(renewalsBox!.y + renewalsBox!.height, label).toBeLessThanOrEqual(observationBox!.y)
      expect(activityBox!.y, label).toBeGreaterThan(observationBox!.y)
    }
  }
})

test('Populated dashboard puts the renewal preview right after the judgements on a phone', async ({ api, page }) => {
  api.useProfile(dashboardPopulatedProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/')
  const judgements = await page.getByRole('region', { name: '判断摘要' }).boundingBox()
  const renewals = await page.getByRole('region', { name: '即将续费' }).boundingBox()
  const observation = await page.getByRole('region', { name: '观测证据' }).boundingBox()
  expect(renewals!.y).toBeGreaterThan(judgements!.y + judgements!.height - 1)
  expect(renewals!.y + renewals!.height).toBeLessThanOrEqual(observation!.y)
  await expectNoDocumentOverflow(page)
})
