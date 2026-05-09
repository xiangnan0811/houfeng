import {
  SUBSCRIPTION_STATUS_LABELS,
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type SubscriptionStatus,
  type VPSLifecycleStatus,
  type VPSRenewalDecision,
  type VPSUsageStatus,
} from '../lib/types'

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
