import AxeBuilder from '@axe-core/playwright'

import type { MonitoringInstanceRecord } from '../src/lib/types'
import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile } from './fixtures/profiles'

const UNLABELED_INSTANCE = {
  monitoring_instance_id: 'mi_tokyo',
  display_name: 'Tokyo Monitor',
  group: 'edge',
  region: 'ap-northeast-1',
  city: 'Tokyo',
  provider: 'Example Cloud',
  lifecycle_status: '已接入',
  monitoring_status: '启用',
  binding_status: '已绑定',
  labels: [] as string[],
  note: 'first agent',
  current_health_status: '正常',
  last_heartbeat_at: '2026-08-20T08:59:00Z',
  last_sync_at: '2026-08-20T09:00:00Z',
  current_active_incident_count: 0,
  current_primary_issue_summary: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-20T09:00:00Z',
  metadata_updated_at: '2026-08-20T09:00:00Z',
} satisfies MonitoringInstanceRecord

test('create target dialog adds the execution label to an unlabeled instance after confirmation', async ({ api, page }) => {
  api.useProfile({
    ...coreRouteProfile('/targets'),
    [apiRouteKey('GET', '/api/monitoring-instances?scope=active')]: { status: 200, body: [UNLABELED_INSTANCE] },
    // 写入前重读：心跳推进了 updated_at，期间资料也被改过；If-Match 必须用重读到的资料令牌。
    [apiRouteKey('GET', '/api/monitoring-instances/mi_tokyo')]: {
      status: 200,
      body: { ...UNLABELED_INSTANCE, updated_at: '2026-08-20T09:00:30Z', metadata_updated_at: '2026-08-20T09:00:05Z' },
    },
    [apiRouteKey('PATCH', '/api/monitoring-instances/mi_tokyo')]: {
      status: 200,
      body: { ...UNLABELED_INSTANCE, labels: ['jp'], updated_at: '2026-08-20T09:05:00Z' },
      expectedBodyKeys: ['group', 'labels', 'note'],
    },
  })
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/targets')
  await page.getByRole('button', { name: '新建第一个目标' }).click()
  const dialog = page.getByRole('dialog', { name: '创建目标' })
  const executionLabels = dialog.getByLabel('执行监控实例标签')
  await executionLabels.focus()
  await expect(dialog.getByText(/现有 1 台监控实例都还没有标签/)).toBeVisible()
  await executionLabels.fill('jp')

  const assign = dialog.getByRole('group', { name: '给监控实例加标签' })
  await assign.getByRole('button', { name: '给 Tokyo Monitor 加上「jp」' }).click()
  const confirm = assign.getByRole('button', { name: '确认添加' })
  await expect(confirm).toBeFocused()
  await expect(confirm).toHaveAccessibleDescription(/加上后，它也会执行其他带这个标签的目标/)
  const confirmScan = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
  expect(confirmScan.violations).toEqual([])
  const patch = page.waitForRequest((request) => request.method() === 'PATCH' && request.url().endsWith('/api/monitoring-instances/mi_tokyo'))
  await confirm.click()
  const request = await patch
  expect(request.postDataJSON()).toEqual({ group: 'edge', labels: ['jp'], note: 'first agent' })
  expect(request.headers()['if-match']).toBe('"2026-08-20T09:00:05Z"')

  await expect(dialog.getByText('将由 Tokyo Monitor 执行。')).toBeVisible()
  await expect(assign).toHaveCount(0)
  await expect(executionLabels).toBeFocused()
  const scan = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
  expect(scan.violations).toEqual([])
})
