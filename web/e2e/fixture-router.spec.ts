import { expect, test } from '@playwright/test'

import { BrowserDiagnostics } from './support/diagnostics'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile, unauthenticatedProfile } from './fixtures/profiles'
import { ApiFixtureController } from './fixtures/router'

async function installRouter(page: import('@playwright/test').Page) {
  const diagnostics = new BrowserDiagnostics()
  await diagnostics.install(page)
  const api = new ApiFixtureController(page, diagnostics)
  await api.install()
  return api
}

test('fails closed for an undeclared API request', async ({ page }) => {
  const api = await installRouter(page)
  api.useProfile(unauthenticatedProfile)
  await page.goto('/login')

  const status = await page.evaluate(async () => {
    const response = await fetch('/api/contract-self-test')
    return response.status
  })

  expect(status).toBe(501)
  expect(() => api.assertNoUnexpectedRequests()).toThrow(
    /GET \/api\/contract-self-test/,
  )
})

test('requires every mutation fixture to declare its exact body keys', async ({ page }) => {
  const api = await installRouter(page)
  api.useProfile({
    ...unauthenticatedProfile,
    [apiRouteKey('POST', '/api/contract-mutation')]: {
      status: 200,
      body: { ok: true },
    },
  })
  await page.goto('/login')

  const status = await page.evaluate(async () => {
    const response = await fetch('/api/contract-mutation', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: true }),
    })
    return response.status
  })

  expect(status).toBe(422)
  expect(() => api.assertNoUnexpectedRequests()).toThrow(
    /POST \/api\/contract-mutation must declare expectedBodyKeys/,
  )
})

test('matches query parameters by canonical key order', async ({ page }) => {
  const api = await installRouter(page)
  api.useProfile({
    ...unauthenticatedProfile,
    [apiRouteKey('GET', '/api/contract-query?a=1&b=2')]: {
      status: 200,
      body: { ok: true },
    },
  })
  await page.goto('/login')

  const result = await page.evaluate(async () => {
    const response = await fetch('/api/contract-query?b=2&a=1')
    return { status: response.status, body: await response.json() as unknown }
  })

  expect(result).toEqual({ status: 200, body: { ok: true } })
  expect(() => api.assertNoUnexpectedRequests()).not.toThrow()
})

test('command audit profile declares only the metadata-safe default query', async ({ page }) => {
  const api = await installRouter(page)
  api.useProfile(coreRouteProfile('/command-audit'))

  await page.goto('/command-audit')
  await expect(page.getByRole('heading', { name: '命令审计', exact: true })).toBeVisible()
  await expect(page.getByText('Tokyo Edge', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '展开 2 个事件' }).click()

  for (const forbidden of [
    'COMMAND_AUDIT_STDOUT_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_STDERR_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_EVENT_OUTPUT_SHOULD_NOT_RENDER',
  ]) {
    await expect(page.getByText(forbidden, { exact: true })).toHaveCount(0)
  }

  const status = await page.evaluate(async () => {
    const response = await fetch('/api/command-audits?cursor=undeclared')
    return response.status
  })

  expect(status).toBe(501)
  expect(() => api.assertNoUnexpectedRequests()).toThrow(
    /GET \/api\/command-audits\?cursor=undeclared/,
  )
})
