import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { coreRouteProfile } from './fixtures/profiles'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

test('record inbox remains keyboard-operable, touch-reachable, and closed at 390px', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/record-inbox'))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/record-inbox')

  await expect(page.getByRole('heading', { name: '记录协作收件箱' })).toBeVisible()
  const inspect = page.getByRole('button', { name: '查看“评论提及”的对象' })
  await inspect.scrollIntoViewIfNeeded()
  await expectLocatorNotClipped(inspect)
  await inspect.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByText('目标：评论 rcm_e2e_001')).toBeVisible()

  const read = page.getByRole('button', { name: '标记“评论提及”为已读' })
  await read.scrollIntoViewIfNeeded()
  await read.click()
  await expect(page.getByText('已读', { exact: true })).toBeVisible()

  const dismiss = page.getByRole('button', { name: '移除“评论提及”' })
  await dismiss.click()
  await expect(page.getByRole('heading', { name: '当前没有待处理通知' })).toBeVisible()
  await expectNoDocumentOverflow(page)
})

test('record inbox mobile state has no serious or critical accessibility violations', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/record-inbox'))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/record-inbox')
  await expect(page.getByRole('heading', { name: '记录协作收件箱' })).toBeVisible()

  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])
})
