import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { coreRouteProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

test('theme preset cards preview each palette with its own tokens and switch the theme', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/settings'))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/settings')
  const group = page.getByRole('group', { name: '主题风格' })
  await expect(group).toBeVisible()

  // 三张预览的强调色互不相同，说明各自用自己风格的 token 渲染，而不是当前主题的颜色。
  const accents = await group.locator('.theme-preset__line--accent').evaluateAll((lines) => lines.map((line) => getComputedStyle(line).backgroundColor))
  expect(accents).toHaveLength(3)
  expect(new Set(accents).size).toBe(3)
  await expect(group.getByRole('button', { name: '观测台' })).toContainText('仅深色')

  await expect(group.getByRole('button', { name: '候风' })).toHaveAttribute('aria-pressed', 'true')
  await group.getByRole('button', { name: '精密' }).click()
  await expect(group.getByRole('button', { name: '精密' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('html')).toHaveClass(/theme-precision-dark/)

  // 切到浅色后可切换的风格改用浅色预览，仅深色的观测台仍为深色。
  await page.getByRole('group', { name: '主题明暗' }).getByRole('button', { name: '浅色' }).click()
  await expect(group.locator('.theme-preset__preview.theme-houfeng-light')).toHaveCount(1)
  await expect(group.locator('.theme-preset__preview.theme-precision-light')).toHaveCount(1)
  await expect(group.locator('.theme-preset__preview.theme-observatory-dark')).toHaveCount(1)
  await expect(group.getByRole('button', { name: '观测台' })).toHaveAccessibleDescription('仅深色')
  await expectNoDocumentOverflow(page)
})

test('asset decision auxiliary badges keep AA contrast while hovered in precision-light', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/asset-decisions'))
  await page.addInitScript(() => {
    localStorage.setItem('houfeng.theme.preset', 'precision')
    localStorage.setItem('houfeng.theme.mode', 'light')
  })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/asset-decisions')
  const strip = page.getByRole('navigation', { name: '资产决策辅助入口' })
  for (const item of await strip.getByRole('button').all()) {
    await item.hover()
    const result = await new AxeBuilder({ page }).include('nav[aria-label="资产决策辅助入口"]').withRules(['color-contrast']).analyze()
    expect(result.violations.map((violation) => violation.nodes.map((node) => node.target))).toEqual([])
  }
})

test('the quiet asset decision judgement spans the full row at tablet width', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/asset-decisions'))
  await page.setViewportSize({ width: 768, height: 900 })
  await page.goto('/asset-decisions')
  const summary = page.getByLabel('资产组合决策当前判断')
  await expect(summary.getByRole('heading', { name: '当前没有需要处理的组合决策' })).toBeVisible()
  const [summaryBox, leadBox] = await Promise.all([summary.boundingBox(), summary.locator('.asset-decision-command-summary__lead').boundingBox()])
  expect(leadBox!.width).toBeGreaterThan(summaryBox!.width * 0.9)
  await expectNoDocumentOverflow(page)
})

test('asset decisions stay quiet without work and render auxiliary entries as one segmented track', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/asset-decisions'))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/asset-decisions')
  const summary = page.getByLabel('资产组合决策当前判断')
  await expect(summary.getByRole('heading', { name: '当前没有需要处理的组合决策' })).toBeVisible()
  // 合同：无待办时不渲染统计卡与警示色，判断占满整行。
  await expect(summary.getByLabel('资产组合决策当前事实')).toHaveCount(0)
  const lead = summary.locator('.asset-decision-command-summary__lead')
  const [summaryBox, leadBox] = await Promise.all([summary.boundingBox(), lead.boundingBox()])
  expect(leadBox!.width).toBeGreaterThan(summaryBox!.width * 0.9)

  const strip = page.getByRole('navigation', { name: '资产决策辅助入口' })
  const items = strip.getByRole('button')
  await expect(items).toHaveCount(4)
  // 入口不再带彩色左边框：左边框宽度为 0，状态只由徽章表达。
  const leftBorders = await items.evaluateAll((buttons) => buttons.map((button) => getComputedStyle(button).borderLeftWidth))
  expect(new Set(leftBorders)).toEqual(new Set(['0px']))
  const tops = await items.evaluateAll((buttons) => buttons.map((button) => Math.round(button.getBoundingClientRect().top)))
  expect(new Set(tops).size).toBe(1)

  await items.filter({ hasText: '单台队列' }).click()
  const active = items.filter({ hasText: '单台队列' })
  await expect(active).toHaveAttribute('aria-pressed', 'true')
  expect(await active.evaluate((button) => getComputedStyle(button).boxShadow)).not.toBe('none')
  await expectNoDocumentOverflow(page)
})
