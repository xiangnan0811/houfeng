import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { ApiError } from '../../../lib/apiRequest'
import { getVPSOverview } from '../../../lib/recordsApi'
import type { VPSOverview } from '../../../lib/types'

export type VPSOverviewStatus = 'loading' | 'ready' | 'error' | 'not_found' | 'unavailable'

export type VPSOverviewState = {
  status: VPSOverviewStatus
  overview: VPSOverview | null
  errorMessage: string | null
  errorCode: string | null
}

export type VPSOverviewCommands = {
  refresh: () => Promise<boolean>
}

function describeFailure(error: unknown): {
  status: VPSOverviewStatus
  message: string
  code: string | null
} {
  if (error instanceof ApiError) {
    if (error.status === 404 || error.code === 'resource_not_found') {
      return { status: 'not_found', message: 'VPS 不存在或无权查看。', code: null }
    }
    if (error.code === 'overview_unavailable') {
      return { status: 'unavailable', message: 'VPS 概览不可用。', code: null }
    }
  }
  return {
    status: 'error',
    message: 'VPS 概览请求或响应校验失败，请重试。',
    code: null,
  }
}

export function useVPSOverview(
  vpsId: string | undefined,
  initialOverview?: VPSOverview | null,
): {
  state: VPSOverviewState
  commands: VPSOverviewCommands
} {
  const normalizedVPSId = vpsId?.trim() ?? ''
  const seededOverview = initialOverview?.identity.vps_id === normalizedVPSId
    ? initialOverview
    : null
  const seeded = Boolean(seededOverview)
  const [state, setState] = useState<VPSOverviewState>(() => (
    seededOverview
      ? {
          status: 'ready',
          overview: seededOverview,
          errorMessage: null,
          errorCode: null,
        }
      : {
          status: 'loading',
          overview: null,
          errorMessage: null,
          errorCode: null,
        }
  ))
  const requestIdRef = useRef(0)
  const seededVPSIdRef = useRef(seeded ? normalizedVPSId : null)
  const inFlightRef = useRef<{ vpsId: string; promise: Promise<boolean> } | null>(null)
  const mountedRef = useRef(false)
  const activeVPSIdRef = useRef(normalizedVPSId)

  const invalidateRequests = useCallback(() => {
    requestIdRef.current++
    inFlightRef.current = null
  }, [])

  const load = useCallback((): Promise<boolean> => {
    const requestedVPSId = normalizedVPSId
    if (!requestedVPSId) {
      invalidateRequests()
      setState({
        status: 'not_found',
        overview: null,
        errorMessage: '缺少 VPS ID。',
        errorCode: null,
      })
      return Promise.resolve(false)
    }
    const active = inFlightRef.current
    if (active?.vpsId === requestedVPSId) return active.promise

    const requestId = ++requestIdRef.current
    setState((prev) => ({
      ...prev,
      status: 'loading',
      overview: prev.overview?.identity.vps_id === requestedVPSId ? prev.overview : null,
      errorMessage: null,
      errorCode: null,
    }))
    const promise = (async () => {
      try {
        const overview = await getVPSOverview(requestedVPSId)
        if (
          !mountedRef.current
          || activeVPSIdRef.current !== requestedVPSId
          || requestId !== requestIdRef.current
        ) return false
        setState({
          status: 'ready',
          overview,
          errorMessage: null,
          errorCode: null,
        })
        return true
      } catch (error) {
        if (
          !mountedRef.current
          || activeVPSIdRef.current !== requestedVPSId
          || requestId !== requestIdRef.current
        ) return false
        const failure = describeFailure(error)
        setState((current) => current.overview?.identity.vps_id === requestedVPSId
          ? {
              status: 'ready',
              overview: current.overview,
              errorMessage: failure.message,
              errorCode: failure.code,
            }
          : {
              status: failure.status,
              overview: null,
              errorMessage: failure.message,
              errorCode: failure.code,
            })
        return false
      }
    })()
    inFlightRef.current = { vpsId: requestedVPSId, promise }
    const clear = () => {
      if (inFlightRef.current?.promise === promise) inFlightRef.current = null
    }
    void promise.then(clear, clear)
    return promise
  }, [invalidateRequests, normalizedVPSId])

  useLayoutEffect(() => {
    if (activeVPSIdRef.current === normalizedVPSId) return
    activeVPSIdRef.current = normalizedVPSId
    invalidateRequests()
  }, [invalidateRequests, normalizedVPSId])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    // Capability gate already fetched a successful overview; skip the duplicate
    // first paint request, including StrictMode's setup replay. Refresh still reloads.
    if (seededVPSIdRef.current === normalizedVPSId) return
    seededVPSIdRef.current = null
    void load()
  }, [load, normalizedVPSId])

  const visibleState = state.overview && state.overview.identity.vps_id !== normalizedVPSId
    ? {
        status: normalizedVPSId ? 'loading' as const : 'not_found' as const,
        overview: null,
        errorMessage: normalizedVPSId ? null : '缺少 VPS ID。',
        errorCode: null,
      }
    : state

  return {
    state: visibleState,
    commands: {
      refresh: load,
    },
  }
}
