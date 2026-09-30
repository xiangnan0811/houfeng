import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import {
  authenticatedProfile,
  comparisonWorkbenchHref,
  comparisonWorkbenchProfile,
  recordDetailProfile,
} from './fixtures/profiles'

// 记录工作区与横向比较在每个运行时主题下都要过 settled axe（观测台浅色回退候风浅色）。
const THEMES = [
  { preset: 'houfeng', mode: 'dark' },
  { preset: 'houfeng', mode: 'light' },
  { preset: 'precision', mode: 'dark' },
  { preset: 'precision', mode: 'light' },
  { preset: 'observatory', mode: 'dark' },
] as const

const TREND_HREF = comparisonWorkbenchHref({
  mode: 'fixed',
  items: [{ snapshot_id: 'evs_cmpleft' }, { snapshot_id: 'evs_cmpright' }],
  baseline: 0,
  alignment: 'actual_coverage',
  tolerance_seconds: 60,
  kind: 'monitoring.host/v1',
  metric: 'cpu_usage_pct',
})

const SURFACES = [
  { name: '记录阅读', path: '/records/rec_e2e001', ready: /^行动项/, profile: () => recordDetailProfile({ populated: true }) },
  { name: '记录编辑', path: '/records/rec_e2e001/edit', ready: /^属性$/, profile: () => recordDetailProfile({ populated: true }) },
  { name: '新建记录', path: '/records/new', ready: /^属性$/, profile: () => authenticatedProfile() },
  { name: '历史修订', path: '/records/rec_e2e001/revisions/rrv_e2e001', ready: /^与当前版本的差异$/, profile: () => recordDetailProfile({ populated: true }) },
  { name: '横向比较', path: TREND_HREF, ready: /^趋势$/, profile: () => comparisonWorkbenchProfile({ mode: 'host-trend' }) },
] as const

for (const theme of THEMES) for (const surface of SURFACES) {
  test(`${surface.name} has no serious or critical axe violations in ${theme.preset}-${theme.mode}`, async ({ api, page }) => {
    api.useProfile(surface.profile())
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addInitScript(({ preset, mode }) => {
      localStorage.setItem('houfeng.theme.preset', preset)
      localStorage.setItem('houfeng.theme.mode', mode)
    }, theme)
    await page.goto(surface.path)
    await expect(page.locator('html')).toHaveClass(`theme-${theme.preset}-${theme.mode}`)
    await expect(page.getByRole('heading', { name: surface.ready })).toBeVisible()
    await page.evaluate(() => document.fonts.ready)

    const result = await new AxeBuilder({ page }).analyze()
    const blocking = result.violations
      .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
      .map((violation) => ({
        id: violation.id,
        impact: violation.impact,
        targets: violation.nodes.map((node) => node.target),
      }))
    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  })
}
