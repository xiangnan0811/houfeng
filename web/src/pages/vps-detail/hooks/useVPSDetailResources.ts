import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { listSubscriptions, listVPSDomains, listVPSServices } from '../../../lib/api'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  SubscriptionRecord,
  VPSOverview,
} from '../../../lib/types'

export type VPSDetailResourceKind = 'subscriptions' | 'services' | 'domains'
export type VPSDetailResourceStatus = 'loading' | 'ready' | 'error'

export type ResourceState<T> = {
  status: VPSDetailResourceStatus
  items: T[]
  error: string | null
}

type ResourceItemMap = {
  subscriptions: SubscriptionRecord[]
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
}

type SettledResource<K extends VPSDetailResourceKind> = {
  overview: VPSOverview
  vpsId: string
  retryRevision: number
  requestId: number
  state: ResourceState<ResourceItemMap[K][number]>
}

type SettledResources = {
  subscriptions: SettledResource<'subscriptions'> | null
  services: SettledResource<'services'> | null
  domains: SettledResource<'domains'> | null
}

type ActiveRequest<K extends VPSDetailResourceKind> = {
  overview: VPSOverview
  vpsId: string
  retryRevision: number
  requestId: number
  promise: Promise<ResourceItemMap[K]>
}

type ActiveRequests = {
  subscriptions: ActiveRequest<'subscriptions'> | null
  services: ActiveRequest<'services'> | null
  domains: ActiveRequest<'domains'> | null
}

type RetryRevisions = Record<VPSDetailResourceKind, number>

const INITIAL_RETRY_REVISIONS: RetryRevisions = {
  subscriptions: 0,
  services: 0,
  domains: 0,
}

const EMPTY_SETTLED_RESOURCES: SettledResources = {
  subscriptions: null,
  services: null,
  domains: null,
}

function createActiveRequests(): ActiveRequests {
  return {
    subscriptions: null,
    services: null,
    domains: null,
  }
}

const RESOURCE_ERROR_COPY: Record<VPSDetailResourceKind, string> = {
  subscriptions: '加载 VPS 订阅失败',
  services: '加载 VPS 服务失败',
  domains: '加载 VPS 域名失败',
}

function describeResourceError(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) return error.message
  return fallback
}

function loadResource<K extends VPSDetailResourceKind>(
  kind: K,
  vpsId: string,
): Promise<ResourceItemMap[K]> {
  switch (kind) {
    case 'subscriptions':
      return listSubscriptions({ vps_id: vpsId, sort: 'renew_at', order: 'asc' }) as Promise<ResourceItemMap[K]>
    case 'services':
      return listVPSServices(vpsId) as Promise<ResourceItemMap[K]>
    case 'domains':
      return listVPSDomains(vpsId) as Promise<ResourceItemMap[K]>
  }
}

export function useVPSDetailResources(overview: VPSOverview): {
  subscriptions: ResourceState<SubscriptionRecord>
  services: ResourceState<AssetServiceRecord>
  domains: ResourceState<AssetDomainRecord>
  retry: (kind: VPSDetailResourceKind) => void
} {
  const vpsId = overview.identity.vps_id
  const [retryRevisions, setRetryRevisions] = useState<RetryRevisions>(INITIAL_RETRY_REVISIONS)
  const retryRevisionsRef = useRef<RetryRevisions>(INITIAL_RETRY_REVISIONS)
  const [settled, setSettled] = useState<SettledResources>(EMPTY_SETTLED_RESOURCES)
  const requestIdRef = useRef(0)
  const currentOverviewRef = useRef(overview)
  const currentVPSIdRef = useRef(vpsId)
  const mountedRef = useRef(false)
  const [activeRequests] = useState<ActiveRequests>(createActiveRequests)
  const activeRequestsRef = useRef<ActiveRequests>(activeRequests)

  useLayoutEffect(() => {
    const previousOverview = currentOverviewRef.current
    currentOverviewRef.current = overview
    currentVPSIdRef.current = vpsId

    if (previousOverview !== overview) {
      for (const kind of ['subscriptions', 'services', 'domains'] as const) {
        const active = activeRequestsRef.current[kind]
        if (active && (active.overview !== overview || active.vpsId !== vpsId)) {
          activeRequestsRef.current[kind] = null
        }
      }
    }
  }, [overview, vpsId])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const ownsRequest = useCallback(<K extends VPSDetailResourceKind>(
    kind: K,
    request: ActiveRequest<K>,
  ): boolean => {
    const active = activeRequestsRef.current[kind] as ActiveRequest<K> | null
    return mountedRef.current
      && active === request
      && active.requestId === request.requestId
      && currentOverviewRef.current === request.overview
      && currentVPSIdRef.current === request.vpsId
      && retryRevisionsRef.current[kind] === request.retryRevision
  }, [])

  const startRequest = useCallback(<K extends VPSDetailResourceKind>(
    kind: K,
    requestedOverview: VPSOverview,
    requestedVPSId: string,
    retryRevision: number,
  ): void => {
    const active = activeRequestsRef.current[kind] as ActiveRequest<K> | null
    if (
      active
      && active.overview === requestedOverview
      && active.vpsId === requestedVPSId
      && active.retryRevision === retryRevision
    ) return

    const requestId = ++requestIdRef.current
    const promise = loadResource(kind, requestedVPSId)
    const request: ActiveRequest<K> = {
      overview: requestedOverview,
      vpsId: requestedVPSId,
      retryRevision,
      requestId,
      promise,
    }
    activeRequestsRef.current[kind] = request as ActiveRequests[K]

    promise.then(
      (items) => {
        setSettled((current) => {
          if (!ownsRequest(kind, request)) return current
          return {
            ...current,
            [kind]: {
              overview: requestedOverview,
              vpsId: requestedVPSId,
              retryRevision,
              requestId,
              state: { status: 'ready', items, error: null },
            },
          } as SettledResources
        })
      },
      (error: unknown) => {
        if (!ownsRequest(kind, request)) return
        setSettled((current) => {
          if (!ownsRequest(kind, request)) return current
          const previous = current[kind] as SettledResource<K> | null
          const previousItems = previous?.vpsId === requestedVPSId ? previous.state.items : []
          return {
            ...current,
            [kind]: {
              overview: requestedOverview,
              vpsId: requestedVPSId,
              retryRevision,
              requestId,
              state: {
                status: 'error',
                items: previousItems,
                error: describeResourceError(error, RESOURCE_ERROR_COPY[kind]),
              },
            },
          } as SettledResources
        })
      },
    )
  }, [ownsRequest])

  useEffect(() => {
    startRequest('subscriptions', overview, vpsId, retryRevisions.subscriptions)
  }, [overview, retryRevisions.subscriptions, startRequest, vpsId])

  useEffect(() => {
    startRequest('services', overview, vpsId, retryRevisions.services)
  }, [overview, retryRevisions.services, startRequest, vpsId])

  useEffect(() => {
    startRequest('domains', overview, vpsId, retryRevisions.domains)
  }, [overview, retryRevisions.domains, startRequest, vpsId])

  const retry = useCallback((kind: VPSDetailResourceKind): void => {
    if (currentOverviewRef.current !== overview || currentVPSIdRef.current !== vpsId) return
    retryRevisionsRef.current = {
      ...retryRevisionsRef.current,
      [kind]: retryRevisionsRef.current[kind] + 1,
    }
    setRetryRevisions((current) => ({ ...current, [kind]: current[kind] + 1 }))
  }, [overview, vpsId])

  const resourceState = useCallback(<K extends VPSDetailResourceKind>(
    kind: K,
    currentRevision: number,
  ): ResourceState<ResourceItemMap[K][number]> => {
    const current = settled[kind] as SettledResource<K> | null
    const currentForVPS = current?.vpsId === vpsId
    if (
      !current
      || !currentForVPS
      || current.overview !== overview
      || current.retryRevision !== currentRevision
    ) {
      return {
        status: 'loading',
        items: currentForVPS ? current.state.items : [],
        error: null,
      }
    }
    return current.state
  }, [overview, settled, vpsId])

  return {
    subscriptions: resourceState('subscriptions', retryRevisions.subscriptions),
    services: resourceState('services', retryRevisions.services),
    domains: resourceState('domains', retryRevisions.domains),
    retry,
  }
}
