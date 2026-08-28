import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { authenticatedProfile, recordDetailProfile } from './fixtures/profiles'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 900 },
] as const

for (const viewport of VIEWPORTS) {
  test(`record editor empty/new state stays operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(authenticatedProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/new')

    await expect(page.getByRole('heading', { name: '新建运维记录' })).toBeVisible()
    const title = page.getByLabel('标题')
    await title.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(title)
    await title.fill('第三晚观测')
    await expect(page.getByLabel('Markdown 源文')).toBeVisible()
    await expect(page.getByRole('button', { name: '发布修订' })).toBeVisible()
    await expectNoDocumentOverflow(page)
  })
}

test('record editor new state has no serious or critical accessibility violations', async ({ api, page }) => {
  api.useProfile(authenticatedProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/new')
  await expect(page.getByRole('heading', { name: '新建运维记录' })).toBeVisible()

  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])
})

for (const viewport of VIEWPORTS) {
  test(`record material drawer stays operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(authenticatedProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/new')
    await expect(page.getByRole('heading', { name: '新建运维记录' })).toBeVisible()

    const openMaterials = page.getByRole('button', { name: '材料与引用' })
    await openMaterials.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(openMaterials)
    await openMaterials.click()

    const drawer = page.getByRole('dialog', { name: '材料与引用' })
    await expect(drawer).toBeVisible()
    await expect(page.getByText('当前修订没有可引用材料')).toBeVisible()
    await expect(page.getByRole('button', { name: '关闭' })).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(drawer).toHaveCount(0)
    await expect(openMaterials).toBeFocused()
    await expectNoDocumentOverflow(page)
  })
}

for (const viewport of VIEWPORTS) {
  test(`published record reading surface stays operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001')

    await expect(page.locator('.page-header').getByRole('heading', {
      name: '第三晚 TCP 观测',
      level: 1,
    })).toBeVisible()
    const preview = page.locator('[data-render-contract="houfeng_markdown/v1"]')
    await expect(preview).toBeVisible()
    await expect(preview.locator('pre code')).toContainText('mtr -rw 203.0.113.7')
    await expect(preview.locator('table td').first()).toHaveText('alpha')
    await expect(preview.locator('[data-ref-id="evs_e2ethirdnight"]')).toHaveClass(/card/u)
    await expect(page.getByText('引用已失效')).toHaveCount(0)
    await expectNoDocumentOverflow(page)
  })
}

for (const viewport of VIEWPORTS) {
  test(`record editor layout modes stay operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001/edit')

    const source = page.getByLabel('Markdown 源文')
    await expect(source).toBeVisible()

    const layout = page.getByRole('toolbar', { name: '编辑布局' })
    await layout.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(layout)
    await layout.getByRole('button', { name: '预览' }).click()
    await expect(source).toHaveCount(0)
    await expect(page.locator('[data-render-contract]')).toBeVisible()

    await layout.getByRole('button', { name: '编辑' }).click()
    await expect(source).toBeVisible()
    await expect(page.locator('[data-render-contract]')).toHaveCount(0)

    await layout.getByRole('button', { name: '分栏' }).click()
    await expect(source).toBeVisible()
    await expect(page.locator('[data-render-contract]')).toBeVisible()
    await expectNoDocumentOverflow(page)
  })
}

for (const viewport of VIEWPORTS) {
  test(`record material drawer inserts a real reference at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001/edit')
    await expect(page.getByLabel('Markdown 源文')).toBeVisible()

    const openMaterials = page.getByRole('button', { name: '材料与引用' })
    await openMaterials.scrollIntoViewIfNeeded()
    await openMaterials.click()

    const drawer = page.getByRole('dialog', { name: '材料与引用' })
    await expect(drawer).toBeVisible()
    await expect(drawer.getByText('证据 evs_e2ethirdnight')).toBeVisible()
    await expect(drawer.getByText('附件 att_e2emtrreport')).toBeVisible()

    const insertAttachment = drawer.getByRole('button', { name: '插入附件 att_e2emtrreport' })
    await expectLocatorNotClipped(insertAttachment)
    await insertAttachment.click()
    await expect(page.getByLabel('Markdown 源文')).toHaveValue(/houfeng-attachment:att_e2emtrreport/u)

    await page.keyboard.press('Escape')
    await expect(drawer).toHaveCount(0)
    await expectNoDocumentOverflow(page)
  })
}

test('a record the server could not model stays readable from source', async ({ api, page }) => {
  api.useProfile(recordDetailProfile({ renderModel: 'unsupported' }))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/rec_e2e001')

  const preview = page.locator('[data-render-contract="houfeng_markdown/v1-live"]')
  await expect(preview).toBeVisible()
  await expect(preview.getByRole('status')).toContainText('按源码渲染')
  // The nesting the server refused to model is exactly what the fallback must show.
  await expect(preview.locator('li ul li').first()).toHaveText('磁盘')
  await expectNoDocumentOverflow(page)
})

test('published record reading surface has no serious or critical accessibility violations', async ({ api, page }) => {
  api.useProfile(recordDetailProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/rec_e2e001')
  await expect(page.locator('.page-header').getByRole('heading', {
    name: '第三晚 TCP 观测',
    level: 1,
  })).toBeVisible()

  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])
})

test('record material drawer has no serious or critical accessibility violations', async ({ api, page }) => {
  api.useProfile(authenticatedProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/new')
  await page.getByRole('button', { name: '材料与引用' }).click()
  await expect(page.getByRole('dialog', { name: '材料与引用' })).toBeVisible()

  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])
})
