import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { insightCalendarDate, SUBSCRIPTION_INSIGHTS_NOW, subscriptionInsightsProfile } from './fixtures/profiles'

// 成本洞察：面板随内容增高，列表一行排开、不裁切；窄屏折成两行。
const WIDTHS = [1440, 1024, 390] as const

const THEMES = [
  { preset: 'houfeng', mode: 'dark' },
  { preset: 'houfeng', mode: 'light' },
  { preset: 'precision', mode: 'dark' },
  { preset: 'precision', mode: 'light' },
  { preset: 'observatory', mode: 'dark' },
] as const

async function openInsights(page: Page) {
  // 浏览器与 fixture 共用同一个“今天”，剩余天数不随运行时刻漂移。
  await page.clock.setFixedTime(SUBSCRIPTION_INSIGHTS_NOW)
  await page.goto('/subscriptions?view=insights')
  const insights = page.getByRole('region', { name: '订阅成本洞察' })
  await expect(insights.getByRole('region', { name: '续费队列' })).toBeVisible()
  await page.evaluate(() => document.fonts.ready)
  return insights
}

async function expectSecondaryTextVisible(locator: ReturnType<Page['locator']>, count: number) {
  await expect(locator).toHaveCount(count)
  const widths = await locator.evaluateAll((elements) => elements.map((element) => ({
    text: element.textContent ?? '',
    width: element.getBoundingClientRect().width,
  })))
  for (const item of widths) expect(item.width, item.text).toBeGreaterThanOrEqual(24)
}

for (const width of WIDTHS) {
  test(`subscription insights show every row without clipping at ${width}px`, async ({ api, page }) => {
    api.useProfile(subscriptionInsightsProfile())
    await page.setViewportSize({ width, height: 900 })
    const insights = await openInsights(page)

    // 说明性文字已移除：月份切换提示只保留在图表无障碍名称里，归档说明收进标题行。
    await expect(insights).not.toContainText('切换月份')
    await expect(insights).not.toContainText('归档不代表服务商已停止扣费')
    await expect(insights.getByRole('region', { name: /月度成本与预算趋势图表，使用左右箭头键切换月份/ })).toBeVisible()
    await expect(insights.getByRole('list', { name: '趋势图图例' })).toBeVisible()

    const composition = insights.getByRole('region', { name: '成本构成' })
    await expect(composition.locator('.subscription-breakdown-row')).toHaveCount(4)
    expect(await composition.evaluate((element) => element.scrollHeight <= element.clientHeight)).toBe(true)

    const queue = insights.getByRole('region', { name: '续费队列' })
    const rows = queue.getByRole('button')
    await expect(rows).toHaveCount(4)
    expect(await queue.evaluate((element) => element.scrollHeight <= element.clientHeight)).toBe(true)
    const first = rows.first()
    await expect(first).toContainText(insightCalendarDate(4))
    await expect(first).toContainText('4 天后')
    await expect(first).toHaveAttribute('data-urgency', 'soon')
    await expect(first.locator('.badge')).toHaveText('待决定')
    await expect(rows.nth(1)).toContainText('汇率过期')
    await expect(rows.nth(2).locator('.badge')).toHaveText('决定不续费')
    await expect(rows.nth(3)).toHaveAttribute('data-urgency', 'later')

    // 宽屏短名称一行排开；窄屏日期行与名称行上下分开。
    const short = rows.nth(3)
    const when = await short.locator('.subscription-renewal-row__when').boundingBox()
    const name = await short.locator('.subscription-renewal-row__name').boundingBox()
    if (width >= 1024) {
      expect(Math.abs(when!.y - name!.y)).toBeLessThanOrEqual(4)
    } else {
      expect(name!.y).toBeGreaterThan(when!.y + when!.height - 2)
    }
    // 超长名称只省略名称本身，次要信息换行后仍可见，不被压成零宽。
    await expectSecondaryTextVisible(insights.locator('.subscription-renewal-row__name small, .subscription-insight-row__name small'), 8)
    await insights.getByRole('tab', { name: '排行', exact: true }).click()
    await expectSecondaryTextVisible(insights.locator('.subscription-ranking-row .subscription-insight-row__name small'), 5)
    await insights.getByRole('tab', { name: '饼图', exact: true }).click()

    const legendItems = insights.locator('.subscription-donut-legend li')
    await expect(legendItems).toHaveCount(5)
    await expect(legendItems.first()).toContainText('CNY 128.00')
    await expect(legendItems.first()).toContainText('39.9%')

    const archived = insights.getByRole('region', { name: '已归档资产潜在扣费' })
    await expect(archived).toContainText('不计入当前成本')
    await expect(archived.getByRole('link', { name: '核对扣费' })).toHaveAttribute('href', '/vps/vps_009')

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow).toBeLessThanOrEqual(0)
  })
}

test('subscription insights ranking keeps one row per subscription and renewal rows open details', async ({ api, page }) => {
  api.useProfile(subscriptionInsightsProfile())
  await page.setViewportSize({ width: 1440, height: 900 })
  const insights = await openInsights(page)
  await insights.getByRole('tab', { name: '排行', exact: true }).click()
  const ranking = insights.getByRole('region', { name: '月成本排行' })
  await expect(ranking.getByRole('button')).toHaveCount(5)
  const firstRow = ranking.getByRole('button').first()
  await expect(firstRow).toContainText('CNY 128.00')
  await expect(firstRow).toContainText('39.9%')
  // 短名称一行排开。
  const row = await ranking.getByRole('button').last().boundingBox()
  expect(row!.height).toBeLessThanOrEqual(44)

  await insights.getByRole('region', { name: '续费队列' }).getByRole('button').first().click()
  await expect(page).toHaveURL(/view=details/)
  await expect(page).toHaveURL(/vps_id=vps_003/)
})

for (const theme of THEMES) {
  test(`subscription insights have no serious or critical axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile(subscriptionInsightsProfile())
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.setViewportSize({ width: 1440, height: 900 })
    await openInsights(page)
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    const result = await new AxeBuilder({ page }).analyze()
    const blocking = result.violations
      .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
      .map((violation) => ({
        id: violation.id,
        impact: violation.impact,
        targets: violation.nodes.map((node) => node.target),
      }))
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  })
}
