import type {
  CreateAssetDomainInput,
  CreateAssetServiceInput,
  CreateVPSExperienceLogInput,
  UpdateVPSAssetInput,
  VPSAssetDetail,
} from '../../lib/types'
import { parseLabels } from '../assetPageUtils'
import type {
  DomainDraftState,
  ExperienceDraftState,
  FactEditFormState,
  ServiceDraftState,
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

export function buildFactEditInput(form: FactEditFormState): UpdateVPSAssetInput {
  if (form.displayName.trim() === '') {
    throw new Error('VPS 名称不能为空。')
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
