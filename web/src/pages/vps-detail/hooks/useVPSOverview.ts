import { useCallback, useEffect, useRef, useState } from 'react'

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
    const code = typeof error.code === 'string' ? error.code : null
    if (error.status === 404) {
      return { status: 'not_found', message: 'VPS 不存在或无权查看。', code }
    }
    if (code === 'overview_unavailable' || error.status === 503) {
      return { status: 'unavailable', message: 'VPS 概览不可用。', code }
    }
    return { status: 'error', message: error.message, code }
  }
  return { status: 'error', message: '加载 VPS 概览失败。', code: null }
}

export function useVPSOverview(
  vpsId: string | undefined,
  initialOverview?: VPSOverview | null,
): {
  state: VPSOverviewState
  commands: VPSOverviewCommands
} {
  const seeded = Boolean(initialOverview)
  const [state, setState] = useState<VPSOverviewState>(() => (
    initialOverview
      ? {
          status: 'ready',
          overview: initialOverview,
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
  const seededRef = useRef(seeded)

  const load = useCallback(async () => {
    if (!vpsId?.trim()) {
      setState({
        status: 'not_found',
        overview: null,
        errorMessage: '缺少 VPS ID。',
        errorCode: null,
      })
      return false
    }
    const requestId = ++requestIdRef.current
    setState((prev) => ({
      ...prev,
      status: 'loading',
      errorMessage: null,
      errorCode: null,
    }))
    try {
      const overview = await getVPSOverview(vpsId.trim())
      if (requestId !== requestIdRef.current) return false
      setState({
        status: 'ready',
        overview,
        errorMessage: null,
        errorCode: null,
      })
      return true
    } catch (error) {
      if (requestId !== requestIdRef.current) return false
      const failure = describeFailure(error)
      setState((current) => current.overview
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
  }, [vpsId])

  useEffect(() => {
    // Capability gate already fetched a successful overview; skip the duplicate
    // first paint request. Refresh still reloads.
    if (seededRef.current) {
      seededRef.current = false
      return
    }
    void load()
  }, [load])

  return {
    state,
    commands: {
      refresh: load,
    },
  }
}
