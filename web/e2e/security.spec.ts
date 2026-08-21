import { expect, test } from '@playwright/test'

import { expect as fixtureExpect, test as fixtureTest } from './fixtures'
import {
  comparisonWorkbenchHref,
  comparisonWorkbenchProfile,
} from './fixtures/profiles'
import { BrowserDiagnostics } from './support/diagnostics'

test('diagnostics rejects an injected console error', async ({ page }) => {
  const diagnostics = new BrowserDiagnostics()
  await diagnostics.install(page)
  await page.goto('about:blank')

  await page.evaluate(() => console.error('houfeng console sentinel'))

  await expect(diagnostics.assertClean(page)).rejects.toThrow(
    /console\.error: houfeng console sentinel/,
  )
})

test('diagnostics rejects injected CSP and unhandled rejection events', async ({ page }) => {
  const diagnostics = new BrowserDiagnostics()
  await diagnostics.install(page)
  await page.goto('about:blank')

  await page.evaluate(() => {
    const cspEvent = new Event('securitypolicyviolation')
    Object.defineProperties(cspEvent, {
      effectiveDirective: { value: 'script-src-elem' },
      blockedURI: { value: 'inline' },
      disposition: { value: 'enforce' },
    })
    window.dispatchEvent(cspEvent)

    const rejectionEvent = new Event('unhandledrejection')
    Object.defineProperty(rejectionEvent, 'reason', {
      value: new Error('houfeng rejection sentinel'),
    })
    window.dispatchEvent(rejectionEvent)
  })

  await expect(diagnostics.assertClean(page)).rejects.toThrow(/csp: script-src-elem inline enforce/)
  await expect(diagnostics.assertClean(page)).rejects.toThrow(
    /unhandledrejection: Error: houfeng rejection sentinel/,
  )
})

fixtureTest('横向比较工作台 404 does not keep restricted identities', async ({ api, page }) => {
  api.useProfile(comparisonWorkbenchProfile({ mode: 'revoked' }))
  await page.goto(comparisonWorkbenchHref({
    mode: 'fixed',
    items: [{ snapshot_id: 'evs_cmpleft' }, { snapshot_id: 'evs_cmpright' }],
    baseline: 0,
    alignment: 'actual_coverage',
    tolerance_seconds: 60,
    kind: 'monitoring.host/v1',
    metric: 'cpu_usage_pct',
  }))
  await fixtureExpect(page.getByRole('heading', { name: '比较不可用' })).toBeVisible()
  await fixtureExpect(page.getByText('evs_restricted')).toHaveCount(0)
  await fixtureExpect(page.getByRole('heading', { name: '趋势' })).toHaveCount(0)
  await fixtureExpect(page.getByRole('button', { name: '另存为记录' })).toHaveCount(0)
})
