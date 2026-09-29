import { coreRouteProfile, recordSearchProfile, subjectActivityProfile } from './fixtures/profiles'
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

test('Record search and subject activity filters use the same single-row chips', async ({ api, page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })

  api.useProfile(recordSearchProfile())
  await page.goto('/records')
  const recordRow = page.getByRole('form', { name: '记录搜索筛选' }).locator('.filter-bar__controls-row')
  const recordChips = recordRow.locator(':scope > .filter-select')
  await expect(recordChips).toHaveCount(5)
  const recordTops = await recordChips.evaluateAll((elements) => elements.map((element) => Math.round(element.getBoundingClientRect().top)))
  expect(new Set(recordTops).size).toBe(1)
  expect(await recordChips.first().evaluate(chipStyle).then((style) => style.direction)).toBe('row')

  // 追加式类型筛选：选中后下拉复位，胶囊如实显示已选数量并高亮。
  const typeChip = recordRow.locator(':scope > .filter-select', { has: page.locator('.filter-select__label', { hasText: /^记录类型$/ }) })
  const idle = await typeChip.evaluate(chipStyle)
  await typeChip.locator('select').selectOption({ label: '排障' })
  await expect(typeChip).toHaveClass(/is-filtered/)
  await expect(typeChip.locator('select option:checked')).toHaveText('已选 1')
  await expect.poll(async () => (await typeChip.evaluate(chipStyle)).background).not.toBe(idle.background)
  await expectNoDocumentOverflow(page)

  api.useProfile(subjectActivityProfile({}))
  await page.goto('/vps/vps_001/activity')
  const activityChips = page.locator('.subject-activity-filters > .filter-select')
  await expect(activityChips).toHaveCount(3)
  const activityTops = await activityChips.evaluateAll((elements) => elements.map((element) => Math.round(element.getBoundingClientRect().top)))
  expect(new Set(activityTops).size).toBe(1)
  expect(await activityChips.first().evaluate(chipStyle).then((style) => style.direction)).toBe('row')
  await expectNoDocumentOverflow(page)

  // 窄屏不再强制纵向：胶囊按共享规则换行，全部可见且不产生横向溢出。
  await page.setViewportSize({ width: 390, height: 900 })
  for (const chip of await activityChips.all()) await expect(chip).toBeVisible()
  const overflow = await page.locator('.subject-activity-filters').evaluate((element) => element.scrollWidth - element.clientWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await expectNoDocumentOverflow(page)
})
