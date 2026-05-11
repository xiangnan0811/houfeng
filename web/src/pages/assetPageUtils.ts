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
  return VPS_USAGE_STATUS_LABELS[value as VPSUsageStatus] ?? value
}

export function renewalLabel(value: VPSRenewalDecision | string): string {
  return VPS_RENEWAL_DECISION_LABELS[value as VPSRenewalDecision] ?? value
}

export function subscriptionStatusLabel(value: SubscriptionStatus | string): string {
  return SUBSCRIPTION_STATUS_LABELS[value as SubscriptionStatus] ?? value
}

export function daysUntilDate(value?: string | null): number | null {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null

  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const targetDay = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
  return Math.ceil((targetDay - today) / MS_PER_DAY)
}

export function renewalTimingLabel(days: number | null): string {
  if (days == null) return '尚无续费日'
  if (days < 0) return `已过期 ${Math.abs(days)} 天`
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
    (!vps.provider_id && !vps.provider_name.trim()) ||
    !vpsLocationHasValue(vps) ||
    (!vps.ssh_host && !vps.ipv4 && !vps.ipv6)
  )
}

export function buildVPSQualityIssues(
  vps: VPSAssetRecord,
  subscription: SubscriptionRecord | null,
): AssetQualityIssue[] {
  const issues: AssetQualityIssue[] = []

  if (!subscription) {
    issues.push({ key: 'missing-subscription', label: '缺订阅', tone: 'critical' })
  }
  if (vps.active_node_link_count <= 0) {
    issues.push({ key: 'unlinked-node', label: '未关联 Node', tone: 'alert' })
  }
  if (!vps.provider_id && !vps.provider_name.trim()) {
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
