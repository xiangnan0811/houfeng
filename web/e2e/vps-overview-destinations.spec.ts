import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import type {
  CreateVPSMonitoringInstanceInput,
  CreateVPSMonitoringInstanceResponse,
  VPSAssetDetail,
  VPSOverview,
  VPSOverviewAnomaly,
} from '../src/lib/types'
import { vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { expect, test } from './fixtures'
import { apiRouteKey, type ApiFixtureProfile } from './fixtures/contracts'
import {
  coreRouteProfile,
  monitoringInstanceDetailProfile,
  subjectActivityFixture,
  vpsOverviewFixture,
  vpsOverviewProfile,
} from './fixtures/profiles'
import { expectMinTouchTarget, expectNoDocumentOverflow } from './support/geometry'

const READY = {
  state: 'ready' as const,
  observed_at: null,
  last_success_at: null,
  reason_code: '',
}

function anomaly(
  ruleId: VPSOverviewAnomaly['rule_id'],
  action: NonNullable<VPSOverviewAnomaly['primary_action']>,
  title = action.label,
): VPSOverviewAnomaly {
  return {
    rule_id: ruleId,
    severity: 'warning',
    title,
    source: 'e2e',
    primary_action: action,
    secondary_actions: [],
  }
}

function overviewWithAnomalies(anomalies: VPSOverviewAnomaly[]): VPSOverview {
  return vpsOverviewFixture({ anomalies })
}

const EXPECTED_MONITORING_CREATE_BODY = {
  display_name: 'Tokyo Edge',
  group: '',
  region: 'Kanto',
  city: 'Tokyo',
  provider: 'Example Cloud',
  labels: ['edge'],
  note: '',
  link_note: 'created from vps detail',
} satisfies CreateVPSMonitoringInstanceInput

const MONITORING_CREATE_BODY_KEYS = [
  'display_name',
  'group',
  'region',
  'city',
  'provider',
  'labels',
  'note',
  'link_note',
] as const satisfies readonly (keyof CreateVPSMonitoringInstanceInput)[]

// Mirrors the authoritative JSON enum values in internal/center/monitoringinstances/types.go.
const CREATED_MONITORING_INSTANCE = {
  monitoring_instance_id: 'mi_created',
  display_name: 'Tokyo Edge',
  group: '',
  region: 'Kanto',
  city: 'Tokyo',
  provider: 'Example Cloud',
  labels: ['edge'],
  note: '',
  lifecycle_status: '待接入',
  monitoring_status: '启用',
  binding_status: '未绑定',
  current_health_status: '正常',
  current_active_incident_count: 0,
  current_primary_issue_summary: '',
  created_at: '2026-08-29T00:00:00Z',
  updated_at: '2026-08-29T00:00:00Z',
  link: {
    link_id: 'link_created',
    vps_id: 'vps_001',
    monitoring_instance_id: 'mi_created',
    linked_at: '2026-08-29T00:00:00Z',
    unlinked_at: null,
    note: 'created from vps detail',
  },
} satisfies CreateVPSMonitoringInstanceResponse

function unlinkedVPSDetail(): VPSAssetDetail {
  return {
    ...vpsAssetFixture({ active_monitoring_instance_link_count: 0 }),
    monitoring_instance_links: [],
  }
}

function unlinkedVPSOverview(): VPSOverview {
  const base = vpsOverviewFixture()
  return vpsOverviewFixture({
    anomalies: [anomaly(
      'monitoring.unlinked.v1',
      { id: 'open_monitoring_instances', label: '创建并接入 agent' },
    )],
    summary: {
      ...base.summary,
      monitoring: { ...base.summary.monitoring, status: 'unlinked' },
    },
    relations: base.relations.map((relation) => (
      relation.kind === 'monitoring_instances'
        ? { ...relation, count: 0, status: 'unlinked' }
        : relation
    )),
  })
}

function firstMonitoringCreateProfile(): ApiFixtureProfile {
  return {
    ...monitoringInstanceDetailProfile('mi_created'),
    ...vpsOverviewProfile({
      overview: unlinkedVPSOverview(),
      detail: unlinkedVPSDetail(),
    }),
    [apiRouteKey('POST', '/api/vps/vps_001/monitoring-instances')]: {
      status: 201,
      body: CREATED_MONITORING_INSTANCE,
      expectedBodyKeys: MONITORING_CREATE_BODY_KEYS,
    },
  }
}

async function expectLocation(page: Page, expected: string) {
  await expect.poll(async () => page.evaluate(() => location.pathname + location.search)).toBe(expected)
}

function routeOwnerProfile(owner: 'monitoring' | 'events' | 'ip-quality'): ApiFixtureProfile {
  if (owner === 'monitoring') return monitoringInstanceDetailProfile('mi_001')
  if (owner === 'events') {
    return {
      ...coreRouteProfile('/events'),
      [apiRouteKey('GET', '/api/events?object_type=monitoring_instance&object_id=mi_001&limit=200')]: {
        status: 200,
        body: { items: [] },
      },
    }
  }
  return {
    [apiRouteKey('GET', '/api/vps/vps_001/ip-quality')]: {
      status: 404,
      body: { error: 'no report', code: 'resource_not_found' },
    },
  }
}

const ROUTE_ACTIONS = [
  {
    name: 'monitoring',
    owner: 'monitoring' as const,
    expected: '/monitoring/mi_001',
    action: anomaly(
      'monitoring.health.abnormal.v1',
      { id: 'open_monitoring', label: '查看监控', route: '/monitoring/mi_001' },
    ),
  },
  {
    name: 'incidents',
    owner: 'events' as const,
    expected: '/events?object_type=monitoring_instance&object_id=mi_001',
    action: anomaly(
      'monitoring.incidents.open.v1',
      { id: 'open_incidents', label: '查看事件', route: '/events?object_type=monitoring_instance&object_id=mi_001' },
    ),
  },
  {
    name: 'elevated IP quality',
    owner: 'ip-quality' as const,
    expected: '/vps/vps_001/ip-quality',
    action: anomaly(
      'ip_quality.risk.elevated.v1',
      { id: 'open_ip_quality', label: '查看 IP 质量', route: '/vps/vps_001/ip-quality' },
    ),
  },
  {
    name: 'stale IP quality',
    owner: 'ip-quality' as const,
    expected: '/vps/vps_001/ip-quality',
    action: anomaly(
      'ip_quality.stale.v1',
      { id: 'open_ip_quality', label: '查看 IP 质量', route: '/vps/vps_001/ip-quality' },
    ),
  },
  {
    name: 'partial IP quality',
    owner: 'ip-quality' as const,
    expected: '/vps/vps_001/ip-quality',
    action: anomaly(
      'ip_quality.partial.v1',
      { id: 'open_ip_quality', label: '查看 IP 质量', route: '/vps/vps_001/ip-quality' },
    ),
  },
] as const

for (const contract of ROUTE_ACTIONS) {
  test(`VPS overview ${contract.name} route action reaches its exact registered owner`, async ({ api, page }) => {
    api.useProfile({
      ...vpsOverviewProfile({ overview: overviewWithAnomalies([contract.action]) }),
      ...routeOwnerProfile(contract.owner),
    })
    if (contract.owner === 'monitoring') {
      await api.allowRuntimeStream('mi_001')
    }
    await page.goto('/vps/vps_001')

    await page.getByRole('link', { name: contract.action.primary_action!.label, exact: true }).click()

    await expectLocation(page, contract.expected)
    if (contract.owner === 'monitoring') {
      await expect(page.getByRole('heading', { name: 'Tokyo Monitor' })).toBeVisible()
      expect(api.requestCount('GET', '/api/monitoring-instances/mi_001')).toBeGreaterThan(0)
      expect(api.requestCount('GET', '/api/monitoring-instances/mi_001/runtime-facts?window=24h')).toBeGreaterThan(0)
      await expect(page.getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true')
    }
    if (contract.owner === 'events') {
      await expect(page.getByRole('heading', { name: '事件流' })).toBeVisible()
      expect(api.requestCount(
        'GET',
        '/api/events?object_type=monitoring_instance&object_id=mi_001&limit=200',
      )).toBe(1)
    }
    if (contract.owner === 'ip-quality') {
      await expect(page.getByRole('heading', { name: 'IP 质量报告加载失败' })).toBeVisible()
      await expect.poll(() => api.requestCount('GET', '/api/vps/vps_001/ip-quality')).toBe(1)
    }
  })
}

const PANEL_COMMANDS = [
  {
    name: 'subscription',
    action: anomaly(
      'renewal.subscription.missing.v1',
      { id: 'open_subscription', label: '管理订阅' },
    ),
    dialog: '订阅事实',
  },
  {
    name: 'renewal decision',
    action: anomaly(
      'renewal.due.soon.v1',
      { id: 'open_renewal_decision', label: '查看续费' },
    ),
    dialog: '续费决策',
  },
] as const

for (const contract of PANEL_COMMANDS) {
  test(`VPS overview ${contract.name} command opens the page-owned dialog`, async ({ api, page }) => {
    api.useProfile(vpsOverviewProfile({ overview: overviewWithAnomalies([contract.action]) }))
    await page.goto('/vps/vps_001')

    await page.getByRole('button', { name: contract.action.primary_action!.label }).click()
    await expect(page.getByRole('dialog', { name: contract.dialog })).toBeVisible()
    expect(api.requestCount('GET', '/api/vps/vps_001')).toBe(1)
    await expectLocation(page, '/vps/vps_001')
  })
}

test('VPS overview creates the first monitoring instance and reaches its onboarding owner', async ({
  api,
  page,
}) => {
  api.useProfile(firstMonitoringCreateProfile())
  await api.allowRuntimeStream('mi_created')
  await page.goto('/vps/vps_001')

  await page.getByRole('button', { name: '创建并接入 agent' }).click()
  const dialog = page.getByRole('dialog', { name: '接入/升级 agent' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: '监控实例名称' })).toHaveValue('Tokyo Edge')

  const createRequestPromise = page.waitForRequest((request) => (
    request.method() === 'POST'
    && new URL(request.url()).pathname === '/api/vps/vps_001/monitoring-instances'
  ))
  const onboardingNavigation = page.waitForURL((url) => (
    url.pathname === '/monitoring/mi_created'
    && url.search === '?onboarding=1&return_vps=vps_001'
  ))
  await dialog.getByRole('button', { name: '接入/升级 agent' }).click()

  const createRequest = await createRequestPromise
  expect(createRequest.postDataJSON()).toEqual(EXPECTED_MONITORING_CREATE_BODY)
  expect(createRequest.headers()['idempotency-key']).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i,
  )
  await onboardingNavigation
  await expect(page.getByRole('heading', { name: 'Tokyo Monitor', exact: true, level: 1 })).toBeVisible()
  await expect(page.getByRole('dialog', { name: '监控实例接入抽屉' })).toBeVisible()
  await api.assertRuntimeStreamConnected('mi_created')
  expect(api.requestCount('GET', '/api/vps/vps_001/overview')).toBe(2)
  expect(api.requestCount('POST', '/api/vps/vps_001/monitoring-instances')).toBe(1)
})

test('Monitoring first-run entry selects an unlinked VPS and consumes the route workbench', async ({
  api,
  page,
}) => {
  const linkedVPS = vpsAssetFixture({
    vps_id: 'vps_linked',
    display_name: 'Linked Osaka',
    active_monitoring_instance_link_count: 1,
  })
  api.useProfile({
    ...coreRouteProfile('/monitoring'),
    ...coreRouteProfile('/vps'),
    ...vpsOverviewProfile({
      overview: unlinkedVPSOverview(),
      detail: unlinkedVPSDetail(),
    }),
    [apiRouteKey('GET', '/api/vps')]: {
      status: 200,
      body: [unlinkedVPSDetail(), linkedVPS],
    },
  })
  await page.goto('/monitoring')

  await page.getByRole('button', { name: '选择未关联 VPS' }).click()
  await expectLocation(page, '/vps?view=unlinked')
  const unlinkedVPS = page.getByRole('button', { name: '选择 Tokyo Edge', exact: true })
  await expect(unlinkedVPS).toBeVisible()
  await expect(page.getByRole('button', { name: '选择 Linked Osaka', exact: true })).toHaveCount(0)
  await unlinkedVPS.click()

  const workbenchNavigation = page.waitForURL((url) => (
    url.pathname === '/vps/vps_001' && url.search === '?workbench=monitoring'
  ))
  await page.getByRole('link', { name: '打开 VPS 详情', exact: true }).click()
  await workbenchNavigation

  const dialog = page.getByRole('dialog', { name: '接入/升级 agent' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: '监控实例名称' })).toHaveValue('Tokyo Edge')
  await expectLocation(page, '/vps/vps_001')
  expect(api.requestCount('POST', '/api/vps/vps_001/monitoring-instances')).toBe(0)
})

test('VPS overview onboarding dialog is operable, mobile-safe, and accessible at 390px', async ({
  api,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  api.useProfile(vpsOverviewProfile({
    overview: unlinkedVPSOverview(),
    detail: unlinkedVPSDetail(),
  }))
  await page.goto('/vps/vps_001')

  await page.getByRole('button', { name: '创建并接入 agent' }).click()
  const dialog = page.getByRole('dialog', { name: '接入/升级 agent' })
  await expect(dialog).toBeVisible()
  const name = dialog.getByRole('textbox', { name: '监控实例名称' })
  await name.fill('Tokyo Edge Mobile')
  await expect(name).toHaveValue('Tokyo Edge Mobile')
  const submit = dialog.getByRole('button', { name: '接入/升级 agent' })
  await submit.scrollIntoViewIfNeeded()
  await expect(submit).toBeVisible()
  await submit.focus()
  await expect(submit).toBeFocused()
  await expectNoDocumentOverflow(page)

  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      targets: violation.nodes.map((node) => node.target),
    }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  expect(api.requestCount('POST', '/api/vps/vps_001/monitoring-instances')).toBe(0)
})

test('VPS overview management menu exits on native Tab from a menuitem', async ({ api, page }) => {
  api.useProfile(vpsOverviewProfile())
  await page.goto('/vps/vps_001')

  const trigger = page.getByRole('button', { name: '管理', exact: true })
  await trigger.click()
  const menu = page.getByRole('menu', { name: '管理' })
  const firstItem = menu.getByRole('menuitem').first()
  await expect(menu).toBeVisible()
  await expect(firstItem).toBeFocused()

  await page.keyboard.press('Tab')
  await expect(menu).toHaveCount(0)
  await expect(trigger).not.toBeFocused()
  await expect(page.locator('body')).not.toBeFocused()
})

test('VPS overview management and retry commands stay on the canonical page', async ({ api, page }) => {
  const overview = overviewWithAnomalies([
    anomaly('lifecycle.blocker.v1', { id: 'open_management', label: '打开管理' }),
    anomaly('source.unavailable.v1', { id: 'retry_overview', label: '重试概览' }),
  ])
  api.useProfile(vpsOverviewProfile({ overview }))
  await page.goto('/vps/vps_001')

  await page.getByRole('button', { name: '打开管理' }).click()
  await expect(page.getByRole('menu', { name: '管理' })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '重试概览' }).click()
  await expect.poll(() => api.requestCount('GET', '/api/vps/vps_001/overview')).toBe(2)
  await expectLocation(page, '/vps/vps_001')
})

test('VPS overview subscription relation reaches the exact filtered subscription owner', async ({ api, page }) => {
  api.useProfile({
    ...coreRouteProfile('/subscriptions'),
    ...vpsOverviewProfile(),
    [apiRouteKey('GET', '/api/subscriptions?vps_id=vps_001')]: { status: 200, body: [] },
  })
  await page.goto('/vps/vps_001')

  await page.getByRole('region', { name: '订阅与续费', exact: true }).getByRole('link', { name: '查看订阅列表', exact: true }).click()

  await expectLocation(page, '/subscriptions?vps_id=vps_001&view=details')
  await expect(page.getByRole('button', { name: '新建订阅', exact: true })).toBeVisible()
  await expect.poll(() => api.requestCount('GET', '/api/subscriptions?vps_id=vps_001')).toBe(1)
})

const RELATION_PANELS = [
  {
    label: '监控实例',
    trigger: '查看实例',
    dialog: '已关联监控实例',
    content: 'Tokyo Monitor',
    apiPath: '/api/vps/vps_001/monitoring-instances',
    offered: ['接入/升级 agent', '解除关联'] as const,
    entry: {
      action: '解除关联',
      dialog: '确认解除监控实例关联',
      role: 'alertdialog' as const,
      writePath: '/api/vps/vps_001/unlink-monitoring-instance',
    },
  },
  {
    label: '服务',
    trigger: '查看服务',
    dialog: '已关联服务',
    content: 'Overview Gateway',
    apiPath: '/api/vps/vps_001/services',
    offered: ['新增服务'] as const,
    entry: {
      action: '新增服务',
      dialog: '新增服务',
      role: 'dialog' as const,
      field: '服务名称',
      writePath: '/api/vps/vps_001/services',
    },
  },
  {
    label: '域名',
    trigger: '查看域名',
    dialog: '已关联域名',
    content: 'edge.example.com',
    apiPath: '/api/vps/vps_001/domains',
    offered: ['新增域名'] as const,
    entry: {
      action: '新增域名',
      dialog: '新增域名',
      role: 'dialog' as const,
      field: '域名',
      writePath: '/api/vps/vps_001/domains',
    },
  },
] as const

for (const contract of RELATION_PANELS) {
  test(`VPS overview ${contract.label} relation opens scoped management entry`, async ({ api, page }) => {
    api.useProfile({
      ...vpsOverviewProfile(),
      [apiRouteKey('GET', '/api/targets')]: { status: 200, body: [] },
    })
    await page.goto('/vps/vps_001')

    await page.getByRole('button', { name: contract.trigger, exact: true }).click()
    const dialog = page.getByRole('dialog', { name: contract.dialog, exact: true })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText(contract.content, { exact: true })).toBeVisible()
    expect(api.requestCount('GET', contract.apiPath)).toBeGreaterThan(0)

    for (const action of contract.offered) {
      await expect(dialog.getByRole('button', { name: action, exact: true })).toBeVisible()
    }

    await dialog.getByRole('button', { name: contract.entry.action, exact: true }).click()
    const entry = page.getByRole(contract.entry.role, { name: contract.entry.dialog, exact: true })
    await expect(entry).toBeVisible()
    if ('field' in contract.entry) {
      await expect(entry.getByRole('textbox', { name: contract.entry.field, exact: true })).toBeVisible()
    }
    await expectLocation(page, '/vps/vps_001')
    expect(api.requestCount('POST', contract.entry.writePath)).toBe(0)
  })
}

test('VPS overview resource row view affordance opens details and restores keyboard focus', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  api.useProfile(vpsOverviewProfile())
  await page.goto('/vps/vps_001')
  const trigger = page.getByRole('button', { name: '查看关联服务：Overview Gateway', exact: true })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: '已关联服务', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('Overview Gateway', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '新增服务', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(dialog).toBeVisible()
})

test('VPS overview fails closed for malicious and mismatched destinations', async ({ api, page }) => {
  const malicious = [
    anomaly('monitoring.health.abnormal.v1', {
      id: 'open_monitoring', label: 'same-origin mismatch', route: '/vps/vps_001',
    }),
    anomaly('monitoring.incidents.open.v1', {
      id: 'open_incidents', label: 'external route', route: 'https://evil.invalid',
    }),
    anomaly('ip_quality.risk.elevated.v1', {
      id: 'open_ip_quality', label: 'protocol-relative route', route: '//evil.invalid/path',
    }),
    anomaly('ip_quality.stale.v1', {
      id: 'open_ip_quality', label: 'backslash route', route: '\\evil.invalid\\path',
    }),
    anomaly('renewal.subscription.missing.v1', {
      id: 'open_subscription', label: 'command with route', route: '/subscriptions?vps_id=vps_001&view=details',
    }),
  ]
  const relations = [
    {
      kind: 'monitoring_instances', count: 1, label: '监控实例', section: READY,
    },
    {
      kind: 'subscriptions', count: 1, label: 'mismatched subscription', route: '/subscriptions', section: READY,
    },
    {
      kind: 'services', count: 1, label: '服务', section: READY,
    },
    {
      kind: 'domains', count: 1, label: '域名', section: READY,
    },
  ] satisfies VPSOverview['relations']
  api.useProfile(vpsOverviewProfile({ overview: vpsOverviewFixture({ anomalies: malicious, relations }) }))
  await page.goto('/vps/vps_001')

  const anomalyLabels = [
    'same-origin mismatch',
    'external route',
    'protocol-relative route',
    'backslash route',
    'command with route',
  ]
  for (const label of anomalyLabels) {
    const text = page.locator('.vps-overview-anomalies__actions').getByText(label, { exact: true })
    await expect(text).toBeVisible()
    expect(await text.evaluate((element) => element.closest('a,button') === null)).toBe(true)
  }
  await expect(page.getByRole('region', { name: '订阅与续费', exact: true }).getByRole('link', { name: '查看订阅列表', exact: true })).toHaveAttribute('href', '/subscriptions?vps_id=vps_001&view=details')

  await expectLocation(page, '/vps/vps_001')
})

test('VPS overview relation dialogs support keyboard navigation and focus return at 390px', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  api.useProfile(vpsOverviewProfile())
  await page.goto('/vps/vps_001')
  await expect.poll(() => page.locator('#vps-section-relations').evaluate(element => (
    element.scrollWidth <= element.clientWidth + 1
  ))).toBe(true)

  const main = page.locator('main#main-content')
  await main.evaluate((element) => {
    element.scrollTop = Math.min(1200, Math.max(80, element.scrollHeight - element.clientHeight))
  })

  const trigger = page.getByRole('button', { name: '查看实例', exact: true })
  await trigger.scrollIntoViewIfNeeded()
  await trigger.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('dialog', { name: '已关联监控实例', exact: true })
  await expect(dialog).toBeVisible()
  await expectNoDocumentOverflow(page)

  const backgroundScroll = await main.evaluate((element) => element.scrollTop)
  expect(backgroundScroll).toBeGreaterThan(0)
  const hit = dialog.locator('.badge').first()
  await expect(hit).toBeVisible()
  const box = await hit.boundingBox()
  expect(box).toBeTruthy()
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2)
  await page.mouse.wheel(0, 650)
  await expect.poll(() => main.evaluate((element) => element.scrollTop)).toBe(backgroundScroll)



  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])

  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()

  for (const kind of ['服务', '域名']) {
    const resourceTrigger = page.getByRole('button', { name: `查看${kind}`, exact: true })
    await resourceTrigger.scrollIntoViewIfNeeded()
    await resourceTrigger.focus()
    await resourceTrigger.press('Enter')
    const resourceDialog = page.getByRole('dialog')
    await expect(resourceDialog.getByRole('listitem').first()).toBeVisible()
    const controls = resourceDialog.locator(':is(button:not([disabled]),a[href],[tabindex="0"]):visible')
    await expect(controls.first()).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(controls.last()).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(controls.first()).toBeFocused()
    for (let index = 0; index < await controls.count(); index += 1) {
      await page.keyboard.press('Tab')
      expect(await resourceDialog.evaluate(element => element.contains(document.activeElement))).toBe(true)
    }
    await expectNoDocumentOverflow(page)
    const resourceAudit = await new AxeBuilder({ page }).analyze()
    expect(resourceAudit.violations.filter(violation => (
      violation.impact === 'serious' || violation.impact === 'critical'
    )).map(violation => violation.id)).toEqual([])
    await page.keyboard.press('Escape')
    await expect(resourceDialog).toHaveCount(0)
    await expect(resourceTrigger).toBeFocused()
  }
})

test('runtime stream fixture rejects foreign-origin sockets even with an allowlisted id', async ({ api, page }) => {
  api.useProfile(vpsOverviewProfile())
  await api.allowRuntimeStream('mi_001')
  await page.goto('/vps/vps_001')

  const foreignURL = 'ws://evil.invalid/api/monitoring-instances/mi_001/runtime-stream'
  await page.evaluate((url) => {
    const socket = new WebSocket(url)
    socket.addEventListener('error', () => undefined)
  }, foreignURL)
  api.acknowledgeUnexpectedRuntimeStream('evil.invalid')

  await expect.poll(async () => {
    const sockets = await api.snapshotRuntimeStreamSockets()
    return sockets.find((socket) => socket.url.includes('evil.invalid'))?.phase ?? ''
  }).toBe('error')
})

test('runtime stream fixture rejects raw path traversal before URL normalization', async ({ api, page }) => {
  api.useProfile(vpsOverviewProfile())
  await api.allowRuntimeStream('mi_001')
  await page.goto('/vps/vps_001')

  const traversalURL = await page.evaluate(() => {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${protocol}//${location.host}/api/monitoring-instances/mi_001/child/../runtime-stream`
  })
  await page.evaluate((url) => {
    const socket = new WebSocket(url)
    socket.addEventListener('error', () => undefined)
  }, traversalURL)
  api.acknowledgeUnexpectedRuntimeStream('/child/../runtime-stream')

  await expect.poll(async () => {
    const sockets = await api.snapshotRuntimeStreamSockets()
    return sockets.find((socket) => socket.url.includes('/child/../runtime-stream'))?.phase ?? ''
  }).toBe('error')
})

for (const workspace of ['workbench', 'ledger'] as const) {
  test('30-asset ' + workspace + ' inventory opens one full detail and restores its context', async ({ api, page }) => {
    const rows = Array.from({ length: 30 }, (_, index) => vpsAssetFixture({
      vps_id: 'vps_' + String(index + 1).padStart(3, '0'),
      display_name: 'Tokyo Edge ' + (index + 1),
    }))
    const selected = rows[29]!
    const overview = vpsOverviewFixture()
    overview.identity = { ...overview.identity, vps_id: selected.vps_id, display_name: selected.display_name }
    overview.anomalies = [anomaly(
      'ip_quality.risk.elevated.v1',
      { id: 'open_ip_quality', label: '查看 IP 质量', route: `/vps/${selected.vps_id}/ip-quality` },
    )]
    api.useProfile({
      ...coreRouteProfile('/vps'),
      [apiRouteKey('GET', '/api/vps')]: { status: 200, body: rows },
      [apiRouteKey('GET', '/api/vps/' + selected.vps_id + '/overview')]: { status: 200, body: overview },
      [apiRouteKey('GET', `/api/subscriptions?vps_id=${selected.vps_id}&sort=renew_at&order=asc`)]: { status: 200, body: [] },
      [apiRouteKey('GET', `/api/vps/${selected.vps_id}/services`)]: { status: 200, body: [] },
      [apiRouteKey('GET', `/api/vps/${selected.vps_id}/domains`)]: { status: 200, body: [] },
      [apiRouteKey('GET', `/api/vps/${selected.vps_id}/ip-quality`)]: {
        status: 404, body: { error: 'no report', code: 'resource_not_found' },
      },
      [apiRouteKey('GET', `/api/subjects/vps/${selected.vps_id}/activity`)]: {
        status: 200,
        body: subjectActivityFixture({
          subject: { kind: 'vps', source_id: selected.vps_id, identity: { display_name: selected.display_name }, live_route: `/vps/${selected.vps_id}`, status: 'live' },
        }),
      },
    })
    await page.setViewportSize({ width: 1440, height: 1000 })
    await page.goto('/vps?workspace=' + workspace + '&q=Tokyo&source=inventory-flow')
    const pick = page.getByRole('button', { name: '选择 ' + selected.display_name, exact: true })
    await pick.scrollIntoViewIfNeeded()
    await pick.focus()
    await pick.press('Enter')
    await expect(pick).toHaveAttribute('aria-pressed', 'true')
    if (workspace === 'workbench') {
      const quick = page.getByRole('region', { name: 'VPS 快速查看' })
      await expect(quick).toBeVisible()
      expect(await quick.evaluate(panel => panel.closest('tr')?.previousElementSibling?.querySelector('[aria-expanded="true"]')?.getAttribute('aria-label'))).toBe('选择 ' + selected.display_name)
      await pick.press('Enter')
      await expect(pick).toHaveAttribute('aria-expanded', 'false')
      await expect(quick).toHaveCount(0)
      await expect(pick).toHaveAttribute('aria-pressed', 'true')
      await pick.focus()
      await pick.press('Enter')
      await expect(quick).toBeVisible()
    }
    await page.setViewportSize({ width: 390, height: 844 })
    await page.locator('main#main-content').evaluate(main => { main.scrollTop = 56 })
    expect(await page.locator('main#main-content').evaluate(main => main.scrollTop)).toBeGreaterThan(0)
    await page.getByRole('link', { name: '打开 VPS 详情', exact: true }).click()
    await expect(page).toHaveURL(url => url.pathname === '/vps/' + selected.vps_id)
    await expect(page.getByRole('heading', { name: selected.display_name, exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: '返回 VPS 列表', exact: true })).toBeVisible()
    await expect.poll(() => page.getByRole('link', { name: '返回 VPS 列表', exact: true }).evaluate(link => {
      const rect = link.getBoundingClientRect()
      const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)
      return rect.top >= 0 && rect.bottom <= innerHeight && rect.left >= 0 && rect.right <= innerWidth && !!hit && link.contains(hit)
    })).toBe(true)
    const manage = page.getByRole('button', { name: '管理', exact: true })
    await manage.click()
    await expect(page.getByRole('menu')).toBeVisible()
    await manage.click()
    await expect(page.getByRole('menu')).toHaveCount(0)
    const returnLink = page.getByRole('link', { name: '返回 VPS 列表', exact: true })
    await expectMinTouchTarget(returnLink)
    const inventoryHref = await returnLink.getAttribute('href')
    const toc = page.getByRole('navigation', { name: '当前页分区' })
    await toc.getByText('页面目录').click()
    await toc.getByRole('link', { name: '运行观测' }).click()

    await expect(page).toHaveURL(url => url.hash === '#vps-section-monitoring')
    await expect(returnLink).toHaveAttribute('href', inventoryHref!)

    await expect.poll(() => page.evaluate(() => {
      const target = document.getElementById('vps-section-monitoring')!.getBoundingClientRect()
      return target.top >= document.querySelector('.topbar')!.getBoundingClientRect().bottom && target.top < innerHeight
    })).toBe(true)
    await page.goBack()
    await expect(page).toHaveURL(url => url.pathname === '/vps/' + selected.vps_id && url.hash === '')
    await expect(returnLink).toHaveAttribute('href', inventoryHref!)
    await page.getByRole('navigation', { name: '主体局部导航' }).getByRole('link', { name: '活动', exact: true }).click()
    await expect(page.getByRole('link', { name: '返回 VPS 列表', exact: true })).toHaveAttribute('href', inventoryHref!)
    await page.getByRole('link', { name: '返回详情', exact: true }).click()
    await expect(page).toHaveURL(url => url.pathname === `/vps/${selected.vps_id}`)
    await expect(returnLink).toHaveAttribute('href', inventoryHref!)
    await page.locator('#vps-section-activity').getByRole('link', { name: '查看全部', exact: true }).click()
    await expect(page).toHaveURL(url => url.pathname === `/vps/${selected.vps_id}/activity`)
    await expect(returnLink).toHaveAttribute('href', inventoryHref!)
    await page.getByRole('link', { name: '返回详情', exact: true }).click()
    await expect(page).toHaveURL(url => url.pathname === `/vps/${selected.vps_id}`)
    for (const reportEntry of [
      page.locator('#vps-section-monitoring a[href$="/ip-quality"]'),
      page.getByRole('link', { name: '查看 IP 质量', exact: true }),
    ]) {
      await reportEntry.click()
      await expect(page).toHaveURL(url => url.pathname === `/vps/${selected.vps_id}/ip-quality`)
      await expect(returnLink).toHaveAttribute('href', inventoryHref!)
      await page.getByRole('link', { name: '返回 VPS 详情', exact: true }).click()
      await expect(page).toHaveURL(url => url.pathname === `/vps/${selected.vps_id}`)
      await expect(returnLink).toHaveAttribute('href', inventoryHref!)
    }
    await page.getByRole('link', { name: '返回 VPS 列表', exact: true }).click()
    await expect(page).toHaveURL(url => url.pathname === '/vps' && url.searchParams.get('workspace') === workspace && url.searchParams.get('q') === 'Tokyo' && url.searchParams.get('selected') === selected.vps_id && url.searchParams.get('source') === 'inventory-flow')
    await expect(pick).toHaveAttribute('aria-pressed', 'true')
    if (workspace === 'workbench') {
      await expect(pick).toHaveAttribute('aria-expanded', 'false')
      await page.getByRole('button', { name: '选择 ' + rows[0]!.display_name, exact: true }).click()
      await expect(page.getByRole('region', { name: 'VPS 快速查看' }).getByRole('link', { name: '打开 VPS 详情', exact: true })).toBeInViewport({ ratio: 1 })
    }
  })
}

