import { describe, expect, it } from 'vitest'

import type { StateChangeEventRecord, SubscriptionRenewalQueueItem } from '../../lib/types'
import {
  DASHBOARD_PANEL_LIMIT,
  RENEWAL_QUEUE_CAP,
  buildRecentActivity,
  buildRenewalPanel,
  calendarDaysBetween,
  incidentTrend,
  notificationChannelGap,
  trendTotal,
  utcCalendarDate,
} from './dashboardPanels'
import { remoteError, remoteLoading, remoteSuccess } from './dashboardRemoteState'
import { dashboardOverviewFixture, subscriptionOverviewFixture } from './dashboardTestFixtures'

// 续费“今天”按 UTC 日计算，与后端窗口同源；UTC 正午在任何测试机时区下都是 2026-09-29。
const NOW = Date.parse('2026-09-29T12:00:00Z')

function event(index: number, overrides: Partial<StateChangeEventRecord> = {}): StateChangeEventRecord {
  return {
    event_id: `ev_${index}`,
    incident_id: `inc_${index}`,
    incident_class: 'heartbeat',
    object_type: 'monitoring_instance',
    object_id: 'mi_001',
    event_type: 'incident_started',
    severity: '告警',
    summary: `摘要 ${index}`,
    created_at: new Date(NOW - index * 60_000).toISOString(),
    ...overrides,
  }
}

function renewal(
  index: number,
  renewAt: string | null,
  exchangeRateStatus: SubscriptionRenewalQueueItem['exchange_rate_status'] = 'fresh',
): SubscriptionRenewalQueueItem {
  return {
    subscription_id: `sub_${index}`,
    vps_id: `vps_${index}`,
    vps_display_name: `VPS ${index}`,
    display_name: `Plan ${index}`,
    provider_name: 'Example Cloud',
    renew_at: renewAt,
    monthly_price_base: 10 + index,
    base_currency: 'CNY',
    currency: 'USD',
    renewal_decision: 'keep',
    lifecycle_status: 'active',
    exchange_rate_status: exchangeRateStatus,
  }
}

describe('buildRecentActivity', () => {
  it('keeps the newest bounded preview with labelled tones', () => {
    const overview = dashboardOverviewFixture({
      recent_events: [
        event(6),
        event(0, { severity: '严重' }),
        event(1, { event_type: 'incident_recovered', severity: '正常' }),
        event(2),
        event(3),
        event(4),
        event(5),
      ],
    })

    const items = buildRecentActivity(overview)

    expect(items).toHaveLength(DASHBOARD_PANEL_LIMIT)
    expect(items.map((item) => item.key)).toEqual(['ev_0', 'ev_1', 'ev_2', 'ev_3', 'ev_4'])
    expect(items[0]).toMatchObject({ label: '异常开始', tone: 'critical', summary: '摘要 0' })
    expect(items[1]).toMatchObject({ label: '异常恢复', tone: 'normal' })
    expect(items[2]?.tone).toBe('alert')
  })

  it('returns an empty preview when the summary has no events', () => {
    expect(buildRecentActivity(dashboardOverviewFixture({ recent_events: [] }))).toEqual([])
  })
})

describe('buildRenewalPanel', () => {
  it('reports loading and unavailable subscription sources without inventing empty queues', () => {
    expect(buildRenewalPanel(remoteLoading(), NOW)).toEqual({ status: 'loading' })
    expect(buildRenewalPanel(remoteError('订阅不可用'), NOW)).toEqual({ status: 'unavailable', error: '订阅不可用' })
  })

  it('sorts dated renewals ascending, bounds the preview, and counts the returned window', () => {
    const renewals = [
      renewal(1, '2026-10-20'),
      renewal(2, '2026-10-01'),
      renewal(3, null),
      renewal(4, '2026-10-05T12:00:00Z'),
      renewal(5, '2026-11-01'),
      renewal(6, '2026-10-10'),
      renewal(7, '2026-12-01'),
    ]
    const panel = buildRenewalPanel(
      remoteSuccess(subscriptionOverviewFixture({ upcoming_renewals: renewals }), new Date(NOW).toISOString()),
      NOW,
    )

    expect(panel.status).toBe('ready')
    if (panel.status !== 'ready') return
    expect(panel.count).toBe(6)
    expect(panel.capped).toBe(false)
    expect(panel.items).toHaveLength(DASHBOARD_PANEL_LIMIT)
    expect(panel.items.map((item) => item.key)).toEqual(['sub_2', 'sub_4', 'sub_6', 'sub_1', 'sub_5'])
    expect(panel.items[0]).toMatchObject({ renewDate: '2026-10-01', daysLeft: 2, to: '/vps/vps_2', monthlyPrice: 12 })
    // 带时间的值按印出的日期计算日历差，不再对瞬间向上取整。
    expect(panel.items[1]).toMatchObject({ renewDate: '2026-10-05', daysLeft: 6 })
  })

  it('previews overdue renewals separately with the backend total', () => {
    const overdue = [renewal(7, '2026-09-20'), renewal(8, '2026-09-01'), renewal(9, '2026-09-25'), renewal(10, '2026-09-28')]
    const panel = buildRenewalPanel(remoteSuccess(subscriptionOverviewFixture({
      upcoming_renewals: [renewal(1, '2026-10-01')],
      overdue_renewals: overdue,
      overdue_renewal_count: 15,
    }), new Date(NOW).toISOString()), NOW)
    expect(panel.status).toBe('ready')
    if (panel.status !== 'ready') return
    expect(panel.overdueCount).toBe(15)
    expect(panel.overdue.map((item) => [item.key, item.daysLeft])).toEqual([['sub_8', -28], ['sub_7', -9], ['sub_9', -4]])
    expect(panel.count).toBe(1)
  })

  it('treats a center without overdue fields as having no overdue renewals', () => {
    const panel = buildRenewalPanel(remoteSuccess(subscriptionOverviewFixture({ upcoming_renewals: [] }), new Date(NOW).toISOString()), NOW)
    expect(panel).toMatchObject({ status: 'ready', overdue: [], overdueCount: 0 })
  })

  it('marks a queue at the backend cap as possibly truncated', () => {
    const renewals = Array.from({ length: RENEWAL_QUEUE_CAP }, (_, index) => renewal(index, `2026-10-${String(index + 1).padStart(2, '0')}`))
    const panel = buildRenewalPanel(remoteSuccess(subscriptionOverviewFixture({ upcoming_renewals: renewals }), new Date(NOW).toISOString()), NOW)
    expect(panel).toMatchObject({ status: 'ready', count: RENEWAL_QUEUE_CAP, capped: true })
  })

  it('distinguishes today, past, and stale exchange-rate amounts', () => {
    const renewals = [
      renewal(1, '2026-09-29'),
      renewal(2, '2026-09-27'),
      renewal(3, '2026-10-02', 'stale'),
    ]
    const panel = buildRenewalPanel(remoteSuccess(subscriptionOverviewFixture({ upcoming_renewals: renewals }), new Date(NOW).toISOString()), NOW)
    expect(panel.status === 'ready' ? panel.items.map((item) => [item.key, item.daysLeft, item.rateStatus]) : null).toEqual([
      ['sub_2', -2, 'fresh'],
      ['sub_1', 0, 'fresh'],
      ['sub_3', 3, 'stale'],
    ])
  })

  it('measures days left from the server snapshot UTC day across local midnights and skewed receipt', () => {
    const originalTZ = process.env.TZ
    try {
      const renewals = [renewal(1, '2026-09-29'), renewal(2, '2026-09-30'), renewal(3, '2026-12-28')]
      // 东八区 2026-09-30 00:30 = UTC 2026-09-29 16:30：后端仍把 09-29 算作今天；浏览器接收时间已是次日。
      process.env.TZ = 'Asia/Shanghai'
      // 时区切换必须生效，否则在 UTC 进程上会假通过。
      expect(new Date('2026-09-29T16:30:00Z').getDate()).toBe(30)
      const shanghaiAfterMidnight = buildRenewalPanel(remoteSuccess(
        subscriptionOverviewFixture({ upcoming_renewals: renewals, snapshot_generated_at: '2026-09-29T16:30:00Z' }),
        '2026-09-30T00:00:05Z',
      ))
      expect(shanghaiAfterMidnight.status === 'ready' ? shanghaiAfterMidnight.items.map((item) => item.daysLeft) : null).toEqual([0, 1, 90])
      // 美西 2026-09-29 17:00 = UTC 2026-09-30 00:00：窗口远端仍是第 90 天；客户端时钟慢一天也不影响。
      process.env.TZ = 'America/Los_Angeles'
      expect(new Date('2026-09-30T00:00:00Z').getDate()).toBe(29)
      const laEvening = buildRenewalPanel(remoteSuccess(
        subscriptionOverviewFixture({
          upcoming_renewals: renewals.slice(1).concat(renewal(4, '2026-12-29')),
          snapshot_generated_at: '2026-09-30T00:00:00Z',
        }),
        '2026-09-29T00:00:00Z',
      ))
      expect(laEvening.status === 'ready' ? laEvening.items.map((item) => item.daysLeft) : null).toEqual([0, 89, 90])
    } finally {
      if (originalTZ === undefined) delete process.env.TZ
      else process.env.TZ = originalTZ
    }
  })

  it('falls back to the receipt time when the snapshot time is invalid', () => {
    const panel = buildRenewalPanel(remoteSuccess(
      subscriptionOverviewFixture({ upcoming_renewals: [renewal(1, '2026-10-09')], snapshot_generated_at: 'invalid' }),
      '2026-09-29T08:00:00Z',
    ))
    expect(panel.status === 'ready' ? panel.items[0]?.daysLeft : null).toBe(10)
    expect(panel).toMatchObject({ status: 'ready', estimated: true })
  })

  it('does not mark server-dated panels as estimated', () => {
    const panel = buildRenewalPanel(remoteSuccess(
      subscriptionOverviewFixture({ upcoming_renewals: [renewal(1, '2026-10-09')], snapshot_generated_at: '2026-09-29T08:00:00Z' }),
      '2026-09-29T08:00:01Z',
    ))
    expect(panel).toMatchObject({ status: 'ready', estimated: false })
  })
})

describe('calendar dates', () => {
  it('diffs printed calendar dates instead of instants', () => {
    expect(utcCalendarDate(NOW)).toBe('2026-09-29')
    expect(utcCalendarDate(Date.parse('2026-09-29T23:59:59Z'))).toBe('2026-09-29')
    expect(calendarDaysBetween('2026-09-29', '2026-09-29T23:00:00Z')).toBe(0)
    expect(calendarDaysBetween('2026-09-29', '2026-10-01')).toBe(2)
    expect(calendarDaysBetween('2026-09-29', '2026-09-20')).toBe(-9)
    expect(calendarDaysBetween('2026-12-31', '2027-01-01')).toBe(1)
    expect(calendarDaysBetween('2026-09-29', 'not-a-date')).toBeNull()
  })

  it('sums the hourly trend for the accessible summary', () => {
    expect(trendTotal([0, 2, 1, 0])).toBe(3)
  })
})

describe('incidentTrend', () => {
  it('hides the trend unless the backend returns 24 hourly buckets', () => {
    expect(incidentTrend(dashboardOverviewFixture())).toBeNull()
    expect(incidentTrend(dashboardOverviewFixture({ new_incident_trend_24h: [1, 2, 3] }))).toBeNull()
    const trend = Array.from({ length: 24 }, (_, index) => index % 2)
    expect(incidentTrend(dashboardOverviewFixture({ new_incident_trend_24h: trend }))).toEqual(trend)
  })
})

describe('notificationChannelGap', () => {
  it('flags observed objects without any notification channel', () => {
    expect(notificationChannelGap(dashboardOverviewFixture({ total_monitoring_instance_count: 1 }))).toBe(true)
    expect(notificationChannelGap(dashboardOverviewFixture({ total_target_count: 2 }))).toBe(true)
  })

  it('stays quiet before anything is observed or once a channel is configured', () => {
    expect(notificationChannelGap(dashboardOverviewFixture({ total_monitoring_instance_count: 0, total_target_count: 0 }))).toBe(false)
    expect(notificationChannelGap(dashboardOverviewFixture({ total_monitoring_instance_count: 1, notification_status: { telegram_configured: true } }))).toBe(false)
    expect(notificationChannelGap(dashboardOverviewFixture({ total_monitoring_instance_count: 1, notification_status: { telegram_runtime_managed: true } }))).toBe(false)
    expect(notificationChannelGap(dashboardOverviewFixture({ total_monitoring_instance_count: 1, notification_status: { feishu_configured: true } }))).toBe(false)
  })
})
