import type {
  ActiveIncidentRecord,
  AssetDomainListFilter,
  AssetDomainRecord,
  AssetContextForMonitoringInstance,
  AssetContextForTarget,
  AssetServiceListFilter,
  AssetServiceRecord,
  ApplyCancellationInput,
  CancellationPreview,
  CreateAssetDomainInput,
  CreateProviderInput,
  CreateAssetServiceInput,
  CreateMonitoringInstanceInput,
  CreateProbeItemInput,
  CreateSubscriptionInput,
  CreateVPSMonitoringInstanceInput,
  CreateVPSMonitoringInstanceResponse,
  CreateTargetInput,
  CreateVPSAssetInput,
  CreateVPSExperienceLogInput,
  CreateVPSSubscriptionInput,
  LinkVPSMonitoringInstanceInput,
  LifecycleActionResult,
  UpdateProbeItemInput,
  DashboardOverview,
  EventListFilter,
  EventListResponse,
  IncidentListFilter,
  MonitoringInstanceInstallCommandIssue,
  MonitoringInstanceOnboardingState,
  MonitoringInstanceRecord,
  MonitoringInstanceRuntimeFacts,
  MonitoringInstanceSparklinesResponse,
  ProbeItemRecord,
  ProviderRecord,
  SettingsRecord,
  StateChangeEventRecord,
  SettingsUpdateInput,
  SubscriptionListFilter,
  SubscriptionRecord,
  TargetRecord,
  TargetRuntimeFacts,
  TargetSparklinesResponse,
  UnlinkVPSMonitoringInstanceInput,
  UpdateMonitoringInstanceMetadataInput,
  UpdateProviderInput,
  UpdateSubscriptionInput,
  UpdateTargetMetadataInput,
  UpdateVPSAssetInput,
  VPSAssetDetail,
  VPSAssetListFilter,
  VPSAssetRecord,
  VPSAssetUpdateResult,
  VPSExperienceLogRecord,
  VPSMonitoringInstanceLinkRecord,
  VPSMonitoringInstanceSummary,
  VPSSummary,
  VPSTimeline,
} from './types'

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

// Set by AuthProvider on mount; fired when any /api/* responds with 401.
let onUnauthorized: (() => void) | undefined
export function setUnauthorizedHandler(h: (() => void) | undefined): void {
  onUnauthorized = h
}

async function request(path: string, init?: RequestInit): Promise<string> {
  const response = await fetch(path, {
    headers: { Accept: 'application/json' },
    cache: 'no-store',
    credentials: 'include',
    ...init,
  })

  const rawBody = await response.text()

  if (response.status === 401) {
    onUnauthorized?.()
    throw new ApiError(401, 'unauthenticated')
  }

  if (!response.ok) {
    let message = `Request failed: ${response.status}`
    if (rawBody.trim()) {
      try {
        const errorBody = JSON.parse(rawBody) as { error?: string; message?: string }
        message = errorBody.error ?? errorBody.message ?? rawBody
      } catch {
        message = rawBody
      }
    }
    throw new ApiError(response.status, message)
  }

  return rawBody
}

export async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const rawBody = await request(path, init)
  return JSON.parse(rawBody) as T
}

export async function requestEmpty(path: string, init?: RequestInit): Promise<void> {
  await request(path, init)
}

export function postJSON<T>(path: string): Promise<T> {
  return requestJSON<T>(path, { method: 'POST' })
}

export function postJSONBody<T>(path: string, body: unknown): Promise<T> {
  return requestJSON<T>(path, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
}

type PatchJSONOptions = {
  ifMatch?: string
}

function patchJSONBody<T>(path: string, body: unknown, options: PatchJSONOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'Content-Type': 'application/json',
  }
  if (options.ifMatch) {
    headers['If-Match'] = `"${options.ifMatch}"`
  }

  return requestJSON<T>(path, {
    method: 'PATCH',
    headers,
    body: JSON.stringify(body),
  })
}

function withQuery(
  path: string,
  filter?: Record<string, string | number | boolean | null | undefined>,
): string {
  if (!filter) return path

  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filter)) {
    if (value == null) continue
    if (typeof value === 'boolean' && !value) continue
    const normalized = typeof value === 'string' ? value.trim() : String(value)
    if (!normalized) continue
    query.set(key, normalized)
  }

  const suffix = query.toString()
  return suffix ? `${path}?${suffix}` : path
}

export function listMonitoringInstances() {
  return requestJSON<MonitoringInstanceRecord[]>('/api/monitoring-instances')
}

export function createMonitoringInstance(input: CreateMonitoringInstanceInput): Promise<MonitoringInstanceRecord> {
  return postJSONBody<MonitoringInstanceRecord>('/api/monitoring-instances', {
    ...input,
    lifecycle_status: '待接入',
  })
}

export function getMonitoringInstance(monitoringInstanceId: string) {
  return requestJSON<MonitoringInstanceRecord>(`/api/monitoring-instances/${monitoringInstanceId}`)
}

type MetadataUpdateOptions = {
  expectedUpdatedAt?: string
}

export function updateMonitoringInstanceMetadata(
  monitoringInstanceId: string,
  input: UpdateMonitoringInstanceMetadataInput,
  options: MetadataUpdateOptions = {},
) {
  return patchJSONBody<MonitoringInstanceRecord>(`/api/monitoring-instances/${monitoringInstanceId}`, input, {
    ifMatch: options.expectedUpdatedAt,
  })
}

export function getMonitoringInstanceRuntimeFacts(monitoringInstanceId: string, timeWindow = '24h') {
  return requestJSON<MonitoringInstanceRuntimeFacts>(
    `/api/monitoring-instances/${monitoringInstanceId}/runtime-facts?window=${timeWindow}`,
  )
}

export function listMonitoringInstanceSparklines(metrics: string[]) {
  const qs = new URLSearchParams({
    metrics: metrics.join(','),
    window: '24h',
    downsample: '24',
  })
  return requestJSON<MonitoringInstanceSparklinesResponse>(`/api/monitoring-instances/sparklines?${qs}`)
}

export function enterMonitoringInstanceMaintenance(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceRecord>(
    `/api/monitoring-instances/${monitoringInstanceId}/runtime/enter-maintenance`,
  )
}

export function exitMonitoringInstanceMaintenance(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceRecord>(
    `/api/monitoring-instances/${monitoringInstanceId}/runtime/exit-maintenance`,
  )
}

export function pauseMonitoringInstanceMonitoring(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceRecord>(`/api/monitoring-instances/${monitoringInstanceId}/runtime/pause`)
}

export function resumeMonitoringInstanceMonitoring(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceRecord>(`/api/monitoring-instances/${monitoringInstanceId}/runtime/resume`)
}

export function getMonitoringInstanceOnboarding(monitoringInstanceId: string) {
  return requestJSON<MonitoringInstanceOnboardingState>(
    `/api/monitoring-instances/${monitoringInstanceId}/onboarding`,
  )
}

export function issueMonitoringInstanceInstallCommand(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceInstallCommandIssue>(
    `/api/monitoring-instances/${monitoringInstanceId}/install-command`,
  )
}

export function confirmMonitoringInstanceRebind(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceOnboardingState>(
    `/api/monitoring-instances/${monitoringInstanceId}/binding/confirm-rebind`,
  )
}

export function rejectPendingMonitoringInstanceBinding(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceOnboardingState>(
    `/api/monitoring-instances/${monitoringInstanceId}/binding/reject-pending`,
  )
}

export function resetMonitoringInstanceBinding(monitoringInstanceId: string) {
  return postJSON<MonitoringInstanceOnboardingState>(
    `/api/monitoring-instances/${monitoringInstanceId}/binding/reset`,
  )
}

export function listTargets() {
  return requestJSON<TargetRecord[]>('/api/targets')
}

export function listTargetSparklines() {
  const qs = new URLSearchParams({
    metrics: 'latency',
    window: '24h',
    downsample: '24',
  })
  return requestJSON<TargetSparklinesResponse>(`/api/targets/sparklines?${qs}`)
}

export function createTarget(input: CreateTargetInput): Promise<TargetRecord> {
  return postJSONBody<TargetRecord>('/api/targets', input)
}

export function getTarget(targetId: string) {
  return requestJSON<TargetRecord>(`/api/targets/${targetId}`)
}

export function updateTargetMetadata(
  targetId: string,
  input: UpdateTargetMetadataInput,
  options: MetadataUpdateOptions = {},
) {
  return patchJSONBody<TargetRecord>(`/api/targets/${targetId}`, input, {
    ifMatch: options.expectedUpdatedAt,
  })
}

export function listTargetProbeItems(targetId: string) {
  return requestJSON<ProbeItemRecord[]>(`/api/targets/${targetId}/probe-items`)
}

export function createProbeItem(
  targetId: string,
  input: CreateProbeItemInput,
): Promise<ProbeItemRecord> {
  return postJSONBody<ProbeItemRecord>(`/api/targets/${targetId}/probe-items`, input)
}

export function updateProbeItem(
  targetId: string,
  probeItemId: string,
  input: UpdateProbeItemInput,
): Promise<ProbeItemRecord> {
  return requestJSON<ProbeItemRecord>(`/api/targets/${targetId}/probe-items/${probeItemId}`, {
    method: 'PUT',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  })
}

export function deleteProbeItem(targetId: string, probeItemId: string): Promise<void> {
  return requestEmpty(`/api/targets/${targetId}/probe-items/${probeItemId}`, {
    method: 'DELETE',
  })
}

export function getTargetRuntimeFacts(targetId: string, timeWindow = '24h') {
  return requestJSON<TargetRuntimeFacts>(`/api/targets/${targetId}/runtime-facts?window=${timeWindow}`)
}

export function enterTargetMaintenance(targetId: string) {
  return postJSON<TargetRecord>(`/api/targets/${targetId}/runtime/enter-maintenance`)
}

export function exitTargetMaintenance(targetId: string) {
  return postJSON<TargetRecord>(`/api/targets/${targetId}/runtime/exit-maintenance`)
}

export function pauseTarget(targetId: string) {
  return postJSON<TargetRecord>(`/api/targets/${targetId}/runtime/pause`)
}

export function resumeTarget(targetId: string) {
  return postJSON<TargetRecord>(`/api/targets/${targetId}/runtime/resume`)
}

export function archiveTarget(targetId: string) {
  return postJSON<TargetRecord>(`/api/targets/${targetId}/runtime/archive`)
}

export function restoreTargetToPaused(targetId: string) {
  return postJSON<TargetRecord>(`/api/targets/${targetId}/runtime/restore-to-paused`)
}

export function getDashboard() {
  return requestJSON<DashboardOverview>('/api/dashboard')
}

export function getSettings() {
  return requestJSON<SettingsRecord>('/api/settings')
}

export function updateSettings(settings: SettingsUpdateInput) {
  return requestJSON<SettingsRecord>('/api/settings', {
    method: 'PUT',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(settings),
  })
}

export function listEvents(filter?: EventListFilter) {
  return requestJSON<EventListResponse | StateChangeEventRecord[]>(
    withQuery(
      '/api/events',
      filter
        ? {
            object_type: filter.object_type,
            object_id: filter.object_id,
            severity: filter.severity,
            event_type: filter.event_type,
            limit: filter.limit,
            created_from: filter.created_from,
            created_to: filter.created_to,
            label: filter.label,
            notification_only: filter.notification_only,
            recovery_only: filter.recovery_only,
            maintenance_only: filter.maintenance_only,
            include_backfilled: filter.include_backfilled,
          }
        : undefined,
    ),
  ).then((response) => Array.isArray(response) ? response : response.items)
}

export function listIncidents(filter?: IncidentListFilter) {
  return requestJSON<ActiveIncidentRecord[]>(withQuery('/api/incidents', filter))
}

export function postMonitoringInstanceAction(monitoringInstanceId: string, commandId: string) {
  return postJSONBody<{ action_id: string; command_id: string; status: 'pending' }>(
    `/api/monitoring-instances/${monitoringInstanceId}/actions`,
    {
      command_id: commandId,
    },
  )
}

export type BatchActionResult = {
  monitoring_instance_id: string
  ok: boolean
  error?: string
}

export function postMonitoringInstanceBatch(monitoringInstanceIDs: string[], action: string) {
  return postJSONBody<{ results: BatchActionResult[] }>('/api/monitoring-instances/batch', {
    monitoring_instance_ids: monitoringInstanceIDs,
    action,
  })
}

export function listHistoricalIncidents(objectType: string, objectId: string) {
  return requestJSON<ActiveIncidentRecord[]>(
    `/api/incidents?object_type=${encodeURIComponent(objectType)}&object_id=${encodeURIComponent(
      objectId,
    )}&include_resolved=true`,
  )
}

export function listProviders() {
  return requestJSON<ProviderRecord[]>('/api/providers')
}

export function listAssetServices(filter?: AssetServiceListFilter) {
  return requestJSON<AssetServiceRecord[]>(
    withQuery('/api/services', {
      vps_id: filter?.vps_id,
      target_id: filter?.target_id,
      service_type: filter?.service_type,
      status: filter?.status,
    }),
  )
}

export function createAssetService(input: CreateAssetServiceInput): Promise<AssetServiceRecord> {
  return postJSONBody<AssetServiceRecord>('/api/services', input)
}

export function listAssetDomains(filter?: AssetDomainListFilter) {
  return requestJSON<AssetDomainRecord[]>(
    withQuery('/api/domains', {
      vps_id: filter?.vps_id,
      service_id: filter?.service_id,
      target_id: filter?.target_id,
      status: filter?.status,
    }),
  )
}

export function createAssetDomain(input: CreateAssetDomainInput): Promise<AssetDomainRecord> {
  return postJSONBody<AssetDomainRecord>('/api/domains', input)
}

export function createProvider(input: CreateProviderInput): Promise<ProviderRecord> {
  return postJSONBody<ProviderRecord>('/api/providers', input)
}

export function getProvider(providerId: string) {
  return requestJSON<ProviderRecord>(`/api/providers/${providerId}`)
}

export function updateProvider(providerId: string, input: UpdateProviderInput): Promise<ProviderRecord> {
  return patchJSONBody<ProviderRecord>(`/api/providers/${providerId}`, input)
}

export function listVPSAssets(filter?: VPSAssetListFilter) {
  return requestJSON<VPSAssetRecord[]>(
    withQuery('/api/vps', {
      provider_id: filter?.provider_id,
      lifecycle_status: filter?.lifecycle_status,
      usage_status: filter?.usage_status,
      renewal_decision: filter?.renewal_decision,
    }),
  )
}

export function createVPSAsset(input: CreateVPSAssetInput): Promise<VPSAssetRecord> {
  return postJSONBody<VPSAssetRecord>('/api/vps', input)
}

export function getVPSAsset(vpsId: string) {
  return requestJSON<VPSAssetDetail>(`/api/vps/${vpsId}`)
}

export function updateVPSAsset(vpsId: string, input: UpdateVPSAssetInput): Promise<VPSAssetUpdateResult> {
  return patchJSONBody<VPSAssetUpdateResult>(`/api/vps/${vpsId}`, input)
}

export function getVPSCancellationPreview(vpsId: string) {
  return requestJSON<CancellationPreview>(`/api/vps/${vpsId}/cancellation-preview`)
}

export function applyVPSCancellation(vpsId: string, input: ApplyCancellationInput): Promise<LifecycleActionResult> {
  return postJSONBody<LifecycleActionResult>(`/api/vps/${vpsId}/cancellation`, input)
}

export function getVPSTimeline(vpsId: string) {
  return requestJSON<VPSTimeline>(`/api/vps/${vpsId}/timeline`)
}

export function listVPSExperienceLogs(vpsId: string) {
  return requestJSON<VPSExperienceLogRecord[]>(`/api/vps/${vpsId}/experience-logs`)
}

export function createVPSExperienceLog(vpsId: string, input: CreateVPSExperienceLogInput): Promise<VPSExperienceLogRecord> {
  return postJSONBody<VPSExperienceLogRecord>(`/api/vps/${vpsId}/experience-logs`, input)
}

export function listVPSServices(vpsId: string) {
  return requestJSON<AssetServiceRecord[]>(`/api/vps/${vpsId}/services`)
}

export function createVPSService(vpsId: string, input: CreateAssetServiceInput): Promise<AssetServiceRecord> {
  const body: Omit<CreateAssetServiceInput, 'vps_id'> = {
    target_id: input.target_id,
    name: input.name,
    service_type: input.service_type,
    status: input.status,
    url: input.url,
    port: input.port,
    labels: input.labels,
    note: input.note,
  }
  return postJSONBody<AssetServiceRecord>(`/api/vps/${vpsId}/services`, body)
}

export function listVPSDomains(vpsId: string) {
  return requestJSON<AssetDomainRecord[]>(`/api/vps/${vpsId}/domains`)
}

export function createVPSDomain(vpsId: string, input: CreateAssetDomainInput): Promise<AssetDomainRecord> {
  const body: Omit<CreateAssetDomainInput, 'vps_id'> = {
    service_id: input.service_id,
    target_id: input.target_id,
    domain_name: input.domain_name,
    purpose: input.purpose,
    status: input.status,
    registrar: input.registrar,
    expires_at: input.expires_at,
    auto_renew: input.auto_renew,
    https_enabled: input.https_enabled,
    labels: input.labels,
    note: input.note,
  }
  return postJSONBody<AssetDomainRecord>(`/api/vps/${vpsId}/domains`, body)
}

export function listVPSMonitoringInstances(vpsId: string) {
  return requestJSON<VPSMonitoringInstanceSummary[]>(`/api/vps/${vpsId}/monitoring-instances`)
}

export function createVPSMonitoringInstance(
  vpsId: string,
  input: CreateVPSMonitoringInstanceInput = {},
): Promise<CreateVPSMonitoringInstanceResponse> {
  return postJSONBody<CreateVPSMonitoringInstanceResponse>(`/api/vps/${vpsId}/monitoring-instances`, input)
}

export function linkVPSMonitoringInstance(vpsId: string, input: LinkVPSMonitoringInstanceInput): Promise<VPSMonitoringInstanceLinkRecord> {
  return postJSONBody<VPSMonitoringInstanceLinkRecord>(`/api/vps/${vpsId}/link-monitoring-instance`, input)
}

export function unlinkVPSMonitoringInstance(vpsId: string, input: UnlinkVPSMonitoringInstanceInput): Promise<VPSMonitoringInstanceLinkRecord> {
  return postJSONBody<VPSMonitoringInstanceLinkRecord>(`/api/vps/${vpsId}/unlink-monitoring-instance`, input)
}

export function listVPSForMonitoringInstance(monitoringInstanceId: string) {
  return requestJSON<VPSSummary[]>(`/api/monitoring-instances/${monitoringInstanceId}/vps`)
}

export function listMonitoringInstanceAssetContexts() {
  return requestJSON<AssetContextForMonitoringInstance[]>('/api/asset-context/monitoring-instances').then((contexts) =>
    Array.isArray(contexts) ? contexts : [],
  )
}

export function listTargetAssetContexts() {
  return requestJSON<AssetContextForTarget[]>('/api/asset-context/targets').then((contexts) =>
    Array.isArray(contexts) ? contexts : [],
  )
}

export function listSubscriptions(filter?: SubscriptionListFilter) {
  return requestJSON<SubscriptionRecord[]>(
    withQuery('/api/subscriptions', {
      vps_id: filter?.vps_id,
      status: filter?.status,
      renew_before: filter?.renew_before,
      renew_after: filter?.renew_after,
      renew_within_days: filter?.renew_within_days,
      sort: filter?.sort,
      order: filter?.order,
    }),
  )
}

export function createSubscription(input: CreateSubscriptionInput): Promise<SubscriptionRecord> {
  return postJSONBody<SubscriptionRecord>('/api/subscriptions', input)
}

export function createVPSSubscription(vpsId: string, input: CreateVPSSubscriptionInput): Promise<SubscriptionRecord> {
  return postJSONBody<SubscriptionRecord>(`/api/vps/${vpsId}/subscriptions`, input)
}

export function getSubscription(subscriptionId: string) {
  return requestJSON<SubscriptionRecord>(`/api/subscriptions/${subscriptionId}`)
}

export function updateSubscription(subscriptionId: string, input: UpdateSubscriptionInput): Promise<SubscriptionRecord> {
  return patchJSONBody<SubscriptionRecord>(`/api/subscriptions/${subscriptionId}`, input)
}
