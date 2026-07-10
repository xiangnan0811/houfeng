import { useEffect, useState } from 'react'

import { PageState } from '../components/PageState'
import { ApiError, getDashboard, getSubscriptionOverview, listVPSAssets } from '../lib/api'
import type { DashboardOverview, SubscriptionOverview, VPSAssetRecord } from '../lib/types'
import { DashboardCommandSurface } from './dashboard/DashboardCommandSurface'
import { buildDashboardModel } from './dashboard/dashboardModel'
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
}

const INITIAL_RESOURCES: DashboardResources = {
  overview: remoteLoading(),
  vps: remoteLoading(),
  subscription: remoteLoading(),
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

export function DashboardPage() {
  const [resources, setResources] = useState<DashboardResources>(INITIAL_RESOURCES)
  const [overviewReloadKey, setOverviewReloadKey] = useState(0)
  const [supportingReloadKey, setSupportingReloadKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    getDashboard()
      .then((overview) => {
        if (cancelled) return
        setResources((current) => ({
          ...current,
          overview: remoteSuccess(overview, new Date().toISOString()),
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setResources((current) => ({
          ...current,
          overview: remoteError(errorMessage(error, '加载工作台失败')),
        }))
      })
    return () => { cancelled = true }
  }, [overviewReloadKey])

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
    setResources((current) => ({ ...current, overview: remoteLoading() }))
    setOverviewReloadKey((current) => current + 1)
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

  return (
    <div className="page-stack dashboard-page">
      <DashboardCommandSurface
        model={model}
        supportingLoading={supportingLoading}
        onRetrySupporting={model.degradations.length > 0 ? retrySupportingResources : undefined}
      />
    </div>
  )
}
