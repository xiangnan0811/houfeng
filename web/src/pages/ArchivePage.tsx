import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { MonoDigits } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, listSubscriptions, listVPSAssets } from '../lib/api'
import type { SubscriptionRecord, VPSAssetRecord } from '../lib/types'
import { ArchiveVPSWorkspace } from './archive/ArchiveVPSWorkspace'

type PageState = {
  loading: boolean
  error: string | null
  vps: VPSAssetRecord[]
  subscriptions: SubscriptionRecord[]
}

const INITIAL_STATE: PageState = {
  loading: true,
  error: null,
  vps: [],
  subscriptions: [],
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
        setState({
          loading: false,
          error: null,
          vps,
          subscriptions,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          loading: false,
          error: describeError(error, '加载归档资产失败'),
          vps: [],
          subscriptions: [],
        })
      })

    return () => {
      cancelled = true
    }
  }, [])

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
                <span>查看方式</span>
                <strong>列表进入详情</strong>
              </div>
            </div>
          </section>

          <ArchiveVPSWorkspace
            vpsRows={state.vps}
            subscriptions={state.subscriptions}
          />
        </>
      )}
    </div>
  )
}
