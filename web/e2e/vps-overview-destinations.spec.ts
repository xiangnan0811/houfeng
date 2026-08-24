import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

import type { VPSOverview, VPSOverviewAnomaly } from '../src/lib/types'
import { expect, test } from './fixtures'
import { apiRouteKey, type ApiFixtureProfile } from './fixtures/contracts'
import {
  coreRouteProfile,
  vpsOverviewFixture,
  vpsOverviewProfile,
} from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

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

async function expectLocation(page: Page, expected: string) {
  await expect.poll(async () => page.evaluate(() => location.pathname + location.search)).toBe(expected)
}

function routeOwnerProfile(owner: 'monitoring' | 'events' | 'ip-quality'): ApiFixtureProfile {
  if (owner === 'monitoring') return coreRouteProfile('/monitoring')
  if (owner === 'events') {
    return {
      ...coreRouteProfile('/events'),
      [apiRouteKey('GET', '/api/events?object_type=monitoring_instance&limit=200')]: {
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
    expected: '/monitoring?abnormal=1',
    action: anomaly(
      'monitoring.health.abnormal.v1',
      { id: 'open_monitoring', label: '查看监控', route: '/monitoring?abnormal=1' },
    ),
  },
  {
    name: 'incidents',
    owner: 'events' as const,
    expected: '/events?object_type=monitoring_instance',
    action: anomaly(
      'monitoring.incidents.open.v1',
      { id: 'open_incidents', label: '查看事件', route: '/events?object_type=monitoring_instance' },
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
    await page.goto('/vps/vps_001')

    await page.getByRole('link', { name: contract.action.primary_action!.label }).click()
    await expectLocation(page, contract.expected)
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

  const card = page.locator('.vps-overview-relations__item').filter({ hasText: '订阅' })
  await card.getByRole('link').click()
  await expectLocation(page, '/subscriptions?vps_id=vps_001')
  await expect(page.getByRole('heading', { name: /订阅成本中枢/ })).toBeVisible()
})

const RELATION_PANELS = [
  {
    label: '监控实例',
    dialog: '关联监控实例',
    content: 'Tokyo Monitor',
    apiPath: '/api/vps/vps_001/monitoring-instances',
  },
  {
    label: '服务',
    dialog: '关联服务',
    content: 'Overview Gateway',
    apiPath: '/api/vps/vps_001/services',
  },
  {
    label: '域名',
    dialog: '关联域名',
    content: 'edge.example.com',
    apiPath: '/api/vps/vps_001/domains',
  },
] as const

for (const contract of RELATION_PANELS) {
  test(`VPS overview ${contract.label} relation opens its scoped read-only panel`, async ({ api, page }) => {
    api.useProfile(vpsOverviewProfile())
    await page.goto('/vps/vps_001')

    const card = page.locator('.vps-overview-relations__item').filter({ hasText: contract.label })
    await card.locator('button.vps-overview-relations__link').click()
    await expect(page.getByRole('dialog', { name: contract.dialog })).toBeVisible()
    await expect(page.getByText(contract.content, { exact: true })).toBeVisible()
    expect(api.requestCount('GET', contract.apiPath)).toBe(1)
    await expect(page.getByRole('dialog', { name: contract.dialog }).getByRole('button', {
      name: /新增|解除关联|接入\/升级/,
    })).toHaveCount(0)
  })
}

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
      id: 'open_subscription', label: 'command with route', route: '/subscriptions?vps_id=vps_001',
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
  const relationText = page.locator('.vps-overview-relations__label')
    .getByText('mismatched subscription', { exact: true })
  await expect(relationText).toBeVisible()
  expect(await relationText.evaluate((element) => element.closest('a,button') === null)).toBe(true)
  await expectLocation(page, '/vps/vps_001')
})

test('VPS overview relation dialog is keyboard-safe, mobile-safe, and accessible at 390px', async ({ api, page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  api.useProfile(vpsOverviewProfile())
  await page.goto('/vps/vps_001')

  const card = page.locator('.vps-overview-relations__item').filter({ hasText: '监控实例' })
  const trigger = card.locator('button.vps-overview-relations__link')
  await trigger.scrollIntoViewIfNeeded()
  await trigger.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('dialog', { name: '关联监控实例' })
  await expect(dialog).toBeVisible()
  await expectNoDocumentOverflow(page)

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
})
