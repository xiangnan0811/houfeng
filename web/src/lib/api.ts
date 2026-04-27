import type {
  ActiveIncidentRecord,
  CreateProbeItemInput,
  CreateTargetInput,
  DashboardOverview,
  EventListFilter,
  IncidentListFilter,
  NodeEnrollmentTokenIssue,
  NodeOnboardingState,
  NodeRecord,
  NodeRuntimeFacts,
  ProbeItemRecord,
  SettingsRecord,
  StateChangeEventRecord,
  SettingsUpdateInput,
  TargetRecord,
  TargetRuntimeFacts,
} from './types'

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { Accept: 'application/json' },
    cache: 'no-store',
    ...init,
  })

  if (!response.ok) {
    let message = `Request failed: ${response.status}`
    const rawBody = await response.text()
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

  return (await response.json()) as T
}

function postJSON<T>(path: string): Promise<T> {
  return requestJSON<T>(path, { method: 'POST' })
}

function postJSONBody<T>(path: string, body: unknown): Promise<T> {
  return requestJSON<T>(path, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
}

function withQuery(
  path: string,
  filter?: Record<string, string | number | null | undefined>,
): string {
  if (!filter) return path

  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(filter)) {
    if (value == null) continue
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

export function getNode(nodeId: string) {
  return requestJSON<NodeRecord>(`/api/nodes/${nodeId}`)
}

export function getNodeRuntimeFacts(nodeId: string) {
  return requestJSON<NodeRuntimeFacts>(`/api/nodes/${nodeId}/runtime-facts`)
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

export function listTargetProbeItems(targetId: string) {
  return requestJSON<ProbeItemRecord[]>(`/api/targets/${targetId}/probe-items`)
}

export function createProbeItem(
  targetId: string,
  input: CreateProbeItemInput,
): Promise<ProbeItemRecord> {
  return postJSONBody<ProbeItemRecord>(`/api/targets/${targetId}/probe-items`, input)
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
  return requestJSON<StateChangeEventRecord[]>(withQuery('/api/events', filter))
}

export function listIncidents(filter?: IncidentListFilter) {
  return requestJSON<ActiveIncidentRecord[]>(withQuery('/api/incidents', filter))
}
