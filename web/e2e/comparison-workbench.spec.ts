import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import { expect, test } from './fixtures'
import {
  comparisonWorkbenchHref,
  comparisonWorkbenchProfile,
} from './fixtures/profiles'
import {
  expectLocatorNotClipped,
  expectMinTouchTarget,
  expectNoDocumentOverflow,
} from './support/geometry'

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 900 },
] as const

const HOST_HREF = comparisonWorkbenchHref({
  mode: 'fixed',
  items: [{ snapshot_id: 'evs_cmpleft' }, { snapshot_id: 'evs_cmpright' }],
  baseline: 0,
  alignment: 'actual_coverage',
  tolerance_seconds: 60,
  kind: 'monitoring.host/v1',
  metric: 'cpu_usage_pct',
})

const CANDIDATE_HREF = comparisonWorkbenchHref({
  mode: 'candidate',
  subjects: [
    { kind: 'vps', id: 'vps_cmpleft' },
    { kind: 'vps', id: 'vps_cmpright' },
  ],
  kind: 'monitoring.host/v1',
  metric: 'cpu_usage_pct',
})

const METADATA_HREF = comparisonWorkbenchHref({
  mode: 'fixed',
  items: [{ snapshot_id: 'evs_cmpleft' }, { snapshot_id: 'evs_cmpright' }],
  baseline: 0,
  alignment: 'actual_coverage',
  tolerance_seconds: 60,
  kind: 'command.audit/v1',
})

const SEED_HREF = comparisonWorkbenchHref({
  mode: 'fixed',
  items: [{ snapshot_id: 'evs_cmpleft' }],
  baseline: 0,
  alignment: 'actual_coverage',
  tolerance_seconds: 60,
  kind: 'monitoring.host/v1',
})

function controlledPromise(): { promise: Promise<void>; resolve: () => void } {
  let resolvePromise: (() => void) | undefined
  const promise = new Promise<void>((resolve) => {
    resolvePromise = resolve
  })
  return {
    promise,
    resolve: () => {
      if (!resolvePromise) throw new Error('controlled promise was not initialized')
      resolvePromise()
    },
  }
}

async function expectNoBlockingAxe(page: Page): Promise<void> {
  await page.evaluate(() => document.fonts.ready)
  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      description: violation.description,
      targets: violation.nodes.map((node) => node.target),
    }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
}

for (const viewport of VIEWPORTS) {
  test(`横向比较工作台 host-partial stays operable at ${viewport.width}x${viewport.height}`, async ({
    api,
    page,
  }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    api.useProfile(comparisonWorkbenchProfile({ mode: 'host-partial' }))
    await page.goto(HOST_HREF)

    const review = page.getByRole('heading', { name: '可比性审查' })
    const trend = page.getByRole('heading', { name: '趋势' })
    await expect(review).toBeVisible()
    await expect(page.getByRole('listitem').filter({ hasText: '覆盖不完整' })).toBeVisible()
    await expect(trend).toBeVisible()
    expect(await page.evaluate(() => {
      const reviewHeading = document.getElementById('comparison-review-heading')
      const trendHeading = document.getElementById('comparison-trend-heading')
      if (!reviewHeading || !trendHeading) return false
      return (reviewHeading.compareDocumentPosition(trendHeading) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
    })).toBe(true)
    await expect(page.getByRole('img', { name: /第 \d+ 项/ }).locator('polyline')).toHaveCount(2)

    const save = page.getByRole('button', { name: '另存为记录' })
    await save.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(save)
    await expectMinTouchTarget(save)
    await expectNoDocumentOverflow(page)
    await expectNoBlockingAxe(page)
  })
}

test('横向比较工作台 keeps candidate confirmation from calling compare', async ({ api, page }) => {
  api.useProfile(comparisonWorkbenchProfile({ mode: 'candidates' }))
  await page.goto(CANDIDATE_HREF)

  await expect(page.getByRole('button', { name: '确认候选并比较' })).toBeVisible()
  expect(api.requestCount('POST', '/api/evidence/comparison-candidates')).toBe(1)
  expect(api.requestCount('POST', '/api/evidence/comparisons')).toBe(0)

  await page.getByRole('button', { name: '确认候选并比较' }).click()
  await expect(page.getByRole('button', { name: '另存为记录' })).toBeVisible()
  expect(api.requestCount('POST', '/api/evidence/comparisons')).toBe(1)
})

test('横向比较工作台 selection shell stays recoverable below two items', async ({ api, page }) => {
  api.useProfile(comparisonWorkbenchProfile({ mode: 'host-partial' }))
  await page.goto(SEED_HREF)

  await expect(page.getByText(/至少选择 2 项才能比较/)).toBeVisible()
  await expect(page.getByRole('heading', { name: '趋势' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '另存为记录' })).toHaveCount(0)
  expect(api.requestCount('POST', '/api/evidence/comparisons')).toBe(0)
})

test('横向比较工作台 metadata-only explains the gap and still allows save', async ({ api, page }) => {
  api.useProfile(comparisonWorkbenchProfile({ mode: 'metadata-only' }))
  await page.goto(METADATA_HREF)

  await expect(page.getByRole('heading', { name: '可比性审查' })).toBeVisible()
  await expect(page.getByText('仅元数据，无数值比较')).toBeVisible()
  await expect(page.getByRole('heading', { name: '趋势' })).toHaveCount(0)
  await expect(page.getByRole('img', { name: /第 \d+ 项/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '另存为记录' })).toBeVisible()
})

test('横向比较工作台 incompatible selection shows reasons and no invented series', async ({
  api,
  page,
}) => {
  api.useProfile(comparisonWorkbenchProfile({ mode: 'incompatible' }))
  await page.goto(HOST_HREF)

  await expect(page.getByRole('listitem').filter({ hasText: 'schema 不兼容' })).toBeVisible()
  await expect(page.getByText('没有兼容的证据类型可比较。')).toBeVisible()
  await expect(page.getByRole('heading', { name: '趋势' })).toHaveCount(0)
  await expect(page.getByRole('img', { name: /第 \d+ 项/ })).toHaveCount(0)
})

test('横向比较工作台 can cancel a long compare without keeping the late result', async ({
  api,
  diagnostics,
  page,
}) => {
  const gate = controlledPromise()
  api.useProfile(comparisonWorkbenchProfile({
    mode: 'host-partial',
    compareWaitFor: gate.promise,
  }))
  diagnostics.allowRequestFailure('POST', '/api/evidence/comparisons')
  await page.goto(HOST_HREF)

  await expect(page.getByRole('heading', { name: '正在加载比较' })).toBeVisible()
  const cancel = page.getByRole('button', { name: '取消比较' })
  await expectMinTouchTarget(cancel)
  await cancel.click()
  await expect(page.getByText(/已取消比较/)).toBeVisible()
  await expect(page.getByRole('heading', { name: '趋势' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '另存为记录' })).toHaveCount(0)
  gate.resolve()
  await expect(page.getByRole('heading', { name: '趋势' })).toHaveCount(0)
})

test('横向比较工作台 keyboard can select, switch kinds, save, then revoke', async ({
  api,
  page,
}) => {
  api.useProfile(comparisonWorkbenchProfile({ mode: 'candidates', includeSave: true }))
  await page.goto(CANDIDATE_HREF)

  const confirm = page.getByRole('button', { name: '确认候选并比较' })
  await confirm.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('button', { name: '另存为记录' })).toBeVisible()

  const kinds = page.getByRole('group', { name: '比较证据类型' })
  const probe = kinds.getByRole('button', { name: 'monitoring.probe/v2' })
  await probe.focus()
  await page.keyboard.press('Enter')
  await expect(probe).toHaveAttribute('aria-pressed', 'true')

  await page.getByLabel('记录标题').fill('第三晚横向比较')
  await page.getByRole('textbox', { name: '人工结论' }).fill('人工结论只进修订')
  const save = page.getByRole('button', { name: '另存为记录' })
  await save.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByText('已保存为 rec_cmpsaved01')).toBeVisible()
  expect(api.requestCount('POST', '/api/records')).toBe(1)

  api.useProfile(comparisonWorkbenchProfile({ mode: 'revoked' }))
  await page.getByLabel('对齐', { exact: true }).selectOption('common_overlap')
  await expect(page.getByRole('heading', { name: '比较不可用' })).toBeVisible()
  await expect(page.getByText('evs_restricted')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '趋势' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '另存为记录' })).toHaveCount(0)
})

test('横向比较工作台 390px folds conditions and scrolls only the named matrix', async ({
  api,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  api.useProfile(comparisonWorkbenchProfile({ mode: 'host-partial' }))
  await page.goto(HOST_HREF)

  const summary = page.locator('summary').filter({ has: page.getByRole('heading', { name: '比较条件' }) })
  await expect(page.getByLabel('请求开始')).toBeVisible()
  await summary.click()
  await expect(page.getByLabel('请求开始')).toBeHidden()
  await summary.click()
  await expect(page.getByLabel('请求开始')).toBeVisible()

  const heading = page.getByRole('heading', { name: '对齐矩阵' })
  const region = page.getByRole('region', { name: '对齐矩阵' })
  await expect(heading).toBeVisible()
  await expect(page.getByRole('button', { name: 'monitoring.host/v1' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByText(/当前指标 cpu_usage_pct/)).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '覆盖' })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '桶数' })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '质量' })).toBeVisible()
  const captions = await page.locator('figcaption').allTextContents()
  expect(captions.length).toBeGreaterThan(0)
  expect(captions.every((caption) => caption.includes('cpu_usage_pct'))).toBe(true)
  await expect(region).toHaveAttribute('tabindex', '0')
  await region.scrollIntoViewIfNeeded()
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
