import type {
  ActiveIncidentRecord,
  DashboardOverview,
  EventListFilter,
  IncidentListFilter,
  NodeEnrollmentTokenIssue,
  NodeOnboardingState,
  NodeRecord,
  NodeRuntimeFacts,
  ProbeItemRecord,
  StateChangeEventRecord,
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

export function getTarget(targetId: string) {
  return requestJSON<TargetRecord>(`/api/targets/${targetId}`)
}

export function listTargetProbeItems(targetId: string) {
  return requestJSON<ProbeItemRecord[]>(`/api/targets/${targetId}/probe-items`)
}

export function getTargetRuntimeFacts(targetId: string) {
  return requestJSON<TargetRuntimeFacts>(`/api/targets/${targetId}/runtime-facts`)
}

export function getDashboard() {
  return requestJSON<DashboardOverview>('/api/dashboard')
}

export function listEvents(filter?: EventListFilter) {
  return requestJSON<StateChangeEventRecord[]>(withQuery('/api/events', filter))
}

export function listIncidents(filter?: IncidentListFilter) {
  return requestJSON<ActiveIncidentRecord[]>(withQuery('/api/incidents', filter))
}
