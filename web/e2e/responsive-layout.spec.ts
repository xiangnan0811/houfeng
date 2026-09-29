import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile, dashboardPopulatedProfile, vpsOverviewProfile } from './fixtures/profiles'
import { expect, test } from './fixtures'
import { expectNoDocumentOverflow } from './support/geometry'

async function sidebarWidth(page: import('@playwright/test').Page) {
  return page.locator('.sidebar').evaluate((element) => Math.round(element.getBoundingClientRect().width))
}

test('tablet widths start with the collapsed rail and the toggle expands it', async ({ api, page }) => {
  api.useProfile(dashboardPopulatedProfile())
  await page.setViewportSize({ width: 820, height: 1180 })
  await page.goto('/')
  const toggle = page.getByRole('button', { name: '展开侧边栏' })
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  expect(await sidebarWidth(page)).toBeLessThanOrEqual(64)
  await toggle.click()
  await expect(page.getByRole('button', { name: '折叠侧边栏' })).toHaveAttribute('aria-expanded', 'true')
  await expect.poll(() => sidebarWidth(page)).toBeGreaterThan(200)
  await expectNoDocumentOverflow(page)
})

test('crossing the tablet breakpoint resets the sidebar to that width default', async ({ api, page }) => {
  api.useProfile(dashboardPopulatedProfile())
  await page.setViewportSize({ width: 820, height: 1180 })
  await page.goto('/')
  await expect(page.getByRole('button', { name: '展开侧边栏' })).toBeVisible()
  expect(await sidebarWidth(page)).toBeLessThanOrEqual(64)

  // 未手动操作：拉到桌面宽度自动展开，再缩回平板自动收起。
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(page.getByRole('button', { name: '折叠侧边栏' })).toHaveAttribute('aria-expanded', 'true')
  await expect.poll(() => sidebarWidth(page)).toBeGreaterThan(200)
  await page.setViewportSize({ width: 1024, height: 900 })
  await expect(page.getByRole('button', { name: '展开侧边栏' })).toHaveAttribute('aria-expanded', 'false')
  await expect.poll(() => sidebarWidth(page)).toBeLessThanOrEqual(64)
})

test('dashboard judgement detail keeps a readable width on tablets', async ({ api, page }) => {
  api.useProfile(dashboardPopulatedProfile())
  for (const width of [820, 1024]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/')
    const tile = page.locator('.dashboard-judgement', { has: page.locator('.dashboard-judgement__trend') }).first()
    await expect(tile).toBeVisible()
    const [detail, trend] = await Promise.all([
      tile.locator('.dashboard-judgement__detail').boundingBox(),
      tile.locator('.dashboard-judgement__trend').boundingBox(),
    ])
    // 窄卡片：趋势图移到说明文字下方，说明不再保留右侧 112px 预留，文字可用宽度足够。
    const paddingRight = await tile.locator('.dashboard-judgement__detail').evaluate((element) => parseFloat(getComputedStyle(element).paddingRight))
    expect(paddingRight).toBe(0)
    expect(detail!.width - paddingRight).toBeGreaterThan(150)
    expect(trend!.y).toBeGreaterThanOrEqual(detail!.y + detail!.height - 1)
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  const wideTile = page.locator('.dashboard-judgement', { has: page.locator('.dashboard-judgement__trend') }).first()
  const [wideDetail, wideTrend] = await Promise.all([
    wideTile.locator('.dashboard-judgement__detail').boundingBox(),
    wideTile.locator('.dashboard-judgement__trend').boundingBox(),
  ])
  // 桌面宽度仍与说明并排：两者纵向重叠，趋势图在说明右侧预留区。
  expect(wideTrend!.y).toBeLessThan(wideDetail!.y + wideDetail!.height)
  expect(wideTrend!.x).toBeGreaterThan(wideDetail!.x + 150)
})

test('VPS directory inspector keeps renewal names readable on tablets', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/vps'))
  await page.setViewportSize({ width: 820, height: 1180 })
  await page.goto('/vps?workspace=ledger')
  const schedule = page.getByRole('region', { name: 'VPS 检查器' }).getByRole('region', { name: '续费排期' })
  const name = schedule.locator('.vps-overview__renewal-name').first()
  await expect(name).toBeVisible()
  // 手动展开侧栏让检查器变到最窄，仍须保持名称可读。
  await page.getByRole('button', { name: '展开侧边栏' }).click()
  await expect(page.getByRole('button', { name: '折叠侧边栏' })).toBeVisible()
  const date = schedule.locator('.vps-overview__renewal-date').first()
  const [nameBox, dateBox, rowBox] = await Promise.all([name.boundingBox(), date.boundingBox(), schedule.locator('.vps-overview__renewal').first().boundingBox()])
  // 两行布局：名称独占一行（占满整行宽度），日期在其下方。
  expect(dateBox!.y).toBeGreaterThanOrEqual(nameBox!.y + nameBox!.height - 1)
  expect(nameBox!.width).toBeGreaterThan(rowBox!.width * 0.8)
  expect(nameBox!.height).toBeLessThan(40)
  await expectNoDocumentOverflow(page)
})

test('narrow page headers and settings rows stack instead of squeezing labels', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  api.useProfile(coreRouteProfile('/vps'))
  await page.goto('/vps')
  const title = page.getByRole('heading', { level: 1, name: 'VPS 资产' })
  const titleBox = await title.boundingBox()
  const lineHeight = await title.evaluate((element) => parseFloat(getComputedStyle(element).lineHeight))
  expect(titleBox!.height).toBeLessThan(lineHeight * 1.5)
  const actionsBox = await page.locator('.page__head .page__actions').boundingBox()
  expect(actionsBox!.y).toBeGreaterThanOrEqual(titleBox!.y + titleBox!.height - 1)

  api.useProfile(coreRouteProfile('/settings'))
  await page.goto('/settings')
  const label = page.locator('.settings-row .sr-label', { hasText: '风格' })
  const group = page.getByRole('group', { name: '主题风格' })
  const [labelBox, groupBox] = await Promise.all([label.boundingBox(), group.boundingBox()])
  expect(groupBox!.y).toBeGreaterThanOrEqual(labelBox!.y + labelBox!.height - 1)
  expect(labelBox!.width).toBeGreaterThan(20)
  // 风格卡片左对齐：第一张卡片与标签左边缘对齐。
  const firstCard = await group.getByRole('button').first().boundingBox()
  expect(Math.abs(firstCard!.x - labelBox!.x)).toBeLessThanOrEqual(2)
  await expectNoDocumentOverflow(page)

  // 641–760px 的监控页头同样纵向并左对齐，不被页面自身规则居中。
  await page.setViewportSize({ width: 720, height: 900 })
  api.useProfile(coreRouteProfile('/monitoring'))
  await page.goto('/monitoring')
  const monitoringTitle = await page.getByRole('heading', { level: 1, name: '监控' }).boundingBox()
  const headBox = await page.locator('.monitoring-page__head').boundingBox()
  expect(Math.abs(monitoringTitle!.x - headBox!.x)).toBeLessThanOrEqual(2)
})

test('narrow shell search expands on tap, keeps results tappable, and gives the breadcrumb the freed width', async ({ api, page }) => {
  api.useProfile({
    ...coreRouteProfile('/vps'),
    [apiRouteKey('GET', '/api/monitoring-instances')]: { status: 200, body: [] },
    [apiRouteKey('GET', '/api/targets')]: { status: 200, body: [] },
    [apiRouteKey('GET', '/api/records/search?q=Tokyo&limit=4')]: { status: 200, body: { items: [], generation: 1 } },
    ...vpsOverviewProfile(),
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/vps')
  const input = page.getByRole('combobox', { name: '全局搜索' })
  const rest = await input.boundingBox()
  expect(rest!.width).toBeGreaterThanOrEqual(44)
  expect(rest!.width).toBeLessThanOrEqual(48)

  // 真实点按展开，输入并提交，结果面板与输入框左右对齐，点结果能导航。
  await input.click()
  await expect.poll(async () => (await input.boundingBox())!.width).toBeGreaterThan(300)
  await input.fill('Tokyo')
  await input.press('Enter')
  const option = page.getByRole('option', { name: /Tokyo Edge/ }).first()
  await expect(option).toBeVisible()
  const [inputBox, menuBox] = await Promise.all([input.boundingBox(), page.locator('.global-search__menu').boundingBox()])
  expect(Math.abs(menuBox!.x - inputBox!.x)).toBeLessThanOrEqual(1)
  expect(Math.abs((menuBox!.x + menuBox!.width) - (inputBox!.x + inputBox!.width))).toBeLessThanOrEqual(1)
  await option.click()
  await expect(page).toHaveURL(/\/vps\/vps_001/)

  // 面包屑页：搜索收起后，当前页标题可使用超过一半的顶栏宽度；点空白处收回搜索。
  const crumb = page.locator('.topbar .tp-vps-crumb')
  await expect(crumb).toBeVisible()
  const [crumbBox, topbarBox] = await Promise.all([crumb.boundingBox(), page.locator('.topbar').boundingBox()])
  expect(crumbBox!.width).toBeGreaterThan(topbarBox!.width * 0.5)
  await input.click()
  await expect.poll(async () => (await input.boundingBox())!.width).toBeGreaterThan(300)
  await page.mouse.click(200, 600)
  await expect.poll(async () => (await input.boundingBox())!.width).toBeLessThanOrEqual(48)
  await expectNoDocumentOverflow(page)
})
