import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type DashboardOverview,
  type StateChangeEventRecord,
  type SubscriptionOverview,
  type SubscriptionRenewalQueueItem,
} from '../../lib/types'
import type { DashboardTone } from './dashboardModel'
import type { RemoteState } from './dashboardRemoteState'

/** 工作台面板只做有界预览，完整列表交给事件流与订阅页。 */
export const DASHBOARD_PANEL_LIMIT = 5
const DAY_MS = 24 * 60 * 60 * 1000

export type DashboardActivityItem = {
  key: string
  label: string
  summary: string
  createdAt: string
  tone: Exclude<DashboardTone, 'neutral'>
}

export type DashboardRenewalItem = {
  key: string
  name: string
  provider: string
  /** 仅日期部分（YYYY-MM-DD），兼容后端返回日期或完整时间戳；剩余天数按同一日历计算。 */
  renewDate: string
  /** 距续费日的 UTC 日历天数；0 为今天，负数为已过（正常队列不会出现，兜底显示）。 */
  daysLeft: number
  /** 缺汇率时没有折算金额。过期汇率仍保留数值，并由 rateStatus 标明。 */
  monthlyPrice: number | null
  rateStatus: SubscriptionRenewalQueueItem['exchange_rate_status']
  currency: string
  to: string
}

/**
 * 订阅摘要的续费队列只覆盖 UTC 日窗口的未来 90 天，且后端最多返回 12 条（subscriptioncosts.service）。
 * 达到上限时界面显示“至少 12 项”，不把截断后的条数说成总数。
 */
export const RENEWAL_QUEUE_WINDOW_DAYS = 90
export const RENEWAL_QUEUE_CAP = 12

export type DashboardRenewalPanel =
  | { status: 'loading' }
  | { status: 'unavailable'; error: string }
  | {
    status: 'ready'
    items: DashboardRenewalItem[]
    count: number
    capped: boolean
    /** 摘要生成时间无效、剩余天数按浏览器接收时间估算时为 true，界面需标明。 */
    estimated: boolean
  }

function activityTone(event: StateChangeEventRecord): Exclude<DashboardTone, 'neutral'> {
  if (event.event_type === 'incident_recovered') return 'normal'
  if (event.event_type === 'incident_started' || event.event_type === 'incident_escalated') {
    if (event.severity === '严重') return 'critical'
    if (event.severity === '告警') return 'alert'
    return 'notice'
  }
  if (event.event_type.includes('maintenance')) return 'maintenance'
  return 'notice'
}

export function buildRecentActivity(overview: DashboardOverview): DashboardActivityItem[] {
  return [...overview.recent_events]
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))
    .slice(0, DASHBOARD_PANEL_LIMIT)
    .map((event, index) => ({
      key: event.event_id ?? `${event.incident_id}-${event.event_type}-${index}`,
      label: STATE_CHANGE_EVENT_TYPE_LABELS[event.event_type] ?? event.event_type,
      summary: event.summary,
      createdAt: event.created_at,
      tone: activityTone(event),
    }))
}

const CALENDAR_DATE = /^(\d{4})-(\d{2})-(\d{2})/

function calendarDayNumber(date: string): number | null {
  const match = CALENDAR_DATE.exec(date)
  if (!match) return null
  const day = Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
  return Number.isNaN(day) ? null : Math.round(day / DAY_MS)
}

/**
 * UTC 日历中的日期（YYYY-MM-DD），作为“今天”。续费队列的成员由后端按 UTC 日窗口
 * [今天, 今天+90] 筛选（subscriptioncosts.subscriptionDay），剩余天数必须用同一日历，
 * 否则本地与 UTC 跨日时会在“未来 90 天”里出现“已过”或 91 天。
 */
export function utcCalendarDate(instant: number): string {
  return new Date(instant).toISOString().slice(0, 10)
}

/** 两个日历日期之间的整天差，不对时间瞬间取整；任一方不是日期时返回 null。 */
export function calendarDaysBetween(today: string, target: string): number | null {
  const from = calendarDayNumber(today)
  const to = calendarDayNumber(target)
  return from == null || to == null ? null : to - from
}

/**
 * 剩余天数以订阅摘要 snapshot_generated_at 的 UTC 日期为“今天”：后端用同一时刻确定窗口，
 * 不受浏览器接收时间或客户端时钟偏差影响；该字段无效时退回接收时间。保持渲染纯函数。
 */
export function buildRenewalPanel(
  subscription: RemoteState<SubscriptionOverview>,
  now?: number,
): DashboardRenewalPanel {
  if (subscription.status === 'loading') return { status: 'loading' }
  if (subscription.status === 'error') return { status: 'unavailable', error: subscription.error }
  const generatedAt = Date.parse(subscription.value.snapshot_generated_at)
  const estimated = now == null && Number.isNaN(generatedAt)
  const today = utcCalendarDate(now ?? (estimated ? Date.parse(subscription.loadedAt) : generatedAt))
  const queue = subscription.value.upcoming_renewals
  const dated = queue
    .map((item) => ({ item, renewDate: (item.renew_at ?? '').slice(0, 10) }))
    .filter(({ renewDate }) => calendarDayNumber(renewDate) != null)
  const items = [...dated]
    .sort((left, right) => left.renewDate.localeCompare(right.renewDate))
    .slice(0, DASHBOARD_PANEL_LIMIT)
    .map(({ item, renewDate }) => ({
      key: item.subscription_id,
      name: item.display_name || item.vps_display_name || item.vps_id,
      provider: item.provider_name,
      renewDate,
      daysLeft: calendarDaysBetween(today, renewDate) ?? 0,
      monthlyPrice: item.monthly_price_base ?? null,
      rateStatus: item.exchange_rate_status,
      currency: item.base_currency || subscription.value.base_currency,
      to: `/vps/${encodeURIComponent(item.vps_id)}`,
    }))
  return { status: 'ready', items, count: dated.length, capped: queue.length >= RENEWAL_QUEUE_CAP, estimated }
}

/** 近 24 小时新增异常合计，用于指标卡的可访问描述。 */
export function trendTotal(trend: number[]): number {
  return trend.reduce((sum, value) => sum + (Number.isFinite(value) ? value : 0), 0)
}

/** 24h 新增异常趋势；后端未提供或长度不符时返回 null，界面隐藏趋势而不是画 0。 */
export function incidentTrend(overview: DashboardOverview): number[] | null {
  const trend = overview.new_incident_trend_24h
  return Array.isArray(trend) && trend.length === 24 ? trend : null
}
