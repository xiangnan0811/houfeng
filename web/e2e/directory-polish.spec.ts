import type { Locator } from '@playwright/test'

import { vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

async function overflowX(region: Locator) {
  return region.evaluate((element) => element.scrollWidth - element.clientWidth)
}

test('wide tables show a scroll hint only while they actually overflow', async ({ api, page }) => {
  for (const [route, name, hint] of [
    ['/providers', '服务商与入口', '横向滚动查看完整列'],
    ['/command-audit', '审计记录', '可横向滚动；聚焦表格区域后可使用方向键浏览。'],
  ] as const) {
    api.useProfile(coreRouteProfile(route))
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(route)
    const region = page.getByRole('region', { name })
    await expect(region).toBeVisible()
    await expect(region).toHaveAttribute('tabindex', '0')
    expect(await overflowX(region)).toBeLessThanOrEqual(1)
    await expect(page.getByText(hint)).toHaveCount(0)
    await expect(region).not.toHaveAttribute('aria-describedby')

    // 窄屏溢出时提示出现并作为区域描述，键盘仍可横向滚动。
    await page.setViewportSize({ width: 390, height: 900 })
    await expect.poll(() => overflowX(region)).toBeGreaterThan(1)
    await expect(page.getByText(hint)).toBeVisible()
    await expect(region).toHaveAccessibleDescription(hint)
    await region.focus()
    await page.keyboard.press('ArrowRight')
    await expect.poll(() => region.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
    await expectNoDocumentOverflow(page)

    // 回到桌面宽度后提示与描述一并移除，区域仍可聚焦。
    await page.setViewportSize({ width: 1440, height: 900 })
    await expect(page.getByText(hint)).toHaveCount(0)
    await expect(region).not.toHaveAttribute('aria-describedby')
    await expect(region).toHaveAttribute('tabindex', '0')
  }
})

test('provider directory header does not repeat its summary and the narrow summary is a two-column grid', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/providers'))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/providers')
  const summary = page.getByLabel('服务商目录摘要')
  await expect(summary).toBeVisible()
  await expect(page.locator('.page__head .badge')).toHaveCount(0)
  await expect(summary).not.toContainText('外部口碑源入口')
  const tabs = page.getByRole('group', { name: '服务商视图' })
  const search = page.getByRole('searchbox', { name: '搜索服务商' })
  const [tabsBox, searchBox] = await Promise.all([tabs.boundingBox(), search.boundingBox()])
  // 视图切换与搜索同一行。
  expect(Math.abs((tabsBox!.y + tabsBox!.height / 2) - (searchBox!.y + searchBox!.height / 2))).toBeLessThanOrEqual(2)

  await page.setViewportSize({ width: 390, height: 900 })
  const lefts = await summary.locator(':scope > span').evaluateAll((items) => items.map((item) => Math.round(item.getBoundingClientRect().left)))
  expect(new Set(lefts).size).toBe(2)
  await expectNoDocumentOverflow(page)
})

test('record inbox marks unread items and shows old timestamps once', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/record-inbox'))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/record-inbox')
  const item = page.getByRole('list', { name: '记录通知' }).getByRole('listitem').first()
  await expect(item).toHaveClass(/is-unread/)
  const signal = item.locator('.record-inbox-item__signal')
  expect(await signal.evaluate((element) => getComputedStyle(element).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)')
  // 夹具事件早于相对时间范围（30 天），relative 已退回绝对日期：只显示一次，不出现“日期 · 日期”。
  await page.clock.setFixedTime(new Date('2026-09-29T04:00:00Z'))
  await page.reload()
  const timestamp = page.getByRole('list', { name: '记录通知' }).getByRole('listitem').first().locator('.timestamp')
  await expect(timestamp).toHaveText(/^2026\/08\/17 \d\d:\d\d$/)
  await expect(item.getByRole('button', { name: '查看“评论提及”的对象' })).toBeVisible()
})

test('ledger directory attention text keeps the warning colour', async ({ api, page }) => {
  api.useProfile({
    ...coreRouteProfile('/vps'),
    [apiRouteKey('GET', '/api/vps')]: {
      status: 200,
      body: [vpsAssetFixture({ renewal_decision: 'cancel', auto_renew_check: 'unchecked' })],
    },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/vps?workspace=ledger')
  const attention = page.locator('.vps-ledger__item-attention')
  await expect(attention).toHaveText('已决定不续费，请核对服务商自动续费')
  const [warn, line] = await Promise.all([
    attention.evaluate((element) => getComputedStyle(element).color),
    page.locator('.vps-ledger__item-st').first().evaluate((element) => getComputedStyle(element).color),
  ])
  expect(warn).not.toBe(line)
  const expected = await page.evaluate(() => {
    const probe = document.createElement('span')
    probe.style.color = 'var(--warn)'
    document.body.append(probe)
    const color = getComputedStyle(probe).color
    probe.remove()
    return color
  })
  expect(warn).toBe(expected)
})
