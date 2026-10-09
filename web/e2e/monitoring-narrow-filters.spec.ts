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

  // 收起时筛选区明显更矮，把首屏让给列表。
  const collapsedHeight = (await filters.boundingBox())!.height
  expect(collapsedHeight).toBeLessThanOrEqual(200)

  // 键盘展开：其余筛选原位出现。
  await toggle.focus()
  await page.keyboard.press('Enter')
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  for (const control of await filters.locator('.filter-bar__controls-row > :is(.filter-select, .filter-multiselect)').all()) {
    await expect(control).toBeVisible()
  }
  await expectNoDocumentOverflow(page)
  expect((await filters.boundingBox())!.height).toBeGreaterThan(collapsedHeight + 60)
  await page.keyboard.press('Space')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')

  // 已生效筛选的 chip 可直接清除，计数随之归零。
  await filters.getByRole('button', { name: '移除筛选 健康状态: 告警' }).click()
  await expect(page).not.toHaveURL(/health=/)
  await expect(filters.getByRole('button', { name: '筛选', exact: true })).toBeVisible()
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
