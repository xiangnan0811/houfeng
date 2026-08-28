import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Navigate, useParams, useSearchParams } from 'react-router-dom'

import { PageState } from '../components/PageState'
import { ApiError } from '../lib/apiRequest'
import { getVPSOverview, overviewHasRecordsV2Read } from '../lib/recordsApi'
import type { VPSOverview } from '../lib/types'
import { RouteModuleFallback } from '../app/RouteModuleFallback'
import { VPSOverviewPageView } from './vps-detail/VPSOverviewPageView'
import { VPSOverviewManagementActions } from './vps-detail/VPSOverviewManagementActions'
import { useVPSManagementController } from './vps-detail/hooks/useVPSManagementController'
import { useVPSOverview } from './vps-detail/hooks/useVPSOverview'
import { parseOverviewWorkbench } from './vps-detail/vpsManagementHelpers'
import { createVPSWriteOwnerStore } from './vps-detail/vpsWriteOwnerStore'

const LegacyVPSDetailPage = lazy(() =>
  import('./vps-detail/LegacyVPSDetail').then((module) => ({ default: module.LegacyVPSDetail })),
)

type GateMode = 'probing' | 'overview' | 'legacy' | 'archive' | 'not_found' | 'error'
type SettledGate = {
  vpsId: string
  revision: number
  mode: Exclude<GateMode, 'probing'>
  error: string | null
  overview: VPSOverview | null
}
type GateProbe = {
  vpsId: string
  revision: number
  promise: Promise<VPSOverview>
}
const SAFE_OVERVIEW_FAILURE = 'VPS 概览请求或响应校验失败，请重试。'
const SAFE_VPS_NOT_FOUND = '该 VPS 不存在，或当前账号无权查看。'

function isReadonlyArchiveLifecycle(status: string | undefined): boolean {
  return status === 'cancelled' || status === 'archived'
}

/**
 * Canonical `/vps/:id` entry. Capability-off or an explicitly unavailable overview
 * falls back to the legacy composition. Identity 404 (VPS missing/unauthorized) stays
 * on the overview empty state — it must not open the legacy workbench. Overview
 * load failures after the gate selects overview are surfaced — they do not
 * silently fall back.
 *
 * Legacy is a separate async chunk so overview e2e / production first paint do
 * not pay for the full workbench graph.
 */
export function VPSDetailPage() {
  const { vpsId } = useParams()
  const normalizedVPSId = vpsId?.trim() ?? ''
  const [settledGate, setSettledGate] = useState<SettledGate | null>(null)
  const [probeRevision, setProbeRevision] = useState(0)
  const probeRef = useRef<GateProbe | null>(null)
  const [legacyWriteOwnerStore] = useState(createVPSWriteOwnerStore)
  const currentVPSIdRef = useRef(normalizedVPSId)
  const mountedRef = useRef(false)

  useLayoutEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useLayoutEffect(() => {
    currentVPSIdRef.current = normalizedVPSId
  }, [normalizedVPSId])

  function revalidateInvalidatedLegacyWrite(vpsId: string) {
    if (!mountedRef.current || currentVPSIdRef.current !== vpsId) return
    setProbeRevision((revision) => revision + 1)
  }

  useEffect(() => {
    let cancelled = false
    if (!normalizedVPSId) {
      return () => {
        cancelled = true
      }
    }

    let probe = probeRef.current
    if (
      !probe
      || probe.vpsId !== normalizedVPSId
      || probe.revision !== probeRevision
    ) {
      probe = {
        vpsId: normalizedVPSId,
        revision: probeRevision,
        promise: getVPSOverview(normalizedVPSId),
      }
      probeRef.current = probe
    }

    void probe.promise
      .then((overview) => {
        if (cancelled || probeRef.current !== probe) return
        if (isReadonlyArchiveLifecycle(overview.identity.lifecycle_status)) {
          setSettledGate({
            vpsId: normalizedVPSId,
            revision: probeRevision,
            mode: 'archive',
            error: null,
            overview,
          })
        } else if (overviewHasRecordsV2Read(overview)) {
          setSettledGate({
            vpsId: normalizedVPSId,
            revision: probeRevision,
            mode: 'overview',
            error: null,
            overview,
          })
        } else {
          setSettledGate({
            vpsId: normalizedVPSId,
            revision: probeRevision,
            mode: 'legacy',
            error: null,
            overview: null,
          })
        }
      })
      .catch((error: unknown) => {
        if (cancelled || probeRef.current !== probe) return
        if (error instanceof ApiError) {
          if (error.status === 404 || error.code === 'resource_not_found') {
            setSettledGate({
              vpsId: normalizedVPSId,
              revision: probeRevision,
              mode: 'not_found',
              error: SAFE_VPS_NOT_FOUND,
              overview: null,
            })
            return
          }
          if (error.code === 'overview_unavailable') {
            setSettledGate({
              vpsId: normalizedVPSId,
              revision: probeRevision,
              mode: 'legacy',
              error: null,
              overview: null,
            })
            return
          }
        }
        setSettledGate({
          vpsId: normalizedVPSId,
          revision: probeRevision,
          mode: 'error',
          error: SAFE_OVERVIEW_FAILURE,
          overview: null,
        })
      })

    return () => {
      cancelled = true
    }
  }, [normalizedVPSId, probeRevision])

  const ownedGate = settledGate?.vpsId === normalizedVPSId
    && settledGate.revision === probeRevision
    ? settledGate
    : null
  const gate: GateMode = !normalizedVPSId ? 'not_found' : (ownedGate?.mode ?? 'probing')
  const gateError = !normalizedVPSId ? SAFE_VPS_NOT_FOUND : (ownedGate?.error ?? null)
  const seededOverview = ownedGate?.overview ?? null

  if (gate === 'probing') {
    return <PageState kind="loading" title="正在判定 VPS 详情形态" />
  }

  if (gate === 'not_found') {
    return (
      <PageState
        kind="error"
        title="未找到 VPS"
        description={gateError ?? SAFE_VPS_NOT_FOUND}
      />
    )
  }

  if (gate === 'error') {
    return (
      <PageState
        kind="error"
        title="无法加载 VPS 概览"
        description={gateError ?? SAFE_OVERVIEW_FAILURE}
        action={(
          <button
            type="button"
            className="btn sm secondary"
            onClick={() => setProbeRevision((revision) => revision + 1)}
          >
            重试
          </button>
        )}
      />
    )
  }

  if (gate === 'archive') {
    return <Navigate to={`/archive/${encodeURIComponent(normalizedVPSId)}`} replace />
  }

  if (gate === 'legacy') {
    return (
      <Suspense fallback={<RouteModuleFallback label="正在加载 VPS 详情" />}>
        <LegacyVPSDetailPage
          writeOwnerStore={legacyWriteOwnerStore}
          onViewAuthorityInvalidatedWriteSettled={revalidateInvalidatedLegacyWrite}
        />
      </Suspense>
    )
  }

  return <VPSOverviewRoute vpsId={normalizedVPSId} initialOverview={seededOverview} />
}

function VPSOverviewRoute({
  vpsId,
  initialOverview,
}: {
  vpsId: string | undefined
  initialOverview: VPSOverview | null
}) {
  const { state, commands } = useVPSOverview(vpsId, initialOverview)
  const management = useVPSManagementController()
  const managementTriggerRef = useRef<HTMLButtonElement>(null)
  const [searchParams, setSearchParams] = useSearchParams()

  useEffect(() => {
    if (!searchParams.has('workbench')) return
    const panel = parseOverviewWorkbench(searchParams.get('workbench'))
    if (panel) management.openPanel(panel)
    const next = new URLSearchParams(searchParams)
    next.delete('workbench')
    setSearchParams(next, { replace: true })
  }, [management, searchParams, setSearchParams])

  if (state.status === 'loading' && !state.overview) {
    return <PageState kind="loading" title="正在加载 VPS 概览" />
  }

  if (state.status === 'not_found') {
    return (
      <PageState
        kind="error"
        title="未找到 VPS"
        description={state.errorMessage ?? undefined}
      />
    )
  }

  if (state.status === 'unavailable' || state.status === 'error' || !state.overview) {
    return (
      <PageState
        kind="error"
        title="VPS 概览不可用"
        description={state.errorMessage ?? undefined}
      />
    )
  }

  const lifecycleStatus = state.overview.identity.lifecycle_status
  if (lifecycleStatus === 'cancelled' || lifecycleStatus === 'archived') {
    return <Navigate to={`/archive/${encodeURIComponent(vpsId ?? '')}`} replace />
  }

  return (
    <>
      <VPSOverviewPageView
        overview={state.overview}
        management={management}
        managementTriggerRef={managementTriggerRef}
        onRefresh={commands.refresh}
        retrying={state.status === 'loading'}
        refreshError={state.errorMessage}
      />
      <VPSOverviewManagementActions
        vpsId={state.overview.identity.vps_id}
        displayName={state.overview.identity.display_name}
        lifecycleStatus={state.overview.identity.lifecycle_status}
        management={management}
        managementTriggerRef={managementTriggerRef}
        onOverviewRefresh={commands.refresh}
      />
    </>
  )
}
