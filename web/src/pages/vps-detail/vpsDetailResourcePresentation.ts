import { BILLING_PERIOD_UNIT_OPTIONS, normalizeBillingPeriodUnit, renewalModeFromLegacy } from '../../lib/assetOptions'
import { formatDate, formatMoney } from '../../lib/format'
import {
  ASSET_DOMAIN_STATUS_LABELS,
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  type AssetDomainRecord,
  type AssetServiceRecord,
  type AssetServiceType,
  type SubscriptionRecord,
  type VPSMonitoringInstanceSummary,
} from '../../lib/types'
import type { ResourceState } from './hooks/useVPSDetailResources'

function unnamed(value: string | undefined, fallback: string): string {
  const trimmed = value?.trim() ?? ''
  return trimmed || fallback
}

export function readyResourceState<T>(items: T[], error: string | null, pending = false): ResourceState<T> {
  if (pending) return { status: 'loading', items, error: null }
  if (error) return { status: 'error', items, error }
  return { status: 'ready', items, error: null }
}

export function subscriptionResourceName(item: SubscriptionRecord): string {
  return unnamed(item.display_name, '未命名订阅')
}


export type VPSIdentityMetaField = {
  label: string
  value: string
  mono?: boolean
  timestamp?: boolean
}

export function vpsIdentityMetaFields(input: {
  vpsId: string
  providerName?: string
  location?: string
  ipv4?: string
  updatedAt?: string
}): VPSIdentityMetaField[] {
  const items: VPSIdentityMetaField[] = []
  const provider = input.providerName?.trim()
  const location = input.location?.trim()
  const ipv4 = input.ipv4?.trim()
  if (provider && provider !== '—') items.push({ label: '服务商', value: provider })
  if (location && location !== '—') items.push({ label: '位置', value: location })
  if (ipv4 && ipv4 !== '—') items.push({ label: 'IP', value: ipv4, mono: true })
  if (input.vpsId.trim()) items.push({ label: 'ID', value: input.vpsId, mono: true })
  const updatedAt = input.updatedAt?.trim()
  if (updatedAt) items.push({ label: '更新', value: updatedAt, timestamp: true })
  return items
}



export function subscriptionDueLabel(
  item: SubscriptionRecord,
  options?: { plannedCancellation?: boolean },
): string | null {
  const plannedCancellation = options?.plannedCancellation === true
  const renewalMode = renewalModeFromLegacy(item)
  const renewAt = item.renew_at?.trim()
  const endsAt = item.ends_at?.trim()
  const trialEndsAt = item.trial_ends_at?.trim()
  const parts: string[] = []

  if (renewalMode === 'auto_cancelled' && endsAt) {
    parts.push(`${plannedCancellation ? '账期到期' : '到期'} ${formatDate(endsAt)}`)
  } else {
    if (renewAt) parts.push(`${plannedCancellation ? '登记续费日' : '续费'} ${formatDate(renewAt)}`)
    if (endsAt) parts.push(`${plannedCancellation ? '账期到期' : '到期'} ${formatDate(endsAt)}`)
  }
  if (trialEndsAt) parts.push(`试用到期 ${formatDate(trialEndsAt)}`)

  return parts.length > 0 ? parts.join(' · ') : null
}

export function subscriptionCadenceLabel(item: SubscriptionRecord): string {
  const length = item.billing_period_length
  const fallback = item.billing_months
  const normalizedLength =
    typeof length === 'number' && Number.isFinite(length) && length > 0
      ? length
      : fallback && fallback > 0
        ? fallback
        : 1
  const unitLabel = item.billing_period_unit || fallback
    ? BILLING_PERIOD_UNIT_OPTIONS.find((option) => option.value === normalizeBillingPeriodUnit(item.billing_period_unit || 'month'))?.label ?? '月'
    : '月'
  if (normalizedLength === 1) return `/${unitLabel}`
  if (unitLabel === '月') return `/${normalizedLength}个月`
  return `/${normalizedLength}${unitLabel}`
}

export function subscriptionResourceSummary(item: SubscriptionRecord): string {
  const parts = [subscriptionCadenceLabel(item)]
  if (item.currency.trim()) parts.push(formatMoney(item.price, item.currency))
  const due = subscriptionDueLabel(item)
  if (due) parts.push(due)
  return parts.filter(Boolean).join(' · ')
}


export function serviceResourceName(item: AssetServiceRecord): string {
  return unnamed(item.name, '未命名服务')
}

export function serviceResourceSummary(item: AssetServiceRecord): string {
  const parts: string[] = []
  if (item.url.trim()) parts.push(item.url)
  if (item.port != null) parts.push(`端口 ${item.port}`)
  const typeLabel = ASSET_SERVICE_TYPE_LABELS[item.service_type as AssetServiceType] ?? item.service_type
  if (typeLabel) parts.push(typeLabel)
  parts.push(ASSET_SERVICE_STATUS_LABELS[item.status] ?? item.status)
  return parts.join(' · ')
}

export function serviceResourceType(item: AssetServiceRecord): string {
  return ASSET_SERVICE_TYPE_LABELS[item.service_type as AssetServiceType] ?? item.service_type
}

export function serviceResourceStatus(item: AssetServiceRecord): string {
  return ASSET_SERVICE_STATUS_LABELS[item.status] ?? item.status
}

export function serviceResourceExtra(item: AssetServiceRecord): string {
  const parts: string[] = []
  if (item.url.trim()) parts.push(item.url)
  if (item.port != null) parts.push(`端口 ${item.port}`)
  return parts.join(' · ')
}


export function domainResourceName(item: AssetDomainRecord): string {
  return unnamed(item.domain_name, '未命名域名')
}

export function domainResourceSummary(item: AssetDomainRecord): string {
  const parts = [ASSET_DOMAIN_STATUS_LABELS[item.status] ?? item.status]
  if (item.purpose.trim()) parts.push(item.purpose)
  if (item.expires_at?.trim()) parts.push(`到期 ${formatDate(item.expires_at)}`)
  return parts.join(' · ')
}


export function domainResourceStatus(item: AssetDomainRecord): string {
  return ASSET_DOMAIN_STATUS_LABELS[item.status] ?? item.status
}

export function domainResourceExtra(item: AssetDomainRecord): string {
  const parts: string[] = []
  if (item.purpose.trim()) parts.push(item.purpose)
  if (item.expires_at?.trim()) parts.push(`到期 ${formatDate(item.expires_at)}`)
  return parts.join(' · ')
}

const MONITORING_CONFIGURATION_LABELS: Record<string, string> = {
  启用: '启用',
  维护中: '维护中',
  暂停: '暂停',
}

export function monitoringConfigurationLabel(status: string): string {
  const trimmed = status.trim()
  if (!trimmed) return '监控配置未记录'
  return MONITORING_CONFIGURATION_LABELS[trimmed] ?? trimmed
}

export function monitoringObservedHealthLabel(status: string): string {
  const trimmed = status.trim()
  if (!trimmed || trimmed === '—') return '观测健康未记录'
  return trimmed
}

export function monitoringInstanceName(
  item: Pick<VPSMonitoringInstanceSummary, 'display_name' | 'monitoring_instance_id'>,
): string {
  return unnamed(item.display_name, unnamed(item.monitoring_instance_id, '未命名监控实例'))
}

export function monitoringProviderLabel(provider: string): string {
  const trimmed = provider.trim()
  if (!trimmed || trimmed === '—') return '服务商未记录'
  return trimmed
}

export function monitoringRegionLabel(region?: string, city?: string): string {
  const parts = [region, city].map((part) => part?.trim()).filter((part) => Boolean(part) && part !== '—')
  return parts.length > 0 ? parts.join(' · ') : '区域未记录'
}

export function monitoringInstanceDetailHref(vpsId: string, monitoringInstanceId: string): string {
  return `/monitoring/${encodeURIComponent(monitoringInstanceId)}?return_vps=${encodeURIComponent(vpsId)}`
}


export function httpHref(value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  try {
    const url = new URL(trimmed)
    if (url.protocol === 'http:' || url.protocol === 'https:') return trimmed
  } catch {
    return null
  }
  return null
}

