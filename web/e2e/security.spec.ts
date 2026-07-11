import { expect, test } from '@playwright/test'

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
