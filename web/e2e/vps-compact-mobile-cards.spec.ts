import { expect, test } from './fixtures'
import { coreRouteProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

test('the VPS table view collapses each machine into a compact card on a phone and keeps details behind the expand', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/vps'))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/vps?workspace=workbench')

  const row = page.locator('.vps-workbench__row').first()
  await expect(row).toBeVisible()
  const height = (await row.boundingBox())!.height
  expect(height).toBeLessThanOrEqual(140)
  await expect(row.locator('.vps-workbench__kicker').first()).toBeHidden()
  await expect(row.locator('td').nth(1)).toBeHidden()
  await expect(row.locator('td').nth(4)).toBeHidden()
  // 状态与续费并排在名称下面。
  const status = (await row.locator('td').nth(2).boundingBox())!
  const renewal = (await row.locator('td').nth(3).boundingBox())!
  expect(Math.abs(status.y - renewal.y)).toBeLessThan(2)
  await expectNoDocumentOverflow(page)

  await row.locator('.vps-workbench__name').click()
  const accordion = page.locator('.vps-workbench__accordion').first()
  await expect(accordion.getByText('位置', { exact: true })).toBeVisible()
  await expect(accordion.getByText('监控实例', { exact: true })).toBeVisible()
})
