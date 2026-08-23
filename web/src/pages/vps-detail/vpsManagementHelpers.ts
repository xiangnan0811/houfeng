import { ApiError } from '../../lib/apiRequest'
import type { RenewalSubscriptionLinkage } from '../../lib/types'

export type ManagementFeedbackAction = {
  to: string
  label: string
  panel?: 'subscription' | 'cancellation'
}

export function describeManagementError(error: unknown, fallback: string): string {
  if (error instanceof ApiError || error instanceof Error) return error.message
  return fallback
}

export function subscriptionLinkageNotice(
  linkage?: RenewalSubscriptionLinkage | null,
  prefix = '续费决策已更新，资产历史已刷新',
): string {
  if (!linkage || linkage.status === 'none') return prefix
  return `${prefix}。${linkage.message}`
}

export function subscriptionLinkageAction(
  linkage: RenewalSubscriptionLinkage | null | undefined,
  vpsId: string,
): ManagementFeedbackAction | null {
  if (!linkage) return null
  if (linkage.status === 'no_active_subscription') {
    if (linkage.candidate_count > 0) {
      return {
        to: `/vps/${encodeURIComponent(vpsId)}?workbench=cancellation`,
        label: '打开取消/退役',
        panel: 'cancellation',
      }
    }
    return {
      to: `/vps/${encodeURIComponent(vpsId)}?workbench=subscription`,
      label: '创建/更新订阅',
      panel: 'subscription',
    }
  }
  if (linkage.status === 'multiple_active_subscriptions') {
    return { to: `/subscriptions?vps_id=${encodeURIComponent(vpsId)}`, label: '去订阅页选择处理' }
  }
  if (linkage.subscription_id) {
    return { to: `/subscriptions?vps_id=${encodeURIComponent(vpsId)}`, label: '查看关联订阅' }
  }
  return null
}
