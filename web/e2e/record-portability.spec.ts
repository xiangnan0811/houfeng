import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { recordSearchProfile } from './fixtures/profiles'
import {
  expectLocatorNotClipped,
  expectMinTouchTarget,
  expectNoDocumentOverflow,
} from './support/geometry'

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 900 },
] as const

async function expectNoBlockingAxe(page: Page): Promise<void> {
  await page.evaluate(() => document.fonts.ready)
  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      targets: violation.nodes.map((node) => node.target),
    }))
  expect(blocking).toEqual([])
}

async function expectOperableControl(locator: Locator, viewportWidth: number): Promise<void> {
  await locator.scrollIntoViewIfNeeded()
  await expectLocatorNotClipped(locator)
  if (viewportWidth === 390) await expectMinTouchTarget(locator)
}

for (const viewport of VIEWPORTS) {
  test(`record search import and export stay operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordSearchProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records')

    await expect(page.getByRole('heading', { name: '运维记录' })).toBeVisible()
    await expect(page.getByRole('dialog', { name: '导入记录' })).toHaveCount(0)
    await expect(page.getByRole('region', { name: '记录导入' })).toHaveCount(0)
    await expect(page.getByRole('region', { name: '记录导出' })).toHaveCount(0)

    const importButton = page.getByRole('button', { name: '导入', exact: true })
    await importButton.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(importButton)
    await importButton.focus()
    await expect(importButton).toBeFocused()
    await page.keyboard.press('Enter')

    const importDialog = page.getByRole('dialog', { name: '导入记录' })
    await expect(importDialog).toBeVisible()
    const importPanel = importDialog.getByRole('region', { name: '记录导入' })
    await expect(importPanel).toBeVisible()

    const dryRun = importPanel.getByRole('button', { name: '预检导入' })
    const apply = importPanel.getByRole('button', { name: '确认应用' })
    await expect(dryRun).toBeVisible()
    await expect(dryRun).toBeDisabled()
    await expect(apply).toBeDisabled()

    await importPanel.getByLabel('归档文件').setInputFiles({
      name: 'archive.zip',
      mimeType: 'application/zip',
      buffer: Buffer.from('PK'),
    })
    await expect(dryRun).toBeEnabled()
    await expect(apply).toBeDisabled()
    await expectOperableControl(dryRun, viewport.width)
    await expectNoDocumentOverflow(page)

    await page.keyboard.press('Escape')
    await expect(importDialog).toHaveCount(0)
    await expect(importButton).toBeFocused()

    const exportToggle = page.locator('summary').filter({ hasText: /^导出选中记录$/ })
    await exportToggle.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(exportToggle)
    await exportToggle.focus()
    await expect(exportToggle).toBeFocused()
    await page.keyboard.press('Enter')

    const exportPanel = page.getByRole('region', { name: '记录导出' })
    await expect(exportPanel).toBeVisible()
    await expect(exportPanel.getByText('当前导出：第三晚 TCP 观测（rec_e2e001）')).toBeVisible()

    const preview = exportPanel.getByRole('button', { name: '预览导出' })
    const download = exportPanel.getByRole('button', { name: '下载' })
    await expect(download).toBeDisabled()
    await expectOperableControl(preview, viewport.width)
    await preview.focus()
    await page.keyboard.press('Enter')
    await expect(download).toBeEnabled()
    expect(api.requestCount('POST', '/api/record-export-previews')).toBe(1)
    await expectOperableControl(download, viewport.width)
    await expectNoDocumentOverflow(page)
  })
}

test('record search import and export have no serious or critical accessibility violations at 390px', async ({ api, page }) => {
  api.useProfile(recordSearchProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records')
  await expect(page.getByRole('heading', { name: '运维记录' })).toBeVisible()

  const exportToggle = page.locator('summary').filter({ hasText: /^导出选中记录$/ })
  await exportToggle.click()
  await expect(page.getByRole('region', { name: '记录导出' })).toBeVisible()
  await expectNoBlockingAxe(page)

  await page.getByRole('button', { name: '导入', exact: true }).click()
  const importDialog = page.getByRole('dialog', { name: '导入记录' })
  await expect(importDialog).toBeVisible()
  await expect(importDialog.getByRole('region', { name: '记录导入' })).toBeVisible()
  await expect(importDialog.getByRole('button', { name: '预检导入' })).toBeVisible()

  await expectNoBlockingAxe(page)
})
