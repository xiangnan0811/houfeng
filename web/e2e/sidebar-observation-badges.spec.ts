import { dashboardOverviewFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile } from './fixtures/profiles'

function profileWithGaps(counts = { abnormal_target_count: 0, unobserved_target_count: 4, stale_target_count: 2 }) {
  return {
    ...coreRouteProfile('/targets'),
    [apiRouteKey('GET', '/api/dashboard')]: {
      status: 200,
      body: dashboardOverviewFixture({
        snapshot_generated_at: new Date().toISOString(),
        ...counts,
      }),
    },
  }
}

test('observation gaps are badges on a fixed 入口探测 item and keep one current destination', async ({ api, page }) => {
  api.useProfile(profileWithGaps())
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/targets')

  const observation = page.getByRole('group', { name: '观测' })
  await expect(observation.locator('.nav-item .nav-text')).toHaveText(['监控', '入口探测', '事件'])
  const targets = observation.getByRole('link', { name: '入口探测，4 个尚无观测，2 个观测过期' })
  const unobserved = observation.getByRole('link', { name: '尚无观测，4 个尚无观测' })
  const stale = observation.getByRole('link', { name: '观测过期，2 个观测过期' })
  await expect(unobserved).toBeVisible()
  await expect(stale).toBeVisible()
  for (const badge of [unobserved, stale]) {
    const box = await badge.boundingBox()
    expect(box?.width).toBeGreaterThanOrEqual(24)
    expect(box?.height).toBeGreaterThanOrEqual(24)
  }

  await unobserved.click()
  await expect(page).toHaveURL(/\/targets\?view=unobserved$/)
  await expect(targets).toHaveAttribute('aria-current', 'page')
  await expect(unobserved).not.toHaveAttribute('aria-current')
  await expect(page.locator('nav [aria-current="page"]')).toHaveCount(1)
})

test('the icon rail hides the category badges and marks 入口探测 with a gap dot', async ({ api, page }) => {
  api.useProfile(profileWithGaps())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/targets')

  const targets = page.getByRole('link', { name: '入口探测，4 个尚无观测，2 个观测过期' })
  await expect(targets).toBeVisible()
  await expect(page.getByRole('link', { name: '尚无观测，4 个尚无观测' })).toBeHidden()
  const dot = await targets.evaluate((element) => {
    const style = getComputedStyle(element, '::after')
    return { display: style.display, width: style.width }
  })
  expect(dot).toEqual({ display: 'block', width: '6px' })
  // 窄栏里图标与其他导航项一样居中。
  const offsets = await page.locator('nav .nav-item').evaluateAll((links) => links.map((link) => {
    const box = link.getBoundingClientRect()
    const icon = link.querySelector('svg')!.getBoundingClientRect()
    return Math.round((icon.left + icon.width / 2) - (box.left + box.width / 2))
  }))
  expect(new Set(offsets)).toEqual(new Set([0]))
})

test('large counts on all three target badges stay inside the expanded sidebar row', async ({ api, page }) => {
  api.useProfile(profileWithGaps({ abnormal_target_count: 1000, unobserved_target_count: 1000, stale_target_count: 1000 }))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/targets')

  const row = page.locator('.nav-item-row')
  await expect(row.locator('.nav-badge')).toHaveText('99+')
  await expect(row.locator('.nav-subbadge')).toHaveText(['99+', '99+'])
  const fit = await row.evaluate((element) => {
    const link = element.querySelector<HTMLElement>('.nav-item')!
    const rowBox = element.getBoundingClientRect()
    const badges = Array.from(element.querySelectorAll<HTMLElement>('.nav-badge, .nav-subbadge')).map((badge) => badge.getBoundingClientRect())
    return {
      linkClipped: link.scrollWidth > link.clientWidth,
      badgesInside: badges.every((box) => box.left >= rowBox.left && box.right <= rowBox.right),
    }
  })
  expect(fit).toEqual({ linkClipped: false, badgesInside: true })
})
