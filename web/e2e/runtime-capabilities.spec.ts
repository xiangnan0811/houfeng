import { apiRouteKey } from './fixtures/contracts'
import {
  authenticatedProfile,
  authenticatedUser,
  coreRouteProfile,
  dashboardProfile,
  recordSearchProfile,
} from './fixtures/profiles'
import { expect, test } from './fixtures'

const RECORDS_OFF = { records: false, comparison: false, portability: false }

test('records off direct URL stays on the feature-off state and does not load records', async ({ api, page }) => {
  api.useProfile(authenticatedProfile({}, undefined, RECORDS_OFF))

  await page.goto('/records')

  await expect(page.getByRole('heading', { name: '记录平台未启用' })).toBeVisible()
  await expect(page.getByRole('link', { name: '运维记录' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: '命令审计' })).toBeVisible()
  expect(api.requestCount('GET', '/api/records/search')).toBe(0)
  expect(api.requestCount('GET', '/api/record-notifications/unread-count')).toBe(0)
  expect(api.requestCount('GET', '/api/subjects/vps/vps_001/activity')).toBe(0)
})

test('records off still loads independent asset decisions', async ({ api, page }) => {
  api.useProfile({
    ...coreRouteProfile('/asset-decisions'),
    [apiRouteKey('GET', '/api/auth/me')]: {
      status: 200,
      body: authenticatedUser(RECORDS_OFF),
    },
  })

  await page.goto('/asset-decisions')

  await expect(page.getByRole('heading', { name: '资产组合决策' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '记录平台未启用' })).toHaveCount(0)
  expect(api.requestCount(
    'GET',
    '/api/asset-decisions/records?view=needs_decision&renew_within_days=30',
  )).toBeGreaterThan(0)
  expect(api.requestCount('GET', '/api/records/search')).toBe(0)
})

test('comparison off blocks the compare page without comparison requests', async ({ api, page }) => {
  api.useProfile(authenticatedProfile({}, undefined, {
    records: true,
    comparison: false,
    portability: true,
  }))

  await page.goto('/records/compare')

  await expect(page.getByRole('heading', { name: '比较功能关闭' })).toBeVisible()
  expect(api.requestCount('POST', '/api/evidence/comparison-candidates')).toBe(0)
  expect(api.requestCount('POST', '/api/evidence/comparisons')).toBe(0)
})

test('portability and comparison off hide export, import, and compare entry points', async ({ api, page }) => {
  api.useProfile({
    ...recordSearchProfile(),
    [apiRouteKey('GET', '/api/auth/me')]: {
      status: 200,
      body: authenticatedUser({ records: true, comparison: false, portability: false }),
    },
  })

  await page.goto('/records')

  await expect(page.getByRole('heading', { name: '运维记录' })).toBeVisible()
  await expect(page.getByRole('link', { name: '第三晚 TCP 观测' })).toBeVisible()
  await expect(page.getByRole('link', { name: '新建记录' })).toBeVisible()
  await expect(page.getByRole('link', { name: '横向比较' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '导入' })).toHaveCount(0)
  await expect(page.getByText('导出选中记录')).toHaveCount(0)
  expect(api.requestCount('GET', '/api/records/search')).toBeGreaterThan(0)
  expect(api.requestCount('POST', '/api/record-export-previews')).toBe(0)
})

test('a non-401 capability read stays on retry instead of the login page', async ({ api, page, diagnostics }) => {
  diagnostics.allowHttpError('GET', '/api/auth/me', 503)
  api.useProfile({
    [apiRouteKey('GET', '/api/auth/me')]: {
      status: 503,
      body: { error: 'unavailable' },
    },
  })

  await page.goto('/')

  await expect(page.getByRole('heading', { name: '能力读取错误' })).toBeVisible()
  await expect(page.getByLabel('用户名')).toHaveCount(0)

  api.useProfile(dashboardProfile())
  await page.getByRole('button', { name: '重试' }).click()

  await expect(page.getByRole('link', { name: '工作台' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '能力读取错误' })).toHaveCount(0)
  await expect(page.getByLabel('用户名')).toHaveCount(0)
})
