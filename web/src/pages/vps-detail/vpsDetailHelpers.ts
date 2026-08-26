import type {
  BillingPeriodUnit,
  CreateAssetDomainInput,
  CreateAssetServiceInput,
  CreateVPSMonitoringInstanceInput,
  CreateVPSSubscriptionInput,
  CreateVPSExperienceLogInput,
  ExtendVPSValidityInput,
  RenewalMode,
  UpdateVPSAssetInput,
  VPSAssetDetail,
} from '../../lib/types'
import {
  billingCycleFromPeriod,
  billingMonthsFromPeriod,
  legacyFlagsFromRenewalMode,
  normalizeBillingPeriodUnit,
  normalizeCurrency,
  normalizePaymentMethod,
  normalizeRenewalMode,
} from '../../lib/assetOptions'
import { parseLabels } from '../assetPageUtils'
import type {
  DomainDraftState,
  ExperienceDraftState,
  FactEditFormState,
  MonitoringInstanceCreateDraftState,
  ServiceDraftState,
  SubscriptionDraftState,
  ValidityExtensionDraftState,
  VPSDetailPageState,
  VPSDetailSelectorState,
} from './types'

export const INITIAL_STATE: VPSDetailPageState = {
  vpsId: null,
  error: null,
  detail: null,
  timeline: null,
  services: [],
  domains: [],
  subscriptions: [],
  subscriptionsError: null,
  ipQuality: null,
  ipQualityError: null,
  cancellationPreview: null,
  cancellationPreviewError: null,
  cancellationResult: null,
}

export const INITIAL_SELECTOR_STATE: VPSDetailSelectorState = {
  monitoringInstancesLoading: false,
  monitoringInstancesError: null,
  monitoring: [],
  providersLoading: false,
  providersError: null,
  providers: [],
  targetsLoading: false,
  targetsError: null,
  targets: [],
}

export const INITIAL_EXPERIENCE_DRAFT: ExperienceDraftState = {
  category: 'note',
  severity: 'info',
  summary: '',
  details: '',
  occurredAt: '',
}

export const INITIAL_SERVICE_DRAFT: ServiceDraftState = {
  name: '',
  serviceType: 'web',
  status: 'active',
  targetID: '',
  url: '',
  port: '',
  labels: '',
  note: '',
}

export const INITIAL_SUBSCRIPTION_DRAFT: SubscriptionDraftState = {
  price: '',
  currency: 'USD',
  customCurrency: '',
  billingPeriodUnit: 'month',
  billingPeriodLength: '1',
  startedAt: '',
  renewAt: '',
  renewalMode: 'manual',
  paymentMethod: '',
  customPaymentMethod: '',
  note: '',
}

export const INITIAL_VALIDITY_EXTENSION_DRAFT: ValidityExtensionDraftState = {
  extendTo: '',
  reason: '',
  fee: '0',
  currency: 'USD',
  customCurrency: '',
  sourceType: 'compensation',
  customSourceType: '',
}

export function monitoringInstanceCreateDraftFromDetail(detail: VPSAssetDetail): MonitoringInstanceCreateDraftState {
  return {
    displayName: detail.display_name,
    group: '',
    region: detail.region || detail.country || '未确认',
    city: detail.city || detail.datacenter || '未确认',
    provider: detail.provider_name || '未关联服务商',
    labels: detail.labels.join(', '),
    note: detail.note,
    linkNote: 'created from vps detail',
  }
}

export const INITIAL_DOMAIN_DRAFT: DomainDraftState = {
  domainName: '',
  status: 'active',
  purpose: '',
  serviceID: '',
  targetID: '',
  registrar: '',
  expiresAt: '',
  autoRenew: false,
  httpsEnabled: true,
  labels: '',
  note: '',
}

export function buildSubscriptionInput(form: SubscriptionDraftState): CreateVPSSubscriptionInput {
  const price = Number.parseFloat(form.price.trim())
  if (!Number.isFinite(price) || price < 0) {
    throw new Error('价格必须为非负数字。')
  }
  const billingPeriodLength = Number.parseInt(form.billingPeriodLength.trim(), 10)
  if (!Number.isInteger(billingPeriodLength) || billingPeriodLength <= 0) {
    throw new Error('计费周期长度必须大于 0。')
  }
  const billingPeriodUnit = normalizeBillingPeriodUnit(form.billingPeriodUnit) as BillingPeriodUnit
  const billingMonths = billingMonthsFromPeriod(billingPeriodUnit, billingPeriodLength)
  const currency = normalizeCurrency(form.currency === '__custom' ? form.customCurrency : form.currency)
  if (!/^[A-Z]{3}$/.test(currency)) {
    throw new Error('币种必须为 3 位大写代码。')
  }
  const renewalMode = normalizeRenewalMode(form.renewalMode) as RenewalMode
  const legacyRenewalFlags = legacyFlagsFromRenewalMode(renewalMode)
  const paymentMethod = normalizePaymentMethod(form.paymentMethod === '__custom' ? form.customPaymentMethod : form.paymentMethod)
  return {
    price,
    currency,
    billing_cycle: billingCycleFromPeriod(billingPeriodUnit, billingPeriodLength),
    billing_months: billingMonths,
    billing_period_unit: billingPeriodUnit,
    billing_period_length: billingPeriodLength,
    started_at: form.startedAt || null,
    renew_at: form.renewAt || null,
    auto_renew: legacyRenewalFlags.auto_renew,
    auto_renew_cancelled: legacyRenewalFlags.auto_renew_cancelled,
    renewal_mode: renewalMode,
    payment_method: paymentMethod,
    note: form.note.trim(),
  }
}

export function buildValidityExtensionInput(form: ValidityExtensionDraftState): ExtendVPSValidityInput {
  const extendTo = form.extendTo.trim()
  if (!extendTo) {
    throw new Error('延长至日期不能为空。')
  }
  const reason = form.reason.trim()
  if (!reason) {
    throw new Error('延长原因不能为空。')
  }
  const fee = Number.parseFloat(form.fee.trim() || '0')
  if (!Number.isFinite(fee) || fee < 0) {
    throw new Error('延长费用必须为非负数字。')
  }
  const currency = normalizeCurrency(form.currency === '__custom' ? form.customCurrency : form.currency)
  if (!/^[A-Z]{3}$/.test(currency)) {
    throw new Error('费用币种必须为 3 位大写代码。')
  }
  const sourceType = (form.sourceType === '__custom' ? form.customSourceType : form.sourceType).trim()
  if (!sourceType) {
    throw new Error('来源类型不能为空。')
  }
  return {
    extend_to: extendTo,
    reason,
    fee,
    fee_currency: currency,
    source_type: sourceType,
  }
}

export function buildMonitoringInstanceCreateInput(form: MonitoringInstanceCreateDraftState): CreateVPSMonitoringInstanceInput {
  const displayName = form.displayName.trim()
  if (!displayName) {
    throw new Error('监控实例名称不能为空。')
  }
  return {
    display_name: displayName,
    group: form.group.trim(),
    region: form.region.trim(),
    city: form.city.trim(),
    provider: form.provider.trim(),
    labels: parseLabels(form.labels),
    note: form.note.trim(),
    link_note: form.linkNote.trim() || 'created from vps detail',
  }
}

export function detailToFactEditForm(detail: VPSAssetDetail): FactEditFormState {
  return {
    displayName: detail.display_name,
    providerID: detail.provider_id ?? '',
    providerName: detail.provider_name,
    productName: detail.product_name,
    orderRef: detail.order_ref,
    country: detail.country,
    region: detail.region,
    city: detail.city,
    datacenter: detail.datacenter,
    ipv4: detail.ipv4,
    ipv6: detail.ipv6,
    sshHost: detail.ssh_host,
    sshPort: String(detail.ssh_port),
    sshUser: detail.ssh_user,
    osName: detail.os_name,
    virtualization: detail.virtualization,
    usageStatus: detail.usage_status,
    importance: detail.importance,
    labels: detail.labels.join(', '),
    note: detail.note,
  }
}

const FACT_EDIT_COMPARE_FIELDS: ReadonlyArray<{
  key: keyof FactEditFormState
  label: string
}> = [
  { key: 'displayName', label: '名称' },
  { key: 'providerID', label: '服务商' },
  { key: 'providerName', label: '服务商名称' },
  { key: 'productName', label: '产品名' },
  { key: 'orderRef', label: '订单号' },
  { key: 'country', label: '国家 / 地区' },
  { key: 'region', label: '区域' },
  { key: 'city', label: '城市' },
  { key: 'datacenter', label: '数据中心' },
  { key: 'ipv4', label: 'IPv4' },
  { key: 'ipv6', label: 'IPv6' },
  { key: 'sshHost', label: 'SSH Host' },
  { key: 'sshPort', label: 'SSH 端口' },
  { key: 'sshUser', label: 'SSH 用户' },
  { key: 'osName', label: '系统' },
  { key: 'virtualization', label: '虚拟化' },
  { key: 'usageStatus', label: '使用状态' },
  { key: 'importance', label: '重要性' },
  { key: 'labels', label: '标签' },
  { key: 'note', label: '备注' },
]

function normalizeFactEditField(form: FactEditFormState, key: keyof FactEditFormState): string {
  switch (key) {
    case 'displayName':
    case 'providerName':
    case 'productName':
    case 'orderRef':
    case 'country':
    case 'region':
    case 'city':
    case 'datacenter':
    case 'ipv4':
    case 'ipv6':
    case 'sshHost':
    case 'sshUser':
    case 'osName':
    case 'virtualization':
    case 'note':
      return form[key].trim()
    case 'providerID':
      return form.providerID.trim()
    case 'sshPort': {
      const sshPort = Number.parseInt(form.sshPort.trim(), 10)
      if (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535) {
        return form.sshPort
      }
      return String(sshPort)
    }
    case 'usageStatus':
      return form.usageStatus
    case 'importance':
      return form.importance.trim() || 'normal'
    case 'labels':
      return parseLabels(form.labels).join('\u0000')
  }
}

function factEditFieldChanged(
  left: FactEditFormState,
  right: FactEditFormState,
  key: keyof FactEditFormState,
): boolean {
  return normalizeFactEditField(left, key) !== normalizeFactEditField(right, key)
}

export function mergeFactDraftWithLatest(
  base: FactEditFormState,
  draft: FactEditFormState,
  latest: VPSAssetDetail,
): FactEditFormState {
  const latestForm = detailToFactEditForm(latest)
  const merged = { ...latestForm }
  for (const { key } of FACT_EDIT_COMPARE_FIELDS) {
    if (factEditFieldChanged(base, draft, key)) {
      Object.assign(merged, { [key]: draft[key] })
    }
  }
  return merged
}

export function compareFactDraftAgainstLatest(
  base: FactEditFormState,
  draft: FactEditFormState,
  latest: VPSAssetDetail,
): Array<{ field: string; yours: string; latest: string }> {
  const latestForm = detailToFactEditForm(latest)
  return FACT_EDIT_COMPARE_FIELDS
    .filter(({ key }) => (
      factEditFieldChanged(base, draft, key)
      && factEditFieldChanged(draft, latestForm, key)
    ))
    .map(({ key, label }) => ({
      field: label,
      yours: String(draft[key] ?? ''),
      latest: String(latestForm[key] ?? ''),
    }))
}

export function buildFactEditInput(form: FactEditFormState): UpdateVPSAssetInput {
  if (form.displayName.trim() === '') {
    throw new Error('VPS 名称不能为空。')
  }
  if (!form.ipv4.trim() && !form.sshHost.trim()) {
    throw new Error('IPv4 或 SSH Host 至少需要填写一个。')
  }
  const sshPort = Number.parseInt(form.sshPort.trim(), 10)
  if (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535) {
    throw new Error('SSH 端口必须为 1 到 65535。')
  }

  return {
    display_name: form.displayName.trim(),
    provider_id: form.providerID.trim() || null,
    provider_name: form.providerName.trim(),
    product_name: form.productName.trim(),
    order_ref: form.orderRef.trim(),
    country: form.country.trim(),
    region: form.region.trim(),
    city: form.city.trim(),
    datacenter: form.datacenter.trim(),
    ipv4: form.ipv4.trim(),
    ipv6: form.ipv6.trim(),
    ssh_host: form.sshHost.trim(),
    ssh_port: sshPort,
    ssh_user: form.sshUser.trim(),
    os_name: form.osName.trim(),
    virtualization: form.virtualization.trim(),
    usage_status: form.usageStatus,
    importance: form.importance.trim() || 'normal',
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

export function buildExperienceLogInput(form: ExperienceDraftState): CreateVPSExperienceLogInput {
  const summary = form.summary.trim()
  if (!summary) {
    throw new Error('经验摘要不能为空。')
  }
  const occurredAt = form.occurredAt.trim()
  const occurredAtISO = occurredAt ? new Date(occurredAt).toISOString() : null

  return {
    category: form.category,
    severity: form.severity,
    summary,
    details: form.details.trim(),
    occurred_at: occurredAtISO,
  }
}

export function buildServiceInput(form: ServiceDraftState): CreateAssetServiceInput {
  const name = form.name.trim()
  if (!name) {
    throw new Error('服务名称不能为空。')
  }
  const rawPort = form.port.trim()
  let port: number | null = null
  if (rawPort) {
    const parsed = Number.parseInt(rawPort, 10)
    if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65535) {
      throw new Error('服务端口必须为 1 到 65535。')
    }
    port = parsed
  }

  return {
    name,
    service_type: form.serviceType,
    status: form.status,
    target_id: form.targetID.trim() || null,
    url: form.url.trim(),
    port,
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

function normalizeDomainName(value: string): string {
  return value.trim().toLowerCase().replace(/\.$/, '')
}

export function buildDomainInput(form: DomainDraftState): CreateAssetDomainInput {
  const domainName = normalizeDomainName(form.domainName)
  if (!domainName) {
    throw new Error('域名不能为空。')
  }
  const hasInvalidChars = /[/:@?#\s]/.test(domainName) ||
    domainName.includes('[') ||
    domainName.includes(']') ||
    domainName.includes('\\')
  if (!domainName.includes('.') || hasInvalidChars) {
    throw new Error('域名必须是不带协议、路径和空格的完整域名。')
  }

  return {
    domain_name: domainName,
    status: form.status,
    purpose: form.purpose.trim(),
    service_id: form.serviceID.trim() || null,
    target_id: form.targetID.trim() || null,
    registrar: form.registrar.trim(),
    expires_at: form.expiresAt.trim() || null,
    auto_renew: form.autoRenew,
    https_enabled: form.httpsEnabled,
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}
