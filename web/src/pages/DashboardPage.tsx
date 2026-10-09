import { useCallback, useEffect, useState } from 'react'

import { PageState } from '../components/PageState'
import { getSubscriptionOverview, listVPSAssets } from '../lib/api'
import { ApiError } from '../lib/apiRequest'
import { getDashboard } from '../lib/observabilityApi'
import { useVisibleRefresh } from '../lib/useVisibleRefresh'
import type { DashboardOverview, SubscriptionOverview, VPSAssetRecord } from '../lib/types'
import { DashboardCommandSurface } from './dashboard/DashboardCommandSurface'
import { buildDashboardModel } from './dashboard/dashboardModel'
import { buildRecentActivity, buildRenewalPanel, incidentTrend } from './dashboard/dashboardPanels'
import {
  remoteError,
  remoteLoading,
  remoteSuccess,
  type RemoteState,
} from './dashboard/dashboardRemoteState'

type DashboardResources = {
  overview: RemoteState<DashboardOverview>
  vps: RemoteState<VPSAssetRecord[]>
  subscription: RemoteState<SubscriptionOverview>
  overviewRefreshFailed: boolean
}

const INITIAL_RESOURCES: DashboardResources = {
  overview: remoteLoading(),
  vps: remoteLoading(),
  subscription: remoteLoading(),
  overviewRefreshFailed: false,
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

function clearsDashboardOverview(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 || error.status === 403 || error.status === 404)
}

function overviewFailureMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && (error.status === 401 || error.status === 403)) return '授权失败'
  if (error instanceof ApiError && error.status === 404) return '工作台不存在'
  return errorMessage(error, fallback)
}

export function DashboardPage() {
  const [resources, setResources] = useState<DashboardResources>(INITIAL_RESOURCES)
  const [supportingReloadKey, setSupportingReloadKey] = useState(0)
  const loadOverview = useCallback(async ({ isCurrent }: { isCurrent: () => boolean }) => {
    try {
      const overview = await getDashboard()
      if (!isCurrent()) return
      setResources((current) => ({
        ...current,
        overview: remoteSuccess(overview, new Date().toISOString()),
        overviewRefreshFailed: false,
      }))
    } catch (error: unknown) {
      if (!isCurrent()) return
      if (clearsDashboardOverview(error)) {
        setResources((current) => ({
          ...current,
          overview: remoteError(overviewFailureMessage(error, '加载工作台失败')),
          overviewRefreshFailed: false,
        }))
        return
      }
      const message = overviewFailureMessage(error, '加载工作台失败')
      setResources((current) => {
        if (current.overview.status === 'success') {
          return { ...current, overviewRefreshFailed: true }
        }
        return {
          ...current,
          overview: remoteError(message),
          overviewRefreshFailed: false,
        }
      })
    }
  }, [])
  const { refresh: refreshOverview, invalidate: invalidateOverview } = useVisibleRefresh(loadOverview)

  useEffect(() => {
    void refreshOverview()
  }, [refreshOverview])

  useEffect(() => {
    let cancelled = false
    listVPSAssets()
      .then((vps) => {
        if (cancelled) return
        setResources((current) => ({
          ...current,
          vps: remoteSuccess(vps, new Date().toISOString()),
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setResources((current) => ({
          ...current,
          vps: remoteError(errorMessage(error, '加载 VPS 清单失败')),
        }))
      })

    getSubscriptionOverview()
      .then((subscription) => {
        if (cancelled) return
        setResources((current) => ({
          ...current,
          subscription: remoteSuccess(subscription, new Date().toISOString()),
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setResources((current) => ({
          ...current,
          subscription: remoteError(errorMessage(error, '加载订阅摘要失败')),
        }))
      })
    return () => { cancelled = true }
  }, [supportingReloadKey])

  function retryOverview() {
    invalidateOverview()
    setResources((current) => ({
      ...current,
      overview: remoteLoading(),
      overviewRefreshFailed: false,
    }))
    void refreshOverview()
  }

  function retrySupportingResources() {
    setResources((current) => ({
      ...current,
      vps: remoteLoading(),
      subscription: remoteLoading(),
    }))
    setSupportingReloadKey((current) => current + 1)
  }

  const model = buildDashboardModel(resources)

  if (model.status === 'loading') {
    return <PageState kind="loading" title="正在加载工作台…" />
  }

  if (model.status === 'error') {
    return (
      <PageState
        kind="error"
        eyebrow="工作台"
        title="工作台不可用"
        description={model.error}
        technicalSummary={model.error}
        action={
          <button type="button" className="btn primary" onClick={retryOverview}>
            重试
          </button>
        }
      />
    )
  }

  const supportingLoading =
    resources.vps.status === 'loading' || resources.subscription.status === 'loading'
  const overview = resources.overview.status === 'success' ? resources.overview.value : null

  return (
    <div className="page dashboard-page">
      <DashboardCommandSurface
        model={model}
        incidentTrend={overview ? incidentTrend(overview) : null}
        activity={overview ? buildRecentActivity(overview) : []}
        renewals={buildRenewalPanel(resources.subscription)}
        supportingLoading={supportingLoading}
        overviewRefreshFailed={resources.overviewRefreshFailed}
        {...(model.degradations.length > 0
          ? { onRetrySupporting: retrySupportingResources }
          : {})}
      />
    </div>
  )
}
