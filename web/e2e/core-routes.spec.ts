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
  { name: 'Monitoring', path: '/monitoring', heading: /^监控$/, workflow: { role: 'link', name: '从 VPS 接入 agent' } },
  { name: 'Targets', path: '/targets', heading: /^入口探测$/, workflow: { role: 'link', name: '组合决策' } },
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
