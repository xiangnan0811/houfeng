import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom'

import { Button } from '../components/atoms'
import { IPQualityCollectButton, IPQualityCollectNotice } from '../components/ip-quality/IPQualityCollectControls'
import { IPQualityDashboard } from '../components/ip-quality/IPQualityDashboard'
import { IPQualityHeader } from '../components/ip-quality/IPQualityHeader'
import { useIPQualityCollect, type IPQualityCollectController } from '../components/ip-quality/useIPQualityCollect'
import { PageState } from '../components/PageState'
import { ApiError, getVPSIPQuality, getVPSIPQualityReport } from '../lib/api'
import type { VPSIPQualityReport } from '../lib/types'
import './vps-detail/VPSDetailWorkspace.css'

type PageLoadState = {
  requestKey: string | null
  error: string | null
  report: VPSIPQualityReport | null
}

const INITIAL_STATE: PageLoadState = {
  requestKey: null,
  error: null,
  report: null,
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function emptyDescription(collect: IPQualityCollectController): string {
  const status = collect.status
  if (collect.active) return '采集完成后会自动显示报告。'
  if (status && !status.available) return '满足采集条件后，agent 会自动上报。'
  return 'agent 还没有上报有效结果，可以立即采集一次。'
}

export function VPSIPQualityPage() {
  const { vpsId } = useParams()
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const reportId = searchParams.get('report_id')?.trim() || ''
  const requestKey = vpsId ? `${vpsId}:${reportId}` : null
  const [reloadKey, setReloadKey] = useState(0)
  const [state, setState] = useState<PageLoadState>(INITIAL_STATE)

  useEffect(() => {
    if (!vpsId) return
    let cancelled = false

    const request = reportId ? getVPSIPQualityReport(vpsId, reportId) : getVPSIPQuality(vpsId)
    request
      .then((report) => {
        if (cancelled) return
        setState({ requestKey, error: null, report })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((prev) => {
          // 后台刷新失败时保留当前报告，只有首次加载失败才进入错误页。
          if (prev.requestKey === requestKey && prev.report) return prev
          return { requestKey, error: describeError(error, '加载 IP 质量报告失败'), report: null }
        })
      })

    return () => { cancelled = true }
  }, [vpsId, reportId, requestKey, reloadKey])

  // 立即采集完成后在原地刷新报告，不闪回加载页。
  const refreshLatest = useCallback(() => {
    if (!reportId) setReloadKey((key) => key + 1)
  }, [reportId])
  const reportReady = state.requestKey === requestKey && !state.error
  const collect = useIPQualityCollect(vpsId, refreshLatest, reportReady)

  const detailPath = vpsId ? `/vps/${encodeURIComponent(vpsId)}` : '/vps'
  const returnLink = <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>

  if (!vpsId) {
    return (
      <PageState
        kind="empty"
        eyebrow="IP 质量"
        title="缺少 VPS ID"
        description="需要从 VPS 详情页进入对应 IP 质量报告。"
        action={<Link className="btn sm secondary" to="/vps">返回 VPS 列表</Link>}
      />
    )
  }

  if (state.requestKey !== requestKey) {
    return <PageState kind="loading" eyebrow="IP 质量" title="正在加载 IP 质量报告" />
  }

  if (state.error) {
    return (
      <PageState
        kind="error"
        eyebrow="IP 质量"
        title="IP 质量报告加载失败"
        technicalSummary={state.error}
        action={(
          <>
            <Button size="sm" onClick={() => {
              setState(INITIAL_STATE)
              setReloadKey((key) => key + 1)
            }}>重试</Button>
            {returnLink}
          </>
        )}
      />
    )
  }

  const report = state.report
  const summary = report?.summary ?? null

  if (!report || !summary) {
    return (
      <div className="page vps-detail-workspace ipq-page">
        <IPQualityHeader detailPath={detailPath} collect={collect} viewingHistory={reportId !== ''} showCollect={false} />
        <IPQualityCollectNotice collect={collect} vpsId={vpsId} />
        <PageState
          kind="empty"
          surface="empty"
          title="暂无 IP 质量报告"
          description={emptyDescription(collect)}
          action={reportId ? returnLink : <IPQualityCollectButton collect={collect} />}
        />
      </div>
    )
  }

  return <IPQualityDashboard report={report} summary={summary} detailPath={detailPath} collect={collect} vpsId={vpsId} />
}
