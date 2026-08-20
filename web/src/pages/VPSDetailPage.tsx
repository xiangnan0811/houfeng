import { lazy, Suspense, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'

import { PageState } from '../components/PageState'
import { ApiError } from '../lib/apiRequest'
import { getVPSOverview, overviewHasRecordsV2Read } from '../lib/recordsApi'
import type { VPSOverview } from '../lib/types'
import { RouteModuleFallback } from '../app/RouteModuleFallback'
import { VPSOverviewPageView } from './vps-detail/VPSOverviewPageView'
import { useVPSManagementController } from './vps-detail/hooks/useVPSManagementController'
import { useVPSOverview } from './vps-detail/hooks/useVPSOverview'

const LegacyVPSDetailPage = lazy(() =>
  import('./vps-detail/LegacyVPSDetail').then((module) => ({ default: module.LegacyVPSDetail })),
)

type GateMode = 'probing' | 'overview' | 'legacy' | 'not_found' | 'error'

/**
 * Canonical `/vps/:id` entry. Capability-off or missing overview endpoint falls
 * back to the legacy composition. Identity 404 (VPS missing/unauthorized) stays
 * on the overview empty state — it must not open the legacy workbench. Overview
 * load failures after the gate selects overview are surfaced — they do not
 * silently fall back.
 *
 * Legacy is a separate async chunk so overview e2e / production first paint do
 * not pay for the full workbench graph.
 */
export function VPSDetailPage() {
  const { vpsId } = useParams()
  const [gate, setGate] = useState<GateMode>('probing')
  const [gateError, setGateError] = useState<string | null>(null)
  const [seededOverview, setSeededOverview] = useState<VPSOverview | null>(null)

  useEffect(() => {
    let cancelled = false
    // eslint-disable-next-line react-hooks/set-state-in-effect -- route-param gate: reset probe state when vpsId changes, then async getVPSOverview decides overview vs legacy
    setGate('probing')
    setGateError(null)
    setSeededOverview(null)

    if (!vpsId?.trim()) {
      setGate('not_found')
      return () => {
        cancelled = true
      }
    }

    void getVPSOverview(vpsId.trim())
      .then((overview) => {
        if (cancelled) return
        if (overviewHasRecordsV2Read(overview)) {
          setSeededOverview(overview)
          setGate('overview')
        } else {
          setGate('legacy')
        }
      })
      .catch((error: unknown) => {
        if (cancelled) return
        if (error instanceof ApiError) {
          if (error.status === 404 || error.code === 'resource_not_found') {
            setGate('not_found')
            setGateError(error.message)
            return
          }
          if (error.status === 503 || error.code === 'overview_unavailable') {
            setGate('legacy')
            return
          }
          setGate('error')
          setGateError(error.message)
          return
        }
        setGate('legacy')
      })

    return () => {
      cancelled = true
    }
  }, [vpsId])

  if (gate === 'probing') {
    return <PageState kind="loading" title="正在判定 VPS 详情形态" />
  }

  if (gate === 'not_found') {
    return (
      <PageState
        kind="error"
        title="未找到 VPS"
        description={gateError ?? '该 VPS 不存在，或当前账号无权查看。'}
      />
    )
  }

  if (gate === 'error') {
    return (
      <PageState
        kind="error"
        title="无法加载 VPS 概览"
        description={gateError ?? '概览探测失败，未回退到旧页面以免掩盖真实错误。'}
      />
    )
  }

  if (gate === 'legacy') {
    return (
      <Suspense fallback={<RouteModuleFallback label="正在加载 VPS 详情" />}>
        <LegacyVPSDetailPage />
      </Suspense>
    )
  }

  return <VPSOverviewRoute vpsId={vpsId} initialOverview={seededOverview} />
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
        technicalSummary={state.errorCode}
      />
    )
  }

  return (
    <VPSOverviewPageView
      overview={state.overview}
      management={management}
      onRefresh={commands.refresh}
      onManagePanel={() => {
        // Panel selection is owned by the management controller; mutation owners
        // refresh the overview via commands.refresh after writes.
      }}
    />
  )
}
