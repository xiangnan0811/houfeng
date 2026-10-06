import AxeBuilder from '@axe-core/playwright'
import type { Page, Route } from '@playwright/test'

import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { authenticatedProfile, recordDetailProfile } from './fixtures/profiles'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

const VPS_ID = 'vps_0123456789abcdef'
const PREVIEW_KEYS = [
  'kind', 'metrics', 'precision_seconds', 'record_id', 'requested_window', 'schema_version',
  'sensitive_topology_fields', 'source_id', 'source_type',
] as const

type PreviewRequest = {
  record_id?: string
  kind: string
  schema_version: number
  source_type: string
  source_id: string
  requested_window: { start: string; end: string }
  metrics: string[]
}

// 预览响应必须回显请求的窗口与来源，固定 fixture 做不到：按请求生成，每次一个新的采集意图。
async function routeCapturePreviews(page: Page, recordId: string): Promise<PreviewRequest[]> {
  const requests: PreviewRequest[] = []
  await page.route('**/api/evidence/capture-previews', async (route: Route) => {
    const body = route.request().postDataJSON() as PreviewRequest
    requests.push(body)
    const now = Date.now()
    const sequence = String(requests.length).padStart(2, '0')
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        record_id: body.record_id ?? recordId,
        snapshot_id: `evs_e2ecaptured${sequence}`,
        capture_intent_id: `evi_e2ecapture${sequence}`,
        kind: body.kind,
        schema_version: body.schema_version,
        subject: { type: 'vps', id: VPS_ID, display_name: 'VPS Alpha' },
        source: { type: body.source_type, id: body.source_id, display_name: '' },
        requested_window: body.requested_window,
        actual_window: body.requested_window,
        observed_at: body.requested_window.end,
        quality: { status: 'complete' },
        quota: { status: 'allowed' },
        estimated_canonical_bytes: 48_213,
        previewed_at: new Date(now).toISOString(),
        valid_until: new Date(now + 15 * 60 * 1000).toISOString(),
      }),
    })
  })
  return requests
}

function monitoringInstanceRoutes(vpsId: string, instances: { id: string; name: string }[]) {
  return {
    [apiRouteKey('GET', `/api/vps/${vpsId}/monitoring-instances?scope=current`)]: {
      status: 200,
      body: instances.map((instance) => ({ monitoring_instance_id: instance.id, display_name: instance.name })),
    },
  }
}

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 1024, height: 768 },
  { width: 390, height: 900 },
]) {
  test(`captured evidence stays pending and publishes with the revision at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile({
      ...recordDetailProfile({ populated: true, revisionSave: { evidenceSnapshotIds: ['evs_e2ethirdnight'] } }),
      ...monitoringInstanceRoutes(VPS_ID, [{ id: 'mi_e2ealpha', name: 'alpha 主机监控' }]),
    })
    const previews = await routeCapturePreviews(page, 'rec_e2e001')
    await page.setViewportSize(viewport)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001/edit')
    await page.getByRole('button', { name: '管理材料' }).click()
    const dialog = page.getByRole('dialog', { name: '材料与引用' })
    await dialog.getByRole('button', { name: '采集证据' }).click()

    // 记录主体只有 VPS：默认来源是它名下的监控实例。
    await expect(dialog.getByLabel('来源')).toHaveText(/VPS Alpha · alpha 主机监控/u)
    await expect(dialog.getByLabel('证据类型')).toHaveValue('monitoring.host')
    const generate = dialog.getByRole('button', { name: '生成预览' })
    await expectLocatorNotClipped(generate)
    await generate.click()
    const preview = dialog.getByRole('region', { name: '证据预览' })
    await expect(preview.getByText('数据完整')).toBeVisible()
    await expect(preview.getByText(/前发布有效/u)).toBeVisible()
    await expectNoDocumentOverflow(page)

    expect(Object.keys(previews[0]!).sort()).toEqual([...PREVIEW_KEYS])
    expect(previews[0]).toMatchObject({ record_id: 'rec_e2e001', kind: 'monitoring.host', source_type: 'monitoring_instance', source_id: 'mi_e2ealpha' })
    expect(previews[0]!.metrics).toEqual([...previews[0]!.metrics].sort())

    await preview.getByRole('button', { name: '加入记录' }).click()
    await expect(dialog.getByRole('status')).toHaveText(/已加入/u)
    await expect(dialog.getByText('待保存')).toBeVisible()
    await expect(dialog.getByRole('button', { name: '移除主机监控 · VPS Alpha · alpha 主机监控' })).toBeVisible()
    await expectNoDocumentOverflow(page)
    const axe = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
    expect(axe.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([])

    await page.keyboard.press('Escape')
    await page.getByLabel('保存原因').fill('补充第三晚监控证据')
    const revisionRequest = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === '/api/records/rec_e2e001/revisions')
    await page.getByRole('button', { name: '发布修订' }).click()
    expect((await revisionRequest).postDataJSON()).toMatchObject({
      evidence_items: [{ existing_snapshot_id: 'evs_e2ethirdnight' }, { capture_intent_id: 'evi_e2ecapture01' }],
    })
  })
}

test('a new record publishes captured evidence on the pre-allocated record id', async ({ api, page }) => {
  const draft = {
    draft_id: 'rdf_e2e_new',
    etag: 'rdt1_e2e_new',
    payload: {},
    version: 1,
    warning_at: '2026-08-21T10:00:00Z',
    created_at: '2026-08-20T10:00:00Z',
    updated_at: '2026-08-20T10:00:00Z',
    expires_at: '2026-08-22T10:00:00Z',
  }
  api.useProfile({
    ...recordDetailProfile(),
    ...authenticatedProfile({
      [apiRouteKey('GET', '/api/vps')]: { status: 200, body: [{ vps_id: 'vps_e2ebeta', display_name: 'VPS Beta' }] },
      ...monitoringInstanceRoutes('vps_e2ebeta', [{ id: 'mi_e2ebeta', name: 'beta 主机监控' }]),
      [apiRouteKey('POST', '/api/record-drafts')]: { status: 200, body: draft, expectedBodyKeys: ['payload'] },
      [apiRouteKey('PATCH', `/api/record-drafts/${draft.draft_id}`)]: {
        status: 200,
        body: { ...draft, etag: 'rdt2_e2e_new', version: 2 },
        expectedBodyKeys: ['payload'],
      },
      [apiRouteKey('POST', '/api/records')]: {
        status: 201,
        body: { record_id: 'rec_e2e001', revision_id: 'rrv_e2e001', revision_no: 1, created: true, replayed: false },
        expectedBodyKeys: ['draft_id', 'draft_etag', 'record_id', 'evidence_items'],
      },
    }),
  })
  const previews = await routeCapturePreviews(page, 'rec_e2e001')
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/records/new')
  await page.getByLabel('标题').fill('beta 对照观测')
  await page.getByRole('button', { name: '管理材料' }).click()
  const dialog = page.getByRole('dialog', { name: '材料与引用' })
  await dialog.getByRole('button', { name: '采集证据' }).click()

  // 新记录还没有主体：直接从其他 VPS 里选。
  await expect(dialog.getByLabel('来源')).toHaveValue('other')
  await dialog.getByLabel('VPS').selectOption('vps_e2ebeta')
  await dialog.getByLabel('监控实例').selectOption('mi_e2ebeta')
  await dialog.getByRole('button', { name: '生成预览' }).click()
  await dialog.getByRole('region', { name: '证据预览' }).getByRole('button', { name: '加入记录' }).click()
  expect(previews[0]).not.toHaveProperty('record_id')
  await expect(dialog.getByText('待保存')).toBeVisible()
  await page.keyboard.press('Escape')

  const createRequest = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === '/api/records')
  await page.getByRole('button', { name: '发布修订' }).click()
  expect((await createRequest).postDataJSON()).toEqual({
    draft_id: draft.draft_id,
    draft_etag: expect.any(String),
    record_id: 'rec_e2e001',
    evidence_items: [{ capture_intent_id: 'evi_e2ecapture01' }],
  })
  await expect(page).toHaveURL(/\/records\/rec_e2e001$/u)
})
