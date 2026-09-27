import { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { Navigate, useLocation, useParams, useSearchParams } from 'react-router-dom'

import { PageState } from '../components/PageState'
import { ApiError } from '../lib/apiRequest'
import { getVPSOverview, overviewHasRecordsV2Read } from '../lib/recordsApi'
import type { VPSOverview } from '../lib/types'
import { VPSOverviewPageView } from './vps-detail/VPSOverviewPageView'
import { RestoreReorganizationPanel } from '../components/RestoreReorganizationPanel'
import { VPSOverviewManagementActions } from './vps-detail/VPSOverviewManagementActions'
import { useVPSManagementController } from './vps-detail/hooks/useVPSManagementController'
import { useVPSOverview } from './vps-detail/hooks/useVPSOverview'
import { parseOverviewWorkbench } from './vps-detail/vpsManagementHelpers'
import { useOptionalVPSWriteRegistry } from '../lib/vpsWriteRegistry-context'
import { createVPSWriteOwnerStore, type VPSWriteOwnerStore } from './vps-detail/vpsWriteOwnerStore'
import { READ_ONLY_PREVIEW } from '../lib/readOnlyPreview'

import './vps-detail/VPSDetailWorkspace.css'

type GateMode = 'probing' | 'overview' | 'archive' | 'not_found' | 'error'
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
  return status === 'archived'
}

/** Canonical fresh-install overview route; failed authority reads never open a second write surface. */
export function VPSDetailPage() {
  const { pathname, hash } = useLocation()

  useLayoutEffect(() => {
    const main = document.getElementById('main-content')
    if (main && !hash) main.scrollTop = 0
  }, [pathname, hash])

  return (
    <div className="vps-detail-route">
      <VPSDetailRoute />
    </div>
  )
}

function VPSDetailRoute() {
  const { vpsId } = useParams()
  const location = useLocation()
  const normalizedVPSId = vpsId?.trim() ?? ''
  const [settledGate, setSettledGate] = useState<SettledGate | null>(null)
  const [probeRevision, setProbeRevision] = useState(0)
  const probeRef = useRef<GateProbe | null>(null)
  const contextWriteOwnerStore = useOptionalVPSWriteRegistry()
  const [localWriteOwnerStore] = useState(createVPSWriteOwnerStore)
  const writeOwnerStore = contextWriteOwnerStore ?? localWriteOwnerStore
  const writeOwners = useSyncExternalStore(
    writeOwnerStore.subscribe,
    writeOwnerStore.getSnapshot,
    writeOwnerStore.getSnapshot,
  )
  const [viewTokenNamespace] = useState(() => crypto.randomUUID())

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
            mode: 'error',
            error: SAFE_OVERVIEW_FAILURE,
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
              mode: 'error',
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
  const viewIdentity = `${location.key}:${normalizedVPSId}:${probeRevision}:${gate}`
  const viewToken = `${viewTokenNamespace}:${viewIdentity}`
  const inheritedOwnerRef = useRef<{ vpsId: string; token: string } | null>(null)
  const currentWriteOwner = writeOwners.get(normalizedVPSId)

  useEffect(() => {
    if (currentWriteOwner && currentWriteOwner.viewToken !== viewToken) {
      inheritedOwnerRef.current = {
        vpsId: normalizedVPSId,
        token: currentWriteOwner.token,
      }
      return
    }
    const inheritedOwner = inheritedOwnerRef.current
    if (!currentWriteOwner && inheritedOwner?.vpsId === normalizedVPSId) {
      inheritedOwnerRef.current = null
      setProbeRevision((revision) => revision + 1)
    }
  }, [currentWriteOwner, normalizedVPSId, viewToken])

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
    return <Navigate to={`/archive/${encodeURIComponent(normalizedVPSId)}`} replace state={location.state} />
  }

  return (
    <VPSOverviewRoute
      vpsId={normalizedVPSId}
      initialOverview={seededOverview}
      writeOwnerStore={writeOwnerStore}
      viewToken={viewToken}
    />
  )
}

function VPSOverviewRoute({
  vpsId,
  initialOverview,
  writeOwnerStore,
  viewToken,
}: {
  vpsId: string | undefined
  initialOverview: VPSOverview | null
  writeOwnerStore: VPSWriteOwnerStore
  viewToken: string
}) {
  const { state, commands } = useVPSOverview(vpsId, initialOverview)
  const management = useVPSManagementController()
  const managementTriggerRef = useRef<HTMLButtonElement>(null)
  const [associationRefreshGeneration, setAssociationRefreshGeneration] = useState(0)
  const noteMonitoringAssociationChanged = useCallback(() => {
    setAssociationRefreshGeneration((current) => current + 1)
  }, [])
  const [searchParams, setSearchParams] = useSearchParams()
  const location = useLocation()
  const openManagementPanel = management.openPanel
  const pendingWorkbenchPanelRef = useRef<ReturnType<typeof parseOverviewWorkbench>>(null)

  useEffect(() => {
    if (searchParams.has('workbench')) {
      const parsed = parseOverviewWorkbench(searchParams.get('workbench'))
      pendingWorkbenchPanelRef.current = READ_ONLY_PREVIEW ? null : parsed
      const next = new URLSearchParams(searchParams)
      next.delete('workbench')
      setSearchParams(next, { replace: true, state: location.state })
      return
    }

    const pendingPanel = pendingWorkbenchPanelRef.current
    if (!pendingPanel) return
    pendingWorkbenchPanelRef.current = null
    openManagementPanel(pendingPanel)
  }, [openManagementPanel, searchParams, setSearchParams, location.state])


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
  if (lifecycleStatus === 'archived') {
    return <Navigate to={`/archive/${encodeURIComponent(vpsId ?? '')}`} replace state={location.state} />
  }

  return (
    <>
      {searchParams.get('reorganize') === '1' ? (
        <RestoreReorganizationPanel
          key={state.overview.identity.vps_id}
          vpsId={state.overview.identity.vps_id}
          refreshGeneration={associationRefreshGeneration}
          onEditUsage={() => management.openPanel('facts')}
          onEditDecision={() => management.openPanel('decision')}
          onCreateMonitoring={() => management.openPanel('monitoring-instance-create')}
        />
      ) : null}
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
        management={management}
        managementTriggerRef={managementTriggerRef}
        onOverviewRefresh={commands.refresh}
        onMonitoringAssociationChanged={noteMonitoringAssociationChanged}
        writeOwnerStore={writeOwnerStore}
        viewToken={viewToken}
      />
    </>
  )
}
