import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

async function expectNoBlockingAxe(page: Page) {
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({ id: violation.id, targets: violation.nodes.map((node) => node.target) }))).toEqual([])
}

test('renders and operates all four ready collaboration components in a real browser', async ({ page }) => {
  await page.goto('/?state=ready')
  for (const heading of ['协作责任', '行动队列', '协作评论', '关注策略']) {
    await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  }

  const owner = page.getByLabel('负责人')
  await owner.focus()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('status')).toContainText('owner:')

  await page.getByRole('button', { name: '编辑“复核异常证据”' }).click()
  await page.getByLabel('行动标题').fill('复核已更新证据')
  await page.getByRole('button', { name: '保存行动' }).click()
  await expect(page.getByRole('status')).toContainText('action:update:2')

  const redact = page.getByRole('button', { name: '请求遮盖该评论' })
  await redact.focus()
  await redact.click()
  const dialog = page.getByRole('alertdialog', { name: '确认永久遮盖评论' })
  await expect(dialog).toBeVisible()
  await expect(dialog.locator(':focus')).toHaveCount(1)
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(redact).toBeFocused()
  await expectNoBlockingAxe(page)
})

test('keeps all four components touch-reachable without document overflow at 390px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/?state=ready')
  const watch = page.getByRole('button', { name: '关注全部更新' })
  await watch.scrollIntoViewIfNeeded()
  await watch.tap()
  await expect(page.getByRole('status')).toContainText('watch:watching')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await expectNoBlockingAxe(page)
})

for (const state of ['loading', 'empty', 'error', 'revoked', 'deleted'] as const) {
  test(`renders all four ${state} component states without stale controls`, async ({ page }) => {
    await page.goto(`/?state=${state}`)
    await expect(page.getByRole('heading', { name: '记录协作值班台' })).toBeVisible()
    await expect(page.getByLabel('负责人')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '关注全部更新' })).toHaveCount(0)
    if (state === 'empty') {
      await expect(page.getByText('暂无行动项')).toBeVisible()
      await expect(page.getByText('暂无评论')).toBeVisible()
      await expect(page.getByRole('button', { name: '新增行动' })).toBeVisible()
      await expect(page.getByLabel('评论内容')).toBeVisible()
    } else {
      await expect(page.getByRole('button', { name: '新增行动' })).toHaveCount(0)
      await expect(page.getByLabel('评论内容')).toHaveCount(0)
    }
    await expectNoBlockingAxe(page)
  })
}
