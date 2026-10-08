import type { Locator } from '@playwright/test'

import { vpsAssetFixture } from '../src/pages/dashboard/dashboardTestFixtures'
import { expect, test } from './fixtures'
import { apiRouteKey } from './fixtures/contracts'
import { coreRouteProfile } from './fixtures/profiles'
import { expectNoDocumentOverflow } from './support/geometry'

const DAY = 24 * 60 * 60 * 1000
const names = ['Tokyo Edge', 'LA-Bench', 'Frankfurt-02', 'Seoul-Proxy', 'SJC-Build', 'Paris-Git', 'NYC-Monitor', 'HK-Relay', 'SG-Backup']
const rows = names.map((name, index) => vpsAssetFixture({
  vps_id: `vps_${String(index + 1).padStart(3, '0')}`,
  display_name: name,
  provider_name: ['Example Cloud', 'RackNerd', 'Hetzner', 'Vultr'][index % 4]!,
  country: ['JP', 'US', 'DE', 'KR', 'US', 'FR', 'US', 'HK', 'SG'][index]!,
  renewal_decision: index === 2 ? 'cancel' : index === 4 ? 'unreviewed' : 'keep',
  note: index === 2 ? '迁移计划：年底前把服务切到 SJC-Build。' : '',
}))

// 浏览器时钟固定在上海时间 2026-09-29 12:00（playwright.config 的 timezoneId），
// 续费日按同一日历生成，避免 Node 与浏览器时区不同或跨午夜导致天数漂移。
const FIXED_NOW = new Date('2026-09-29T04:00:00Z')

function calendarDate(offsetDays: number): string {
  return new Date(FIXED_NOW.getTime() + offsetDays * DAY).toISOString().slice(0, 10)
}

// 前 7 台有订阅，续费日由近到远；后 2 台无订阅，用来验证“无续费日”计数。
const subscriptions = rows.slice(0, 7).map((row, index) => ({
  subscription_id: `sub_${index}`, vps_id: row.vps_id, price: 5 + index * 4, currency: 'USD',
  billing_cycle: 'monthly', billing_months: 1, billing_period_unit: 'month', billing_period_length: 1,
  monthly_price: 5 + index * 4, monthly_price_base: (5 + index * 4) * 7, yearly_price_base: (5 + index * 4) * 84,
  base_currency: 'CNY', exchange_rate: 7, exchange_rate_date: calendarDate(-1), exchange_rate_status: 'fresh',
  budget_status: 'ok', next_reminder_at: null, started_at: '2026-01-01', renew_at: calendarDate(index * 12 + 2),
  auto_renew: true, auto_renew_cancelled: false, renewal_mode: 'auto', status: 'active', payment_method: 'card',
  display_name: `${row.display_name} 订阅`, cost_category: 'compute', labels: [], note: '',
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
}))

async function box(locator: Locator) {
  const rect = await locator.boundingBox()
  if (!rect) throw new Error('element is not rendered')
  return rect
}

const REM = 16

for (const width of [1920, 1440, 1100]) {
  test(`ledger inspector summarizes the list and groups selected facts on desktop (${width})`, async ({ api, page }) => {
    api.useProfile({
      ...coreRouteProfile('/vps'),
      [apiRouteKey('GET', '/api/vps')]: { status: 200, body: rows },
      [apiRouteKey('GET', '/api/subscriptions?sort=renew_at&order=asc')]: { status: 200, body: subscriptions },
    })
    await page.clock.setFixedTime(FIXED_NOW)
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/vps?workspace=ledger')
    const inspector = page.getByRole('region', { name: 'VPS 检查器' })
    await expect(inspector.getByRole('heading', { name: '当前列表' })).toBeVisible()
    // 布局断点作用于检查器自身宽度（容器查询），按实测宽度判断预期，而不是按视口猜测。
    const inspectorWidth = await inspector.evaluate((element) => element.clientWidth)

    // 续费排期有界：最近 5 台可点选，其余以计数说明。
    const schedule = inspector.getByRole('region', { name: '续费排期' })
    await expect(schedule.getByRole('listitem')).toHaveCount(5)
    await expect(schedule).toContainText('另有 2 台排在之后 · 2 台无续费日')
    await expect(schedule.getByRole('listitem').first()).toContainText('Tokyo Edge')
    await expect(schedule.getByRole('listitem').first()).toContainText('2 天后')
    // 汇总只做计数与日期，不解读金额。
    await expect(inspector).not.toContainText('USD')

    // 计数分布：≥28rem 两栏，≥60rem 四栏同一行；不在检查器里产生横向溢出。
    expect(inspectorWidth).toBeGreaterThanOrEqual(28 * REM)
    const counts = await Promise.all(['续费意向', '用途', '服务商', '地区'].map((name) => box(inspector.getByRole('region', { name }))))
    expect(Math.abs(counts[0]!.y - counts[1]!.y)).toBeLessThanOrEqual(1)
    expect(counts[1]!.x).toBeGreaterThan(counts[0]!.x + counts[0]!.width)
    const countRows = new Set(counts.map((rect) => Math.round(rect.y))).size
    expect(countRows).toBe(inspectorWidth >= 60 * REM ? 1 : 2)
    await expect(inspector.getByRole('region', { name: '地区' })).toContainText('其余 3 类')

    await schedule.getByRole('button', { name: /Frankfurt-02/ }).click()
    await expect(page).toHaveURL((url) => url.searchParams.get('selected') === 'vps_003')
    await expect(page.getByRole('button', { name: '选择 Frankfurt-02', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect(inspector.getByRole('heading', { name: 'Frankfurt-02' })).toBeVisible()
    await expect(inspector).toContainText('已决定不续费，请核对服务商自动续费')

    // 选中后：详情入口在身份行（首屏可见）。
    const [title, action] = await Promise.all([
      box(inspector.getByRole('heading', { name: 'Frankfurt-02' })),
      box(inspector.getByRole('link', { name: '打开 VPS 详情' })),
    ])
    expect(action.y).toBeLessThan(title.y + title.height)
    const [identity, business, evidence, note] = await Promise.all([
      box(inspector.getByRole('region', { name: '资产身份' })),
      box(inspector.getByRole('region', { name: '经营与续费' })),
      box(inspector.getByRole('region', { name: '监控与证据' })),
      box(inspector.getByRole('region', { name: '备注' })),
    ])
    if (inspectorWidth >= 44 * REM) {
      // 宽检查器：两栏排布，经营组跨两行，监控证据紧跟资产身份，备注通栏。
      expect(Math.abs(identity.y - business.y)).toBeLessThanOrEqual(1)
      expect(business.x).toBeGreaterThan(identity.x + identity.width)
      expect(Math.abs(evidence.x - identity.x)).toBeLessThanOrEqual(1)
      expect(evidence.y).toBeLessThan(business.y + business.height)
      expect(note.width).toBeGreaterThan(business.x + business.width - identity.x - 2)
    } else {
      // 窄检查器：按阅读顺序上下堆叠。
      expect(business.y).toBeGreaterThanOrEqual(identity.y + identity.height)
      expect(evidence.y).toBeGreaterThanOrEqual(business.y + business.height)
      expect(note.y).toBeGreaterThanOrEqual(evidence.y + evidence.height)
    }

    expect(await inspector.evaluate((element) => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1)
    await expectNoDocumentOverflow(page)
  })
}
