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
  subjectActivityFixture,
  subjectActivityProfile,
  vpsOverviewFixture,
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
        rule_id: 'renewal.due_soon',
        severity: 'warning',
        title: '续费临期',
        detail: '7 天内到期',
        source: 'subscription',
        primary_action: { id: 'open', label: '处理续费', route: '/vps/vps_001' },
        secondary_actions: [],
      }],
    }),
  }))
  await page.goto('/vps/vps_001')
  await expect(page.getByRole('heading', { name: '需要关注' })).toBeVisible()
  await expect(page.getByRole('link', { name: '处理续费' })).toBeVisible()
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
