import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { subjectActivityPopulatedProfile } from './fixtures/profiles'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

const VIEWPORTS = [
  { width: 1440, height: 1000 },
  { width: 1024, height: 768 },
  { width: 390, height: 900 },
] as const

const THEMES = [
  { preset: 'houfeng', mode: 'dark' },
  { preset: 'houfeng', mode: 'light' },
  { preset: 'precision', mode: 'dark' },
  { preset: 'precision', mode: 'light' },
  { preset: 'observatory', mode: 'dark' },
] as const

const SUBJECTS = [
  { kind: 'vps', path: '/vps/vps_001', title: 'Tokyo Edge', kindLabel: 'VPS' },
  { kind: 'monitoring_instance', path: '/monitoring/mi_001', title: 'alpha 主机监控', kindLabel: '监控实例' },
  { kind: 'target', path: '/targets/tg_001', title: 'API 443 入口', kindLabel: '入口探测' },
] as const

for (const viewport of VIEWPORTS) for (const subject of SUBJECTS) {
  test(`${subject.kind} activity header and timeline stay compact at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(subjectActivityPopulatedProfile({ kind: subject.kind }))
    await page.setViewportSize(viewport)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto(`${subject.path}/activity`)

    const header = page.locator('.page__head')
    await expect(header.getByRole('heading', { level: 1, name: subject.title })).toBeVisible()
    await expect(header.getByLabel('主体身份')).toContainText(subject.kindLabel)
    await expectLocatorNotClipped(header.getByRole('link', { name: '返回详情' }))
    await expectLocatorNotClipped(header.getByRole('link', { name: '新建记录' }))

    const row = page.locator('.unified-timeline__item').filter({ hasText: 'TCP 重传率升高' })
    await expect(row).toBeVisible()
    // 按本地日（Asia/Shanghai）分组，每天一张面板。
    await expect(page.locator('.unified-timeline__day-title')).toHaveText(['2026-08-19', '2026-08-18', '2026-08-17'])
    if (viewport.width <= 640) {
      // 窄屏标题行落在时间下一行。
      const clock = await row.locator('.unified-timeline__clock').boundingBox()
      const title = await row.getByRole('heading', { name: 'TCP 重传率升高' }).boundingBox()
      if (!clock || !title) throw new Error('expected timeline row boxes')
      expect(title.y).toBeGreaterThanOrEqual(clock.y + clock.height - 1)
    }
    if (viewport.width > 640) {
      // 时间、标题、通道与回填标签在同一行。
      const clock = await row.locator('.unified-timeline__clock').boundingBox()
      const title = await row.getByRole('heading', { name: 'TCP 重传率升高' }).boundingBox()
      const tag = await row.getByText('回填', { exact: true }).boundingBox()
      if (!clock || !title || !tag) throw new Error('expected timeline row boxes')
      expect(Math.abs(clock.y - title.y)).toBeLessThan(8)
      expect(Math.abs(tag.y - title.y)).toBeLessThan(8)
    }
    await expectNoDocumentOverflow(page)
  })
}

test('evidence view keeps comparison and evidence links on the item line', async ({ api, page }) => {
  api.useProfile(subjectActivityPopulatedProfile({ view: 'evidence' }))
  await page.goto('/vps/vps_001/evidence')
  const row = page.locator('.unified-timeline__item').filter({ hasText: '第三晚主机负载' })
  await expect(row.getByRole('link', { name: '查看证据' })).toHaveAttribute('href', '/evidence/evs_e2eview')
  await expect(row.getByRole('link', { name: '加入横向比较' })).toBeVisible()
  await expect(page.locator('.page__head').getByRole('link', { name: '横向比较' })).toBeVisible()
})

test('records view lists only record lifecycle items', async ({ api, page }) => {
  api.useProfile(subjectActivityPopulatedProfile({ view: 'records' }))
  await page.goto('/vps/vps_001/records')
  await expect(page.locator('.unified-timeline__item')).toHaveCount(2)
  await expect(page.getByText('确认监控告警时间线')).toHaveCount(0)
  await expectNoDocumentOverflow(page)
})

test('header actions wrap left at 720px', async ({ api, page }) => {
  api.useProfile(subjectActivityPopulatedProfile())
  await page.setViewportSize({ width: 720, height: 900 })
  await page.goto('/vps/vps_001/activity')
  const back = page.locator('.page__head').getByRole('link', { name: '返回详情' })
  const title = page.locator('.page__head').getByRole('heading', { level: 1 })
  const backBox = await back.boundingBox()
  const titleBox = await title.boundingBox()
  if (!backBox || !titleBox) throw new Error('expected header boxes')
  expect(backBox.x).toBeLessThan(titleBox.x)
})

test('a degraded source is announced once with its state', async ({ api, page }) => {
  api.useProfile(subjectActivityPopulatedProfile({ degraded: true }))
  await page.goto('/vps/vps_001/activity')
  await expect(page.getByRole('status').filter({ hasText: '部分来源暂不可用' })).toBeVisible()
  await expect(page.getByRole('list', { name: '来源状态' })).toContainText('命令审计：过期')
})

for (const theme of THEMES) for (const target of [
  { kind: 'vps', path: '/vps/vps_001/activity', degraded: true },
  { kind: 'monitoring_instance', path: '/monitoring/mi_001/activity', degraded: false },
  { kind: 'target', path: '/targets/tg_001/activity', degraded: false },
] as const) {
  test(`${target.kind} activity has no serious or critical axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile(subjectActivityPopulatedProfile({ kind: target.kind, degraded: target.degraded }))
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.goto(target.path)
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    await expect(page.locator('.unified-timeline__item').first()).toBeVisible()
    await page.evaluate(() => document.fonts.ready)
    const result = await new AxeBuilder({ page }).analyze()
    const blocking = result.violations
      .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
      .map((violation) => ({ id: violation.id, impact: violation.impact, targets: violation.nodes.map((node) => node.target) }))
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  })
}
