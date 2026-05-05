import type {
  ActiveIncidentRecord,
  CreateNodeInput,
  CreateProbeItemInput,
  CreateTargetInput,
  UpdateProbeItemInput,
  DashboardOverview,
  EventListFilter,
  IncidentListFilter,
  NodeEnrollmentTokenIssue,
  NodeOnboardingState,
  NodeRecord,
  NodeRuntimeFacts,
  NodeSparklinesResponse,
  ProbeItemRecord,
  SettingsRecord,
  StateChangeEventRecord,
  SettingsUpdateInput,
  TargetRecord,
  TargetRuntimeFacts,
  UpdateNodeMetadataInput,
  UpdateTargetMetadataInput,
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

export function listNodes() {
  return requestJSON<NodeRecord[]>('/api/nodes')
}

export function createNode(input: CreateNodeInput): Promise<NodeRecord> {
  return postJSONBody<NodeRecord>('/api/nodes', {
    ...input,
    lifecycle_status: '待接入',
  })
}

export function getNode(nodeId: string) {
  return requestJSON<NodeRecord>(`/api/nodes/${nodeId}`)
}

type MetadataUpdateOptions = {
  expectedUpdatedAt?: string
}

export function updateNodeMetadata(
  nodeId: string,
  input: UpdateNodeMetadataInput,
  options: MetadataUpdateOptions = {},
) {
  return patchJSONBody<NodeRecord>(`/api/nodes/${nodeId}`, input, {
    ifMatch: options.expectedUpdatedAt,
  })
}

export function getNodeRuntimeFacts(nodeId: string) {
  return requestJSON<NodeRuntimeFacts>(`/api/nodes/${nodeId}/runtime-facts`)
}

export function listNodeSparklines(metrics: string[]) {
  const qs = new URLSearchParams({
    metrics: metrics.join(','),
    window: '24h',
    downsample: '24',
  })
  return requestJSON<NodeSparklinesResponse>(`/api/nodes/sparklines?${qs}`)
}

export function enterNodeMaintenance(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/runtime/enter-maintenance`)
}

export function exitNodeMaintenance(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/runtime/exit-maintenance`)
}

export function pauseNodeMonitoring(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/runtime/pause`)
}

export function resumeNodeMonitoring(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/runtime/resume`)
}

export function retireNode(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/lifecycle/retire`)
}

export function restoreRetiredNodeToObserving(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/lifecycle/restore-to-observing`)
}

export function getNodeOnboarding(nodeId: string) {
  return requestJSON<NodeOnboardingState>(`/api/nodes/${nodeId}/onboarding`)
}

export function issueNodeEnrollmentToken(nodeId: string) {
  return postJSON<NodeEnrollmentTokenIssue>(`/api/nodes/${nodeId}/enrollment-token`)
}

export function confirmNodeRebind(nodeId: string) {
  return postJSON<NodeOnboardingState>(`/api/nodes/${nodeId}/binding/confirm-rebind`)
}

export function rejectPendingNodeBinding(nodeId: string) {
  return postJSON<NodeOnboardingState>(`/api/nodes/${nodeId}/binding/reject-pending`)
}

export function resetNodeBinding(nodeId: string) {
  return postJSON<NodeOnboardingState>(`/api/nodes/${nodeId}/binding/reset`)
}

export function listTargets() {
  return requestJSON<TargetRecord[]>('/api/targets')
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

export function getTargetRuntimeFacts(targetId: string) {
  return requestJSON<TargetRuntimeFacts>(`/api/targets/${targetId}/runtime-facts`)
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
  return requestJSON<StateChangeEventRecord[]>(
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
          }
        : undefined,
    ),
  )
}

export function listIncidents(filter?: IncidentListFilter) {
  return requestJSON<ActiveIncidentRecord[]>(withQuery('/api/incidents', filter))
}

/**
 * Used by the node detail history drawer. Returns both active and resolved
 * incidents (the backend currently only retains active rows in
 * `active_incidents`, so the resolved set is forward-compatible).
 */
export function listHistoricalIncidents(objectType: string, objectId: string) {
  return requestJSON<ActiveIncidentRecord[]>(
    `/api/incidents?object_type=${encodeURIComponent(objectType)}&object_id=${encodeURIComponent(
      objectId,
    )}&include_resolved=true`,
  )
}
