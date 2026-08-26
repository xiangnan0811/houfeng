import { test as base } from '@playwright/test'

import { BrowserDiagnostics } from '../support/diagnostics'
import { ApiFixtureController } from './router'

type HoufengFixtures = {
  api: ApiFixtureController
  diagnostics: BrowserDiagnostics
}

export const test = base.extend<HoufengFixtures>({
  diagnostics: [async ({ page }, use) => {
    const diagnostics = new BrowserDiagnostics()
    await diagnostics.install(page)
    await use(diagnostics)
    await diagnostics.assertClean(page)
  }, { auto: true }],
  api: [async ({ page, diagnostics }, use) => {
    const api = new ApiFixtureController(page, diagnostics)
    await api.install()
    await use(api)
    api.assertNoUnexpectedRequests()
    await api.assertRuntimeStreamsClean()
  }, { auto: true }],
})

export { expect } from '@playwright/test'
