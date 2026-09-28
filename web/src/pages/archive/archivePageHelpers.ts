import { periodLabel } from '../../lib/assetOptions'
import { formatMoney, formatOptional } from '../../lib/format'
import type { SubscriptionRecord, VPSAssetRecord, VPSTimeline } from '../../lib/types'
import { renewalLabel, subscriptionStatusLabel } from '../assetPageUtils'

export function subscriptionsForVPS(
  subscriptions: SubscriptionRecord[],
  vpsID: string | null,
): SubscriptionRecord[] {
  if (!vpsID) return []
  return subscriptions.filter((subscription) => subscription.vps_id === vpsID)
}

export function selectedVPS(rows: VPSAssetRecord[], selectedVPSID: string | null): VPSAssetRecord | null {
  if (!selectedVPSID) return null
  return rows.find((row) => row.vps_id === selectedVPSID) ?? null
}

export function lifecycleTone(status: VPSAssetRecord['lifecycle_status']): 'neutral' | 'offline' {
  return status === 'archived' ? 'offline' : 'neutral'
}

export function subscriptionMonthlySummary(subscriptions: SubscriptionRecord[], emptyLabel = '无关联订阅'): string {
  if (subscriptions.length === 0) return emptyLabel

  const totals = new Map<string, number>()
  for (const subscription of subscriptions) {
    const currency = subscription.currency || '---'
    totals.set(currency, (totals.get(currency) ?? 0) + subscription.monthly_price)
  }

  return Array.from(totals.entries())
    .map(([currency, total]) => `${formatMoney(total, currency)}/月`)
    .join(' + ')
}

/** Local calendar day for either a plain `YYYY-MM-DD` fact or a full timestamp. */
export function archiveDayLabel(value?: string | null): string {
  if (!value) return '—'
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return value.replaceAll('-', '/')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(date)
}

/** Calendar span between service start and archive, e.g. `1 年 3 个月` or `10 个月 26 天`. */
export function archiveServiceSpanLabel(start?: string | null, end?: string | null): string | null {
  if (!start || !end) return null
  // Plain `YYYY-MM-DD` billing dates are calendar days in the operator's zone, not UTC midnights.
  const localDay = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) ? new Date(`${value}T00:00:00`) : new Date(value)
  const from = localDay(start)
  const to = localDay(end)
  if (Number.isNaN(from.getTime()) || Number.isNaN(to.getTime())) return null
  // Compare local calendar days as UTC ordinals so time of day and DST-shortened days never lose a day.
  const fromDay = Date.UTC(from.getFullYear(), from.getMonth(), from.getDate())
  const toDay = Date.UTC(to.getFullYear(), to.getMonth(), to.getDate())
  if (toDay < fromDay) return null
  let months = (to.getFullYear() - from.getFullYear()) * 12 + (to.getMonth() - from.getMonth())
  if (to.getDate() < from.getDate()) months -= 1
  // Clamp to the target month's last day so Jan 31 + 1 month lands on Feb 28/29, not Mar 3.
  const anchorMonth = new Date(Date.UTC(from.getFullYear(), from.getMonth() + months, 1))
  const anchorMonthDays = new Date(Date.UTC(anchorMonth.getUTCFullYear(), anchorMonth.getUTCMonth() + 1, 0)).getUTCDate()
  const anchorDay = Date.UTC(anchorMonth.getUTCFullYear(), anchorMonth.getUTCMonth(), Math.min(from.getDate(), anchorMonthDays))
  const days = Math.round((toDay - anchorDay) / 86_400_000)
  const years = Math.floor(months / 12)
  const restMonths = months % 12
  if (years > 0) return restMonths > 0 ? `${years} 年 ${restMonths} 个月` : `${years} 年`
  if (months > 0) return days > 0 ? `${months} 个月 ${days} 天` : `${months} 个月`
  return `${days} 天`
}

/**
 * Subscriptions that describe what the server cost when it was retired: every row whose billing
 * period covers the archive day, otherwise the most recent row per currency. Currencies stay separate.
 */
export function archiveClosingSubscriptions(subscriptions: SubscriptionRecord[], archivedAt?: string | null): SubscriptionRecord[] {
  const archiveDay = archivedAt
    ? /^\d{4}-\d{2}-\d{2}$/.test(archivedAt)
      ? archivedAt
      : new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(archivedAt))
    : ''
  const covering = archiveDay
    ? subscriptions.filter((subscription) => (
        (subscription.started_at ?? '').slice(0, 10) <= archiveDay && (!subscription.renew_at || subscription.renew_at.slice(0, 10) >= archiveDay)
      ))
    : []
  if (covering.length > 0) return covering
  const latestByCurrency = new Map<string, SubscriptionRecord>()
  for (const subscription of subscriptions) {
    const key = subscription.currency || '---'
    const current = latestByCurrency.get(key)
    const day = (subscription.renew_at || subscription.started_at || '').slice(0, 10)
    if (!current || day > (current.renew_at || current.started_at || '').slice(0, 10)) latestByCurrency.set(key, subscription)
  }
  return [...latestByCurrency.values()]
}

export type ArchiveTimelineEntry = {
  key: string
  kind: 'decision' | 'price' | 'spec' | 'ip'
  kindLabel: string
  title: string
  detail: string
  time: string
}

function eventTime(value?: string | null): number {
  const parsed = value ? Date.parse(value) : Number.NaN
  return Number.isNaN(parsed) ? -Infinity : parsed
}

/** Merges renewal, price, spec and IP facts into one newest-first lifetime timeline. */
export function archiveTimelineEntries(timeline: VPSTimeline): ArchiveTimelineEntry[] {
  const entries: ArchiveTimelineEntry[] = [
    ...timeline.renewal_decisions.map((record) => ({
      key: record.decision_id,
      kind: 'decision' as const,
      kindLabel: '续费决策',
      title: `${renewalLabel(record.from_decision ?? 'unreviewed')} → ${renewalLabel(record.to_decision)}`,
      detail: record.reason || '未记录原因',
      time: record.decided_at || record.created_at,
    })),
    ...timeline.price_histories.map((record) => ({
      key: record.price_history_id,
      kind: 'price' as const,
      kindLabel: '价格',
      title: `${formatMoney(record.from_price, record.from_currency)} ${periodLabel(record.from_billing_period_unit, record.from_billing_period_length, record.from_billing_months)} → ${formatMoney(record.to_price, record.to_currency)} ${periodLabel(record.to_billing_period_unit, record.to_billing_period_length, record.to_billing_months)}`,
      detail: [
        record.from_price !== record.from_monthly_price || record.to_price !== record.to_monthly_price
          ? `折合 ${formatMoney(record.from_monthly_price, record.from_currency)} → ${formatMoney(record.to_monthly_price, record.to_currency)} /月`
          : '',
        `订阅 ${record.subscription_id}`,
        `${subscriptionStatusLabel(record.from_status)} → ${subscriptionStatusLabel(record.to_status)}`,
      ].filter(Boolean).join(' · '),
      time: record.changed_at || record.created_at,
    })),
    ...timeline.spec_snapshots.map((record) => ({
      key: record.snapshot_id,
      kind: 'spec' as const,
      kindLabel: '规格',
      title: record.product_name || '规格快照',
      detail: [
        record.ssh_host ? `${record.ssh_user ? `${record.ssh_user}@` : ''}${record.ssh_host}${record.ssh_port ? `:${record.ssh_port}` : ''}` : '',
        record.os_name,
        record.virtualization,
      ].filter(Boolean).join(' · ') || '—',
      time: record.captured_at || record.created_at,
    })),
    ...timeline.ip_histories.map((record) => ({
      key: record.ip_history_id,
      kind: 'ip' as const,
      kindLabel: 'IP',
      title: `IPv4 ${formatOptional(record.from_ipv4)} → ${formatOptional(record.to_ipv4)}`,
      detail: `IPv6 ${formatOptional(record.from_ipv6)} → ${formatOptional(record.to_ipv6)}`,
      time: record.changed_at || record.created_at,
    })),
  ]
  return entries.sort((a, b) => eventTime(b.time) - eventTime(a.time))
}
