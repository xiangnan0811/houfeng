import { vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { coreRouteProfile } from './fixtures/profiles'
import { expect, test } from './fixtures'
import { expectMainDocumentCsp } from './support/diagnostics'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'tablet', width: 1024, height: 768 },
  { name: 'mobile', width: 390, height: 900 },
] as const

const CORE_ROUTES = [
  { name: 'Dashboard', path: '/', heading: /^工作台$/, workflow: { role: 'link', name: '核对 VPS 库存' } },
  { name: 'VPS', path: '/vps', heading: /^VPS 资产$/, workflow: { role: 'link', name: '进入组合决策' } },
  { name: 'Asset Decisions', path: '/asset-decisions', heading: /^资产组合决策$/, workflow: { role: 'heading', name: '决策组扫描' } },
  { name: 'Monitoring', path: '/monitoring', heading: /^监控$/, workflow: { role: 'link', name: '从未关联 VPS 接入' } },
  { name: 'Targets', path: '/targets', heading: /^入口探测$/, workflow: { role: 'button', name: '新建目标' } },
  { name: 'Events', path: '/events', heading: /^事件流$/, workflow: { role: 'button', name: '高级筛选' } },
  { name: 'Command Audit', path: '/command-audit', heading: /^命令审计$/, workflow: { role: 'button', name: '高级筛选' } },
  { name: 'Record Inbox', path: '/record-inbox', heading: /^记录协作收件箱$/, workflow: { role: 'button', name: '查看“评论提及”的对象' } },
  { name: 'Providers', path: '/providers', heading: /服务商目录$/, workflow: { role: 'button', name: '新建服务商' } },
  { name: 'Subscriptions', path: '/subscriptions', heading: /订阅成本中枢$/, workflow: { role: 'button', name: '新建订阅' } },
  { name: 'Settings', path: '/settings', heading: /^系统设置$/, workflow: { role: 'tab', name: '监控策略' } },
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
      await expect(page.getByRole('heading', { name: route.heading })).toBeVisible()

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



