import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { MonoDigits } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { VPSTimelinePanel } from '../components/VPSTimelinePanel'
import {
  ApiError,
  getVPSAsset,
  getVPSTimeline,
  listSubscriptions,
  listVPSAssets,
  listVPSDomains,
  listVPSServices,
} from '../lib/api'
import {
  type AssetDomainRecord,
  type AssetServiceRecord,
  type SubscriptionRecord,
  type VPSAssetDetail,
  type VPSAssetRecord,
  type VPSTimeline,
} from '../lib/types'
import { ArchiveMonitoringPanel, ArchiveServicesPanel, ArchiveDomainsPanel } from './archive/ArchiveContextPanels'
import { ArchiveVPSWorkspace } from './archive/ArchiveVPSWorkspace'
import { selectedVPS, subscriptionsForVPS } from './archive/archivePageHelpers'

type PageState = {
  loading: boolean
  error: string | null
  vps: VPSAssetRecord[]
  subscriptions: SubscriptionRecord[]
  selectedVPSID: string | null
  archiveResult: ArchiveResult | null
}

type ArchiveResult = {
  vpsID: string
  detail: VPSAssetDetail | null
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
  timeline: VPSTimeline | null
  error: string | null
}

const INITIAL_STATE: PageState = {
  loading: true,
  error: null,
  vps: [],
  subscriptions: [],
  selectedVPSID: null,
  archiveResult: null,
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function renderEmptyArchive(action?: ReactNode) {
  return (
    <PageStateView
      kind="empty"
      surface="empty"
      title="尚无归档资产"
      description="已取消或已归档的 VPS 会从运营页移出，并在这里保留只读历史。"
      action={action}
    />
  )
}

export function ArchivePage() {
  const [state, setState] = useState<PageState>(INITIAL_STATE)

  useEffect(() => {
    let cancelled = false

    Promise.all([
      listVPSAssets({ asset_scope: 'archived' }),
      listSubscriptions({ asset_scope: 'archived', sort: 'renew_at', order: 'asc' }),
    ])
      .then(([vps, subscriptions]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          loading: false,
          error: null,
          vps,
          subscriptions,
          selectedVPSID: current.selectedVPSID ?? vps[0]?.vps_id ?? null,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          loading: false,
          error: describeError(error, '加载归档资产失败'),
          vps: [],
          subscriptions: [],
          selectedVPSID: null,
        }))
      })

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!state.selectedVPSID) {
      return
    }

    let cancelled = false
    const vpsID = state.selectedVPSID

    Promise.all([
      getVPSAsset(vpsID),
      listVPSServices(vpsID),
      listVPSDomains(vpsID),
      getVPSTimeline(vpsID),
    ])
      .then(([detail, services, domains, timeline]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          archiveResult: { vpsID, detail, services, domains, timeline, error: null },
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          archiveResult: {
            vpsID,
            detail: null,
            services: [],
            domains: [],
            timeline: null,
            error: describeError(error, '加载归档资产上下文失败'),
          },
        }))
      })

    return () => {
      cancelled = true
    }
  }, [state.selectedVPSID])

  const currentVPS = useMemo(
    () => selectedVPS(state.vps, state.selectedVPSID),
    [state.vps, state.selectedVPSID],
  )
  const currentSubscriptions = useMemo(
    () => subscriptionsForVPS(state.subscriptions, state.selectedVPSID),
    [state.subscriptions, state.selectedVPSID],
  )
  const currentArchiveResult = state.archiveResult?.vpsID === state.selectedVPSID
    ? state.archiveResult
    : null
  const archiveContextLoading = Boolean(state.selectedVPSID && !currentArchiveResult)
  const currentMonitoring = currentArchiveResult?.detail?.monitoring_instance_links ?? []
  const currentServices = currentArchiveResult?.services ?? []
  const currentDomains = currentArchiveResult?.domains ?? []

  return (
    <div className="page-stack archive-page animate-in">
      <div className="page-header">
        <div>
          <h1 className="page-title">归档资产</h1>
          <p className="page-subtitle">已取消、已归档 VPS 的只读资产历史。</p>
        </div>
        <div className="header-actions">
          <Link className="btn sm secondary" to="/vps">返回 VPS</Link>
        </div>
      </div>

      {state.loading ? (
        <PageStateView kind="loading" title="正在加载归档资产" />
      ) : state.error ? (
        <PageStateView
          kind="error"
          title="归档资产加载失败"
          description="归档入口暂时不可用。"
          technicalSummary={state.error}
        />
      ) : state.vps.length === 0 ? (
        renderEmptyArchive(<Link className="btn sm secondary" to="/vps">返回 VPS</Link>)
      ) : (
        <>
          <section className="hero-panel archive-page__summary">
            <div className="hero-panel__content">
              <p className="hero-panel__eyebrow">ARCHIVE LEDGER</p>
              <h2 className="hero-panel__title">历史资产仍保留为判断依据</h2>
              <p className="hero-panel__description">
                这些 VPS 不再进入运营、订阅和资产组合决策主流程；保留账单和时间线用于回看服务商质量、成本与取消依据。
              </p>
            </div>
            <div className="hero-panel__meta">
              <div className="hero-meta-card">
                <span>归档 VPS</span>
                <strong><MonoDigits>{state.vps.length}</MonoDigits></strong>
              </div>
              <div className="hero-meta-card">
                <span>历史订阅</span>
                <strong><MonoDigits>{state.subscriptions.length}</MonoDigits></strong>
              </div>
              <div className="hero-meta-card">
                <span>当前查看</span>
                <strong>{currentVPS?.display_name ?? '未选择'}</strong>
              </div>
            </div>
          </section>

          <ArchiveVPSWorkspace
            vpsRows={state.vps}
            selectedVPS={currentVPS}
            subscriptions={currentSubscriptions}
            onSelectVPS={(vpsID) => setState((current) => ({ ...current, selectedVPSID: vpsID }))}
          />

          {archiveContextLoading ? (
            <PageStateView kind="loading" title="正在加载归档上下文" compact />
          ) : currentArchiveResult?.error ? (
            <PageStateView
              kind="error"
              title="归档上下文加载失败"
              description="归档资产基础信息仍可查看。"
              technicalSummary={currentArchiveResult.error}
              compact
            />
          ) : currentArchiveResult ? (
            <>
              <ArchiveMonitoringPanel monitoring={currentMonitoring} />
              <ArchiveServicesPanel services={currentServices} />
              <ArchiveDomainsPanel domains={currentDomains} />
              {currentArchiveResult.timeline ? <VPSTimelinePanel timeline={currentArchiveResult.timeline} /> : null}
            </>
          ) : null}
        </>
      )}
    </div>
  )
}
