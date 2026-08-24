import type {
  DashboardOverview,
  ProviderRecord,
  SubscriptionOverview,
  VPSAssetRecord,
} from '../src/lib/types'
import {
  dashboardOverviewFixture,
  subscriptionOverviewFixture,
  vpsAssetFixture,
} from '../src/pages/dashboard/dashboardTestFixtures'

import { apiRouteKey } from './fixtures/contracts'
import { expect, test } from './fixtures'
import {
  authenticatedProfile,
  comparisonWorkbenchHref,
  comparisonWorkbenchProfile,
  subjectActivityFixture,
  subjectActivityProfile,
  vpsOverviewFixture,
  vpsOverviewPartialFixture,
  vpsOverviewProfile,
} from './fixtures/profiles'

function controlledPromise(): { promise: Promise<void>; resolve: () => void } {
  let resolvePromise: (() => void) | undefined
  const promise = new Promise<void>((resolve) => {
    resolvePromise = resolve
  })
  return {
    promise,
    resolve: () => {
      if (!resolvePromise) throw new Error('controlled promise was not initialized')
      resolvePromise()
    },
  }
}

function dashboardStateProfile(options: {
  overview?: DashboardOverview
  vps?: readonly VPSAssetRecord[]
  vpsStatus?: number
  subscription?: SubscriptionOverview
  dashboardWaitFor?: Promise<void>
} = {}) {
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/dashboard')]: {
      status: 200,
      body: options.overview ?? dashboardOverviewFixture(),
      ...(options.dashboardWaitFor ? { waitFor: options.dashboardWaitFor } : {}),
    },
    [apiRouteKey('GET', '/api/vps')]: {
      status: options.vpsStatus ?? 200,
      body: (options.vpsStatus ?? 200) >= 400
        ? { error: 'VPS inventory unavailable' }
        : options.vps ?? [vpsAssetFixture()],
    },
    [apiRouteKey('GET', '/api/subscriptions/overview')]: {
      status: 200,
      body: options.subscription ?? subscriptionOverviewFixture(),
    },
  })
}

const PROVIDER = {
  provider_id: 'pv_state',
  name: 'State Test Cloud',
  website: '',
  panel_url: '',
  account_hint: '',
  country: 'JP',
  note: '',
  rating: null,
  labels: [],
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-10T06:00:00Z',
} satisfies ProviderRecord

function providersStateProfile(options: {
  providers?: readonly ProviderRecord[]
  status?: number
  waitFor?: Promise<void>
} = {}) {
  const status = options.status ?? 200
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/providers')]: {
      status,
      body: status >= 400
        ? { error: 'provider directory unavailable' }
        : options.providers ?? [],
      ...(options.waitFor ? { waitFor: options.waitFor } : {}),
    },
    [apiRouteKey('GET', '/api/vps')]: { status: 200, body: [] },
    [apiRouteKey('GET', '/api/subscriptions')]: { status: 200, body: [] },
  })
}

const DASHBOARD_MODES = [
  {
    name: 'critical',
    overview: dashboardOverviewFixture({
      abnormal_monitoring_instance_count: 2,
      severe_monitoring_instance_count: 1,
    }),
    action: '处理严重异常',
    href: '/events?severity=严重',
  },
  {
    name: 'abnormal',
    overview: dashboardOverviewFixture({
      abnormal_monitoring_instance_count: 1,
      severe_monitoring_instance_count: 0,
    }),
    action: '处理观测异常',
    href: '/monitoring?abnormal=1',
  },
  {
    name: 'maintenance',
    overview: dashboardOverviewFixture({
      maintenance_monitoring_instance_count: 1,
    }),
    action: '查看维护事件',
    href: '/events?maintenance_only=1',
  },
  {
    name: 'onboarding',
    overview: dashboardOverviewFixture({
      total_monitoring_instance_count: 0,
      total_target_count: 0,
    }),
    vps: [],
    action: '创建第一台 VPS',
    href: '/vps',
  },
  {
    name: 'stable',
    overview: dashboardOverviewFixture(),
    action: '核对 VPS 库存',
    href: '/vps',
  },
] as const

test('keeps Dashboard loading visible until its controlled response is released', async ({
  api,
  page,
}) => {
  const gate = controlledPromise()
  api.useProfile(dashboardStateProfile({ dashboardWaitFor: gate.promise }))

  await page.goto('/')

  await expect(page.getByRole('heading', { name: '正在加载工作台…' })).toBeVisible()
  gate.resolve()
  await expect(page.getByRole('link', { name: '核对 VPS 库存' })).toBeVisible()
})

for (const mode of DASHBOARD_MODES) {
  test(`renders the Dashboard ${mode.name} command surface`, async ({ api, page }) => {
    api.useProfile(dashboardStateProfile({
      overview: mode.overview,
      ...('vps' in mode ? { vps: mode.vps } : {}),
    }))

    await page.goto('/')

    const action = page.getByRole('link', { name: mode.action, exact: true })
    await expect(action).toBeVisible()
    await expect(action).toHaveAttribute('href', mode.href)
    await expect(page.getByRole('region', { name: '工作台决策面' })).toBeVisible()
    if (mode.name === 'critical') {
      await expect(page.getByText('异常总数 2（严重已包含）', { exact: true })).toBeVisible()
    }
  })
}

test('does not turn a VPS 503 into Dashboard onboarding', async ({ api, page }) => {
  api.useProfile(dashboardStateProfile({
    overview: dashboardOverviewFixture({
      total_monitoring_instance_count: 0,
      total_target_count: 0,
    }),
    vpsStatus: 503,
  }))

  await page.goto('/')

  await expect(page.getByRole('heading', { name: '部分事实待确认' })).toBeVisible()
  await expect(page.getByRole('link', { name: '创建第一台 VPS' })).toHaveCount(0)
  await expect(page.getByText('局部数据不可用', { exact: true }).first()).toBeVisible()
})

test('Dashboard supporting retry does not reload its successful overview', async ({ api, page }) => {
  api.useProfile(dashboardStateProfile({ vpsStatus: 503 }))
  await page.goto('/')
  await expect(page.getByRole('button', { name: '重试局部数据' })).toBeVisible()

  expect(api.requestCount('GET', '/api/dashboard')).toBe(2)
  expect(api.requestCount('GET', '/api/vps')).toBe(1)
  expect(api.requestCount('GET', '/api/subscriptions/overview')).toBe(1)

  api.useProfile(dashboardStateProfile())
  await page.getByRole('button', { name: '重试局部数据' }).click()
  await expect(page.getByRole('heading', { name: '当前没有紧急处理项' })).toBeVisible()

  expect(api.requestCount('GET', '/api/dashboard')).toBe(2)
  expect(api.requestCount('GET', '/api/vps')).toBe(2)
  expect(api.requestCount('GET', '/api/subscriptions/overview')).toBe(2)
})

test('renders PageState loading until the provider response gate opens', async ({ api, page }) => {
  const gate = controlledPromise()
  api.useProfile(providersStateProfile({ providers: [PROVIDER], waitFor: gate.promise }))

  await page.goto('/providers')

  await expect(page.getByRole('heading', { name: '正在加载服务商目录…' })).toBeVisible()
  gate.resolve()
  await expect(page.getByText(PROVIDER.name, { exact: true }).first()).toBeVisible()
})

test('renders the PageState empty surface for an explicit empty provider list', async ({
  api,
  page,
}) => {
  api.useProfile(providersStateProfile())

  await page.goto('/providers')

  await expect(page.getByRole('heading', { name: '尚未记录服务商' })).toBeVisible()
  await expect(page.getByRole('button', { name: '创建第一个服务商' })).toBeVisible()
})

test('retries only the provider page request group after a PageState error', async ({
  api,
  page,
}) => {
  api.useProfile(providersStateProfile({ status: 503 }))
  await page.goto('/providers')
  await expect(page.getByRole('heading', { name: '加载失败' })).toBeVisible()

  expect(api.requestCount('GET', '/api/dashboard')).toBe(1)
  expect(api.requestCount('GET', '/api/providers')).toBe(1)
  expect(api.requestCount('GET', '/api/vps')).toBe(1)
  expect(api.requestCount('GET', '/api/subscriptions')).toBe(1)

  api.useProfile(providersStateProfile({ providers: [PROVIDER] }))
  await page.getByRole('button', { name: '重试' }).click()

  await expect(page.getByText(PROVIDER.name, { exact: true }).first()).toBeVisible()
  expect(api.requestCount('GET', '/api/dashboard')).toBe(1)
  expect(api.requestCount('GET', '/api/providers')).toBe(2)
  expect(api.requestCount('GET', '/api/vps')).toBe(2)
  expect(api.requestCount('GET', '/api/subscriptions')).toBe(2)
})

test('renders the PageState success surface with provider workflow data', async ({
  api,
  page,
}) => {
  api.useProfile(providersStateProfile({ providers: [PROVIDER] }))

  await page.goto('/providers')

  await expect(page.getByText(PROVIDER.name, { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('region', { name: '服务商与入口' })).toBeVisible()
})

test('VPS 概览 keeps loading until the overview response is released', async ({
  api,
  page,
}) => {
  const gate = controlledPromise()
  api.useProfile(vpsOverviewProfile({ overviewWaitFor: gate.promise }))

  await page.goto('/vps/vps_001')
  await expect(page.getByRole('heading', { name: /正在(判定|加载)/ })).toBeVisible()
  gate.resolve()
  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()
})

test('VPS 概览 rejects an empty 200 response and recovers only after retry', async ({
  api,
  page,
}) => {
  api.useProfile(authenticatedProfile({
    [apiRouteKey('GET', '/api/vps/vps_001/overview')]: {
      status: 200,
      body: {},
    },
  }))

  await page.goto('/vps/vps_001')

  await expect(page.getByRole('heading', { name: '无法加载 VPS 概览' })).toBeVisible()
  await expect(page.getByText('VPS 概览请求或响应校验失败，请重试。')).toBeVisible()
  await expect(page.locator('.vps-detail-page')).toHaveCount(0)
  expect(api.requestCount('GET', '/api/vps/vps_001/overview')).toBe(1)

  api.useProfile(vpsOverviewProfile())
  await page.getByRole('button', { name: '重试' }).click()

  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()
  expect(api.requestCount('GET', '/api/vps/vps_001/overview')).toBe(2)
})

test('VPS 概览 healthy surface omits anomaly chrome', async ({ api, page }) => {
  api.useProfile(vpsOverviewProfile({ overview: vpsOverviewFixture({ anomalies: [] }) }))
  await page.goto('/vps/vps_001')
  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()
  await expect(page.getByText('需要关注')).toHaveCount(0)
  await expect(page.getByText('动作：无')).toHaveCount(0)
  await expect(page.getByRole('link', { name: '新建记录' })).toBeVisible()
  await expect(page.getByRole('link', { name: '时间线' })).toBeVisible()
  await expect(page.getByRole('button', { name: '管理' })).toBeVisible()
})

test('VPS 概览 anomaly surface inserts attention before summary', async ({ api, page }) => {
  api.useProfile(vpsOverviewProfile({
    overview: vpsOverviewFixture({
      anomalies: [{
        rule_id: 'renewal.due.soon.v1',
        severity: 'warning',
        title: '续费临期',
        detail: '7 天内到期',
        source: 'subscription',
        primary_action: { id: 'open_renewal_decision', label: '处理续费' },
        secondary_actions: [],
      }],
    }),
  }))
  await page.goto('/vps/vps_001')
  await expect(page.getByRole('heading', { name: '需要关注' })).toBeVisible()
  await expect(page.getByRole('button', { name: '处理续费' })).toBeVisible()
})

test('VPS 概览 keeps partial freshness local and retries only the full overview', async ({ api, page }) => {
  const partial = vpsOverviewPartialFixture()
  api.useProfile(vpsOverviewProfile({ overview: partial }))
  await page.goto('/vps/vps_001')

  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()
  await expect(page.getByLabel('IP 质量新鲜度')).toContainText('数据陈旧')
  await expect(page.getByLabel('续费新鲜度')).toContainText('暂不可用')
  await expect(page.getByLabel('服务新鲜度')).toContainText('暂不可用')
  await expect(page.getByText('最近活动暂不可用，无法确认是否为空。')).toBeVisible()
  const serviceCard = page.locator('.vps-overview-relations__item').filter({ hasText: '服务' })
  await expect(serviceCard.locator('.vps-overview-relations__count')).toHaveText('—')

  const refreshGate = controlledPromise()
  api.useProfile(vpsOverviewProfile({ overview: partial, overviewWaitFor: refreshGate.promise }))
  const retry = page.getByRole('button', { name: '重试 IP 质量' })
  await retry.focus()
  await page.keyboard.press('Enter')

  await expect.poll(() => api.requestCount('GET', '/api/vps/vps_001/overview')).toBe(2)
  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()
  await expect(retry).toBeDisabled()
  refreshGate.resolve()
  await expect(retry).toBeEnabled()
})

test('单主体时间线 loading / empty / local-error states', async ({ api, page }) => {
  const gate = controlledPromise()
  api.useProfile(subjectActivityProfile({ activityWaitFor: gate.promise }))
  await page.goto('/vps/vps_001/activity')
  await expect(page.getByRole('heading', { name: '正在加载活动' })).toBeVisible()
  gate.resolve()
  await expect(page.getByText('E2E 时间线条目')).toBeVisible()

  api.useProfile(subjectActivityProfile({
    activity: subjectActivityFixture({ items: [] }),
  }))
  await page.goto('/vps/vps_001/activity')
  await expect(page.getByText('主体尚无活动')).toBeVisible()

  api.useProfile(subjectActivityProfile({ activityStatus: 503 }))
  await page.goto('/vps/vps_001/activity')
  await expect(page.getByRole('heading', { name: '活动投影不可用' })).toBeVisible()
})

test('横向比较工作台 keeps loading until the compare gate opens', async ({ api, page }) => {
  const gate = controlledPromise()
  api.useProfile(comparisonWorkbenchProfile({
    mode: 'host-partial',
    compareWaitFor: gate.promise,
  }))
  await page.goto(comparisonWorkbenchHref({
    mode: 'fixed',
    items: [{ snapshot_id: 'evs_cmpleft' }, { snapshot_id: 'evs_cmpright' }],
    baseline: 0,
    alignment: 'actual_coverage',
    tolerance_seconds: 60,
    kind: 'monitoring.host/v1',
    metric: 'cpu_usage_pct',
  }))
  await expect(page.getByRole('heading', { name: '正在加载比较' })).toBeVisible()
  await expect(page.getByRole('button', { name: '取消比较' })).toBeVisible()
  gate.resolve()
  await expect(page.getByRole('heading', { name: '可比性审查' })).toBeVisible()
})
