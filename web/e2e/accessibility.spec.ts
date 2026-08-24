import AxeBuilder from '@axe-core/playwright'
import type { AssetDecisionScenarioTemplateDetail } from '../src/lib/types'

import { expect, test } from './fixtures'
import {
  comparisonWorkbenchHref,
  comparisonWorkbenchProfile,
  coreRouteProfile,
  dashboardProfile,
  subjectActivityProfile,
  vpsOverviewPartialFixture,
  vpsOverviewProfile,
} from './fixtures/profiles'
import { apiRouteKey } from './fixtures/contracts'
import { expectNoDocumentOverflow } from './support/geometry'

const AXE_SURFACES = [
  { name: 'AppShell and Dashboard', path: '/', heading: /^工作台$/ },
  { name: 'Settings', path: '/settings', heading: /^系统设置$/ },
  { name: 'VPS', path: '/vps', heading: /^VPS 资产$/ },
  { name: 'Asset Decisions', path: '/asset-decisions', heading: /^资产组合决策$/ },
  { name: 'Command Audit', path: '/command-audit', heading: /^命令审计$/ },
  { name: 'Record Inbox', path: '/record-inbox', heading: /^记录协作收件箱$/ },
] as const

for (const surface of AXE_SURFACES) {
  test(`${surface.name} has no serious or critical axe violations`, async ({ api, page }) => {
    api.useProfile(coreRouteProfile(surface.path))
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto(surface.path)
    await expect(page.getByRole('heading', { name: surface.heading })).toBeVisible()
    await page.evaluate(() => document.fonts.ready)

    const result = await new AxeBuilder({ page }).analyze()
    const blocking = result.violations
      .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
      .map((violation) => ({
        id: violation.id,
        impact: violation.impact,
        description: violation.description,
        targets: violation.nodes.map((node) => node.target),
      }))

    expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  })
}

test('VPS partial freshness has no blocking axe violations and exposes keyboard retry', async ({ api, page }) => {
  api.useProfile(vpsOverviewProfile({ overview: vpsOverviewPartialFixture() }))
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/vps/vps_001')
  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()

  const retry = page.getByRole('button', { name: '重试 IP 质量' })
  await retry.focus()
  await expect(retry).toBeFocused()
  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({ id: violation.id, targets: violation.nodes.map((node) => node.target) }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
})

test('Command Audit keeps output metadata-only and owns narrow-screen table scrolling and dialog focus', async ({
  api,
  page,
}) => {
  api.useProfile(coreRouteProfile('/command-audit'))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/command-audit')

  await expect(page.getByRole('heading', { name: '命令审计', exact: true })).toBeVisible()
  const results = page.getByRole('region', { name: '审计记录' })
  await expect(results).toBeVisible()
  const geometry = await results.evaluate((element) => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
  }))
  expect(geometry.scrollWidth).toBeGreaterThan(geometry.clientWidth + 1)
  await expectNoDocumentOverflow(page)

  await results.focus()
  await expect(results).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => results.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)

  await page.getByRole('button', { name: '展开 2 个事件' }).click()
  await expect(page.getByRole('region', { name: 'act_e2e_command_audit 原始审计事件' })).toBeVisible()
  for (const forbidden of [
    'COMMAND_AUDIT_STDOUT_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_STDERR_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_DETAILS_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_EVENT_OUTPUT_SHOULD_NOT_RENDER',
    'COMMAND_AUDIT_EVENT_DETAILS_SHOULD_NOT_RENDER',
  ]) {
    await expect(page.getByText(forbidden, { exact: true })).toHaveCount(0)
  }

  const advanced = page.getByRole('button', { name: '高级筛选' })
  await advanced.focus()
  await advanced.click()
  const dialog = page.getByRole('dialog', { name: '命令审计高级筛选' })
  await expect(dialog).toBeVisible()
  const close = dialog.getByRole('button', { name: '关闭' })
  await expect(close).toBeFocused()
  const apply = dialog.getByRole('button', { name: '应用高级筛选' })
  await apply.focus()
  await page.keyboard.press('Tab')
  await expect(close).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(advanced).toBeFocused()
})

test('skip link moves real keyboard focus to the main landmark', async ({ api, page }) => {
  api.useProfile(dashboardProfile())
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '工作台', exact: true })).toBeVisible()

  await page.keyboard.press('Tab')
  const skipLink = page.getByRole('link', { name: '跳到主内容' })
  await expect(skipLink).toBeFocused()
  await page.keyboard.press('Enter')

  await expect(page.locator('main#main-content')).toBeFocused()
})

test('Settings tabs keep one tab stop and move focus only after the panel commits', async ({
  api,
  page,
}) => {
  api.useProfile(coreRouteProfile('/settings'))
  await page.goto('/settings')

  const tablist = page.getByRole('tablist', { name: '系统设置分区' })
  const appearance = tablist.getByRole('tab', { name: '外观' })
  const notification = tablist.getByRole('tab', { name: '通知' })
  const advanced = tablist.getByRole('tab', { name: '高级' })
  await expect(appearance).toHaveAttribute('tabindex', '0')
  await expect(tablist.getByRole('tab', { selected: false })).toHaveCount(4)

  await appearance.focus()
  await page.keyboard.press('ArrowRight')
  await expect(notification).toBeFocused()
  await expect(notification).toHaveAttribute('aria-selected', 'true')
  await expect(appearance).toHaveAttribute('tabindex', '-1')

  const panelID = await notification.getAttribute('aria-controls')
  if (!panelID) throw new Error('selected Settings tab must own a tabpanel id')
  const panel = page.locator(`#${panelID}`)
  await expect(panel).toHaveAttribute('role', 'tabpanel')
  await expect(panel).toHaveAttribute('aria-labelledby', await notification.getAttribute('id') ?? '')

  await page.keyboard.press('End')
  await expect(advanced).toBeFocused()
  await expect(advanced).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Home')
  await expect(appearance).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(advanced).toBeFocused()
})

test('user menu supports arrow navigation, Escape restore, and native Tab exit', async ({
  api,
  page,
}) => {
  api.useProfile(dashboardProfile())
  await page.goto('/')

  const trigger = page.getByRole('button', { name: 'e2e-admin 用户菜单' })
  await trigger.focus()
  await page.keyboard.press('ArrowDown')
  const menu = page.getByRole('menu')
  const changePassword = menu.getByRole('menuitem', { name: '修改密码' })
  const logout = menu.getByRole('menuitem', { name: '退出登录' })
  await expect(changePassword).toBeFocused()

  await page.keyboard.press('ArrowDown')
  await expect(logout).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(trigger).toBeFocused()

  await page.keyboard.press('ArrowDown')
  await expect(changePassword).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(menu).toHaveCount(0)
  await expect(trigger).not.toBeFocused()
  await expect(page.locator('body')).not.toBeFocused()
})

const CUSTOM_TEMPLATE = {
  template_id: 'adt_e2e_primary_standby',
  builtin: false,
  status: 'active',
  scenario: 'primary_standby',
  title: 'E2E 主备模板',
  goal: '验证嵌套弹层焦点合同',
  note: '只用于浏览器 fixture',
  member_count: 0,
  created_at: '2026-07-10T06:00:00Z',
  updated_at: '2026-07-10T06:00:00Z',
  archived_at: null,
  members: [],
} satisfies AssetDecisionScenarioTemplateDetail

test('nested Modal closes one layer per Escape and preserves focus and body lock', async ({
  api,
  page,
}) => {
  api.useProfile({
    ...coreRouteProfile('/asset-decisions'),
    [apiRouteKey('GET', '/api/asset-decisions/scenario-templates')]: {
      status: 200,
      body: [CUSTOM_TEMPLATE],
    },
    [apiRouteKey('GET', `/api/asset-decisions/scenario-templates/${CUSTOM_TEMPLATE.template_id}`)]: {
      status: 200,
      body: CUSTOM_TEMPLATE,
    },
  })
  await page.goto('/asset-decisions')

  await page.getByRole('button', { name: '场景与组合' }).click()
  const useTemplate = page.getByRole('button', { name: '使用模板' })
  await expect(useTemplate).toBeVisible()
  await useTemplate.focus()
  await useTemplate.click()

  const parent = page.locator('[role="dialog"][aria-label="资产决策场景模板详情"]')
  await expect(parent).toBeVisible()
  await parent.getByRole('tab', { name: '状态' }).click()
  const archive = parent.getByRole('button', { name: '归档模板' })
  await archive.focus()
  await archive.click()

  const child = page.getByRole('alertdialog', { name: '确认归档模板' })
  await expect(child).toBeVisible()
  await expect(parent).toHaveAttribute('aria-hidden', 'true')
  await expect(parent).toHaveAttribute('inert', '')
  await expect.poll(() => page.evaluate(() => document.body.style.overflow)).toBe('hidden')

  const childButtons = child.getByRole('button')
  const firstChildButton = childButtons.first()
  const lastChildButton = childButtons.last()
  await lastChildButton.focus()
  await page.keyboard.press('Tab')
  await expect(firstChildButton).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(child).toHaveCount(0)
  await expect(parent).toBeVisible()
  await expect(parent).not.toHaveAttribute('inert', '')
  await expect(archive).toBeFocused()
  await expect.poll(() => page.evaluate(() => document.body.style.overflow)).toBe('hidden')

  await page.keyboard.press('Escape')
  await expect(parent).toHaveCount(0)
  await expect(useTemplate).toBeFocused()
  await expect.poll(() => page.evaluate(() => document.body.style.overflow)).not.toBe('hidden')
  await expect(page).toHaveURL('/asset-decisions')
})

test('VPS 概览 has no serious or critical axe violations', async ({ api, page }) => {
  api.useProfile(vpsOverviewProfile())
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/vps/vps_001')
  await expect(page.getByRole('heading', { name: 'Tokyo Edge' })).toBeVisible()
  await page.evaluate(() => document.fonts.ready)

  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      description: violation.description,
      targets: violation.nodes.map((node) => node.target),
    }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  await expectNoDocumentOverflow(page)
})

test('单主体时间线 has no serious or critical axe violations', async ({ api, page }) => {
  api.useProfile(subjectActivityProfile())
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/vps/vps_001/activity')
  await expect(page.getByText('E2E 时间线条目')).toBeVisible()
  await page.evaluate(() => document.fonts.ready)

  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      description: violation.description,
      targets: violation.nodes.map((node) => node.target),
    }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  await expectNoDocumentOverflow(page)
})

test('横向比较工作台 has no serious or critical axe violations', async ({ api, page }) => {
  api.useProfile(comparisonWorkbenchProfile({ mode: 'host-partial' }))
  await page.setViewportSize({ width: 390, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto(comparisonWorkbenchHref({
    mode: 'fixed',
    items: [{ snapshot_id: 'evs_cmpleft' }, { snapshot_id: 'evs_cmpright' }],
    baseline: 0,
    alignment: 'actual_coverage',
    tolerance_seconds: 60,
    kind: 'monitoring.host/v1',
    metric: 'cpu_usage_pct',
  }))
  await expect(page.getByRole('heading', { name: '可比性审查' })).toBeVisible()
  await page.evaluate(() => document.fonts.ready)

  const result = await new AxeBuilder({ page }).analyze()
  const blocking = result.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      description: violation.description,
      targets: violation.nodes.map((node) => node.target),
    }))
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
  await expectNoDocumentOverflow(page)
})
