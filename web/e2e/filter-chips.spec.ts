import { coreRouteProfile } from './fixtures/profiles'
import { expect, test } from './fixtures'
import { expectNoDocumentOverflow } from './support/geometry'

function chipStyle(element: Element) {
  const style = getComputedStyle(element)
  return {
    direction: style.flexDirection,
    border: style.borderTopColor,
    background: style.backgroundColor,
  }
}

test('Monitoring filters render as visible labelled chips with an applied-value highlight', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/monitoring'))
  // 关闭过渡，计算样式读取的是最终状态而不是过渡中的中间值。
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/monitoring?health=告警&q=tokyo')
  const row = page.locator('.monitoring-page__filters .filter-bar__controls-row')
  await expect(row).toBeVisible()
  await page.evaluate(() => document.fonts.ready)

  const chips = row.locator(':scope > :is(.filter-select, .filter-multiselect)')
  await expect(chips).toHaveCount(8)
  // 合同要求所有即时筛选项可见、不折叠；逐项检查可见，并排成同一行。
  for (const chip of await chips.all()) await expect(chip).toBeVisible()
  const tops = await chips.evaluateAll((elements) => elements.map((element) => Math.round(element.getBoundingClientRect().top)))
  expect(new Set(tops).size).toBe(1)

  const chipSelect = (label: string) => row
    .locator(':scope > .filter-select', { has: page.locator('.filter-select__label', { hasText: new RegExp(`^${label}$`) }) })
  const health = chipSelect('健康')
  const runStatus = chipSelect('运行')
  await expect(health.locator('select')).toHaveValue('告警')
  await expect(runStatus.locator('select option:checked')).toHaveText('全部')

  const filtered = await health.evaluate(chipStyle)
  const idle = await runStatus.evaluate(chipStyle)
  expect(filtered.direction).toBe('row')
  expect(idle.direction).toBe('row')
  // 高亮是真实的计算样式差异，不只是类名。
  expect(filtered.border).not.toBe(idle.border)
  expect(filtered.background).not.toBe(idle.background)

  // 文本搜索有值时同样高亮。
  await expect(row.locator('.monitoring-page__search-field')).toHaveClass(/is-filtered/)

  // 键盘聚焦时胶囊整体显示焦点边框。
  await runStatus.locator('select').focus()
  await expect.poll(async () => (await runStatus.evaluate(chipStyle)).border).not.toBe(idle.border)

  // 多选弹层不随胶囊内容宽度缩窄；按钮名称包含可见维度名。
  const labels = row.getByRole('button', { name: '标签 全部' })
  await labels.click()
  const popover = row.locator('.filter-multiselect__popover')
  await expect(popover).toBeVisible()
  // 打开态只有胶囊外层一圈焦点环，触发器自身不再叠加阴影。
  expect(await labels.evaluate((element) => getComputedStyle(element).boxShadow)).toBe('none')
  const [chipWidth, popoverWidth] = await Promise.all([
    row.locator(':scope > .filter-multiselect').evaluate((element) => element.getBoundingClientRect().width),
    popover.evaluate((element) => element.getBoundingClientRect().width),
  ])
  expect(popoverWidth).toBeGreaterThanOrEqual(Math.max(chipWidth, 250))

  const mainOverflow = await page.locator('.main').evaluate((element) => element.scrollWidth - element.clientWidth)
  expect(mainOverflow).toBeLessThanOrEqual(1)
  await expectNoDocumentOverflow(page)
})

test('Filter drawers keep the stacked label layout outside the filter bar', async ({ api, page }) => {
  api.useProfile(coreRouteProfile('/events'))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/events')
  const barChip = page.locator('.filter-bar__controls-row > .filter-select').first()
  await expect(barChip).toBeVisible()
  expect(await barChip.evaluate((element) => getComputedStyle(element).flexDirection)).toBe('row')
  await page.getByRole('button', { name: '高级筛选' }).click()
  const drawerSelect = page.locator('.drawer .filter-select, [role="dialog"] .filter-select').first()
  await expect(drawerSelect).toBeVisible()
  expect(await drawerSelect.evaluate((element) => getComputedStyle(element).flexDirection)).toBe('column')
})
