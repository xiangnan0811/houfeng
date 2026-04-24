import type {
  NodeRecord,
  NodeRuntimeFacts,
  ProbeItemRecord,
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

async function requestJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, {
    headers: { Accept: 'application/json' },
    cache: 'no-store',
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

export function listNodes() {
  return requestJSON<NodeRecord[]>('/api/nodes')
}

export function getNode(nodeId: string) {
  return requestJSON<NodeRecord>(`/api/nodes/${nodeId}`)
}

export function getNodeRuntimeFacts(nodeId: string) {
  return requestJSON<NodeRuntimeFacts>(`/api/nodes/${nodeId}/runtime-facts`)
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
