import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

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

for (const viewport of VIEWPORTS) {
  test(`record search import and export stay operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordSearchProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records')

    const importPanel = page.getByRole('region', { name: '记录导入' })
    const exportPanel = page.getByRole('region', { name: '记录导出' })
    await expect(importPanel).toBeVisible()
    await expect(exportPanel).toBeVisible()
    await expect(page.getByText('当前导出：第三晚 TCP 观测（rec_e2e001）')).toBeVisible()

    const preview = exportPanel.getByRole('button', { name: '预览导出' })
    await preview.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(preview)
    if (viewport.width === 390) await expectMinTouchTarget(preview)
    await preview.focus()
    await page.keyboard.press('Enter')
    await expect(exportPanel.getByText('record.md · 12 字节')).toBeVisible()

    const download = exportPanel.getByRole('button', { name: '下载' })
    await download.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(download)
    if (viewport.width === 390) await expectMinTouchTarget(download)
    await expect(download).toBeEnabled()

    const dryRun = importPanel.getByRole('button', { name: '预检导入' })
    await dryRun.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(dryRun)
    if (viewport.width === 390) await expectMinTouchTarget(dryRun)
    await expectNoDocumentOverflow(page)
  })
}

test('record search import and export have no serious or critical accessibility violations at 390px', async ({ api, page }) => {
  api.useProfile(recordSearchProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records')
  await expect(page.getByRole('heading', { name: '运维记录' })).toBeVisible()
  await expectNoBlockingAxe(page)
})
