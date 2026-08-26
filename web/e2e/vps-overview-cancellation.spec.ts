import type { Page } from '@playwright/test'

import type { CancellationPreview, VPSOverview } from '../src/lib/types'
import { vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { vpsOverviewFixture, vpsOverviewProfile } from './fixtures/profiles'

const READY = {
  state: 'ready' as const,
  observed_at: null,
  last_success_at: null,
  reason_code: '',
}

function cancellationPreview(): CancellationPreview {
  return {
    vps: vpsAssetFixture(),
    subscriptions: [],
    monitoring_instance_links: [],
    services: [],
    domains: [],
    target_links: [],
    recommended_steps: [],
    warnings: [],
    blockers: [],
    preview_digest: 'digest-cancel-e2e',
  }
}

function cancelOverview(): VPSOverview {
  const overview = vpsOverviewFixture({
    relations: [
      {
        kind: 'monitoring_instances', count: 0, label: '监控实例', section: READY,
      },
      {
        kind: 'subscriptions', count: 1, status: 'cancel', route: '/subscriptions?vps_id=vps_001',
        label: '订阅', section: READY,
      },
      {
        kind: 'services', count: 0, label: '服务', section: READY,
      },
      {
        kind: 'domains', count: 0, label: '域名', section: READY,
      },
    ],
  })
  overview.identity.renewal_decision = 'cancel'
  return overview
}

async function expectLocation(page: Page, expected: string) {
  await expect.poll(async () => page.evaluate(() => location.pathname + location.search)).toBe(expected)
}

test('setting renewal decision to cancel exposes the cancellation workbench', async ({ api, page }) => {
  const keepOverview = vpsOverviewFixture()
  expect(keepOverview.identity.renewal_decision).not.toBe('cancel')
  expect(keepOverview.relations.some((row) => row.kind === 'subscriptions' && row.count === 1)).toBeTruthy()

  api.useProfile({
    ...vpsOverviewProfile({ overview: keepOverview }),
    [apiRouteKey('PATCH', '/api/vps/vps_001')]: {
      status: 200,
      body: {
        ...keepOverview.identity,
        renewal_decision: 'cancel',
        monitoring_instance_links: [],
      },
      expectedBodyKeys: ['renewal_decision', 'renewal_reason'],
      waitFor: {
        then(resolve?: () => void) {
          api.useProfile({
            ...vpsOverviewProfile({ overview: cancelOverview() }),
            [apiRouteKey('GET', '/api/vps/vps_001/cancellation-preview')]: {
              status: 200,
              body: cancellationPreview(),
            },
          })
          resolve?.()
          return Promise.resolve()
        },
      } as Promise<void>,
    },
  })
  await page.goto('/vps/vps_001')

  await page.getByRole('button', { name: '管理' }).click()
  await expect(page.getByRole('menuitem', { name: '取消 / 退役' })).toHaveCount(0)
  await page.getByRole('menuitem', { name: '续费决策' }).click()

  const decisionDialog = page.getByRole('dialog', { name: '续费决策' })
  await decisionDialog.locator('select').selectOption('cancel')
  await decisionDialog.getByLabel('决策理由').fill('准备取消')
  await decisionDialog.getByRole('button', { name: '保存续费决策' }).click()

  await expect(page.getByRole('dialog', { name: '续费决策' })).toHaveCount(0)
  await page.getByRole('button', { name: '管理' }).click()
  await expect(page.getByRole('menuitem', { name: '取消 / 退役' })).toBeVisible()
  await page.getByRole('menuitem', { name: '取消 / 退役' }).click()
  await expect(page.getByRole('dialog', { name: '取消 / 退役' })).toBeVisible()
  expect(api.requestCount('PATCH', '/api/vps/vps_001')).toBe(1)
  expect(api.requestCount('GET', '/api/vps/vps_001/cancellation-preview')).toBe(1)
})

test('active VPS with a cancel renewal decision exposes the cancellation workbench', async ({ api, page }) => {
  api.useProfile({
    ...vpsOverviewProfile({ overview: cancelOverview() }),
    [apiRouteKey('GET', '/api/vps/vps_001/cancellation-preview')]: {
      status: 200,
      body: cancellationPreview(),
    },
  })
  await page.goto('/vps/vps_001')

  await page.getByRole('button', { name: '管理' }).click()
  await expect(page.getByRole('menuitem', { name: '取消 / 退役' })).toBeVisible()
  await page.getByRole('menuitem', { name: '取消 / 退役' }).click()
  await expect(page.getByRole('dialog', { name: '取消 / 退役' })).toBeVisible()
  expect(api.requestCount('GET', '/api/vps/vps_001/cancellation-preview')).toBe(1)
  await expectLocation(page, '/vps/vps_001')
})

test('workbench=cancellation opens the cancellation panel when Overview is on', async ({ api, page }) => {
  api.useProfile({
    ...vpsOverviewProfile({ overview: cancelOverview() }),
    [apiRouteKey('GET', '/api/vps/vps_001/cancellation-preview')]: {
      status: 200,
      body: cancellationPreview(),
    },
  })
  await page.goto('/vps/vps_001?workbench=cancellation')

  await expect(page.getByRole('dialog', { name: '取消 / 退役' })).toBeVisible()
  expect(api.requestCount('GET', '/api/vps/vps_001/cancellation-preview')).toBe(1)
  await expect.poll(async () => page.evaluate(() => location.search)).toBe('')
})
