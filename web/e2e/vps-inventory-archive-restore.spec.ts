import type { Page } from '@playwright/test'

import type {
  ArchiveReview,
  VPSAssetDetail,
  VPSOverview,
  VPSTimeline,
} from '../src/lib/types'
import { vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { expect, test } from './fixtures'
import { apiRouteKey, type ApiFixtureProfile } from './fixtures/contracts'
import {
  coreRouteProfile,
  vpsOverviewFixture,
  vpsOverviewProfile,
} from './fixtures/profiles'

const INVENTORY_PATH = '/vps?workspace=ledger&q=Tokyo&selected=vps_001'
const EMPTY_TIMELINE: VPSTimeline = {
  vps_id: 'vps_001',
  renewal_decisions: [],
  price_histories: [],
  ip_histories: [],
  spec_snapshots: [],
  experience_logs: [],
}

function detailFromOverview(overview: VPSOverview, lifecycle: VPSAssetDetail['lifecycle_status']): VPSAssetDetail {
  return {
    ...vpsAssetFixture({
      vps_id: overview.identity.vps_id,
      display_name: overview.identity.display_name,
      lifecycle_status: lifecycle,
    }),
    monitoring_instance_links: [],
  }
}

function archiveReview(overview: VPSOverview, options: {
  lifecycle: VPSAssetDetail['lifecycle_status']
  eligible: boolean
  blockers?: string[]
}): ArchiveReview {
  return {
    vps: detailFromOverview(overview, options.lifecycle),
    subscriptions: [],
    monitoring_instance_links: [],
    services: [],
    domains: [],
    target_links: [],
    warnings: [],
    blockers: options.blockers ?? [],
    eligible: options.eligible,
  }
}

function toCancelOverview(): VPSOverview {
  const overview = vpsOverviewFixture()
  overview.identity.lifecycle_status = 'to_cancel'
  return overview
}

function liveProfile(overview: VPSOverview): ApiFixtureProfile {
  return {
    ...coreRouteProfile('/vps'),
    ...vpsOverviewProfile({
      overview,
      detail: detailFromOverview(overview, 'to_cancel'),
      subscriptions: [],
      services: [],
      domains: [],
    }),
    [apiRouteKey('GET', '/api/vps/vps_001/archive-review')]: {
      status: 200,
      body: archiveReview(overview, { lifecycle: 'to_cancel', eligible: true }),
    },
  }
}

function archivedProfile(overview: VPSOverview): ApiFixtureProfile {
  const archivedOverview = vpsOverviewFixture({
    identity: { ...overview.identity, lifecycle_status: 'archived' },
  })
  return {
    ...coreRouteProfile('/vps'),
    ...vpsOverviewProfile({
      overview: archivedOverview,
      detail: detailFromOverview(archivedOverview, 'archived'),
      subscriptions: [],
      services: [],
      domains: [],
    }),
    [apiRouteKey('GET', '/api/vps/vps_001/archive-review')]: {
      status: 200,
      body: archiveReview(archivedOverview, {
        lifecycle: 'archived',
        eligible: false,
        blockers: ['VPS 已归档，只能在归档详情页只读查看或执行受控恢复。'],
      }),
    },
    [apiRouteKey('GET', '/api/vps/vps_001/timeline')]: { status: 200, body: EMPTY_TIMELINE },
    [apiRouteKey('GET', '/api/subscriptions?asset_scope=all&order=asc&sort=renew_at&vps_id=vps_001')]: {
      status: 200,
      body: [],
    },
  }
}

function restoredProfile(overview: VPSOverview): ApiFixtureProfile {
  const idleOverview = vpsOverviewFixture({
    identity: { ...overview.identity, lifecycle_status: 'idle', usage_status: 'idle' },
  })
  return {
    ...coreRouteProfile('/vps'),
    ...vpsOverviewProfile({
      overview: idleOverview,
      detail: detailFromOverview(idleOverview, 'idle'),
      subscriptions: [],
      services: [],
      domains: [],
    }),
  }
}

async function openInventoryDetail(page: Page) {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(INVENTORY_PATH)
  await page.getByRole('link', { name: '打开 VPS 详情', exact: true }).click()
  await expect(page).toHaveURL((url) => url.pathname === '/vps/vps_001')
  await expect(page.getByRole('heading', { name: 'Tokyo Edge', exact: true })).toBeVisible()
}

async function confirmArchive(page: Page) {
  await page.getByRole('button', { name: '管理', exact: true }).click()
  const menu = page.getByRole('menu', { name: '管理' })
  await expect(menu).toBeVisible()
  await menu.getByRole('menuitem', { name: '归档' }).click()
  const dialog = page.getByRole('alertdialog', { name: '确认归档 VPS' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('textbox', { name: '输入 VPS 名称确认归档' }).fill('Tokyo Edge')
  await dialog.getByRole('button', { name: '确认归档' }).click()
}

async function historyUserState(page: Page) {
  return page.evaluate(() => {
    const state = window.history.state as { usr?: unknown } | null
    return state?.usr ?? null
  })
}

test('inventory provenance survives archive, restore, and the return link', async ({ api, page }) => {
  const overview = toCancelOverview()
  api.useProfile({
    ...liveProfile(overview),
    [apiRouteKey('POST', '/api/vps/vps_001/archive')]: {
      status: 200,
      body: archiveReview(overview, {
        lifecycle: 'archived',
        eligible: false,
        blockers: ['VPS 已归档，只能在归档详情页只读查看或执行受控恢复。'],
      }),
      expectedBodyKeys: ['confirmation_name'],
      waitFor: {
        then(resolve?: () => void) {
          api.useProfile({
            ...archivedProfile(overview),
            [apiRouteKey('POST', '/api/vps/vps_001/restore-from-archive')]: {
              status: 200,
              body: vpsAssetFixture({ lifecycle_status: 'idle', usage_status: 'idle', archived_at: null }),
              expectNoBody: true,
              waitFor: {
                then(restoreResolve?: () => void) {
                  api.useProfile(restoredProfile(overview))
                  restoreResolve?.()
                  return Promise.resolve()
                },
              } as Promise<void>,
            },
          })
          resolve?.()
          return Promise.resolve()
        },
      } as Promise<void>,
    },
  })

  await openInventoryDetail(page)
  const returnLink = page.getByRole('link', { name: '返回 VPS 列表', exact: true })
  await expect(returnLink).toHaveAttribute('href', INVENTORY_PATH)

  await confirmArchive(page)
  await expect(page).toHaveURL((url) => url.pathname === '/archive/vps_001')
  await expect(page.getByRole('heading', { name: /Tokyo Edge/ })).toBeVisible()
  await expect.poll(async () => historyUserState(page)).toEqual({ vpsInventoryHref: INVENTORY_PATH })
  expect(api.requestCount('POST', '/api/vps/vps_001/archive')).toBe(1)

  await page.getByRole('button', { name: '恢复为闲置' }).click()
  const restoreDialog = page.getByRole('alertdialog', { name: '确认恢复归档 VPS' })
  await restoreDialog.getByRole('button', { name: '确认恢复' }).click()

  await expect(page).toHaveURL((url) => url.pathname === '/vps/vps_001')
  await expect(page.getByRole('heading', { name: 'Tokyo Edge', exact: true })).toBeVisible()
  await expect(returnLink).toHaveAttribute('href', INVENTORY_PATH)
  expect(api.requestCount('POST', '/api/vps/vps_001/restore-from-archive')).toBe(1)

  await returnLink.click()
  await expect(page).toHaveURL((url) => (
    url.pathname === '/vps'
    && url.searchParams.get('workspace') === 'ledger'
    && url.searchParams.get('q') === 'Tokyo'
    && url.searchParams.get('selected') === 'vps_001'
  ))
})

test('archived overview auto-redirects to archive detail without a write', async ({ api, page }) => {
  const overview = vpsOverviewFixture({
    identity: { ...vpsOverviewFixture().identity, lifecycle_status: 'archived' },
  })
  api.useProfile(archivedProfile(overview))

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/vps/vps_001')
  await expect(page).toHaveURL((url) => url.pathname === '/archive/vps_001')
  await expect(page.getByRole('heading', { name: /Tokyo Edge/ })).toBeVisible()
  expect(api.requestCount('POST', '/api/vps/vps_001/archive')).toBe(0)
  expect(api.requestCount('POST', '/api/vps/vps_001/restore-from-archive')).toBe(0)
})

test('failed archive write stays on detail and keeps inventory provenance', async ({ api, page }) => {
  const overview = toCancelOverview()
  api.useProfile({
    ...liveProfile(overview),
    [apiRouteKey('POST', '/api/vps/vps_001/archive')]: {
      status: 409,
      body: { error: 'archive conflict' },
      expectedBodyKeys: ['confirmation_name'],
    },
  })

  await openInventoryDetail(page)
  const returnLink = page.getByRole('link', { name: '返回 VPS 列表', exact: true })
  await expect(returnLink).toHaveAttribute('href', INVENTORY_PATH)
  await confirmArchive(page)

  await expect(page.getByRole('alertdialog', { name: '确认归档 VPS' }).getByText('archive conflict')).toBeVisible()
  await expect(page).toHaveURL((url) => url.pathname === '/vps/vps_001')
  await expect(returnLink).toHaveAttribute('href', INVENTORY_PATH)
  expect(api.requestCount('POST', '/api/vps/vps_001/archive')).toBe(1)
})

test('failed restore write stays on archive detail and keeps inventory provenance', async ({ api, page }) => {
  const overview = toCancelOverview()
  api.useProfile({
    ...liveProfile(overview),
    [apiRouteKey('POST', '/api/vps/vps_001/archive')]: {
      status: 200,
      body: archiveReview(overview, {
        lifecycle: 'archived',
        eligible: false,
        blockers: ['VPS 已归档，只能在归档详情页只读查看或执行受控恢复。'],
      }),
      expectedBodyKeys: ['confirmation_name'],
      waitFor: {
        then(resolve?: () => void) {
          api.useProfile({
            ...archivedProfile(overview),
            [apiRouteKey('POST', '/api/vps/vps_001/restore-from-archive')]: {
              status: 409,
              body: { error: 'restore conflict' },
              expectNoBody: true,
            },
          })
          resolve?.()
          return Promise.resolve()
        },
      } as Promise<void>,
    },
  })

  await openInventoryDetail(page)
  await confirmArchive(page)
  await expect(page).toHaveURL((url) => url.pathname === '/archive/vps_001')
  await expect.poll(async () => historyUserState(page)).toEqual({ vpsInventoryHref: INVENTORY_PATH })

  await page.getByRole('button', { name: '恢复为闲置' }).click()
  const restoreDialog = page.getByRole('alertdialog', { name: '确认恢复归档 VPS' })
  await restoreDialog.getByRole('button', { name: '确认恢复' }).click()
  await expect(restoreDialog.getByText('restore conflict')).toBeVisible()
  await expect(page).toHaveURL((url) => url.pathname === '/archive/vps_001')
  await expect.poll(async () => historyUserState(page)).toEqual({ vpsInventoryHref: INVENTORY_PATH })
  expect(api.requestCount('POST', '/api/vps/vps_001/restore-from-archive')).toBe(1)
})
