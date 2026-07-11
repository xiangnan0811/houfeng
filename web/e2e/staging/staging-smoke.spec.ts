import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'

import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from '@playwright/test'
import type { ProviderRecord } from '../../src/lib/types'
import {
  dashboardOverviewFixture,
  subscriptionOverviewFixture,
  vpsAssetFixture,
} from '../../src/pages/dashboard/dashboardTestFixtures'
import { canonicalApiPath } from '../fixtures/contracts'
import { expectNoDocumentOverflow } from '../support/geometry'
import { StagingAudit } from './audit'

type AuditLane = 'real-environment' | 'deployed-frontend-injection'

type ApiInjection = Readonly<{
  method: 'GET'
  path: string
  status: number
  body: unknown
  waitFor?: Promise<void>
}>

type HealthResponse = Readonly<{
  name: string
  version: string
  status: string
}>

const AUDIT_DIRECTORY = join(process.cwd(), 'test-results', 'staging-audit')

const CORE_ROUTES = [
  { name: 'dashboard', path: '/', heading: /^工作台$/ },
  { name: 'vps', path: '/vps', heading: /^VPS 资产$/ },
  { name: 'asset-decisions', path: '/asset-decisions', heading: /^资产组合决策$/ },
  { name: 'monitoring', path: '/monitoring', heading: /^监控$/ },
  { name: 'targets', path: '/targets', heading: /^入口探测$/ },
  { name: 'events', path: '/events', heading: /^事件流$/ },
  { name: 'providers', path: '/providers', heading: /服务商目录$/ },
  { name: 'subscriptions', path: '/subscriptions', heading: /订阅成本中枢$/ },
  { name: 'settings', path: '/settings', heading: /^系统设置$/ },
] as const

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'tablet', width: 1024, height: 768 },
  { name: 'mobile', width: 390, height: 900 },
] as const

const DASHBOARD_MODES = [
  {
    name: 'critical',
    overview: dashboardOverviewFixture({
      abnormal_monitoring_instance_count: 2,
      severe_monitoring_instance_count: 1,
    }),
    vps: [vpsAssetFixture()],
    action: '处理严重异常',
  },
  {
    name: 'abnormal',
    overview: dashboardOverviewFixture({
      abnormal_monitoring_instance_count: 1,
      severe_monitoring_instance_count: 0,
    }),
    vps: [vpsAssetFixture()],
    action: '处理观测异常',
  },
  {
    name: 'maintenance',
    overview: dashboardOverviewFixture({
      maintenance_monitoring_instance_count: 1,
    }),
    vps: [vpsAssetFixture()],
    action: '查看维护事件',
  },
  {
    name: 'onboarding',
    overview: dashboardOverviewFixture({
      total_monitoring_instance_count: 0,
      total_target_count: 0,
    }),
    vps: [],
    action: '创建第一台 VPS',
  },
  {
    name: 'stable',
    overview: dashboardOverviewFixture(),
    vps: [vpsAssetFixture()],
    action: '核对 VPS 库存',
  },
] as const

const LONG_PROVIDER_NAME = `Staging resilience ${'long-provider-name-'.repeat(12)}`
const LONG_PROVIDERS = Array.from({ length: 64 }, (_, index) => ({
  provider_id: `pv_staging_${String(index).padStart(3, '0')}`,
  name: index === 0 ? LONG_PROVIDER_NAME : `Staging Provider ${index + 1}`,
  website: 'https://example.invalid',
  panel_url: 'https://console.example.invalid',
  account_hint: 'non-sensitive-staging-fixture',
  country: index % 2 === 0 ? 'JP' : 'DE',
  note: 'deployed-frontend injection fixture',
  rating: 4,
  labels: ['staging', `batch-${index % 4}`],
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-11T00:00:00Z',
})) satisfies ProviderRecord[]

function requiredEnvironment(name: string): string {
  const value = process.env[name]?.trim()
  if (!value) throw new Error(`missing ${name}`)
  return value
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseHealthResponse(value: unknown): HealthResponse {
  if (
    !isRecord(value) ||
    typeof value.name !== 'string' ||
    typeof value.version !== 'string' ||
    typeof value.status !== 'string'
  ) {
    throw new Error('staging health response does not match the expected contract')
  }
  return { name: value.name, version: value.version, status: value.status }
}

function controlledPromise(): { promise: Promise<void>; resolve: () => void } {
  let release: (() => void) | undefined
  let released = false
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return {
    promise,
    resolve: () => {
      if (released) return
      released = true
      if (!release) throw new Error('controlled response gate was not initialized')
      release()
    },
  }
}

async function installApiInjections(
  page: Page,
  injections: readonly ApiInjection[],
): Promise<() => Promise<void>> {
  const handler = async (route: Route): Promise<void> => {
    const request = route.request()
    const path = canonicalApiPath(request.url())
    const injection = injections.find(
      (candidate) => candidate.method === request.method() && candidate.path === path,
    )
    if (!injection) {
      await route.continue()
      return
    }
    if (injection.waitFor) await injection.waitFor
    await route.fulfill({
      status: injection.status,
      contentType: 'application/json',
      body: JSON.stringify(injection.body),
    })
  }
  await page.route('**/api/**', handler)
  return async () => page.unroute('**/api/**', handler)
}

function dashboardInjections(options: {
  overview: ReturnType<typeof dashboardOverviewFixture>
  vps: readonly ReturnType<typeof vpsAssetFixture>[]
  dashboardWaitFor?: Promise<void>
  vpsStatus?: number
}): ApiInjection[] {
  const vpsStatus = options.vpsStatus ?? 200
  return [
    {
      method: 'GET',
      path: '/api/dashboard',
      status: 200,
      body: options.overview,
      ...(options.dashboardWaitFor ? { waitFor: options.dashboardWaitFor } : {}),
    },
    {
      method: 'GET',
      path: '/api/vps',
      status: vpsStatus,
      body: vpsStatus >= 400 ? { error: 'injected VPS failure' } : options.vps,
    },
    {
      method: 'GET',
      path: '/api/subscriptions/overview',
      status: 200,
      body: subscriptionOverviewFixture(),
    },
  ]
}

async function captureAuditScreenshot(
  page: Page,
  audit: StagingAudit,
  filename: string,
): Promise<void> {
  const screenshotDirectory = join(AUDIT_DIRECTORY, 'screenshots')
  await mkdir(screenshotDirectory, { recursive: true })
  const relativePath = `screenshots/${filename}`
  await page.screenshot({
    path: join(AUDIT_DIRECTORY, relativePath),
    animations: 'disabled',
    caret: 'hide',
    mask: [
      page.locator('input[autocomplete="username"], input[type="password"]'),
      page.locator('.user-chip'),
    ],
  })
  audit.recordScreenshot(relativePath)
}

async function captureDocumentResponse(
  audit: StagingAudit,
  response: Awaited<ReturnType<Page['goto']>>,
): Promise<void> {
  if (!response) throw new Error('staging navigation did not return a main document response')
  expect(response.status()).toBe(200)
  audit.captureMainDocumentHeaders(response)
}

async function gotoAuditedRoute(
  page: Page,
  audit: StagingAudit,
  route: { path: string; heading: string | RegExp },
): Promise<void> {
  if (page.url() !== 'about:blank') await audit.assertClean(page)
  const response = await page.goto(route.path, { waitUntil: 'domcontentloaded' })
  await captureDocumentResponse(audit, response)
  const viewport = page.viewportSize()
  if (!viewport) throw new Error('staging route must have an explicit viewport')
  audit.recordRoute(route.path, viewport.width, viewport.height)

  const main = page.locator('main#main-content')
  await expect(main).toBeVisible()
  await expect(main).not.toBeEmpty()
  await expect(page.getByRole('heading', { name: route.heading }).first()).toBeVisible()
  await page.evaluate(() => document.fonts.ready)
  await expectNoDocumentOverflow(page)
  await audit.assertClean(page)
}

async function readRawLayerDays(page: Page): Promise<number> {
  const value = await page.evaluate(async () => {
    const response = await fetch('/api/settings', {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
    if (!response.ok) return null
    const body: unknown = await response.json()
    if (!body || typeof body !== 'object') return null
    const retention = Reflect.get(body, 'retention_policy')
    if (!retention || typeof retention !== 'object') return null
    const rawDays = Reflect.get(retention, 'raw_layer_days')
    return typeof rawDays === 'number' ? rawDays : null
  })
  if (!Number.isInteger(value) || value == null || value < 30) {
    throw new Error('staging settings readback did not return a valid raw retention value')
  }
  return value
}

function buildRestorationPayload(body: unknown, rawLayerDays: number): unknown {
  if (!isRecord(body) || !isRecord(body.retention_policy)) {
    throw new Error('settings mutation payload was unavailable for rollback')
  }
  return {
    ...body,
    retention_policy: {
      ...body.retention_policy,
      raw_layer_days: rawLayerDays,
    },
  }
}

async function restoreRawLayerDays(
  page: Page,
  audit: StagingAudit,
  rawLayerDays: number,
  capturedPayload: unknown,
): Promise<unknown | null> {
  let headerFailure: unknown = null
  let uiFailure: unknown = null
  try {
    const response = await page.goto('/settings?tab=monitoring', { waitUntil: 'domcontentloaded' })
    try {
      await captureDocumentResponse(audit, response)
    } catch (error) {
      headerFailure = error
    }
    const input = page.getByLabel('原始层保留天数')
    await expect(input).toBeVisible()
    await input.fill(String(rawLayerDays))
    await page.getByRole('button', { name: '保存设置' }).click()
    await expect(page.getByText('设置已保存', { exact: true })).toBeVisible()
  } catch (error) {
    uiFailure = error
  }

  if (uiFailure) {
    const payload = buildRestorationPayload(capturedPayload, rawLayerDays)
    const result = await page.evaluate(async (restorePayload) => {
      const response = await fetch('/api/settings', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(restorePayload),
      })
      return { ok: response.ok, status: response.status }
    }, payload)
    if (!result.ok) {
      throw new Error(`staging settings rollback failed with HTTP ${result.status}`)
    }
  }

  expect(await readRawLayerDays(page)).toBe(rawLayerDays)
  return headerFailure
}

async function exerciseCustomTemplateCancel(page: Page): Promise<void> {
  await page.getByRole('button', { name: '场景与组合' }).click()
  const cards = page.locator('.asset-decision-template-launcher')
  await expect(cards.first()).toBeVisible()

  let customCard = null as ReturnType<Page['locator']> | null
  for (let index = 0; index < await cards.count(); index += 1) {
    const card = cards.nth(index)
    if (await card.getByText('内置', { exact: true }).count() === 0) {
      customCard = card
      break
    }
  }
  if (!customCard) {
    throw new Error('staging test data must include one non-sensitive custom scenario template')
  }

  const mutationRequests: string[] = []
  const mutationListener = (request: Request): void => {
    const url = new URL(request.url())
    if (
      url.pathname.startsWith('/api/asset-decisions/scenario-templates/') &&
      request.method() !== 'GET'
    ) {
      mutationRequests.push(`${request.method()} ${url.pathname}`)
    }
  }
  page.on('request', mutationListener)
  try {
    await customCard.getByRole('button', { name: '使用模板' }).click()
    const parent = page.locator('[role="dialog"][aria-label="资产决策场景模板详情"]')
    await expect(parent).toBeVisible()
    await parent.getByRole('tab', { name: '状态' }).click()
    const statusCommand = parent.getByRole('button', { name: /归档模板|重新启用/ })
    await statusCommand.click()
    const confirmation = page.getByRole('alertdialog', {
      name: /确认归档模板|确认重新启用模板/,
    })
    await expect(confirmation).toBeVisible()
    await expect(parent).toHaveAttribute('inert', '')
    await confirmation.getByRole('button', { name: '取消' }).click()
    await expect(confirmation).toHaveCount(0)
    await expect(parent).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(parent).toHaveCount(0)
  } finally {
    page.off('request', mutationListener)
  }
  expect(mutationRequests, 'cancel-only template flow must not send a mutation').toEqual([])
}

test('audits the authenticated staging release without retaining credentials or raw bodies', async ({
  page,
}) => {
  const expectedVersion = requiredEnvironment('HOUFENG_EXPECTED_VERSION')
  const username = requiredEnvironment('HOUFENG_STAGING_USERNAME')
  const password = requiredEnvironment('HOUFENG_STAGING_PASSWORD')
  const audit = new StagingAudit()
  await audit.install(page)
  audit.allowHttpError('/api/auth/me', 401)

  let observedVersion = 'unverified'
  let runFailure: unknown = null
  let failureCategory: string | null = null
  let activeLane: AuditLane = 'real-environment'
  let activeStep = 'bootstrap'
  let restoreRequired = false
  let originalRawLayerDays: number | null = null
  let capturedSettingsPayload: unknown = null

  const runAuditStep = async (
    lane: AuditLane,
    name: string,
    action: () => Promise<void>,
  ): Promise<void> => {
    activeLane = lane
    activeStep = name
    try {
      await test.step(`${lane}: ${name}`, action)
      audit.recordStep(lane, name)
    } catch (error) {
      audit.recordStep(lane, name, 'failed')
      throw error
    }
  }

  try {
    await runAuditStep('real-environment', 'release version', async () => {
      const response = await page.request.get('/api/healthz', { failOnStatusCode: false })
      expect(response.status()).toBe(200)
      const health = parseHealthResponse(await response.json())
      expect(health.name).toBe('houfeng-center')
      expect(health.status).toBe('ok')
      expect(health.version).toBe(expectedVersion)
      observedVersion = health.version
    })

    await runAuditStep('real-environment', 'UI login', async () => {
      await page.setViewportSize({ width: 1440, height: 1000 })
      const response = await page.goto('/login', { waitUntil: 'domcontentloaded' })
      await captureDocumentResponse(audit, response)
      await page.getByLabel('用户名').fill(username)
      await page.getByLabel('密码').fill(password)
      await page.getByRole('button', { name: '登录', exact: true }).click()
      await expect(page).toHaveURL((url) => url.pathname === '/')
      await expect(page.getByRole('heading', { name: '工作台', exact: true })).toBeVisible()
      await audit.assertClean(page)
    })

    await runAuditStep('real-environment', 'nine core routes', async () => {
      for (const route of CORE_ROUTES) {
        await gotoAuditedRoute(page, audit, route)
        await captureAuditScreenshot(page, audit, `real-${route.name}.png`)
      }
    })

    await runAuditStep('real-environment', 'custom template cancel-only confirmation', async () => {
      await gotoAuditedRoute(page, audit, {
        path: '/asset-decisions',
        heading: /^资产组合决策$/,
      })
      await exerciseCustomTemplateCancel(page)
      await captureAuditScreenshot(page, audit, 'real-asset-template-cancelled.png')
      await audit.assertClean(page)
    })

    await runAuditStep('real-environment', 'reversible settings save and restore', async () => {
      await gotoAuditedRoute(page, audit, {
        path: '/settings?tab=monitoring',
        heading: /^系统设置$/,
      })
      const rawLayerInput = page.getByLabel('原始层保留天数')
      await expect(rawLayerInput).toBeVisible()
      originalRawLayerDays = Number.parseInt(await rawLayerInput.inputValue(), 10)
      if (!Number.isInteger(originalRawLayerDays) || originalRawLayerDays < 30) {
        throw new Error('staging raw retention snapshot was invalid')
      }
      const temporaryRawLayerDays = originalRawLayerDays + 1
      const capturePayload = (request: Request): void => {
        const url = new URL(request.url())
        if (request.method() !== 'PUT' || url.pathname !== '/api/settings') return
        try {
          capturedSettingsPayload = request.postDataJSON()
        } catch {
          capturedSettingsPayload = null
        }
      }
      page.on('request', capturePayload)
      restoreRequired = true
      let mutationFailure: unknown = null
      try {
        await rawLayerInput.fill(String(temporaryRawLayerDays))
        await page.getByRole('button', { name: '保存设置' }).click()
        await expect(page.getByText('设置已保存', { exact: true })).toBeVisible()
        expect(await readRawLayerDays(page)).toBe(temporaryRawLayerDays)
      } catch (error) {
        mutationFailure = error
      } finally {
        page.off('request', capturePayload)
      }
      const restoreAuditFailure = await restoreRawLayerDays(
        page,
        audit,
        originalRawLayerDays,
        capturedSettingsPayload,
      )
      restoreRequired = false
      if (mutationFailure && restoreAuditFailure) {
        throw new AggregateError(
          [mutationFailure, restoreAuditFailure],
          'settings mutation failed and the restoration document failed security audit',
        )
      }
      if (mutationFailure) throw mutationFailure
      if (restoreAuditFailure) throw restoreAuditFailure
      await audit.assertClean(page)
      await captureAuditScreenshot(page, audit, 'real-settings-restored.png')
    })

    await runAuditStep('real-environment', 'theme persistence across reload', async () => {
      await gotoAuditedRoute(page, audit, { path: '/settings', heading: /^系统设置$/ })
      const group = page.getByRole('group', { name: '主题明暗' })
      const selected = group.getByRole('button', { pressed: true })
      const originalMode = await selected.getAttribute('aria-label')
      if (!originalMode) throw new Error('theme control did not expose its selected mode')
      const targetMode = originalMode === '浅色' ? '深色' : '浅色'
      await group.getByRole('button', { name: targetMode, exact: true }).click()
      await expect(group.getByRole('button', { name: targetMode, exact: true })).toHaveAttribute(
        'aria-pressed',
        'true',
      )
      await audit.assertClean(page)
      const response = await page.reload({ waitUntil: 'domcontentloaded' })
      await captureDocumentResponse(audit, response)
      const reloadedGroup = page.getByRole('group', { name: '主题明暗' })
      await expect(reloadedGroup.getByRole('button', { name: targetMode, exact: true })).toHaveAttribute(
        'aria-pressed',
        'true',
      )
      await captureAuditScreenshot(page, audit, 'real-theme-reloaded.png')
      await reloadedGroup.getByRole('button', { name: originalMode, exact: true }).click()
      await audit.assertClean(page)
    })

    await runAuditStep('deployed-frontend-injection', 'Dashboard five modes', async () => {
      for (const mode of DASHBOARD_MODES) {
        const removeInjections = await installApiInjections(page, dashboardInjections(mode))
        try {
          await gotoAuditedRoute(page, audit, { path: '/', heading: /^工作台$/ })
          await expect(page.getByRole('link', { name: mode.action, exact: true })).toBeVisible()
          await captureAuditScreenshot(page, audit, `injected-dashboard-${mode.name}.png`)
          await audit.assertClean(page)
        } finally {
          await removeInjections()
        }
      }
    })

    await runAuditStep('deployed-frontend-injection', 'Dashboard supporting 503', async () => {
      audit.allowHttpError('/api/vps', 503)
      const removeInjections = await installApiInjections(page, dashboardInjections({
        overview: dashboardOverviewFixture({
          total_monitoring_instance_count: 0,
          total_target_count: 0,
        }),
        vps: [],
        vpsStatus: 503,
      }))
      try {
        await gotoAuditedRoute(page, audit, { path: '/', heading: /^工作台$/ })
        await expect(page.getByRole('heading', { name: '部分事实待确认' })).toBeVisible()
        await expect(page.getByRole('link', { name: '创建第一台 VPS' })).toHaveCount(0)
        await captureAuditScreenshot(page, audit, 'injected-dashboard-503.png')
      } finally {
        await removeInjections()
      }
    })

    await runAuditStep('deployed-frontend-injection', 'controlled slow response', async () => {
      const gate = controlledPromise()
      const removeInjections = await installApiInjections(page, dashboardInjections({
        overview: dashboardOverviewFixture(),
        vps: [vpsAssetFixture()],
        dashboardWaitFor: gate.promise,
      }))
      try {
        const response = await page.goto('/', { waitUntil: 'domcontentloaded' })
        await captureDocumentResponse(audit, response)
        await expect(page.getByRole('heading', { name: '正在加载工作台…' })).toBeVisible()
        gate.resolve()
        await expect(page.getByRole('link', { name: '核对 VPS 库存' })).toBeVisible()
        await audit.assertClean(page)
      } finally {
        gate.resolve()
        await removeInjections()
      }
    })

    await runAuditStep('deployed-frontend-injection', 'long list responsive matrix', async () => {
      const removeInjections = await installApiInjections(page, [
        {
          method: 'GET',
          path: '/api/dashboard',
          status: 200,
          body: dashboardOverviewFixture(),
        },
        { method: 'GET', path: '/api/providers', status: 200, body: LONG_PROVIDERS },
        { method: 'GET', path: '/api/vps', status: 200, body: [] },
        { method: 'GET', path: '/api/subscriptions', status: 200, body: [] },
      ])
      try {
        for (const viewport of VIEWPORTS) {
          await page.setViewportSize({ width: viewport.width, height: viewport.height })
          await gotoAuditedRoute(page, audit, {
            path: '/providers',
            heading: /服务商目录$/,
          })
          await expect(page.getByText(LONG_PROVIDER_NAME, { exact: true }).first()).toBeVisible()
          await expectNoDocumentOverflow(page)
          await captureAuditScreenshot(page, audit, `injected-providers-${viewport.name}.png`)
        }
      } finally {
        await removeInjections()
      }
    })
  } catch (error) {
    runFailure = error
    failureCategory = `${activeLane}:${activeStep}`
  } finally {
    if (restoreRequired && originalRawLayerDays != null) {
      try {
        const restoreAuditFailure = await restoreRawLayerDays(
          page,
          audit,
          originalRawLayerDays,
          capturedSettingsPayload,
        )
        restoreRequired = false
        if (restoreAuditFailure && !runFailure) {
          runFailure = restoreAuditFailure
          failureCategory ??= `${activeLane}:${activeStep}`
        }
      } catch {
        runFailure = new Error('staging settings restore failed; operator intervention is required')
        failureCategory = 'real-environment:settings-restore'
      }
    }

    try {
      await audit.assertClean(page)
    } catch (error) {
      if (!runFailure) runFailure = error
      failureCategory ??= `${activeLane}:${activeStep}`
    }

    if (runFailure) {
      try {
        await captureAuditScreenshot(page, audit, 'failure.png')
      } catch {
        // The manifest remains the mandatory artifact if the page is already unavailable.
      }
    }

    try {
      await audit.write({
        directory: AUDIT_DIRECTORY,
        expectedVersion,
        observedVersion,
        browserVersion: page.context().browser()?.version() ?? 'unavailable',
        conclusion: runFailure ? 'failed' : 'passed',
        failureCategory,
      })
    } catch (error) {
      if (!runFailure) runFailure = error
    }
  }

  if (runFailure) throw runFailure
})
