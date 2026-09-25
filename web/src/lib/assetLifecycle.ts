import { ApiError } from './apiRequest'
import type {
  ArchiveBlockerDetail,
  ArchiveReview,
  DependencyCorrectionStatus,
  DependencyImpact,
} from './types'

const EFFECTIVE_DEPENDENCY_CLASSIFICATIONS: Record<string, true> = {
  current: true,
  residual: true,
}

const TERMINAL_VPS_LIFECYCLES: Record<string, true> = {
  cancelled: true,
  archived: true,
}

const MIGRATION_SOURCE_LIFECYCLES: Record<string, true> = {
  active: true,
  idle: true,
  testing: true,
}

export function dependencyClassificationLabel(value: string): string {
  switch (value) {
    case 'current':
      return '当前承载'
    case 'residual':
      return '已取消待整理'
    case 'paused':
      return '已暂停'
    case 'historical':
      return '历史'
    case 'needs_confirmation':
      return '待确认'
    default:
      return value
  }
}

export function impactsForObject(
  impacts: readonly DependencyImpact[],
  objectType: string,
  objectId: string,
): DependencyImpact[] {
  return impacts.filter((impact) => impact.object_type === objectType && impact.object_id === objectId)
}

export function effectiveParentIDs(
  impacts: readonly DependencyImpact[],
  objectType: string,
  objectId: string,
): string[] {
  const parents = new Set<string>()
  for (const impact of impactsForObject(impacts, objectType, objectId)) {
    if (!EFFECTIVE_DEPENDENCY_CLASSIFICATIONS[impact.classification]) continue
    if (impact.vps_id) parents.add(impact.vps_id)
  }
  return [...parents].sort()
}

export function requiresSharedImpactConfirmation(
  impacts: readonly DependencyImpact[],
  objectType: string,
  objectId: string,
): boolean {
  const related = impactsForObject(impacts, objectType, objectId)
  if (related.some((impact) => impact.classification === 'needs_confirmation')) return true
  return effectiveParentIDs(impacts, objectType, objectId).length > 1
}

export function objectAffectsAnotherVPS(
  impacts: readonly DependencyImpact[],
  objectType: string,
  objectId: string,
  currentVPSID: string,
): boolean {
  return impacts.some((impact) => (
    impact.object_type === objectType
    && impact.object_id === objectId
    && impact.vps_id !== currentVPSID
    && (Boolean(EFFECTIVE_DEPENDENCY_CLASSIFICATIONS[impact.classification]) || impact.classification === 'needs_confirmation')
  ))
}

export function sharedObjectKey(objectType: string, objectId: string): string {
  return `${objectType}:${objectId}`
}

export function vpsCanArchive(lifecycleStatus: string): boolean {
  return lifecycleStatus === 'to_cancel' || lifecycleStatus === 'cancelled'
}

export function vpsCanRestore(lifecycleStatus: string): boolean {
  return lifecycleStatus === 'archived'
}

export function vpsCanStartMigration(lifecycleStatus: string): boolean {
  return Boolean(MIGRATION_SOURCE_LIFECYCLES[lifecycleStatus])
}

export function isTerminalVPSLifecycle(lifecycleStatus: string): boolean {
  return Boolean(TERMINAL_VPS_LIFECYCLES[lifecycleStatus])
}

export function replacedDecisionBlocked(lifecycleStatus: string, usageStatus: string): boolean {
  return lifecycleStatus === 'active' || usageStatus === 'in_use'
}

export type DependencyStatusChoice = {
  value: DependencyCorrectionStatus
  label: string
  disabled: boolean
  reason?: string
}

export function dependencyStatusChoices(parentLifecycle: string): DependencyStatusChoice[] {
  const terminal = isTerminalVPSLifecycle(parentLifecycle)
  return [
    {
      value: 'active',
      label: '使用中',
      disabled: terminal,
      ...(terminal ? { reason: '终态 VPS 不能把依赖重新激活' } : {}),
    },
    { value: 'paused', label: '已暂停', disabled: false },
    { value: 'retired', label: '已退役', disabled: false },
  ]
}

export type BlockerHandling = {
  label: string
  href?: string
  inline?: 'service-status' | 'domain-status' | 'residual' | 'restore'
}

export function blockerHandling(detail: ArchiveBlockerDetail, vpsId: string): BlockerHandling {
  const encodedVPS = encodeURIComponent(vpsId)
  const encodedObject = encodeURIComponent(detail.object_id)
  switch (detail.object_type) {
    case 'vps':
      if (detail.resolution_action === 'restore_from_archive') {
        return { label: '恢复归档', inline: 'restore' }
      }
      return { label: '处理取消', href: `/vps/${encodedVPS}?workbench=cancellation` }
    case 'subscription':
      return { label: '处理账单', inline: 'residual' }
    case 'monitoring_instance':
      return { label: '打开监控实例', href: `/monitoring/${encodedObject}` }
    case 'target':
      return { label: '打开入口探测', href: `/targets/${encodedObject}` }
    case 'service':
      return { label: '纠正服务状态', inline: 'service-status' }
    case 'domain':
      return { label: '纠正域名状态', inline: 'domain-status' }
    default:
      return { label: '查看对象' }
  }
}

export function isArchiveReview(value: unknown): value is ArchiveReview {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  if (!('eligible' in value) || typeof value.eligible !== 'boolean') return false
  if (!('blocker_details' in value) || !Array.isArray(value.blocker_details)) return false
  if (!('vps' in value) || typeof value.vps !== 'object' || value.vps === null || Array.isArray(value.vps)) return false
  return 'vps_id' in value.vps && typeof value.vps.vps_id === 'string'
}

export function archiveReviewFromError(error: unknown): ArchiveReview | null {
  if (!(error instanceof ApiError)) return null
  const review = (error as ApiError & { review?: unknown }).review
  return isArchiveReview(review) ? review : null
}

export function isLifecycleActionBlocked(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && error.code === 'lifecycle_action_blocked'
}

export function isSharedImpactConfirmationRequired(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && error.code === 'shared_impact_confirmation_required'
}

export function isManagementReviewStale(error: unknown): boolean {
  return error instanceof ApiError
    && error.status === 409
    && (error.code === 'management_review_stale' || error.code === 'cancellation_preview_stale')
}

export function fieldErrorMessage(error: unknown, field: string): string | null {
  if (!(error instanceof ApiError)) return null
  return error.field_errors.find((item) => item.field === field)?.message ?? null
}
