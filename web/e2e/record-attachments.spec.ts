import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { RECORD_ATTACHMENT_METADATA, recordDetailProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'
import { chartPng } from './support/png'

const ZIP_BYTES = Buffer.from('PK\u0003\u0004 beta node logs')
const UPLOAD = {
  upload_id: 'aup_e2ebetazip',
  attachment_id: 'att_e2ebetazip',
}
const UPLOADED_METADATA = {
  attachment_id: UPLOAD.attachment_id,
  display_name: 'beta-节点日志.zip',
  media_type: 'application/zip',
  size_bytes: ZIP_BYTES.length,
  preview_available: false,
}

function uploadRoutes(state: 'quarantined' | 'available') {
  return {
    [apiRouteKey('POST', '/api/attachment-uploads')]: {
      status: 201,
      body: {
        ...UPLOAD,
        state: 'created',
        expires_at: '2026-08-20T10:15:00Z',
        quota: { logical_bytes: 0, reserved_bytes: ZIP_BYTES.length, physical_bytes: 0, effective_record_bytes: ZIP_BYTES.length, project_warning: false },
        target: {
          transport: 'local',
          upload_url: `/api/attachment-uploads/${UPLOAD.upload_id}/content`,
          method: 'PUT',
          required_headers: ['X-Houfeng-Draft-ID', 'X-Content-SHA256'],
        },
      },
      expectedBodyKeys: ['draft_id', 'display_name', 'media_type', 'declared_size_bytes'],
    },
    [apiRouteKey('PUT', `/api/attachment-uploads/${UPLOAD.upload_id}/content`)]: {
      status: 200,
      body: { ...UPLOAD, size_bytes: ZIP_BYTES.length, sha256: '0'.repeat(64) },
      expectRawBodyBytes: ZIP_BYTES.length,
    },
    [apiRouteKey('POST', `/api/attachment-uploads/${UPLOAD.upload_id}/complete`)]: {
      status: 202,
      body: { ...UPLOAD, state: 'quarantined', quota: { logical_bytes: 0, reserved_bytes: 0, physical_bytes: 0, effective_record_bytes: 0, project_warning: false } },
      expectedBodyKeys: ['draft_id'],
    },
    [apiRouteKey('GET', `/api/attachments/${UPLOAD.attachment_id}`)]: {
      status: 200,
      body: { ...UPLOADED_METADATA, state },
    },
  }
}

test('uploaded attachments pass the safety check before joining the draft', async ({ api, page }) => {
  const base = recordDetailProfile({ populated: true, revisionSave: { evidenceSnapshotIds: ['evs_e2ethirdnight'] } })
  api.useProfile({ ...base, ...uploadRoutes('quarantined') })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/records/rec_e2e001/edit')
  await page.getByRole('button', { name: '管理材料' }).click()
  const dialog = page.getByRole('dialog', { name: '材料与引用' })
  await expect(dialog).toBeVisible()

  const uploadRequest = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === '/api/attachment-uploads')
  const contentRequest = page.waitForRequest((request) => request.method() === 'PUT')
  await dialog.locator('input[type=file]').setInputFiles([
    { name: 'beta-节点日志.zip', mimeType: '', buffer: ZIP_BYTES },
    { name: 'tcpdump-alpha.pcap', mimeType: 'application/vnd.tcpdump.pcap', buffer: Buffer.from('pcap') },
  ])

  // 不支持的类型在本地拦下，不向后端预留；支持的类型按后端接受的媒体类型声明。
  await expect(dialog.getByText('不支持的文件类型')).toBeVisible()
  expect((await uploadRequest).postDataJSON()).toEqual({
    draft_id: 'rdf_e2e_revise',
    display_name: 'beta-节点日志.zip',
    media_type: 'application/zip',
    declared_size_bytes: ZIP_BYTES.length,
  })
  const put = await contentRequest
  expect(put.headers()['x-houfeng-draft-id']).toBe('rdf_e2e_revise')
  expect(put.headers()['x-content-sha256']).toMatch(/^[0-9a-f]{64}$/u)
  await expect(dialog.getByText('安全检查中')).toBeVisible()
  await expect(page.getByText('附件上传中，完成后可发布')).toBeVisible()
  await expect(page.getByRole('button', { name: '发布修订' })).toBeDisabled()
  const axe = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
  expect(axe.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([])

  const draftPatch = page.waitForRequest((request) => request.method() === 'PATCH' && new URL(request.url()).pathname === '/api/record-drafts/rdf_e2e_revise')
  api.useProfile({ ...base, ...uploadRoutes('available') })
  await expect(dialog.getByRole('button', { name: '插入beta-节点日志.zip' })).toBeEnabled()
  await expect(dialog.getByText('安全检查中')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '发布修订' })).toBeEnabled()
  const patched = (await draftPatch).postDataJSON() as { payload: { attachment_ids: string[] } }
  expect(patched.payload.attachment_ids).toEqual([RECORD_ATTACHMENT_METADATA.attachment_id, UPLOAD.attachment_id])
})

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 1024, height: 768 },
  { width: 390, height: 900 },
]) {
  test(`reading page previews and downloads attachments at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    const base = recordDetailProfile({ populated: true })
    const textKey = apiRouteKey('GET', `/api/attachments/${RECORD_ATTACHMENT_METADATA.attachment_id}`)
    api.useProfile({
      ...base,
      [apiRouteKey('GET', `/api/attachments/${RECORD_ATTACHMENT_METADATA.attachment_id}/content?variant=preview`)]: {
        status: 200,
        body: null,
        headers: { 'Content-Type': 'text/plain; charset=utf-8' },
        rawBody: 'HOST: alpha → 203.0.113.7\n  2. 198.51.100.17  3.0%  182.4 ms\n',
      },
      [apiRouteKey('GET', `/api/attachments/${RECORD_ATTACHMENT_METADATA.attachment_id}/content`)]: {
        status: 200,
        body: null,
        headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Content-Disposition': 'attachment' },
        rawBody: 'full mtr report\n',
      },
    })
    await page.setViewportSize(viewport)
    await page.goto('/records/rec_e2e001')
    const aside = page.getByRole('complementary', { name: '记录信息' })
    await expect(aside.getByText('mtr-第三晚-alpha.txt')).toBeVisible()
    await expect(aside.getByText(/^文本 · /u)).toBeVisible()

    await aside.getByRole('button', { name: '预览mtr-第三晚-alpha.txt' }).click()
    const preview = page.getByRole('dialog', { name: 'mtr-第三晚-alpha.txt' })
    await expect(preview.getByText(/198\.51\.100\.17/u)).toBeVisible()
    await expectNoDocumentOverflow(page)
    await page.keyboard.press('Escape')
    await expect(preview).toHaveCount(0)

    const download = page.waitForEvent('download')
    await aside.getByRole('button', { name: '下载mtr-第三晚-alpha.txt' }).click()
    expect((await download).suggestedFilename()).toBe('mtr-第三晚-alpha.txt')

    // 图片与 PDF 的安全预览是后端渲染的 PNG，直接以同源地址显示。
    api.useProfile({
      ...base,
      [textKey]: { status: 200, body: { ...RECORD_ATTACHMENT_METADATA, display_name: 'alpha-重传曲线.png', media_type: 'image/png' } },
      [apiRouteKey('GET', `/api/attachments/${RECORD_ATTACHMENT_METADATA.attachment_id}/content?variant=preview`)]: {
        status: 200,
        body: null,
        headers: { 'Content-Type': 'image/png' },
        rawBody: chartPng(),
      },
    })
    await page.reload()
    await aside.getByRole('button', { name: '预览alpha-重传曲线.png' }).click()
    const image = page.getByRole('dialog', { name: 'alpha-重传曲线.png' }).getByRole('img', { name: 'alpha-重传曲线.png 的安全预览' })
    await expect(image).toBeVisible()
    await expect.poll(() => image.evaluate((element) => (element as HTMLImageElement).naturalWidth)).toBeGreaterThan(0)
    await expectNoDocumentOverflow(page)
  })
}
