import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { PageState as PageStateView } from '../components/PageState'
import { ApiError, listSubscriptions, listVPSAssets } from '../lib/api'
import type { SubscriptionRecord, VPSAssetRecord } from '../lib/types'
import { ArchiveVPSWorkspace } from './archive/ArchiveVPSWorkspace'

type AsyncState<T> = {
  loading: boolean
  error: string | null
  data: T
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
  const [vpsState, setVpsState] = useState<AsyncState<VPSAssetRecord[]>>({
    loading: true,
    error: null,
    data: [],
  })
  const [subscriptionsState, setSubscriptionsState] = useState<AsyncState<SubscriptionRecord[]>>({
    loading: true,
    error: null,
    data: [],
  })

  const vpsGenRef = useRef(0)
  const subsGenRef = useRef(0)

  const fetchVPS = useCallback((gen: number) => {
    listVPSAssets({ asset_scope: 'historical' })
      .then((data) => {
        if (gen === vpsGenRef.current) {
          setVpsState({ loading: false, error: null, data })
        }
      })
      .catch((error: unknown) => {
        if (gen === vpsGenRef.current) {
          setVpsState((prev) => ({
            loading: false,
            error: describeError(error, '加载归档资产失败'),
            data: prev.data,
          }))
        }
      })
  }, [])

  const fetchSubscriptions = useCallback((gen: number) => {
    listSubscriptions({ asset_scope: 'historical', sort: 'renew_at', order: 'asc' })
      .then((data) => {
        if (gen === subsGenRef.current) {
          setSubscriptionsState({ loading: false, error: null, data })
        }
      })
      .catch((error: unknown) => {
        if (gen === subsGenRef.current) {
          setSubscriptionsState((prev) => ({
            loading: false,
            error: describeError(error, '加载历史订阅失败'),
            data: prev.data,
          }))
        }
      })
  }, [])

  useEffect(() => {
    vpsGenRef.current += 1
    subsGenRef.current += 1
    fetchVPS(vpsGenRef.current)
    fetchSubscriptions(subsGenRef.current)
    return () => {
      ++vpsGenRef.current
      ++subsGenRef.current
    }
  }, [fetchVPS, fetchSubscriptions])

  const handleRetryVPS = useCallback(() => {
    const nextGen = ++vpsGenRef.current
    setVpsState((prev) => ({ ...prev, loading: true, error: null }))
    fetchVPS(nextGen)
  }, [fetchVPS])

  const handleRetrySubscriptions = useCallback(() => {
    const nextGen = ++subsGenRef.current
    setSubscriptionsState((prev) => ({ ...prev, loading: true, error: null }))
    fetchSubscriptions(nextGen)
  }, [fetchSubscriptions])

  return (
    <div className="page archive-page">
      <header className="page__head">
        <div>
          <h1 className="page__title">归档资产</h1>
          <p className="page-sub">只读历史台账，保留已退役与取消资产的账单、时间线及取消依据。</p>
        </div>
        <div className="page__actions">
          <Link className="btn sm secondary" to="/vps">返回 VPS</Link>
        </div>
      </header>

      {vpsState.loading ? (
        <PageStateView kind="loading" title="正在加载归档资产" />
      ) : vpsState.error ? (
        <PageStateView
          kind="error"
          title="归档资产加载失败"
          description="归档入口暂时不可用。"
          technicalSummary={vpsState.error}
          action={
            <div className="page-state__actions">
              <button className="btn sm primary" type="button" onClick={handleRetryVPS}>
                重试加载资产
              </button>
              <Link className="btn sm secondary" to="/vps">返回 VPS</Link>
            </div>
          }
        />
      ) : vpsState.data.length === 0 ? (
        renderEmptyArchive(<Link className="btn sm secondary" to="/vps">返回 VPS</Link>)
      ) : (
        <ArchiveVPSWorkspace
          vpsRows={vpsState.data}
          subscriptionsState={subscriptionsState}
          onRetrySubscriptions={handleRetrySubscriptions}
        />
      )}
    </div>
  )
}
