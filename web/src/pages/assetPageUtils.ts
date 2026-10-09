import {
  SUBSCRIPTION_STATUS_LABELS,
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type SubscriptionRecord,
  type SubscriptionStatus,
  type VPSAssetRecord,
  type VPSLifecycleStatus,
  type VPSRenewalDecision,
  type VPSUsageStatus,
} from '../lib/types'

export type AssetQualityIssueTone = 'notice' | 'alert' | 'critical'

export type AssetQualityIssue = {
  key: string
  label: string
  tone: AssetQualityIssueTone
}

const MS_PER_DAY = 24 * 60 * 60 * 1000

export function parseLabels(value: string): string[] {
  const seen = new Set<string>()
  const labels: string[] = []
  for (const raw of value.split(/[,，]/)) {
    const label = raw.trim()
    if (!label || seen.has(label)) continue
    seen.add(label)
    labels.push(label)
  }
  return labels
}

export function lifecycleLabel(value: VPSLifecycleStatus | string): string {
  return VPS_LIFECYCLE_STATUS_LABELS[value as VPSLifecycleStatus] ?? value
}

export function usageLabel(value: VPSUsageStatus | string): string {
  return (VPS_USAGE_STATUS_LABELS[value as VPSUsageStatus] ?? value).trim() || '未标注用途'
}

export function renewalLabel(value: VPSRenewalDecision | string): string {
  return VPS_RENEWAL_DECISION_LABELS[value as VPSRenewalDecision] ?? value
}

export function subscriptionStatusLabel(value: SubscriptionStatus | string): string {
  return SUBSCRIPTION_STATUS_LABELS[value as SubscriptionStatus] ?? value
}

const CALENDAR_DATE = /^(\d{4})-(\d{2})-(\d{2})$/
const ISO_DATE_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})$/

// 续费日等 YYYY-MM-DD 是日历日；带时间（含偏移）的值按瞬间折算到 UTC 日期。非法月日（如 02-30）视为无效，不顺延。
function utcCalendarDay(value: string): number | null {
  const match = CALENDAR_DATE.exec(value)
  if (match) {
    const year = Number(match[1])
    const month = Number(match[2]) - 1
    const day = Number(match[3])
    const date = new Date(Date.UTC(year, month, day))
    return date.getUTCFullYear() === year && date.getUTCMonth() === month && date.getUTCDate() === day
      ? date.getTime()
      : null
  }
  if (!ISO_DATE_TIME.test(value)) return null
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return null
  return Date.UTC(parsed.getUTCFullYear(), parsed.getUTCMonth(), parsed.getUTCDate())
}

/**
 * 续费剩余天数与后端 90 天窗口、30 天计数、overdue_renewals 和 renewal.overdue.v1 同口径：
 * 续费日与“今天”都取 UTC 日历日，不随浏览器时区在午夜前后多算或少算一天。
 * now 可传摘要生成时刻，使列表与摘要同一天。
 */
export function daysUntilDate(value?: string | null, now: Date | number = Date.now()): number | null {
  if (!value) return null
  const target = utcCalendarDay(value)
  const reference = new Date(now)
  if (target == null || Number.isNaN(reference.getTime())) return null
  const today = Date.UTC(reference.getUTCFullYear(), reference.getUTCMonth(), reference.getUTCDate())
  return Math.round((target - today) / MS_PER_DAY)
}

/**
 * 续费天数的参考“今天”：订阅摘要可用时取其生成时刻，与摘要计数同一天；摘要不可用
 * （尚无摘要、读取失败但仍保留旧摘要、时间无效）时退回调用方给的页面时刻。
 */
export function renewalReferenceTime(snapshotGeneratedAt: string | null | undefined, snapshotUsable: boolean, fallback: number): number {
  if (!snapshotUsable) return fallback
  const snapshotAt = Date.parse(snapshotGeneratedAt ?? '')
  return Number.isNaN(snapshotAt) ? fallback : snapshotAt
}

/** 两周内的续费需要尽快处理，日期旁的剩余天数高亮。 */
export const RENEWAL_SOON_DAYS = 14

export type RenewalUrgency = 'overdue' | 'soon' | 'later'

export function renewalUrgency(days: number | null, soonDays: number = RENEWAL_SOON_DAYS): RenewalUrgency {
  if (days != null && days < 0) return 'overdue'
  if (days != null && days <= soonDays) return 'soon'
  return 'later'
}

export function renewalTimingLabel(days: number | null): string {
  if (days == null) return '尚无续费日'
  if (days < 0) return `已逾期 ${Math.abs(days)} 天`
  if (days === 0) return '今天续费'
  return `${days} 天后`
}

export function isSubscriptionInRenewalWindow(
  subscription: SubscriptionRecord | null,
  windowDays: number,
): boolean {
  const days = daysUntilDate(subscription?.renew_at)
  return days != null && days <= windowDays
}

export function groupSubscriptionsByVPS(
  subscriptions: SubscriptionRecord[],
): Map<string, SubscriptionRecord[]> {
  const grouped = new Map<string, SubscriptionRecord[]>()
  for (const subscription of subscriptions) {
    const group = grouped.get(subscription.vps_id) ?? []
    group.push(subscription)
    grouped.set(subscription.vps_id, group)
  }

  for (const group of grouped.values()) {
    group.sort((left, right) => {
      const leftActive = left.status === 'active' ? 0 : 1
      const rightActive = right.status === 'active' ? 0 : 1
      if (leftActive !== rightActive) return leftActive - rightActive
      return subscriptionRenewalSortValue(left) - subscriptionRenewalSortValue(right)
    })
  }

  return grouped
}

export function selectPrimarySubscription(
  grouped: Map<string, SubscriptionRecord[]>,
  vpsID: string,
): SubscriptionRecord | null {
  return grouped.get(vpsID)?.[0] ?? null
}

export function vpsLocationLabel(vps: VPSAssetRecord): string {
  return [vps.country, vps.region, vps.city].filter(Boolean).join(' · ') || '位置缺失'
}

export function vpsAccessLabel(vps: VPSAssetRecord): string {
  return vps.ssh_host || vps.ipv4 || vps.ipv6 || '接入信息缺失'
}

export function hasMissingVPSFacts(vps: VPSAssetRecord): boolean {
  return (
    (!vps.provider_id && !(vps.provider_name ?? '').trim()) ||
    !vpsLocationHasValue(vps) ||
    (!vps.ssh_host && !vps.ipv4 && !vps.ipv6)
  )
}

export function buildVPSQualityIssues(
  vps: VPSAssetRecord,
  subscription: SubscriptionRecord | null,
  options: { includeMissingSubscription?: boolean } = {},
): AssetQualityIssue[] {
  const issues: AssetQualityIssue[] = []
  const includeMissingSubscription = options.includeMissingSubscription ?? true

  if (includeMissingSubscription && !subscription) {
    issues.push({ key: 'missing-subscription', label: '缺订阅', tone: 'critical' })
  }
  if (vps.active_monitoring_instance_link_count <= 0) {
    issues.push({ key: 'unlinked-monitoring-instance', label: '未关联监控实例', tone: 'alert' })
  }
  if (!vps.provider_id && !(vps.provider_name ?? '').trim()) {
    issues.push({ key: 'missing-provider', label: '缺服务商', tone: 'notice' })
  }
  if (!vpsLocationHasValue(vps)) {
    issues.push({ key: 'missing-location', label: '缺位置', tone: 'notice' })
  }
  if (!vps.ssh_host && !vps.ipv4 && !vps.ipv6) {
    issues.push({ key: 'missing-access', label: '缺访问入口', tone: 'notice' })
  }

  return issues
}

function subscriptionRenewalSortValue(subscription: SubscriptionRecord): number {
  if (!subscription.renew_at) return Number.POSITIVE_INFINITY
  const parsed = new Date(subscription.renew_at).getTime()
  return Number.isNaN(parsed) ? Number.POSITIVE_INFINITY : parsed
}

function vpsLocationHasValue(vps: VPSAssetRecord): boolean {
  return Boolean(vps.country || vps.region || vps.city || vps.datacenter)
}
