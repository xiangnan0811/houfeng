import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

test('an empty VPS inventory shows one centered first-use state without blank panes or a double frame', async ({ api, page }) => {
  api.useProfile({
    ...coreRouteProfile('/vps'),
    [apiRouteKey('GET', '/api/vps')]: { status: 200, body: [] },
    [apiRouteKey('GET', '/api/subscriptions?sort=renew_at&order=asc')]: { status: 200, body: [] },
  })
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 900 }]) {
    await page.setViewportSize(viewport)
    await page.goto('/vps')
    const empty = page.locator('.vps-canvas .page-state--empty')
    await expect(empty.getByRole('heading', { name: '还没有录入 VPS' })).toBeVisible()
    await expect(page.locator('.vps-ledger, .vps-workbench, .vps-inspector')).toHaveCount(0)
    const canvasBorder = await page.locator('.vps-canvas').evaluate((element) => getComputedStyle(element).borderTopWidth)
    expect(canvasBorder).toBe('0px')
    await expectNoDocumentOverflow(page)
  }
})
