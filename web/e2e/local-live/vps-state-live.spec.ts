import { randomUUID } from 'node:crypto'
import { expect, test, type Page, type TestInfo } from '@playwright/test'

type JsonObject = Record<string, unknown>
type JsonResult = { status: number; body: unknown }

// This suite writes only to the explicitly selected disposable, loopback Center.
// Real Agent/session and 180-minute safety evidence belong to the matching-binary
// scripts/test-vps-lifecycle-live.sh harness; this browser suite does not fake sync.
test('fresh VPS ownership, shared associations, independent renewal, archive and explicit re-enrollment', async ({ browser }, testInfo) => {
  test.setTimeout(240_000)
  const contexts = await Promise.all([0, 1].map(() => browser.newContext({
    baseURL: testInfo.project.use.baseURL as string,
    viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai',
  })))
  const [pageA, pageB] = await Promise.all(contexts.map((context) => context.newPage()))
  if (!pageA || !pageB) throw new Error('two isolated browser sessions are required')
  const viewports: Array<{ route: string; width: number; height: number; theme: string }> = []
  try {
    for (const page of [pageA, pageB]) await login(page)
    const suffix = randomUUID().slice(0, 8)
    const a = await createVPS(pageA, 'Live A ' + suffix)
    const b = await createVPS(pageB, 'Live B ' + suffix)
    const aID = field(a, 'vps_id')
    const bID = field(b, 'vps_id')
    const aPath = '/api/vps/' + aID
    const bPath = '/api/vps/' + bID
    expect(a.usage_tags).toEqual(['自定义验收', '临时用途'])
    expect(a.validity_mode).toBe('unlimited')

    const subscription = await api(pageA, 'POST', aPath + '/subscriptions', {
      price: 18.5, currency: 'USD', billing_cycle: 'monthly', billing_months: 1,
      billing_period_unit: 'month', billing_period_length: 1,
      started_at: dateOffset(-10), renew_at: dateOffset(90), auto_renew: true,
      auto_renew_cancelled: false, renewal_mode: 'auto', payment_method: 'card',
      note: 'disposable browser acceptance',
    }, 201, { 'Idempotency-Key': randomUUID() })
    const beforeIntent = await api(pageA, 'GET', aPath)
    await api(pageA, 'PATCH', aPath, { renewal_decision: 'cancel', renewal_reason: '人工核对后不续费' }, 200, {
      'If-Match': '"' + field(beforeIntent, 'updated_at') + '"',
    })
    const billing = await api(pageA, 'GET', '/api/subscriptions/' + field(subscription, 'subscription_id'))
    expect(billing.status).toBe('active')
    expect(billing.auto_renew).toBe(true)
    expect(billing.auto_renew_cancelled).toBe(false)
    expect(billing.renewal_mode).toBe('auto')
    expect(billing.price).toBe(18.5)
    expect((await api(pageA, 'GET', aPath)).validity_mode).toBe('unlimited')

    const created = await api(pageA, 'POST', aPath + '/monitoring-instances', {
      display_name: 'Owned MI ' + suffix, labels: ['local-live'],
    }, 201, { 'Idempotency-Key': randomUUID() })
    const miID = field(created, 'monitoring_instance_id')
    const miPath = '/api/monitoring-instances/' + miID
    const pending = await api(pageA, 'GET', miPath)
    expect(pending.vps_id).toBe(aID)
    expect(pending.lifecycle_status).toBe('待接入')
    expect(pending.is_current).toBe(true)
    expect(pending.ever_connected).toBe(false)
    const duplicate = await request(pageA, 'POST', aPath + '/monitoring-instances', {
      display_name: 'Second owned MI ' + suffix, labels: ['local-live'],
    }, { 'Idempotency-Key': randomUUID() })
    expectStatus(duplicate, 409, 'second current monitoring instance')

    const target = await api(pageA, 'POST', '/api/targets', {
      name: 'Shared target ' + suffix, target_type: 'service', host: 'live-' + suffix + '.example.test',
      execution_monitoring_instance_labels: ['local-live'], run_status: '启用', labels: ['local-live'],
    }, 201)
    const targetID = field(target, 'target_id')
    const service = await api(pageA, 'POST', '/api/services', {
      vps_id: aID, name: 'Shared service ' + suffix, service_type: 'web', status: 'active', target_id: targetID,
    }, 201, { 'Idempotency-Key': randomUUID() })
    const domain = await api(pageA, 'POST', '/api/domains', {
      vps_id: aID, domain_name: 'live-' + suffix + '.example.test', status: 'active',
      registrar: 'local fixture', target_id: targetID,
    }, 201, { 'Idempotency-Key': randomUUID() })
    const previewBeforeSharing = await api(pageA, 'GET', aPath + '/archive-review')
    await api(pageB, 'POST', bPath + '/service-associations', {
      object_id: field(service, 'service_id'), address: '127.0.0.1', port: 8080, target_id: targetID,
    }, 201)
    await api(pageB, 'POST', bPath + '/domain-associations', {
      object_id: field(domain, 'domain_id'), target_id: targetID,
    }, 201)
    const stale = await request(pageA, 'POST', aPath + '/archive', {
      confirmation_name: field(a, 'display_name'), reason: 'stale shared association preview',
      preview_digest: field(previewBeforeSharing, 'preview_digest'), idempotency_key: randomUUID(),
      never_connected_confirmation: true,
    })
    expectStatus(stale, 409, 'archive after shared dependency change')
    expect(object(stale.body).code).toBe('archive_preview_stale')
    expect((await api(pageA, 'GET', aPath)).lifecycle_status).toBe('active')

    // Actual UI submission must present manual never-connected confirmation.
    await pageA.goto('/vps/' + aID)
    await expect(pageA.getByRole('heading', { name: field(a, 'display_name'), exact: true })).toBeVisible()
    await pageA.getByRole('button', { name: '管理', exact: true }).click()
    await pageA.getByRole('menuitem', { name: '结束使用并归档' }).click()
    const archiveDialog = pageA.getByRole('alertdialog', { name: '结束使用并归档' })
    await archiveDialog.getByLabel('归档原因', { exact: true }).fill('从未接入，人工核实结束使用')
    await archiveDialog.getByLabel('输入 VPS 名称确认归档').fill(field(a, 'display_name'))
    await expect(archiveDialog.getByRole('button', { name: '结束使用并归档', exact: true })).toBeDisabled()
    await archiveDialog.getByRole('checkbox', { name: '确认此 VPS 从未形成有效 Agent 会话，已人工核实结束使用。' }).check()
    const archiveResponse = pageA.waitForResponse((response) => response.url().endsWith(aPath + '/archive') && response.request().method() === 'POST')
    await archiveDialog.getByRole('button', { name: '结束使用并归档', exact: true }).click()
    expect((await archiveResponse).status()).toBe(200)
    await expect(archiveDialog).toBeHidden()
    expect((await api(pageA, 'GET', aPath)).lifecycle_status).toBe('archived')
    const retired = await api(pageA, 'GET', miPath)
    expect(retired.lifecycle_status).toBe('已退役')
    expect(retired.is_current).toBe(false)
    expect(retired.vps_id).toBe(aID)
    for (const kind of ['service', 'domain']) {
      expect(await api<unknown[]>(pageA, 'GET', aPath + '/' + kind + '-associations?current=true')).toEqual([])
      expect(await api<unknown[]>(pageB, 'GET', bPath + '/' + kind + '-associations?current=true')).toHaveLength(1)
      const history = await api<JsonObject[]>(pageA, 'GET', aPath + '/' + kind + '-associations')
      expect(history).toHaveLength(1)
      expect(history[0]?.ended_at).toBeTruthy()
    }
    expect((await api(pageA, 'GET', '/api/targets/' + targetID)).lifecycle_status).toBe('active')
    expect((await api(pageA, 'GET', '/api/targets/' + targetID)).run_status).toBe('启用')
    expect((await api(pageA, 'GET', '/api/subscriptions/' + field(subscription, 'subscription_id'))).auto_renew).toBe(true)

    await pageA.goto('/monitoring/' + miID)
    await expect(pageA.getByRole('heading', { name: field(pending, 'display_name'), exact: true })).toBeVisible()
    expect(await pageA.getByRole('menuitem', { name: '退役监控实例' }).count()).toBe(0)
    const repeatRetire = await request(pageA, 'POST', miPath + '/lifecycle/retire', {
      reason: 'a retired instance cannot retire again',
    }, { 'Idempotency-Key': randomUUID() })
    expectStatus(repeatRetire, 409, 'repeated retirement')

    await pageA.goto('/archive/' + aID)
    await expect(pageA.getByRole('button', { name: '恢复管理' })).toBeVisible()
    for (const [themeLabel, themeClass, themeName] of [
      ['氛围暗色', 'theme-houfeng-dark', 'dark'], ['精致亮色', 'theme-houfeng-light', 'light'],
    ] as const) {
      await pageA.getByRole('button', { name: '切换主题' }).click()
      await pageA.getByRole('menuitemradio', { name: themeLabel }).click()
      await expect(pageA.locator('html')).toHaveClass(new RegExp(themeClass))
      for (const width of [1440, 390]) {
        await pageA.setViewportSize({ width, height: 1000 })
        await capture(pageA, testInfo, themeName + '-' + width, viewports)
      }
    }
    await pageA.setViewportSize({ width: 1440, height: 1000 })
    await pageA.getByRole('button', { name: '恢复管理' }).click()
    const restoreDialog = pageA.getByRole('alertdialog', { name: '确认恢复归档 VPS' })
    await restoreDialog.getByLabel('恢复原因').fill('恢复台账并显式重新接入')
    await restoreDialog.getByRole('button', { name: '确认恢复', exact: true }).click()
    await expect(pageA).toHaveURL(new RegExp('/vps/' + aID))
    const restored = await api(pageA, 'GET', aPath)
    expect(restored.lifecycle_status).toBe('active')
    expect(restored.usage_tags).toEqual(['闲置'])
    expect(restored.renewal_decision).toBe('cancel')
    expect((await api(pageA, 'GET', miPath)).lifecycle_status).toBe('已退役')
    expect(await api<unknown[]>(pageA, 'GET', aPath + '/service-associations?current=true')).toEqual([])
    expect(await api<unknown[]>(pageA, 'GET', aPath + '/domain-associations?current=true')).toEqual([])
    expect(await api<unknown[]>(pageA, 'GET', aPath + '/monitoring-instances')).toEqual([])
    const history = await api<JsonObject[]>(pageA, 'GET', aPath + '/monitoring-instances?scope=retired')
    expect(history.some((item) => item.monitoring_instance_id === miID)).toBe(true)

    // Opening the drawer never mutates ownership or resumes a retired session.
    await pageA.goto('/monitoring/' + miID + '?onboarding=1&return_vps=' + aID)
    const onboarding = pageA.getByRole('dialog', { name: '监控实例接入抽屉' })
    await expect(onboarding).toBeVisible()
    expect((await api(pageA, 'GET', miPath)).lifecycle_status).toBe('已退役')
    await api(pageA, 'POST', miPath + '/binding/reset', {})
    const reenrolling = await api(pageA, 'GET', miPath)
    expect(reenrolling.monitoring_instance_id).toBe(miID)
    expect(reenrolling.vps_id).toBe(aID)
    expect(reenrolling.lifecycle_status).toBe('待接入')
    expect(reenrolling.is_current).toBe(true)
    expect((await api<JsonObject[]>(pageA, 'GET', bPath + '/monitoring-instances')).some((item) => item.monitoring_instance_id === miID)).toBe(false)

    await testInfo.attach('fresh-lifecycle-browser-evidence.json', {
      contentType: 'application/json',
      body: Buffer.from(JSON.stringify({
        center: testInfo.project.use.baseURL, authenticatedContexts: 2, vpsIDs: [aID, bID],
        monitoringInstanceID: miID, ownedMonitoring: true, sharedAssociationsPreserved: true,
        renewalIntentDidNotAlterBilling: true, staleArchiveRejected: true,
        neverConnectedManualArchive: true, restoredLifecycle: restored.lifecycle_status,
        restoredUsageTags: restored.usage_tags, explicitReenrollment: true, viewports,
        agentAndSafetyEvidence: 'Run scripts/test-vps-lifecycle-live.sh; this suite does not simulate Agent sync or elapse 180 minutes.',
      }, null, 2)),
    })
  } finally {
    await Promise.all(contexts.map((context) => context.close()))
  }
})

async function login(page: Page) {
  const username = process.env.LOCAL_LIVE_USERNAME
  const password = process.env.LOCAL_LIVE_PASSWORD
  if (!username || !password) throw new Error('LOCAL_LIVE_USERNAME and LOCAL_LIVE_PASSWORD are required')
  await page.goto('/login')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL((url) => url.pathname === '/')
}

async function capture(page: Page, info: TestInfo, label: string, evidence: Array<{ route: string; width: number; height: number; theme: string }>) {
  const viewport = page.viewportSize()
  const theme = (await page.locator('html').getAttribute('class'))?.split(/\s+/).find((name) => name.startsWith('theme-'))
  if (!viewport || !theme) throw new Error('viewport or selected theme unavailable')
  await expect(page.locator('body')).toBeVisible()
  await info.attach('local-live-' + label + '.png', { contentType: 'image/png', body: await page.screenshot({ fullPage: true, animations: 'disabled' }) })
  evidence.push({ route: new URL(page.url()).pathname, ...viewport, theme })
}

async function createVPS(page: Page, name: string) {
  return api(page, 'POST', '/api/vps', {
    display_name: name, provider_name: 'local-live', region: 'test-region', city: 'test-city',
    lifecycle_status: 'active', usage_tags: ['自定义验收', '临时用途'], validity_mode: 'unlimited',
    renewal_decision: 'keep', importance: 'normal', labels: ['local-live'], note: 'disposable browser acceptance',
  }, 201, { 'Idempotency-Key': randomUUID() })
}

async function request(page: Page, method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<JsonResult> {
  return page.evaluate(async ({ method, path, body, headers }) => {
    const requestHeaders = new Headers(headers)
    if (body !== undefined) requestHeaders.set('Content-Type', 'application/json')
    const response = await fetch(path, {
      method, credentials: 'same-origin', cache: 'no-store', headers: requestHeaders,
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    })
    const text = await response.text()
    return { status: response.status, body: text ? JSON.parse(text) as unknown : null }
  }, { method, path, body, headers })
}

async function api<T = JsonObject>(page: Page, method: string, path: string, body?: unknown, status = 200, headers: Record<string, string> = {}): Promise<T> {
  const result = await request(page, method, path, body, headers)
  expectStatus(result, status, method + ' ' + path)
  return result.body as T
}

function expectStatus(result: JsonResult, status: number, label: string) {
  if (result.status !== status) {
    const code = result.body && typeof result.body === 'object' && 'code' in result.body ? String(result.body.code) : ''
    throw new Error(label + ': expected HTTP ' + status + '; got ' + result.status + (code ? ' (' + code + ')' : ''))
  }
}
function object(value: unknown): JsonObject {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object response')
  return value as JsonObject
}
function field(value: JsonObject, key: string): string {
  const result = value[key]
  if (typeof result !== 'string') throw new Error('expected string field ' + key)
  return result
}
function dateOffset(days: number) {
  const date = new Date()
  date.setUTCDate(date.getUTCDate() + days)
  return date.toISOString().slice(0, 10)
}
