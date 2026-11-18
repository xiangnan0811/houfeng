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

test('runtime fixture accepts exact evidence-only URL and rejects undeclared stream queries', async ({ page }) => {
  const api = await installRouter(page)
  api.useProfile(unauthenticatedProfile)
  await api.allowRuntimeStream('mi_contract')
  await page.goto('/login')
  await page.evaluate(() => {
    const base = `ws://${location.host}/api/monitoring-instances/mi_contract/runtime-stream`
    new WebSocket(`${base}?include_samples=false`)
    new WebSocket(`${base}?include_samples=false&extra=1`)
  })
  const sockets = await api.snapshotRuntimeStreamSockets()
  expect(sockets.map((socket) => socket.phase)).toEqual(['open', 'error'])
  expect(sockets[0]?.includeSamples).toBe(false)
  await expect(api.assertRuntimeStreamsClean()).rejects.toThrow(/extra=1/)
})

test('record cleanup fixture serves an idle channel and rejects malformed reconcile or socket URLs', async ({ page }) => {
  const api = await installRouter(page)
  api.useProfile(unauthenticatedProfile)
  await page.goto('/login')
  const browserId = 'browser_0e2e0000-0000-4000-8000-000000000001'
  const reconcile = await page.evaluate(async (browser_id) => {
    const post = async (body: unknown) => {
      const response = await fetch('/api/records/client-cleanup/reconcile', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      return { status: response.status, body: await response.json() as unknown }
    }
    const base = `ws://${location.host}/api/records/client-cleanup?browser_id=${browser_id}`
    new WebSocket(base)
    new WebSocket(`${base}&cursor=c%2F1`)
    new WebSocket(`${base}&extra=1`)
    return [
      await post({ browser_id, record_ids: [] }),
      await post({ browser_id, record_ids: ['rec_a'], allocation_references: [{ record_id: 'rec_b', allocation_id: 'rqa_b' }] }),
      await post({ browser_id, record_ids: 'rec_a' }),
    ]
  }, browserId)
  expect(reconcile).toEqual([
    { status: 200, body: { allowed: [], invalidate: [], pending: [] } },
    { status: 200, body: { allowed: ['rec_a'], invalidate: [], pending: ['rec_b'] } },
    { status: 422, body: { error: 'fixture request body mismatch: POST /api/records/client-cleanup/reconcile' } },
  ])
  expect(() => api.assertNoUnexpectedRequests()).toThrow(/client-cleanup\/reconcile body must be/)
  expect((await api.snapshotRecordCleanupSockets()).map((socket) => [socket.browserId, socket.phase])).toEqual([
    [browserId, 'open'],
    [browserId, 'open'],
  ])
  expect((await api.snapshotRuntimeStreamSockets()).map((socket) => socket.phase)).toEqual(['error'])
  await expect(api.assertRuntimeStreamsClean()).rejects.toThrow(/extra=1/)
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

test('accepts an explicitly declared empty mutation body', async ({ page }) => {
  const api = await installRouter(page)
  api.useProfile({
    ...unauthenticatedProfile,
    [apiRouteKey('PUT', '/api/contract-empty-mutation')]: {
      status: 200,
      body: { ok: true },
      expectNoBody: true as const,
    },
  })
  await page.goto('/login')

  const result = await page.evaluate(async () => {
    const response = await fetch('/api/contract-empty-mutation', { method: 'PUT' })
    return { status: response.status, body: await response.json() as unknown }
  })

  expect(result).toEqual({ status: 200, body: { ok: true } })
  expect(() => api.assertNoUnexpectedRequests()).not.toThrow()
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
    'COMMAND_AUDIT_DETAILS_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_EVENT_OUTPUT_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_EVENT_DETAILS_SHOULD_NOT_RENDER',
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
