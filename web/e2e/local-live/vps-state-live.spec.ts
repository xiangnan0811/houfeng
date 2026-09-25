import { randomUUID } from 'node:crypto'
import { expect, test, type Page, type TestInfo } from '@playwright/test'

type JsonObject = Record<string, unknown>
type JsonRequestResult = { status: number; body: unknown }

test('real login, stale shared cancellation, archive restore, and retired MI re-enrollment', async ({ browser }, testInfo) => {
  test.setTimeout(240_000)
  const baseURL = testInfo.project.use.baseURL as string
  const username = requiredEnv('LOCAL_LIVE_USERNAME')
  const password = requiredEnv('LOCAL_LIVE_PASSWORD')
  const contextA = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' })
  const contextB = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' })
  const pageA = await contextA.newPage()
  const pageB = await contextB.newPage()
  const viewportEvidence: Array<{ route: string; width: number; height: number; theme: string }> = []

  try {
    await loginThroughUI(pageA, username, password)
    await loginThroughUI(pageB, username, password)
    await pageA.goto('/vps')
    await expect(pageA).toHaveURL(/\/vps$/)
    await expect(pageA.getByRole('button', { name: '切换主题' })).toBeVisible()

    await chooseTheme(pageA, '氛围暗色', 'theme-houfeng-dark')
    await captureViewport(pageA, testInfo, 'desktop-dark', viewportEvidence)
    await chooseTheme(pageA, '精致亮色', 'theme-houfeng-light')
    await captureViewport(pageA, testInfo, 'desktop-light', viewportEvidence)
    await chooseTheme(pageA, '氛围暗色', 'theme-houfeng-dark')
    await pageA.setViewportSize({ width: 390, height: 900 })
    await captureViewport(pageA, testInfo, 'mobile-dark', viewportEvidence)
    await pageA.setViewportSize({ width: 1440, height: 1000 })
    await chooseTheme(pageA, '精致亮色', 'theme-houfeng-light')
    await pageA.setViewportSize({ width: 390, height: 900 })
    await captureViewport(pageA, testInfo, 'mobile-light', viewportEvidence)

    const me = await apiOK(pageA, 'GET', '/api/auth/me')
    expect(stringField(me, 'username')).toBe(username)
    const health = await apiOK(pageA, 'GET', '/api/healthz')
    expect(stringField(health, 'status')).toBe('ok')
    const centerVersion = stringField(health, 'version')

    const suffix = randomUUID().slice(0, 8)
    const vpsA = await createVps(pageA, `Live A ${suffix}`, '198.51.100.31')
    const vpsB = await createVps(pageA, `Live B ${suffix}`, '198.51.100.32')
    const vpsC = await createVps(pageA, `Live C ${suffix}`, '198.51.100.33')
    const vpsAID = stringField(vpsA, 'vps_id')
    const vpsBID = stringField(vpsB, 'vps_id')
    const vpsCID = stringField(vpsC, 'vps_id')
    const vpsAName = stringField(vpsA, 'display_name')

    const subscription = await apiOK(pageA, 'POST', `/api/vps/${vpsAID}/subscriptions`, {
      price: 18.5,
      currency: 'USD',
      billing_cycle: 'monthly',
      billing_months: 1,
      billing_period_unit: 'month',
      billing_period_length: 1,
      started_at: utcDateOffset(-10),
      renew_at: utcDateOffset(90),
      auto_renew: true,
      auto_renew_cancelled: false,
      renewal_mode: 'auto',
      payment_method: 'card',
      note: 'local-live acceptance subscription',
    }, { 'Idempotency-Key': idempotencyKey('subscription') }, 201)
    const subscriptionID = stringField(subscription, 'subscription_id')
    const beforeIntentVPS = await apiOK(pageA, 'GET', `/api/vps/${vpsAID}`)
    const afterIntentVPS = await apiOK(pageA, 'PATCH', `/api/vps/${vpsAID}`, { renewal_decision: 'cancel' }, {
      'If-Match': `"${stringField(beforeIntentVPS, 'updated_at')}"`,
    })
    expect(stringField(afterIntentVPS, 'renewal_decision')).toBe('cancel')
    const intentSubscriptions = await apiOK<unknown[]>(pageA, 'GET', `/api/vps/${vpsAID}/subscriptions`)
    const intentSubscription = findById(intentSubscriptions, 'subscription_id', subscriptionID)
    expect(stringField(intentSubscription, 'status')).toBe('active')
    expect(intentSubscription.price).toBe(18.5)
    expect(intentSubscription.auto_renew).toBe(false)
    expect(intentSubscription.auto_renew_cancelled).toBe(true)
    expect(stringField(intentSubscription, 'renewal_mode')).toBe('auto_cancelled')

    const monitoringCreate = await apiOK(pageA, 'POST', `/api/vps/${vpsAID}/monitoring-instances`, {
      display_name: `Shared MI ${suffix}`,
      group: 'local-live',
      region: 'test-region',
      city: 'test-city',
      provider: 'local-live-provider',
      labels: ['edge'],
      note: 'shared acceptance monitoring instance',
      link_note: 'initial VPS association',
    }, { 'Idempotency-Key': idempotencyKey('monitoring-instance') }, 201)
    const monitoringInstanceID = stringField(monitoringCreate, 'monitoring_instance_id')
    const initialMI = await apiOK(pageA, 'GET', `/api/monitoring-instances/${monitoringInstanceID}`)
    expect(stringField(initialMI, 'lifecycle_status')).toBe('待接入')
    expect(stringField(initialMI, 'monitoring_status')).toBe('启用')
    await apiOK(pageA, 'POST', `/api/vps/${vpsBID}/link-monitoring-instance`, {
      monitoring_instance_id: monitoringInstanceID,
      note: 'shared with B for local-live acceptance',
    }, {}, 201)

    const target = await apiOK(pageA, 'POST', '/api/targets', {
      name: `Shared Target ${suffix}`,
      target_type: 'service',
      host: `shared-${suffix}.example.test`,
      execution_monitoring_instance_labels: ['edge'],
      run_status: '启用',
      group: 'local-live',
      labels: ['local-live'],
      note: 'shared target for lifecycle acceptance',
    }, {}, 201)
    const targetID = stringField(target, 'target_id')

    const assetsA = await createServiceAndDomain(pageA, vpsAID, targetID, suffix, 'a')
    const assetsB = await createServiceAndDomain(pageA, vpsBID, targetID, suffix, 'b')
    const unknownAssetsA = await createServiceAndDomain(pageA, vpsAID, targetID, suffix, 'a-unknown', 'unknown')
    const originalPreview = await apiOK(pageA, 'GET', `/api/vps/${vpsAID}/cancellation-preview`)
    const originalDigest = stringField(originalPreview, 'preview_digest')

    const assetsC = await createServiceAndDomain(pageB, vpsCID, targetID, suffix, 'c')
    const sharedReferences = [
      { object_type: 'monitoring_instance', object_id: monitoringInstanceID },
      { object_type: 'target', object_id: targetID },
    ]
    const staleAction = cancellationBody(originalDigest, subscriptionID, monitoringInstanceID, targetID, sharedReferences)
    const staleResult = await apiRequest(pageA, 'POST', `/api/vps/${vpsAID}/cancellation`, staleAction)
    expectStatus(staleResult, 409, 'stale cancellation')
    expect(errorCode(staleResult.body)).toBe('cancellation_preview_stale')

    await expectVpsState(pageA, vpsAID, 'active', 'cancel')
    await expectSubscriptionState(pageA, vpsAID, subscriptionID, 'active')
    await expectMonitoringState(pageA, monitoringInstanceID, '待接入', '启用')
    await expectTargetState(pageA, targetID, '启用')
    await expectDependenciesActive(pageA, [assetsA, assetsB, assetsC])

    const freshPreview = await apiOK(pageA, 'GET', `/api/vps/${vpsAID}/cancellation-preview`)
    const freshDigest = stringField(freshPreview, 'preview_digest')
    expect(freshDigest).not.toBe(originalDigest)
    const dependencyImpacts = arrayField(freshPreview, 'dependency_impacts').map(asObject)
    expect(dependencyImpacts.some((impact) => impact.relation_id === assetsC.serviceID || impact.relation_id === assetsC.domainID)).toBe(true)

    const applied = await apiOK(pageA, 'POST', `/api/vps/${vpsAID}/cancellation`, cancellationBody(
      freshDigest,
      subscriptionID,
      monitoringInstanceID,
      targetID,
      sharedReferences,
    ))
    const action = objectField(applied, 'action')
    expect(stringField(action, 'status')).toBe('completed')
    const steps = arrayField(applied, 'steps').map(asObject)
    for (const [objectType, objectID] of [
      ['vps', vpsAID],
      ['subscription', subscriptionID],
      ['monitoring_instance', monitoringInstanceID],
      ['target', targetID],
    ]) {
      expect(steps.some((step) => step.object_type === objectType && step.object_id === objectID && step.status === 'completed')).toBe(true)
    }

    await expectVpsState(pageA, vpsAID, 'cancelled', 'cancel')
    await expectSubscriptionState(pageA, vpsAID, subscriptionID, 'cancelled')
    await expectMonitoringState(pageA, monitoringInstanceID, '已退役', '暂停')
    await expectTargetState(pageA, targetID, '暂停')
    await expectVpsState(pageA, vpsBID, 'active', 'keep')
    await expectVpsState(pageA, vpsCID, 'active', 'keep')
    await expectDependenciesActive(pageA, [assetsA, assetsB, assetsC])

    const blockedArchive = await apiOK(pageA, 'GET', `/api/vps/${vpsAID}/archive-review`)
    expect(blockedArchive.eligible).toBe(false)
    const blockers = arrayField(blockedArchive, 'blocker_details').map(asObject)
    for (const objectID of [unknownAssetsA.serviceID, unknownAssetsA.domainID]) {
      expect(blockers.some((item) =>
        item.object_id === objectID &&
        item.code === 'dependency_needs_confirmation' &&
        item.current_state === 'unknown',
      )).toBe(true)
    }

    await pageA.goto(`/archive/${vpsAID}`)
    await pageA.getByRole('button', { name: '纠正服务状态' }).first().click()
    const correctionDialog = pageA.getByRole('dialog', { name: '纠正服务状态' })
    await expect(correctionDialog).toBeVisible()
    const correctionStatus = correctionDialog.getByLabel('状态')
    await expect(correctionStatus.locator('option', { hasText: '未确认' })).toHaveCount(0)
    await expect(correctionDialog.getByRole('button', { name: '确认纠正' })).toBeDisabled()
    await correctionStatus.selectOption('paused')
    await correctionDialog.getByLabel('原因').fill('accepted cancellation; correct the unknown service dependency status')
    await correctionDialog.getByRole('button', { name: '确认纠正' }).click()
    await expect(correctionDialog).toBeHidden()
    const servicesA = await apiOK<unknown[]>(pageA, 'GET', `/api/vps/${vpsAID}/services`)
    expect(stringField(findById(servicesA, 'service_id', unknownAssetsA.serviceID), 'status')).toBe('paused')
    const pausedDomain = await apiOK(pageA, 'PATCH', `/api/domains/${unknownAssetsA.domainID}/status`, {
      status: 'paused', reason: 'accepted cancellation; correct the unknown domain dependency status',
    })
    expect(stringField(pausedDomain, 'status')).toBe('paused')
    const readyArchive = await apiOK(pageA, 'GET', `/api/vps/${vpsAID}/archive-review`)
    expect(readyArchive.eligible).toBe(true)

    await apiOK(pageA, 'POST', `/api/vps/${vpsAID}/archive`, {
      confirmation_name: vpsAName,
      reason: 'unknown dependency statuses were reviewed and corrected; remaining blockers were resolved',
    })

    const archivedVPS = await apiOK(pageA, 'GET', `/api/vps/${vpsAID}`)
    expect(stringField(archivedVPS, 'lifecycle_status')).toBe('archived')
    const archivedSnapshot = objectField(archivedVPS, 'archived_state_snapshot')
    expect(stringField(archivedSnapshot, 'source')).toBe('archive')
    expect(stringField(archivedSnapshot, 'lifecycle_status')).toBe('cancelled')
    expect(stringField(archivedSnapshot, 'usage_status')).toBe('idle')

    await apiOK(pageA, 'POST', `/api/vps/${vpsAID}/restore-from-archive`, {
      reason: 'restore archived asset for explicit usage review and controlled monitoring recovery',
    })
    const restoredVPS = await apiOK(pageA, 'GET', `/api/vps/${vpsAID}`)
    expect(stringField(restoredVPS, 'lifecycle_status')).toBe('idle')
    expect(stringField(restoredVPS, 'usage_status')).toBe('unknown')
    expect(stringField(restoredVPS, 'renewal_decision')).toBe('cancel')
    expect(restoredVPS.archived_at).toBeNull()
    expect(objectField(restoredVPS, 'archived_state_snapshot')).toEqual(archivedSnapshot)
    const retainedLinks = arrayField(restoredVPS, 'monitoring_instance_links').map(asObject)
    expect(retainedLinks.some((link) => link.monitoring_instance_id === monitoringInstanceID)).toBe(true)
    await expectSubscriptionState(pageA, vpsAID, subscriptionID, 'cancelled')

    const retiredIssue = await apiRequest(pageA, 'POST', `/api/monitoring-instances/${monitoringInstanceID}/enrollment-token`)
    expectStatus(retiredIssue, 409, 'enrollment token while retired')
    expect(errorCode(retiredIssue.body)).toBe('monitoring_instance_retired')
    const retiredResume = await apiRequest(pageA, 'POST', `/api/monitoring-instances/${monitoringInstanceID}/runtime/resume`)
    expectStatus(retiredResume, 409, 'runtime resume while retired')

    const managementReview = await apiOK(pageA, 'GET', `/api/monitoring-instances/${monitoringInstanceID}/management-review`)
    const restoreMI = await apiOK(pageA, 'POST', `/api/monitoring-instances/${monitoringInstanceID}/lifecycle/restore`, {
      reason: 'controlled restore before fresh enrollment',
      preview_digest: stringField(managementReview, 'preview_digest'),
      confirm_shared_impact: true,
    })
    expect(stringField(restoreMI, 'lifecycle_status')).toBe('观察中')
    expect(stringField(restoreMI, 'monitoring_status')).toBe('暂停')

    const enrollmentIssue = await apiOK(pageA, 'POST', `/api/monitoring-instances/${monitoringInstanceID}/enrollment-token`)
    const enrollmentToken = stringField(enrollmentIssue, 'token')
    const fingerprint = `local-live-${suffix}-fingerprint`
    const enrollment = await apiOK(pageA, 'POST', '/api/agent/enroll', { token: enrollmentToken, fingerprint })
    expect(stringField(enrollment, 'status')).toBe('accepted')
    expect(stringField(enrollment, 'monitoring_instance_id')).toBe(monitoringInstanceID)
    expect(stringField(enrollment, 'binding_status')).toBe('已绑定')
    const syncToken = stringField(enrollment, 'sync_token')

    const resumedMI = await apiOK(pageA, 'POST', `/api/monitoring-instances/${monitoringInstanceID}/runtime/resume`)
    expect(stringField(resumedMI, 'monitoring_status')).toBe('启用')
    const observedAt = new Date(Date.now() - 1000).toISOString()
    const syncBatchID = `local-live-${suffix}-sync`
    const syncResponse = await apiOK(pageA, 'POST', '/api/agent/sync', {
      monitoring_instance_id: monitoringInstanceID,
      heartbeats: [{ observed_at: observedAt, agent_version: 'local-live-acceptance', fingerprint, sync_batch_id: syncBatchID }],
      host_samples: [{
        observed_at: observedAt,
        agent_version: 'local-live-acceptance',
        fingerprint,
        sync_batch_id: syncBatchID,
        cpu_usage_pct: 4.2,
        load_1: 0.1,
        load_5: 0.2,
        load_15: 0.3,
        mem_used_pct: 31.5,
        mem_available_bytes: 5_000_000_000,
        mem_total_bytes: 8_000_000_000,
        swap_used_pct: 0,
        disk_used_pct: 42.1,
        disk_total_bytes: 64_000_000_000,
        inode_used_pct: 20,
        net_in_bytes_per_sec: 0,
        net_out_bytes_per_sec: 0,
        cpu_iowait_pct: 0,
        cpu_steal_pct: 0,
        disk_read_bytes_per_sec: 0,
        disk_write_bytes_per_sec: 0,
        disk_busy_pct: 0,
        uptime_seconds: 86400,
      }],
    }, { Authorization: `Bearer ${syncToken}` })
    expect(stringField(syncResponse, 'status')).toBe('accepted')

    const runtimeFacts = await apiOK(pageA, 'GET', `/api/monitoring-instances/${monitoringInstanceID}/runtime-facts?window=24h`)
    const latestSample = objectField(runtimeFacts, 'latest_host_sample')
    expect(stringField(latestSample, 'sync_batch_id')).toBe(syncBatchID)
    expect(stringField(latestSample, 'fingerprint')).toBe(fingerprint)
    const onboarding = await apiOK(pageA, 'GET', `/api/monitoring-instances/${monitoringInstanceID}/onboarding`)
    expect(stringField(onboarding, 'phase')).toBe('接入完成')
    expect(onboarding.has_host_sample).toBe(true)
    expect(stringField(onboarding, 'binding_status')).toBe('已绑定')

    const events = await apiOK(pageA, 'GET', `/api/events?object_type=monitoring_instance&object_id=${encodeURIComponent(monitoringInstanceID)}&limit=100`)
    const eventTypes = arrayField(events, 'items').map(asObject).map((item) => item.event_type)
    for (const eventType of [
      'monitoring_instance_retired',
      'monitoring_instance_restored_to_observing',
      'monitoring_instance_monitoring_resumed',
    ]) {
      expect(eventTypes).toContain(eventType)
    }

    await testInfo.attach('local-live-evidence.json', {
      contentType: 'application/json',
      body: Buffer.from(JSON.stringify({
        center: baseURL,
        centerVersion,
        authenticatedContexts: 2,
        routes: ['/login', '/vps', '/api/healthz', '/api/vps/{id}/subscriptions', '/api/vps/{id}/services', '/api/vps/{id}/domains', '/api/services/{id}/status', '/api/domains/{id}/status', '/api/vps/{id}/cancellation-preview', '/api/vps/{id}/cancellation', '/api/vps/{id}/archive-review', '/api/vps/{id}/archive', '/api/vps/{id}/restore-from-archive', '/api/monitoring-instances/{id}/lifecycle/restore', '/api/agent/enroll', '/api/agent/sync', '/api/monitoring-instances/{id}/runtime-facts', '/api/monitoring-instances/{id}/onboarding', '/api/events'],
        vpsStates: { afterIntent: 'active/cancel with active billing', afterCancellation: 'cancelled', afterArchive: 'archived', afterRestore: 'idle/unknown/cancel' },
        stalePreview: { status: 409, code: errorCode(staleResult.body), changedDependency: assetsC.serviceID },
        sharedObjects: { monitoringInstanceID, targetID },
        restoredMI: { lifecycle: '观察中', monitoring: '启用', onboarding: '接入完成', syncBatchID },
        viewports: viewportEvidence,
        centerProcess: processOwnerEvidence(),
      }, null, 2)),
    })
  } finally {
    await Promise.all([contextA.close(), contextB.close()])
  }
})

function requiredEnv(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required`)
  return value
}

async function loginThroughUI(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL((url) => url.pathname === '/')
}

async function chooseTheme(page: Page, label: string, className: string): Promise<void> {
  await page.getByRole('button', { name: '切换主题' }).click()
  await page.getByRole('menuitemradio', { name: label }).click()
  await expect(page.locator('html')).toHaveClass(new RegExp(className))
}

async function captureViewport(
  page: Page,
  testInfo: TestInfo,
  label: string,
  evidence: Array<{ route: string; width: number; height: number; theme: string }>,
): Promise<void> {
  const viewport = page.viewportSize()
  if (!viewport) throw new Error('browser viewport is unavailable')
  const theme = await page.locator('html').getAttribute('class')
  const expectedTheme = theme?.split(/\s+/).find((name) => name.startsWith('theme-'))
  if (!expectedTheme) throw new Error('the page has no active Houfeng theme')
  await testInfo.attach(`local-live-${label}.png`, {
    contentType: 'image/png',
    body: await page.screenshot({ fullPage: true }),
  })
  evidence.push({ route: new URL(page.url()).pathname, width: viewport.width, height: viewport.height, theme: expectedTheme })
}

async function apiRequest(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<JsonRequestResult> {
  const requestBody = body === undefined ? null : JSON.stringify(body)
  const response = await page.evaluate(async ({ method, path, body, headers }) => {
    const requestHeaders = new Headers(headers)
    if (body !== null) requestHeaders.set('Content-Type', 'application/json')
    const init = {
      method,
      credentials: 'same-origin' as const,
      cache: 'no-store' as const,
      headers: requestHeaders,
    }
    const result = await fetch(path, body === null ? init : { ...init, body })
    let payload: unknown
    const text = await result.text()
    if (text !== '') {
      try {
        payload = JSON.parse(text) as unknown
      } catch {
        payload = undefined
      }
    }
    return { status: result.status, body: payload }
  }, { method, path, body: requestBody, headers })
  return response
}

async function apiOK<T = JsonObject>(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
  headers: Record<string, string> = {},
  status = 200,
): Promise<T> {
  const result = await apiRequest(page, method, path, body, headers)
  expectStatus(result, status, `${method} ${path}`)
  return result.body as T
}

function expectStatus(result: JsonRequestResult, expected: number, label: string): void {
  if (result.status === expected) return
  const code = errorCode(result.body)
  throw new Error(`${label}: expected HTTP ${expected}; got ${result.status}${code ? ` (${code})` : ''}`)
}

function errorCode(value: unknown): string | undefined {
  if (!isObject(value)) return undefined
  return typeof value.code === 'string' ? value.code : undefined
}

function isObject(value: unknown): value is JsonObject {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function asObject(value: unknown): JsonObject {
  if (!isObject(value)) throw new Error('expected a JSON object response')
  return value
}

function objectField(value: JsonObject, key: string): JsonObject {
  return asObject(value[key])
}

function arrayField(value: JsonObject, key: string): unknown[] {
  const items = value[key]
  if (!Array.isArray(items)) throw new Error(`expected ${key} to be a JSON array`)
  return items
}

function stringField(value: JsonObject, key: string): string {
  const field = value[key]
  if (typeof field !== 'string') throw new Error(`expected ${key} to be a string`)
  return field
}

function findById(values: unknown, key: string, id: string): JsonObject {
  if (!Array.isArray(values)) throw new Error('expected a list response')
  const item = values.map(asObject).find((value) => value[key] === id)
  if (!item) throw new Error(`could not read created ${key}`)
  return item
}

function idempotencyKey(scope: string): string {
  return `local-live-${scope}-${randomUUID()}`
}

function utcDateOffset(days: number): string {
  const date = new Date()
  date.setUTCDate(date.getUTCDate() + days)
  return date.toISOString().slice(0, 10)
}

async function createVps(page: Page, displayName: string, ipv4: string): Promise<JsonObject> {
  return apiOK(page, 'POST', '/api/vps', {
    display_name: displayName,
    provider_id: null,
    provider_name: 'local-live-provider',
    product_name: 'acceptance-fixture',
    order_ref: `local-live-${randomUUID()}`,
    country: 'US',
    region: 'test-region',
    city: 'test-city',
    datacenter: 'local-live',
    ipv4,
    ipv6: '',
    ssh_host: '127.0.0.1',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Linux',
    virtualization: 'KVM',
    lifecycle_status: 'active',
    usage_status: 'idle',
    renewal_decision: 'keep',
    importance: 'normal',
    labels: ['local-live'],
    note: 'created by local-live HTTP acceptance',
  }, {}, 201)
}

async function createServiceAndDomain(
  page: Page,
  vpsID: string,
  targetID: string,
  suffix: string,
  label: string,
  status: 'active' | 'unknown' = 'active',
): Promise<{ vpsID: string; serviceID: string; domainID: string }> {
  const service = await apiOK(page, 'POST', `/api/vps/${vpsID}/services`, {
    target_id: targetID,
    name: `Live service ${label.toUpperCase()} ${suffix}`,
    service_type: 'web',
    status,
    url: `https://service-${label}-${suffix}.example.test`,
    port: 443,
    labels: ['local-live'],
    note: `shared-target service ${label}`,
  }, { 'Idempotency-Key': idempotencyKey(`service-${label}`) }, 201)
  const serviceID = stringField(service, 'service_id')
  const domain = await apiOK(page, 'POST', `/api/vps/${vpsID}/domains`, {
    service_id: serviceID,
    target_id: targetID,
    domain_name: `live-${label}-${suffix}.example.test`,
    purpose: 'acceptance service domain',
    status,
    registrar: 'local-live',
    expires_at: null,
    auto_renew: false,
    https_enabled: true,
    labels: ['local-live'],
    note: `shared-target domain ${label}`,
  }, { 'Idempotency-Key': idempotencyKey(`domain-${label}`) }, 201)
  return { vpsID, serviceID, domainID: stringField(domain, 'domain_id') }
}

function cancellationBody(
  previewDigest: string,
  subscriptionID: string,
  monitoringInstanceID: string,
  targetID: string,
  confirmedSharedObjects: Array<{ object_type: string; object_id: string }>,
): JsonObject {
  return {
    reason: 'local-live acceptance: explicitly reviewed selected shared object impacts',
    effective_date: null,
    subscription_ids: [subscriptionID],
    vps_lifecycle_status: 'cancelled',
    monitoring_instance_actions: [{ monitoring_instance_id: monitoringInstanceID, lifecycle_status: '已退役', monitoring_status: '暂停' }],
    target_actions: [{ target_id: targetID, run_status: '暂停' }],
    preview_digest: previewDigest,
    confirmed_shared_objects: confirmedSharedObjects,
  }
}

async function expectVpsState(page: Page, vpsID: string, lifecycle: string, renewal: string): Promise<void> {
  const vps = await apiOK(page, 'GET', `/api/vps/${vpsID}`)
  expect(stringField(vps, 'lifecycle_status')).toBe(lifecycle)
  expect(stringField(vps, 'renewal_decision')).toBe(renewal)
}

async function expectSubscriptionState(page: Page, vpsID: string, subscriptionID: string, status: string): Promise<void> {
  const subscription = await apiOK(page, 'GET', `/api/subscriptions/${subscriptionID}`)
  expect(stringField(subscription, 'vps_id')).toBe(vpsID)
  expect(stringField(subscription, 'status')).toBe(status)
}

async function expectMonitoringState(page: Page, monitoringInstanceID: string, lifecycle: string, monitoring: string): Promise<void> {
  const mi = await apiOK(page, 'GET', `/api/monitoring-instances/${monitoringInstanceID}`)
  expect(stringField(mi, 'lifecycle_status')).toBe(lifecycle)
  expect(stringField(mi, 'monitoring_status')).toBe(monitoring)
}

async function expectTargetState(page: Page, targetID: string, status: string): Promise<void> {
  const target = await apiOK(page, 'GET', `/api/targets/${targetID}`)
  expect(stringField(target, 'run_status')).toBe(status)
}

async function expectDependenciesActive(
  page: Page,
  assets: Array<{ vpsID: string; serviceID: string; domainID: string }>,
): Promise<void> {
  for (const { vpsID, serviceID, domainID } of assets) {
    const services = await apiOK<unknown[]>(page, 'GET', `/api/vps/${vpsID}/services`)
    const domains = await apiOK<unknown[]>(page, 'GET', `/api/vps/${vpsID}/domains`)
    expect(stringField(findById(services, 'service_id', serviceID), 'status')).toBe('active')
    expect(stringField(findById(domains, 'domain_id', domainID), 'status')).toBe('active')
  }
}

function processOwnerEvidence(): { owner: string; stateAtTestCompletion: string } {
  const owner = process.env.LOCAL_LIVE_CENTER_OWNER ?? 'external'
  return {
    owner,
    stateAtTestCompletion: owner === 'launcher-managed' ? 'running; launcher terminates its owned process after the suite' : 'external process; left unchanged',
  }
}
