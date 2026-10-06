import AxeBuilder from '@axe-core/playwright'

import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { authenticatedProfile, recordDetailProfile } from './fixtures/profiles'
import { expectLocatorNotClipped, expectNoDocumentOverflow } from './support/geometry'

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'tablet', width: 1024, height: 768 },
  { name: 'mobile', width: 390, height: 900 },
] as const

for (const viewport of VIEWPORTS) {
  test(`record editor empty/new state stays operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(authenticatedProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/new')

    await expect(page.getByRole('heading', { name: '新建运维记录' })).toBeVisible()
    const title = page.getByLabel('标题')
    await title.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(title)
    await title.fill('第三晚观测')
    await expect(page.getByLabel('Markdown 源文')).toBeVisible()
    await expect(page.getByRole('button', { name: '发布修订' })).toBeVisible()
    await expectNoDocumentOverflow(page)
  })
}

test('record editor new state has no serious or critical accessibility violations', async ({ api, page }) => {
  api.useProfile(authenticatedProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/new')
  await expect(page.getByRole('heading', { name: '新建运维记录' })).toBeVisible()

  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])
})

for (const viewport of VIEWPORTS) {
  test(`record material drawer stays operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(authenticatedProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/new')
    await expect(page.getByRole('heading', { name: '新建运维记录' })).toBeVisible()

    const openMaterials = page.getByRole('button', { name: '管理材料' })
    await openMaterials.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(openMaterials)
    await openMaterials.click()

    const drawer = page.getByRole('dialog', { name: '材料与引用' })
    await expect(drawer).toBeVisible()
    await expect(page.getByText('当前修订没有可引用材料')).toBeVisible()
    await expect(page.getByRole('button', { name: '关闭' })).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(drawer).toHaveCount(0)
    await expect(openMaterials).toBeFocused()
    await expectNoDocumentOverflow(page)
  })
}

for (const viewport of VIEWPORTS) {
  test(`published record reading surface stays operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001')

    await expect(page.locator('.page__head').getByRole('heading', {
      name: '第三晚 TCP 观测',
      level: 1,
    })).toBeVisible()
    const preview = page.locator('[data-render-contract="houfeng_markdown/v1"]')
    await expect(preview).toBeVisible()
    await expect(preview.locator('pre code')).toContainText('mtr -rw 203.0.113.7')
    await expect(preview.locator('table td').first()).toHaveText('alpha')
    await expect(preview.locator('[data-ref-id="evs_e2ethirdnight"]')).toHaveClass(/card/u)
    await expect(page.getByText('引用已失效')).toHaveCount(0)
    await expectNoDocumentOverflow(page)
  })
}

for (const viewport of VIEWPORTS) {
  test(`record editor layout modes stay operable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001/edit')

    const source = page.getByLabel('Markdown 源文')
    await expect(source).toBeVisible()

    const layout = page.getByRole('toolbar', { name: '编辑布局' })
    await layout.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(layout)
    await layout.getByRole('button', { name: '预览' }).click()
    await expect(source).toHaveCount(0)
    await expect(page.locator('[data-render-contract]')).toBeVisible()

    await layout.getByRole('button', { name: '编辑' }).click()
    await expect(source).toBeVisible()
    await expect(page.locator('[data-render-contract]')).toHaveCount(0)

    await layout.getByRole('button', { name: '分栏' }).click()
    await expect(source).toBeVisible()
    await expect(page.locator('[data-render-contract]')).toBeVisible()
    await expectNoDocumentOverflow(page)
  })
}

for (const viewport of VIEWPORTS) {
  test(`record material drawer inserts a real reference at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile())
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001/edit')
    await expect(page.getByLabel('Markdown 源文')).toBeVisible()

    const openMaterials = page.getByRole('button', { name: '管理材料' })
    await openMaterials.scrollIntoViewIfNeeded()
    await openMaterials.click()

    const drawer = page.getByRole('dialog', { name: '材料与引用' })
    await expect(drawer).toBeVisible()
    await expect(drawer.getByText('evs_e2ethirdnight', { exact: true })).toBeVisible()
    await expect(drawer.getByText('att_e2emtrreport', { exact: true })).toBeVisible()

    const insertAttachment = drawer.getByRole('button', { name: '插入附件 att_e2emtrreport' })
    await expectLocatorNotClipped(insertAttachment)
    await insertAttachment.click()
    await expect(page.getByLabel('Markdown 源文')).toHaveValue(/houfeng-attachment:att_e2emtrreport/u)

    await page.keyboard.press('Escape')
    await expect(drawer).toHaveCount(0)
    await expectNoDocumentOverflow(page)
  })
}

test('a record the server could not model stays readable from source', async ({ api, page }) => {
  api.useProfile(recordDetailProfile({ renderModel: 'unsupported' }))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/rec_e2e001')

  const preview = page.locator('[data-render-contract="houfeng_markdown/v1-live"]')
  await expect(preview).toBeVisible()
  await expect(preview.getByRole('status')).toContainText('按源码渲染')
  // The nesting the server refused to model is exactly what the fallback must show.
  await expect(preview.locator('li ul li').first()).toHaveText('磁盘')
  await expectNoDocumentOverflow(page)
})

test('published record reading surface has no serious or critical accessibility violations', async ({ api, page }) => {
  api.useProfile(recordDetailProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/rec_e2e001')
  await expect(page.locator('.page__head').getByRole('heading', {
    name: '第三晚 TCP 观测',
    level: 1,
  })).toBeVisible()

  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])
})

test('record material drawer has no serious or critical accessibility violations', async ({ api, page }) => {
  api.useProfile(authenticatedProfile())
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/new')
  await page.getByRole('button', { name: '管理材料' }).click()
  await expect(page.getByRole('dialog', { name: '材料与引用' })).toBeVisible()

  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => (
    violation.impact === 'serious' || violation.impact === 'critical'
  )).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))).toEqual([])
})

for (const viewport of VIEWPORTS) {
  test(`populated record reading layout keeps document, aside and collaboration ordered at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile({ populated: true }))
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001')

    const document = page.getByRole('article', { name: '记录正文' })
    const aside = page.getByRole('complementary', { name: '记录信息' })
    const actionsHeading = page.getByRole('heading', { name: /^行动项/ })
    await expect(actionsHeading).toBeVisible()
    await expect(page.getByRole('navigation', { name: '正文大纲' })).toBeVisible()
    const table = page.getByRole('region', { name: '正文表格' })
    await expect(table).toHaveAttribute('tabindex', '0')
    if (viewport.width < 500) {
      // 七列宽表在窄屏只在自己的具名区域里横向滚动，键盘可以滚动它。
      await table.scrollIntoViewIfNeeded()
      expect(await table.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(true)
      await table.focus()
      await page.keyboard.press('ArrowRight')
      await expect.poll(() => table.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
    }
    await expect(page.getByRole('checkbox', { name: '已完成' })).toBeChecked()
    // 阅读态不展示草稿同步状态，也不出现指向自身的"阅读"入口。
    await expect(page.getByText('尚未创建草稿')).toHaveCount(0)
    await expect(page.getByRole('link', { name: '阅读' })).toHaveCount(0)

    const documentBox = await document.boundingBox()
    const asideBox = await aside.boundingBox()
    const actionsBox = await actionsHeading.boundingBox()
    if (!documentBox || !asideBox || !actionsBox) throw new Error('expected layout boxes')
    if (viewport.width >= 1200) {
      expect(asideBox.x).toBeGreaterThan(documentBox.x + documentBox.width)
      expect(documentBox.width).toBeGreaterThan(viewport.width * 0.45)
    } else {
      expect(asideBox.y).toBeGreaterThan(documentBox.y + documentBox.height)
      expect(actionsBox.y).toBeGreaterThan(asideBox.y + asideBox.height)
    }

    // 计数是紧凑徽标，不会被拉成整行。
    const count = actionsHeading.locator('.record-count')
    await expect(count).toHaveText('2')
    expect((await count.boundingBox())?.width ?? 999).toBeLessThan(40)

    await expect(page.getByLabel('行动标题')).toHaveCount(0)
    const addAction = page.getByRole('button', { name: '新增行动' })
    await addAction.scrollIntoViewIfNeeded()
    await addAction.click()
    const actionTitle = page.getByLabel('行动标题')
    await expect(actionTitle).toBeVisible()
    const submitAction = page.getByRole('button', { name: '添加行动' })
    await submitAction.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(submitAction)
    await page.getByRole('button', { name: '取消', exact: true }).click()
    await expect(actionTitle).toHaveCount(0)
    await expectNoDocumentOverflow(page)
  })
}

test('record export and import open as dialogs from the header', async ({ api, page }) => {
  api.useProfile(recordDetailProfile({ populated: true }))
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/records/rec_e2e001')

  await expect(page.getByRole('region', { name: '记录导出' })).toHaveCount(0)
  const exportButton = page.getByRole('button', { name: '导出', exact: true })
  await exportButton.click()
  const dialog = page.getByRole('dialog', { name: '导出记录' })
  await expect(dialog.getByRole('region', { name: '记录导出' })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '预览导出' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(exportButton).toBeFocused()

  await page.getByRole('button', { name: '导入', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '导入记录' }).getByRole('region', { name: '记录导入' })).toBeVisible()
})

for (const viewport of VIEWPORTS) {
  test(`historical revision keeps restore and folded body diff reachable at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile({ populated: true }))
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001/revisions/rrv_e2e001')

    await expect(page.getByText('历史修订 #2')).toBeVisible()
    await expect(page.getByRole('link', { name: '当前版本' })).toHaveAttribute('href', '/records/rec_e2e001')
    const diff = page.getByRole('region', { name: '与当前版本的差异' })
    await expect(diff).toBeVisible()
    const hunks = diff.getByRole('region', { name: '正文差异' })
    await expect(hunks).toBeHidden()
    await diff.locator('summary').click()
    await expect(hunks).toBeVisible()

    const restore = page.getByRole('button', { name: '恢复为新修订' })
    await restore.scrollIntoViewIfNeeded()
    await expectLocatorNotClipped(restore)
    await expect(page.getByLabel('恢复原因')).toHaveValue('恢复历史修订')
    await expectNoDocumentOverflow(page)
  })
}

for (const viewport of VIEWPORTS) {
  test(`record editor keeps attributes beside or below the editor at ${viewport.width}x${viewport.height}`, async ({ api, page }) => {
    api.useProfile(recordDetailProfile({ populated: true }))
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/records/rec_e2e001/edit')

    const editor = page.getByRole('region', { name: '正文编辑' })
    const aside = page.getByRole('complementary', { name: '记录属性' })
    await expect(page.getByLabel('Markdown 源文')).toBeVisible()
    await expect(aside.getByLabel('记录类型')).toHaveValue('troubleshooting')
    await expect(aside.getByRole('checkbox', { name: '周衡' })).toBeChecked()
    // 源文代码块里的 "# 复现丢包" 不进入大纲。
    await expect(aside.getByRole('navigation', { name: '正文大纲' }).getByText('复现丢包')).toHaveCount(0)
    const editorBox = await editor.boundingBox()
    const asideBox = await aside.boundingBox()
    if (!editorBox || !asideBox) throw new Error('expected layout boxes')
    if (viewport.width >= 1200) expect(asideBox.x).toBeGreaterThan(editorBox.x + editorBox.width)
    else expect(asideBox.y).toBeGreaterThan(editorBox.y + editorBox.height)
    await expectLocatorNotClipped(page.getByRole('button', { name: '发布修订' }))
    await expectNoDocumentOverflow(page)
  })
}

test('publishing an edited record keeps every existing evidence snapshot in order', async ({ api, page }) => {
  // 修订的证据是请求里的有序全集；漏传会让新修订丢掉全部证据。
  const evidenceSnapshotIds = ['evs_e2ethirdnight', 'evs_e2efirstnight'] as const
  api.useProfile(recordDetailProfile({ revisionSave: { evidenceSnapshotIds } }))
  await page.goto('/records/rec_e2e001/edit')
  await page.getByLabel('保存原因').fill('补充第三晚复现结论')

  const revisionRequest = page.waitForRequest((request) =>
    request.method() === 'POST' && new URL(request.url()).pathname === '/api/records/rec_e2e001/revisions')
  await page.getByRole('button', { name: '发布修订' }).click()
  const body = (await revisionRequest).postDataJSON() as { evidence_items: unknown }
  expect(body.evidence_items).toEqual(evidenceSnapshotIds.map((id) => ({ existing_snapshot_id: id })))
  await expect.poll(() => api.requestCount('POST', '/api/records/rec_e2e001/revisions')).toBe(1)
})

test('re-publishing after resolving a revision conflict rebases the same draft onto the new head', async ({ api, page }) => {
  const base = recordDetailProfile({ revisionSave: { evidenceSnapshotIds: ['evs_e2ethirdnight'] } })
  const recordKey = apiRouteKey('GET', '/api/records/rec_e2e001')
  const loaded = base[recordKey]!.body as { current: Record<string, unknown> }
  // 别人已在服务端发布了新修订：冲突后读取到的新头。
  const advanced = {
    ...loaded,
    current_revision_id: 'rrv_e2eserver',
    lock_version: 5,
    current: { ...loaded.current, revision_id: 'rrv_e2eserver', revision_no: 3, title: '服务端已更新', evidence_snapshot_ids: ['evs_e2eserver'] },
  }
  const staleDraft = base[apiRouteKey('POST', '/api/record-drafts')]!.body as Record<string, unknown>
  const draftPath = `/api/record-drafts/${String(staleDraft.draft_id)}`
  api.useProfile(base)
  await page.goto('/records/rec_e2e001/edit')
  await page.getByLabel('保存原因').fill('补充第三晚复现结论')

  api.useProfile({
    ...base,
    [recordKey]: { status: 200, body: advanced },
    [apiRouteKey('POST', '/api/records/rec_e2e001/revisions')]: {
      status: 409,
      body: {
        code: 'record_revision_conflict',
        message: 'record revision changed',
        field_errors: [],
        recovery: { server_revision_id: 'rrv_e2eserver', server_lock_version: 5, server_authorization_epoch: 2, draft: staleDraft },
      },
      expectedBodyKeys: ['draft_id', 'draft_etag', 'base_revision_id', 'lock_version', 'authorization_epoch', 'evidence_items'],
    },
  })
  await page.getByRole('button', { name: '发布修订' }).click()
  const resolver = page.getByRole('alertdialog', { name: '修订冲突' })
  await expect(resolver).toBeVisible()

  api.useProfile({
    ...base,
    [recordKey]: { status: 200, body: advanced },
    [apiRouteKey('PATCH', draftPath)]: {
      status: 200,
      body: { ...staleDraft, base_revision_id: 'rrv_e2eserver', etag: 'rdt2_e2e_rebased', version: 2 },
      expectedBodyKeys: ['payload', 'base_revision_id'],
    },
  })
  // 解决冲突后自动保存也会触发改基准，监听必须在解决之前注册。
  const rebaseRequest = page.waitForRequest((request) =>
    request.method() === 'PATCH' && new URL(request.url()).pathname === draftPath)
  const revisionRequest = page.waitForRequest((request) =>
    request.method() === 'POST' && new URL(request.url()).pathname === '/api/records/rec_e2e001/revisions')
  await resolver.getByRole('button', { name: '全部保留本地' }).click()
  await expect(resolver).toHaveCount(0)
  await page.getByRole('button', { name: '发布修订' }).click()

  // 同一份草稿（附件归属不变）带 If-Match 原子改到新头，再按新头的锁版本与证据发布。
  const rebase = await rebaseRequest
  expect(rebase.headers()['if-match']).toBe(String(staleDraft.etag))
  expect(rebase.postDataJSON()).toMatchObject({ base_revision_id: 'rrv_e2eserver' })
  expect((await revisionRequest).postDataJSON()).toMatchObject({
    draft_id: staleDraft.draft_id,
    draft_etag: 'rdt2_e2e_rebased',
    base_revision_id: 'rrv_e2eserver',
    lock_version: 5,
    evidence_items: [{ existing_snapshot_id: 'evs_e2eserver' }],
  })
  await expect.poll(() => api.requestCount('POST', '/api/records/rec_e2e001/revisions')).toBe(2)
  // 只有冲突前的那次创建：改基准不再丢弃重建草稿。
  expect(api.requestCount('POST', '/api/record-drafts')).toBe(1)
})
