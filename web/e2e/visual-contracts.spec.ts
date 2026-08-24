import type { Locator, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import {
  comparisonWorkbenchHref,
  comparisonWorkbenchProfile,
  coreRouteProfile,
  subjectActivityProfile,
  vpsOverviewPartialFixture,
  vpsOverviewProfile,
} from './fixtures/profiles'
import { expectLocatorNotClipped, expectMinTouchTarget, expectNoDocumentOverflow } from './support/geometry'

async function expectHitTarget(page: Page, locator: Locator): Promise<void> {
  const box = await locator.boundingBox()
  if (!box) throw new Error('expected command to have a bounding box')
  const hit = await page.evaluate(({ x, y }) => {
    const target = document.elementFromPoint(x, y)
    return target instanceof Element
      ? target.closest('button, a, [role="button"], [role="link"]') != null
      : false
  }, {
    x: box.x + box.width / 2,
    y: box.y + box.height / 2,
  })
  expect(hit).toBe(true)
}

const MOBILE_COMMANDS = [
  {
    name: 'Settings monitoring policy',
    path: '/settings',
    command: (page: Page) => page.getByRole('tab', { name: '监控策略' }),
  },
  {
    name: 'Asset scenario workspace',
    path: '/asset-decisions',
    command: (page: Page) => page.getByRole('button', { name: '场景与组合' }),
  },
  {
    name: 'Provider decision link',
    path: '/providers',
    command: (page: Page) => page.getByRole('link', {
      name: '查看 Example Cloud 服务商组合决策',
    }),
  },
] as const

for (const contract of MOBILE_COMMANDS) {
  test(`${contract.name} remains complete and reachable at 390px`, async ({ api, page }) => {
    await page.setViewportSize({ width: 390, height: 900 })
    api.useProfile(coreRouteProfile(contract.path))
    await page.goto(contract.path)

    const command = contract.command(page)
    await expect(command).toBeVisible()
    await command.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(command)
    await expectHitTarget(page, command)
    await expectNoDocumentOverflow(page)
  })
}

test('Provider wide table scrolls only inside its named keyboard region', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  api.useProfile(coreRouteProfile('/providers'))
  await page.goto('/providers')

  const heading = page.getByRole('heading', { name: '服务商与入口' })
  const region = page.getByRole('region', { name: '服务商与入口' })
  await expect(heading).toBeVisible()
  await expect(region).toHaveAttribute('tabindex', '0')
  await expect(region).toHaveAttribute('aria-labelledby', await heading.getAttribute('id') ?? '')
  const descriptionID = await region.getAttribute('aria-describedby')
  if (!descriptionID) throw new Error('Provider table scroll region must describe keyboard scrolling')
  await expect(page.locator(`#${descriptionID}`)).toBeVisible()

  const before = await region.evaluate((element) => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
    scrollLeft: element.scrollLeft,
  }))
  expect(before.scrollWidth).toBeGreaterThan(before.clientWidth)
  expect(before.scrollLeft).toBe(0)
  const headingX = (await heading.boundingBox())?.x

  await region.focus()
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => region.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
  expect((await heading.boundingBox())?.x).toBe(headingX)
  await expectNoDocumentOverflow(page)
})

test('VPS 概览 remains complete and reachable at 390px', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  api.useProfile(vpsOverviewProfile())
  await page.goto('/vps/vps_001')

  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()
  const command = page.getByRole('link', { name: '新建记录' })
  await expect(command).toBeVisible()
  await command.scrollIntoViewIfNeeded()
  await expectLocatorNotClipped(command)
  await expectHitTarget(page, command)
  await expectNoDocumentOverflow(page)
})

test('VPS 概览 partial freshness stays reachable without document overflow at 390px', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  api.useProfile(vpsOverviewProfile({ overview: vpsOverviewPartialFixture() }))
  await page.goto('/vps/vps_001')

  const retry = page.getByRole('button', { name: '重试 IP 质量' })
  await expect(retry).toBeVisible()
  await retry.scrollIntoViewIfNeeded()
  await expectLocatorNotClipped(retry)
  await expectHitTarget(page, retry)
  await expectMinTouchTarget(retry)
  await expectNoDocumentOverflow(page)
})

test('单主体时间线 remains complete and reachable at 390px', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  api.useProfile(subjectActivityProfile())
  await page.goto('/vps/vps_001/activity')

  await expect(page.getByText('E2E 时间线条目')).toBeVisible()
  const command = page.getByRole('link', { name: '新建记录' })
  await expect(command).toBeVisible()
  await command.scrollIntoViewIfNeeded()
  await expectLocatorNotClipped(command)
  await expectHitTarget(page, command)
  await expectNoDocumentOverflow(page)
})

test('横向比较工作台 save command remains complete and reachable at 390px', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  api.useProfile(comparisonWorkbenchProfile({ mode: 'host-partial' }))
  await page.goto(comparisonWorkbenchHref({
    mode: 'fixed',
    items: [{ snapshot_id: 'evs_cmpleft' }, { snapshot_id: 'evs_cmpright' }],
    baseline: 0,
    alignment: 'actual_coverage',
    tolerance_seconds: 60,
    kind: 'monitoring.host/v1',
    metric: 'cpu_usage_pct',
  }))

  const command = page.getByRole('button', { name: '另存为记录' })
  await expect(command).toBeVisible()
  await command.scrollIntoViewIfNeeded()
  await expectLocatorNotClipped(command)
  await expectHitTarget(page, command)
  await expectMinTouchTarget(command)
  await expectNoDocumentOverflow(page)
})
