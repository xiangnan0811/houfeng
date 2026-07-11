import {
  SUBSCRIPTION_STATUS_LABELS,
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  type AssetContextForTarget,
  type LinkedVPSContext,
} from '../lib/types'

export type AssetContextLike = AssetContextForTarget

export function assetContextHasAttention(context: AssetContextLike | null | undefined): boolean {
  return Boolean(context?.cancellation_attention)
}

export function assetContextPrimarySummary(context: AssetContextLike | null | undefined): LinkedVPSContext | null {
  if (!context || context.summaries.length === 0) return null
  return context.summaries.find((summary) => summary.lifecycle_status === 'to_cancel' || summary.lifecycle_status === 'cancelled') ??
    context.summaries.find((summary) => summary.subscription_state !== 'active' && summary.subscription_state !== 'missing') ??
    context.summaries[0] ??
    null
}

export function assetContextMessage(context: AssetContextLike | null | undefined): string {
  const primary = assetContextPrimarySummary(context)
  if (!primary) return '未关联 VPS'
  return primary.message || `${primary.display_name} · ${vpsLifecycleLabel(primary.lifecycle_status)}`
}

export function vpsLifecycleLabel(value: string): string {
  return VPS_LIFECYCLE_STATUS_LABELS[value as keyof typeof VPS_LIFECYCLE_STATUS_LABELS] ?? value
}

export function vpsRenewalDecisionLabel(value: string): string {
  return VPS_RENEWAL_DECISION_LABELS[value as keyof typeof VPS_RENEWAL_DECISION_LABELS] ?? value
}

export function subscriptionStateLabel(value: string): string {
  if (value === 'missing') return '缺订阅'
  return SUBSCRIPTION_STATUS_LABELS[value as keyof typeof SUBSCRIPTION_STATUS_LABELS] ?? value
}
