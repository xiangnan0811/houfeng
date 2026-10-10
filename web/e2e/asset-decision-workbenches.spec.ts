import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { ASSET_DECISION_NOW, assetDecisionWorkbenchesProfile } from './fixtures/assetDecisionProfiles'
import { expectNoDocumentOverflow } from './support/geometry'

// 资产决策次级工作区：卡片 + 单行扫描列表，所有行可见、不横向滚动，窄屏折行。
const WIDTHS = [1440, 1024, 390] as const

const THEMES = [
  { preset: 'houfeng', mode: 'dark' },
  { preset: 'houfeng', mode: 'light' },
  { preset: 'precision', mode: 'dark' },
  { preset: 'precision', mode: 'light' },
  { preset: 'observatory', mode: 'dark' },
] as const

const PANELS = [
  { entry: '已保存的决定', list: '已保存组合决策', rows: 4 },
  { entry: '自定义分组', list: '场景模板', rows: 9 },
  { entry: '续费窗口', list: '续费窗口', rows: 4 },
  { entry: '逐台处理', list: '逐台处理队列', rows: 6 },
] as const

async function openWorkbench(page: Page, entry: string): Promise<Locator> {
  await page.getByRole('navigation', { name: '资产决策辅助入口' }).getByRole('button', { name: entry }).click()
  const panel = page.getByRole('region', { name: entry })
  await expect(panel).toBeVisible()
  return panel
}

async function gotoDecisions(page: Page) {
  // 浏览器时钟与 fixture 共用同一个“今天”，剩余天数不随运行时刻漂移。
  await page.clock.setFixedTime(ASSET_DECISION_NOW)
  await page.goto('/asset-decisions')
  await expect(page.getByRole('navigation', { name: '资产决策辅助入口' })).toBeVisible()
  await page.evaluate(() => document.fonts.ready)
}

/** 行内名称与次要信息都要有可见宽度：名称省略、次要信息换行，不被压成零宽。 */
async function expectNamesVisible(list: Locator) {
  const widths = await list.locator('.asset-scan-row__name > *').evaluateAll((elements) => elements.map((element) => ({
    text: element.textContent ?? '',
    width: element.getBoundingClientRect().width,
  })))
  expect(widths.length).toBeGreaterThan(0)
  for (const item of widths) expect(item.width, item.text).toBeGreaterThanOrEqual(24)
}

for (const width of WIDTHS) {
  test(`asset decision workbenches list every row without horizontal scrolling at ${width}px`, async ({ api, page }) => {
    api.useProfile(assetDecisionWorkbenchesProfile())
    await page.setViewportSize({ width, height: 900 })
    await gotoDecisions(page)

    for (const panelSpec of PANELS) {
      const panel = await openWorkbench(page, panelSpec.entry)
      const list = panel.getByRole('list', { name: panelSpec.list })
      const rows = list.getByRole('listitem')
      await expect(rows).toHaveCount(panelSpec.rows)
      await expectNamesVisible(list)
      // 不再有眉题或横向滚动的宽表。
      await expect(panel.locator('.section-heading__eyebrow')).toHaveCount(0)
      await expect(panel.locator('table')).toHaveCount(0)

      // 宽屏一行排开；窄屏名称独占一行，其余信息在下方。
      const { name, last } = await rows.first().evaluate((row) => {
        const visible = [...row.children].filter((child) => child.getBoundingClientRect().width > 0)
        const box = (element: Element) => {
          const rect = element.getBoundingClientRect()
          return { y: rect.top, height: rect.height }
        }
        return { name: box(row.querySelector('.asset-scan-row__name')!), last: box(visible[visible.length - 1]!) }
      })
      if (width >= 1024) {
        expect(Math.abs(name.y + name.height / 2 - (last.y + last.height / 2)), panelSpec.entry).toBeLessThanOrEqual(6)
      } else {
        expect(last.y, panelSpec.entry).toBeGreaterThanOrEqual(name.y + name.height - 2)
      }
      await expectNoDocumentOverflow(page)
    }
  })
}

// 中间宽度下列多的逐台处理要按卡片宽度提前折行：名称不被挤成零宽，操作留在卡片内。
// ≤1100px 侧栏默认收起；820/920 另测手动展开侧栏后最窄的卡片。
const MID_LAYOUTS = [
  { width: 760, sidebar: 'collapsed' },
  { width: 820, sidebar: 'collapsed' },
  { width: 820, sidebar: 'expanded' },
  { width: 920, sidebar: 'collapsed' },
  { width: 920, sidebar: 'expanded' },
] as const

for (const layout of MID_LAYOUTS) {
  test(`asset decision workbenches keep names and actions visible at ${layout.width}px with the sidebar ${layout.sidebar}`, async ({ api, page }) => {
    api.useProfile(assetDecisionWorkbenchesProfile())
    await page.setViewportSize({ width: layout.width, height: 900 })
    await gotoDecisions(page)
    if (layout.sidebar === 'expanded') {
      await page.getByRole('button', { name: '展开侧边栏' }).click()
      await expect(page.getByRole('button', { name: '折叠侧边栏' })).toHaveAttribute('aria-expanded', 'true')
    }
    for (const panelSpec of PANELS) {
      const panel = await openWorkbench(page, panelSpec.entry)
      const list = panel.getByRole('list', { name: panelSpec.list })
      await expect(list.getByRole('listitem')).toHaveCount(panelSpec.rows)
      await expectNamesVisible(list)
      const overflow = await panel.evaluate((element) => {
        const right = element.getBoundingClientRect().right
        return Math.max(...[...element.querySelectorAll('.asset-scan-row > *')].map((child) => child.getBoundingClientRect().right - right))
      })
      expect(overflow, panelSpec.entry).toBeLessThanOrEqual(0)
      await expectNoDocumentOverflow(page)
    }
  })
}

test('asset decision workbenches surface the facts each scan row needs', async ({ api, page }) => {
  api.useProfile(assetDecisionWorkbenchesProfile())
  await page.setViewportSize({ width: 1440, height: 900 })
  await gotoDecisions(page)

  // 已保存的决定：只有需要复核的回读状态上徽章，已对齐记录不加噪声。
  const records = (await openWorkbench(page, '已保存的决定')).getByRole('list', { name: '已保存组合决策' }).getByRole('listitem')
  await expect(records.nth(0)).toContainText('受阻 1')
  await expect(records.nth(0)).toContainText('跟进 1/4')
  await expect(records.nth(0)).toContainText('2026-09-30')
  await expect(records.nth(1)).toContainText('需补证据 1')
  await expect(records.nth(2)).toContainText('有漂移 2')
  await expect(records.nth(3)).toContainText('已完成')
  await expect(records.nth(3).locator('.badge')).toHaveCount(1)

  // 自定义分组：7 个内置 + 2 个自定义模板全部可达，生效中的模板不标“启用”。
  const scenarios = await openWorkbench(page, '自定义分组')
  const templates = scenarios.getByRole('list', { name: '场景模板' }).getByRole('listitem')
  await expect(templates.filter({ hasText: '内置' })).toHaveCount(7)
  await expect(scenarios).not.toContainText('启用')
  await expect(templates.filter({ hasText: '欧洲旧机退役清单' })).toContainText('已归档')
  await expect(scenarios.getByRole('list', { name: '自定义资产组合' }).getByRole('listitem')).toHaveCount(3)

  // 续费窗口：日期 + 剩余天数，内部 ID 不进入可见层，入口只在标题行出现一次。
  const renewals = await openWorkbench(page, '续费窗口')
  const renewalRows = renewals.getByRole('list', { name: '续费窗口' }).getByRole('listitem')
  await expect(renewalRows.first()).toContainText('2026-10-06')
  await expect(renewalRows.first()).toContainText('4 天后')
  await expect(renewalRows.first().locator('[data-urgency]')).toHaveAttribute('data-urgency', 'soon')
  await expect(renewalRows.nth(3).locator('[data-urgency]')).toHaveAttribute('data-urgency', 'later')
  await expect(renewalRows.first().getByRole('link', { name: 'Frankfurt Mirror' })).toHaveAttribute('href', '/vps/vps_fra')
  await expect(renewals).not.toContainText('vps_')
  await expect(renewals).not.toContainText('sub_')
  await expect(renewals.getByRole('link', { name: '查看续费取舍组' })).toHaveCount(1)

  // 逐台处理：位置去重、缺订阅入口、汇率过期标记与归档入口。
  const queue = await openWorkbench(page, '逐台处理')
  const queueRows = queue.getByRole('list', { name: '逐台处理队列' }).getByRole('listitem')
  const tokyo = queueRows.filter({ hasText: 'Tokyo Edge' })
  await expect(tokyo).toContainText('Example Cloud · JP · Tokyo')
  await expect(tokyo).not.toContainText('Tokyo · Tokyo')
  await expect(tokyo).toContainText('汇率过期')
  await expect(queueRows.filter({ hasText: 'Singapore Probe' }).getByRole('button', { name: '缺订阅' })).toBeVisible()
  await expect(queueRows.filter({ hasText: 'Singapore Probe' })).toContainText('未关联监控')
  await expect(queue.getByRole('link', { name: '结束使用并归档' })).toHaveCount(2)
  await queue.getByRole('tab', { name: /缺订阅/ }).click()
  await expect(queueRows).toHaveCount(1)
})

for (const theme of THEMES) {
  test(`asset decision workbenches have no serious or critical axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile(assetDecisionWorkbenchesProfile())
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.setViewportSize({ width: 1440, height: 900 })
    await gotoDecisions(page)
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    for (const panelSpec of PANELS) {
      const panel = await openWorkbench(page, panelSpec.entry)
      // 等列表 settled 再扫描，避免只扫到加载态。
      await expect(panel.getByRole('list', { name: panelSpec.list }).getByRole('listitem')).toHaveCount(panelSpec.rows)
      // 自定义分组由模板与自定义组合两个请求组成，两边都 settled 才扫描。
      if (panelSpec.entry === '自定义分组') {
        await expect(panel.getByRole('list', { name: '自定义资产组合' }).getByRole('listitem')).toHaveCount(3)
      }
      const result = await new AxeBuilder({ page }).include('.asset-workbench').analyze()
      const blocking = result.violations
        .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
        .map((violation) => ({ panel: panelSpec.entry, id: violation.id, targets: violation.nodes.map((node) => node.target) }))
      expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
    }
  })
}
