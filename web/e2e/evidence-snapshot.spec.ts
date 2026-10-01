import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { evidenceSnapshotProfile, type EvidenceSnapshotFixtureKind } from './fixtures/profiles'
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

const BODIES: ReadonlyArray<{ kind: Exclude<EvidenceSnapshotFixtureKind, 'unsupported'>; heading: RegExp; visible: string }> = [
  { kind: 'monitoring.host', heading: /^主机监控趋势$/, visible: 'CPU 使用率' },
  { kind: 'monitoring.probe', heading: /^入口探测趋势$/, visible: '延迟' },
  { kind: 'monitoring.event', heading: /^监控事件/, visible: 'TCP 重传率持续高于 2%' },
  { kind: 'ip_quality.report', heading: /^IP 质量报告$/, visible: '服务解锁' },
  { kind: 'subscription.cost', heading: /^订阅成本$/, visible: '折算金额' },
  { kind: 'command.audit', heading: /^命令审计/, visible: 'df_h' },
]

for (const viewport of VIEWPORTS) for (const body of BODIES) {
  test(`${body.kind} evidence reads without overflow at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(evidenceSnapshotProfile({ kind: body.kind }))
    await page.setViewportSize(viewport)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/evidence/evs_e2eview')

    await expect(page.locator('.page__head').getByRole('heading', { level: 1 })).toBeVisible()
    await expect(page.getByRole('heading', { name: body.heading })).toBeVisible()
    await expect(page.getByText(body.visible, { exact: true }).first()).toBeVisible()
    // 原始类型编码与英文枚举不再直接出现在页头。
    await expect(page.locator('.page__head')).not.toContainText(body.kind)
    await expectLocatorNotClipped(page.getByRole('link', { name: '横向比较' }))
    await expectNoDocumentOverflow(page)
  })
}

test('monitoring evidence draws one compact chart per metric and marks the gap', async ({ api, page }) => {
  api.useProfile(evidenceSnapshotProfile({ kind: 'monitoring.host' }))
  await page.goto('/evidence/evs_e2eview')
  const body = page.getByRole('region', { name: '主机监控趋势' })
  await expect(body.getByRole('figure')).toHaveCount(2)
  await expect(body.getByText('峰值', { exact: false }).first()).toBeVisible()
  const chartHeights = await body.getByRole('figure').evaluateAll((figures) => figures.map((figure) => figure.getBoundingClientRect().height))
  expect(Math.max(...chartHeights)).toBeLessThan(220)
  await expect(page.getByText('部分覆盖')).toBeVisible()
})

test('technical details stay folded and keep redaction out of the reading surface', async ({ api, page }) => {
  api.useProfile(evidenceSnapshotProfile({ kind: 'command.audit', sourceUnavailable: true, redacted: true }))
  await page.goto('/evidence/evs_e2eview')

  await expect(page.getByText('来源已不可用')).toBeVisible()
  const tech = page.getByRole('region', { name: '技术细节' })
  await expect(tech.getByText('· 2 个字段已按策略处理')).toBeVisible()
  await expect(tech.getByText('payload.stdout')).toBeHidden()
  await expect(tech.getByText('evs_e2eview')).toBeHidden()
  await tech.getByText('技术细节').click()
  await expect(tech.getByText('payload.stdout')).toBeVisible()
  await expect(tech.getByText('evs_e2eview')).toBeVisible()
})

test('expanded technical details stay inside a 390px viewport', async ({ api, page }) => {
  api.useProfile(evidenceSnapshotProfile({ kind: 'monitoring.probe', redacted: true }))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/evidence/evs_e2eview')
  await expect(page.getByRole('img', { name: '延迟趋势' })).toBeVisible()
  await page.getByRole('region', { name: '技术细节' }).getByText('技术细节').click()
  await expect(page.getByText('payload.actor_ip')).toBeVisible()
  await expectNoDocumentOverflow(page)
})

test('backfilled evidence is flagged in the header and on the event', async ({ api, page }) => {
  api.useProfile(evidenceSnapshotProfile({ kind: 'monitoring.event' }))
  await page.goto('/evidence/evs_e2eview')
  await expect(page.locator('.page__head').getByText('含回填样本')).toBeVisible()
  await expect(page.getByText('回填', { exact: true })).toBeVisible()
})

test('an unregistered renderer fails closed without exposing the payload', async ({ api, page }) => {
  api.useProfile(evidenceSnapshotProfile({ kind: 'unsupported' }))
  await page.goto('/evidence/evs_e2eview')
  await expect(page.getByRole('heading', { name: '不支持的证据类型' })).toBeVisible()
  await expect(page.getByText('df_h')).toHaveCount(0)
  await expect(page.getByRole('region', { name: '技术细节' })).toBeVisible()
})

for (const theme of THEMES) for (const kind of ['monitoring.host', 'monitoring.probe', 'monitoring.event', 'ip_quality.report', 'subscription.cost', 'command.audit'] as const) {
  test(`${kind} evidence has no serious or critical axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile(evidenceSnapshotProfile({ kind, sourceUnavailable: kind === 'command.audit', redacted: kind === 'command.audit' }))
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.goto('/evidence/evs_e2eview')
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    await expect(page.locator('.page__head').getByRole('heading', { level: 1 })).toBeVisible()
    await page.getByRole('region', { name: '技术细节' }).getByText('技术细节').click()
    await page.evaluate(() => document.fonts.ready)

    const result = await new AxeBuilder({ page }).analyze()
    const blocking = result.violations
      .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
      .map((violation) => ({ id: violation.id, impact: violation.impact, targets: violation.nodes.map((node) => node.target) }))
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  })
}
