import { ApiError } from '../../lib/apiRequest'
import type { RenewalSubscriptionLinkage } from '../../lib/types'

export type ManagementFeedbackAction = {
  to: string
  label: string
  panel?: 'subscription' | 'cancellation'
}

const CANCELLATION_LIKE_RENEWAL = new Set(['cancel', 'auto_renew_cancelled', 'migrate'])

export type OverviewWorkbenchPanel = 'cancellation' | 'subscription'

export function parseOverviewWorkbench(value: string | null | undefined): OverviewWorkbenchPanel | null {
  const normalized = value?.trim()
  if (normalized === 'cancellation' || normalized === 'subscription') return normalized
  return null
}

export function isCancellationPreviewStale(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && error.code === 'cancellation_preview_stale'
}

export function isVPSVersionConflict(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && error.code === 'vps_asset_conflict'
}

export function isIdempotencyKeyReused(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && error.code === 'idempotency_key_reused'
}

export function describeManagementError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    if (error.status === 409 && error.code === 'cancellation_preview_stale') {
      return '影响范围已变化，请重新加载预览后再确认'
    }
    if (error.status === 409 && error.code === 'lifecycle_action_blocked') {
      return '当前生命周期状态不允许此操作'
    }
    if (error.status === 409 && error.code === 'lifecycle_transaction_conflict') {
      return '生命周期事务发生冲突，请重试'
    }
    if (error.status === 409 && error.code === 'vps_asset_conflict') {
      return 'VPS 已被其他操作更新，请先加载最新版本后再保存'
    }
    if (error.status === 409 && error.code === 'idempotency_key_reused') {
      return '同一幂等键已用于不同的订阅内容，请重新填写后再创建'
    }
    return error.message
  }
  if (error instanceof Error) return error.message
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
  renewalDecision?: string,
): ManagementFeedbackAction | null {
  if (!linkage) return null
  if (renewalDecision && CANCELLATION_LIKE_RENEWAL.has(renewalDecision)) {
    return {
      to: `/vps/${encodeURIComponent(vpsId)}?workbench=cancellation`,
      label: '继续取消 / 退役',
      panel: 'cancellation',
    }
  }
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
