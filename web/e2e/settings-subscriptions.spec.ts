import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { SETTINGS_NOW, settingsSubscriptionsProfile } from './fixtures/settingsProfiles'
import { expectNoDocumentOverflow } from './support/geometry'

// 设置 › 订阅：紧凑成本字段、一行一条月预算，新增预算与成本设置各自保存。
const THEMES = [
  { preset: 'houfeng', mode: 'dark' },
  { preset: 'houfeng', mode: 'light' },
  { preset: 'precision', mode: 'dark' },
  { preset: 'precision', mode: 'light' },
  { preset: 'observatory', mode: 'dark' },
] as const

for (const width of [1440, 1024, 390] as const) {
  test(`subscription settings show compact fields and one budget per row at ${width}px`, async ({ api, page }) => {
    api.useProfile(settingsSubscriptionsProfile())
    await page.setViewportSize({ width, height: 900 })
    await page.clock.setFixedTime(SETTINGS_NOW)
    await page.goto('/settings?tab=subscriptions')
    await expect(page.getByRole('heading', { name: '成本基准与汇率' })).toBeVisible()
    await page.evaluate(() => document.fonts.ready)

    await expect(page.getByLabel('基准货币')).toHaveValue('CNY')
    await expect(page.getByLabel('提前提醒天数')).toHaveValue('14, 7, 1')
    // 标签不再被转成大写英文。
    await expect(page.getByText('汇率来源', { exact: true })).toBeVisible()
    await expect(page.getByText('汇率 PROVIDER')).toHaveCount(0)

    const rows = page.getByRole('list', { name: '月预算' }).getByRole('listitem')
    await expect(rows).toHaveCount(6)
    await expect(rows.first()).toContainText('当前生效')
    await expect(rows.first()).toContainText('CNY 360.00')
    await expect(rows.first()).toContainText('预警 85%')
    await expect(page.getByRole('list', { name: '月预算' })).not.toContainText('全局月预算')
    await expect(page.getByText('当前生效')).toHaveCount(1)

    if (width >= 1024) {
      // 宽屏每条预算一行：月份、金额、预警与编辑按钮垂直居中在同一行。
      const spreads = await rows.evaluateAll((items) => items.map((item) => {
        const centers = [...item.children].map((child) => {
          const rect = child.getBoundingClientRect()
          return rect.top + rect.height / 2
        })
        return Math.max(...centers) - Math.min(...centers)
      }))
      for (const spread of spreads) expect(spread).toBeLessThanOrEqual(4)
    }
    // 三位币种完整可见，不被紧凑输入裁切。
    expect(await page.getByLabel('基准货币').evaluate((input) => input.scrollWidth <= input.clientWidth)).toBe(true)
    await expectNoDocumentOverflow(page)
  })
}

test('subscription settings show the Fixer key only for Fixer and save budgets on their own', async ({ api, page }) => {
  api.useProfile({
    ...settingsSubscriptionsProfile(),
    [apiRouteKey('PUT', '/api/subscription-monthly-budgets/2026-11')]: {
      status: 200,
      expectedBodyKeys: ['base_currency', 'monthly_limit', 'note', 'warning_pct'],
      body: { budget_month: '2026-11-01', base_currency: 'CNY', monthly_limit: 380, warning_pct: 80, note: '', created_at: '2026-10-02T04:00:00Z', updated_at: '2026-10-02T04:00:00Z' },
    },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.clock.setFixedTime(SETTINGS_NOW)
  await page.goto('/settings?tab=subscriptions')

  await expect(page.getByLabel('Fixer key')).toBeVisible()
  await expect(page.getByLabel('Fixer key')).toHaveAttribute('placeholder', 'fx_****8a2c')
  await page.getByLabel('汇率来源').selectOption('frankfurter')
  await expect(page.getByLabel('Fixer key')).toHaveCount(0)

  const writes: string[] = []
  page.on('request', (request) => {
    if (request.method() !== 'GET' && request.url().includes('/api/')) writes.push(`${request.method()} ${new URL(request.url()).pathname}`)
  })
  const addForm = page.getByRole('form', { name: '新增月预算' })
  await addForm.getByLabel('预算月份').fill('2026-11')
  await addForm.getByLabel('月预算 CNY').fill('380')
  await addForm.getByRole('button', { name: '添加预算' }).click()
  await expect(addForm.getByRole('status')).toHaveText('预算已保存')
  expect(writes).toEqual(['PUT /api/subscription-monthly-budgets/2026-11'])
})

test('subscription settings collapse an empty budget list into one line', async ({ api, page }) => {
  api.useProfile(settingsSubscriptionsProfile({ budgets: [] }))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.clock.setFixedTime(SETTINGS_NOW)
  await page.goto('/settings?tab=subscriptions')
  await expect(page.getByText('尚未配置月预算')).toBeVisible()
  await expect(page.getByRole('list', { name: '月预算' })).toHaveCount(0)
})

for (const theme of THEMES) {
  test(`subscription settings have no serious or critical axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile({
      ...settingsSubscriptionsProfile(),
      [apiRouteKey('PUT', '/api/subscription-monthly-budgets/2026-04')]: {
        status: 200,
        expectedBodyKeys: ['base_currency', 'monthly_limit', 'note', 'warning_pct'],
        body: { budget_month: '2026-04-01', base_currency: 'CNY', monthly_limit: 300, warning_pct: 80, note: '季度复核后下调', created_at: '2026-09-20T08:00:00Z', updated_at: '2026-10-02T04:00:00Z' },
      },
      [apiRouteKey('POST', '/api/subscriptions/exchange-rates/refresh')]: {
        status: 202,
        expectNoBody: true,
        body: {
          items: [{ provider: 'fixer', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'missing', refresh_status: 'failed', attempt_count: 1 }],
        },
      },
    })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.clock.setFixedTime(SETTINGS_NOW)
    await page.goto('/settings?tab=subscriptions')
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    await expect(page.getByRole('list', { name: '月预算' }).getByRole('listitem')).toHaveCount(6)
    const scan = async (state: string) => {
      const result = await new AxeBuilder({ page }).include('.subscription-settings').analyze()
      return result.violations
        .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
        .map((violation) => ({ state, id: violation.id, targets: violation.nodes.map((node) => node.target) }))
    }
    // 第一次在成功提示可见时扫描（再次编辑会清掉它）。
    await page.getByRole('button', { name: '编辑 2026-04 月预算' }).click()
    await page.getByRole('form', { name: '编辑 2026-04 月预算' }).getByRole('button', { name: '保存预算' }).click()
    await expect(page.getByRole('status').filter({ hasText: '2026-04 月预算已保存' })).toBeVisible()
    const blocking = await scan('success notice')
    // 第二次覆盖失败提示、行内编辑与批量覆盖展开。
    await page.getByRole('button', { name: '刷新汇率' }).click()
    await expect(page.getByRole('alert').filter({ hasText: '补取失败：USD' })).toBeVisible()
    await page.getByRole('button', { name: '编辑 2026-07 月预算' }).click()
    await page.getByRole('checkbox', { name: '批量覆盖历史月份' }).check()
    blocking.push(...await scan('alert + editing + bulk'))
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  })
}
