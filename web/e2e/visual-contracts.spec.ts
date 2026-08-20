import type { Locator, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { coreRouteProfile, subjectActivityProfile, vpsOverviewProfile } from './fixtures/profiles'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

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
