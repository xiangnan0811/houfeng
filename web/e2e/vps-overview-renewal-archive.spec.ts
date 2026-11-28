import type { Page } from '@playwright/test'

import type { ArchiveReview, VPSOverview } from '../src/lib/types'
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

function archiveReview(): ArchiveReview {
  return {
    vps: vpsAssetFixture(),
    subscriptions: [],
    monitoring_instance_links: [],
    services: [],
    domains: [],
    target_links: [],
    warnings: [],
    blockers: [],
    blocker_details: [],
    eligible: true,
    preview_digest: 'digest-archive-e2e',
    online_evidence: {
      observed_at: '2026-09-26T04:00:00Z', receiver_generation: 'fixture-generation',
      receiver_healthy: true, healthy_since: '2026-09-26T00:00:00Z', last_health_check_at: '2026-09-26T04:00:00Z',
      earliest_archive_at: null, never_connected: true, manual_confirmation_required: true, instances: [],
    },
  }
}

function cancelOverview(): VPSOverview {
  const overview = vpsOverviewFixture({
    relations: [
      {
        kind: 'monitoring_instances', count: 0, label: '监控实例', section: READY,
      },
      {
        kind: 'subscriptions', count: 1, status: 'cancel', route: '/subscriptions?vps_id=vps_001&view=details',
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

test('saving no-renewal intent preserves a stable archive entry and requires provider verification', async ({ api, page }) => {
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
      expectedBodyKeys: ['renewal_decision', 'renewal_reason', 'renewal_review_at'],
      waitFor: {
        then(resolve?: () => void) {
          api.useProfile({
            ...vpsOverviewProfile({ overview: cancelOverview() }),
            [apiRouteKey('GET', '/api/vps/vps_001/archive-review')]: {
              status: 200,
              body: archiveReview(),
            },
          })
          resolve?.()
          return Promise.resolve()
        },
      } as Promise<void>,
    },
  })
  await page.goto('/vps/vps_001')

  await page.getByRole('button', { name: '管理', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '取消 / 退役' })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: '结束使用并归档' })).toBeVisible()
  await page.getByRole('menuitem', { name: '续费决策' }).click()

  const decisionDialog = page.getByRole('dialog', { name: '续费决策' })
  await decisionDialog.locator('select').selectOption('cancel')
  await expect(decisionDialog.getByText(/请核对服务商自动续费是否已关闭/)).toBeVisible()
  await decisionDialog.getByLabel('决策理由').fill('准备取消')
  await decisionDialog.getByRole('button', { name: '保存续费决策' }).click()

  await expect(page.getByRole('dialog', { name: '续费决策' })).toHaveCount(0)
  await page.getByRole('button', { name: '管理', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '取消 / 退役' })).toHaveCount(0)
  await page.getByRole('menuitem', { name: '结束使用并归档' }).click()
  await expect(page.getByRole('alertdialog', { name: '结束使用并归档' })).toBeVisible()
  expect(api.requestCount('PATCH', '/api/vps/vps_001')).toBe(1)
  expect(api.requestCount('GET', '/api/vps/vps_001/archive-review')).toBe(1)
  expect(api.requestCount('POST', '/api/vps/vps_001/archive')).toBe(0)
})

test('active VPS with an old heartbeat can archive after the receiver restarts', async ({ api, page }) => {
  const review = archiveReview()
  review.online_evidence = {
    observed_at: '2026-11-28T07:43:00Z', receiver_generation: 'restarted',
    receiver_healthy: true, healthy_since: '2026-11-28T06:13:00Z', last_health_check_at: '2026-11-28T07:43:00Z',
    earliest_archive_at: '2026-11-25T04:03:00Z', never_connected: false, manual_confirmation_required: false,
    instances: [{ monitoring_instance_id: 'mi_001', session_id: 'mas_001', ever_connected: true, last_trusted_online_at: '2026-11-25T01:03:00Z' }],
  }
  api.useProfile({
    ...vpsOverviewProfile({ overview: cancelOverview() }),
    [apiRouteKey('GET', '/api/vps/vps_001/archive-review')]: {
      status: 200,
      body: review,
    },
  })
  await page.goto('/vps/vps_001')

  await page.getByRole('button', { name: '管理', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '结束使用并归档' })).toBeVisible()
  await page.getByRole('menuitem', { name: '结束使用并归档' }).click()
  const dialog = page.getByRole('alertdialog', { name: '结束使用并归档' })
  await expect(dialog.getByText('Center 心跳接收服务当前正常。')).toBeVisible()
  await expect(dialog.getByText(/最后可信在线 2026\/11\/25 09:03/)).toBeVisible()
  await expect(dialog.getByText(/最早可在|2026\/11\/28 14:13/)).toHaveCount(0)
  await dialog.getByRole('textbox', { name: '归档原因' }).fill('已过期，停止使用')
  await dialog.getByRole('textbox', { name: '输入 VPS 名称确认归档' }).fill(review.vps.display_name)
  await expect(dialog.getByRole('button', { name: '结束使用并归档' })).toBeEnabled()
  expect(api.requestCount('GET', '/api/vps/vps_001/archive-review')).toBe(1)
  await expectLocation(page, '/vps/vps_001')
})

test('workbench=archive opens the archive preview and consumes the route command', async ({ api, page }) => {
  api.useProfile({
    ...vpsOverviewProfile({ overview: cancelOverview() }),
    [apiRouteKey('GET', '/api/vps/vps_001/archive-review')]: {
      status: 200,
      body: archiveReview(),
    },
  })
  await page.goto('/vps/vps_001?workbench=archive')

  await expect(page.getByRole('alertdialog', { name: '结束使用并归档' })).toBeVisible()
  expect(api.requestCount('GET', '/api/vps/vps_001/archive-review')).toBe(1)
  await expect.poll(async () => page.evaluate(() => location.search)).toBe('')
})
