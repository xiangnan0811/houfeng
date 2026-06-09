import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { IPQualityDashboard } from '../components/ip-quality/IPQualityDashboard'
import { PageState } from '../components/PageState'
import { ApiError, getVPSIPQuality } from '../lib/api'
import type { VPSIPQualityReport } from '../lib/types'

type PageLoadState = {
  requestedVPSId: string | null
  error: string | null
  report: VPSIPQualityReport | null
}

const INITIAL_STATE: PageLoadState = {
  requestedVPSId: null,
  error: null,
  report: null,
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function VPSIPQualityPage() {
  const { vpsId } = useParams()
  const [state, setState] = useState<PageLoadState>(INITIAL_STATE)

  useEffect(() => {
    if (!vpsId) return
    let cancelled = false

    getVPSIPQuality(vpsId)
      .then((report) => {
        if (cancelled) return
        setState({ requestedVPSId: vpsId, error: null, report })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({ requestedVPSId: vpsId, error: describeError(error, '加载 IP 质量报告失败'), report: null })
      })

    return () => { cancelled = true }
  }, [vpsId])

  const detailPath = vpsId ? `/vps/${encodeURIComponent(vpsId)}` : '/vps'

  if (!vpsId) {
    return (
      <PageState
        kind="empty"
        eyebrow="IP QUALITY"
        title="缺少 VPS ID"
        description="需要从 VPS 详情页进入对应 IP 质量报告。"
        action={<Link className="btn sm secondary" to="/vps">返回 VPS 列表</Link>}
      />
    )
  }

  if (state.requestedVPSId !== vpsId) {
    return <PageState kind="loading" eyebrow="IP QUALITY" title="正在加载 IP 质量报告" />
  }

  if (state.error) {
    return (
      <PageState
        kind="error"
        eyebrow="IP QUALITY"
        title="IP 质量报告加载失败"
        technicalSummary={state.error}
        action={<Link className="btn sm secondary" to={detailPath}>返回 VPS 详情</Link>}
      />
    )
  }

  const report = state.report
  const summary = report?.summary ?? null

  if (!report || !summary) {
    return (
      <PageState
        kind="empty"
        eyebrow="IP QUALITY"
        title="尚无可展示的 IP 质量事实"
        description="center 会保留 failure 诊断，但用户侧报告只展示真实出口 IP 事实。等待 agent 下次低频采集后再查看。"
        action={<Link className="btn sm secondary" to={detailPath}>返回 VPS 详情</Link>}
      />
    )
  }

  return <IPQualityDashboard report={report} summary={summary} detailPath={detailPath} />
}
