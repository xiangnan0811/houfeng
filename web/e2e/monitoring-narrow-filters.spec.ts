import { expect, test } from './fixtures'
import { coreRouteProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

test('monitoring filters collapse behind a toggle on a phone while search and applied filters stay visible', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/monitoring'))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/monitoring?health=告警')

  const filters = page.locator('.monitoring-page__filters')
  const toggle = filters.getByRole('button', { name: '筛选 (1)' })
  await expect(toggle).toBeVisible()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(filters.getByRole('searchbox', { name: '搜索监控实例' })).toBeVisible()
  // 已生效的筛选以 chip 显示，可直接清除。
  await expect(filters.locator('.filter-bar__chips')).toContainText('健康状态: 告警')
  await expect(filters.locator('.filter-bar__controls-row > .filter-select').first()).toBeHidden()
  const toggleBox = await toggle.boundingBox()
  expect(toggleBox!.height).toBeGreaterThanOrEqual(44)

  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  for (const control of await filters.locator('.filter-bar__controls-row > :is(.filter-select, .filter-multiselect)').all()) {
    await expect(control).toBeVisible()
  }
  await expectNoDocumentOverflow(page)
})

test('monitoring filters stay fully visible without a toggle on desktop', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/monitoring'))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/monitoring')

  const filters = page.locator('.monitoring-page__filters')
  await expect(filters.getByRole('button', { name: /^筛选/ })).toBeHidden()
  for (const control of await filters.locator('.filter-bar__controls-row > :is(.filter-select, .filter-multiselect)').all()) {
    await expect(control).toBeVisible()
  }
})
