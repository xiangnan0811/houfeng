import { vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

function compactProfile() {
  const base = coreRouteProfile('/vps')
  const subscriptions = base[apiRouteKey('GET', '/api/subscriptions?sort=renew_at&order=asc')]!
  return {
    ...base,
    [apiRouteKey('GET', '/api/vps')]: {
      status: 200,
      body: [
        // 已逾期、已决定不续费且自动续费未核对：提示必须留在紧凑卡片上。
        vpsAssetFixture({ renewal_decision: 'cancel', auto_renew_check: 'unchecked', product_name: '2C4G', os_name: 'Debian 12', virtualization: 'KVM' }),
        vpsAssetFixture({ vps_id: 'vps_002', display_name: 'Osaka Spare', active_monitoring_instance_link_count: 0, product_name: '', os_name: '', virtualization: '' }),
      ],
    },
    [apiRouteKey('GET', '/api/subscriptions?sort=renew_at&order=asc')]: subscriptions,
  }
}

test('the VPS table view collapses each machine into a compact card on a phone and keeps details behind the expand', async ({ api, page }) => {
  api.useProfile(compactProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/vps?workspace=workbench')

  const rows = page.locator('.vps-workbench__row')
  const row = rows.first()
  await expect(row).toBeVisible()
  expect((await row.boundingBox())!.height).toBeLessThanOrEqual(160)
  await expect(row.locator('.vps-workbench__kicker').first()).toBeHidden()
  await expect(row.locator('td').nth(1)).toBeHidden()
  await expect(row.locator('td').nth(4)).toBeHidden()
  // 状态与续费并排；逾期、自动续费核对提示与续费价留在卡片上。
  const status = (await row.locator('td').nth(2).boundingBox())!
  const renewal = (await row.locator('td').nth(3).boundingBox())!
  expect(Math.abs(status.y - renewal.y)).toBeLessThan(2)
  const renewalCell = row.locator('td').nth(3)
  await expect(renewalCell.getByText(/已逾期/)).toBeVisible()
  await expect(renewalCell.getByText('自动续费待核对')).toBeVisible()
  await expect(renewalCell.locator('.vps-workbench__compact-only')).toBeVisible()
  await expect(renewalCell.locator('.vps-workbench__compact-only')).toHaveText(/USD.* · 月付|USD.* · /)
  await expectNoDocumentOverflow(page)

  await row.locator('.vps-workbench__name').click()
  const accordion = page.locator('.vps-workbench__accordion').first()
  for (const label of ['位置', '规格', '用途', '监控实例', 'IP 质量']) {
    await expect(accordion.getByText(label, { exact: true })).toBeVisible()
  }
  await expect(accordion.locator('dd.vps-workbench__compact-only')).toBeVisible()
  await expect(accordion.locator('dd.vps-workbench__compact-only')).toHaveText(/2C4G\s*·\s*Debian 12\s*·\s*KVM/)

  await rows.nth(1).locator('.vps-workbench__name').click()
  const spare = page.locator('.vps-workbench__accordion').first()
  await expect(spare).toContainText('未关联')
  // 规格为空时弱化显示，与位置缺失等空事实一致。
  await expect(spare.locator('dd.vps-workbench__compact-only')).toHaveClass(/vps-quiet-fact/)
  await expect(spare.locator('dd.vps-workbench__compact-only')).toHaveText('规格未填写')
})

test('the expanded row does not repeat the spec on desktop where the row already shows it', async ({ api, page }) => {
  api.useProfile(compactProfile())
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/vps?workspace=workbench')
  await page.locator('.vps-workbench__row').first().locator('.vps-workbench__name').click()
  await expect(page.locator('.vps-workbench__accordion').first().getByText('规格', { exact: true })).toBeHidden()
})
