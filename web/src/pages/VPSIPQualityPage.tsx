import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { Button } from '../components/atoms'
import { collectRequestCoversFamily, type CollectNoticeView } from '../components/ip-quality/ipQualityCollectPresentation'
import { IPQualityDashboard, type IPQualityDashboardView } from '../components/ip-quality/IPQualityDashboard'
import { parseIPVersionParam, selectedReportIPVersion } from '../components/ip-quality/ipQualityPresentation'
import { productEnriching, selectedProduct } from '../components/ip-quality/ipQualityProductPresentation'
import { useIPQualityCollect } from '../components/ip-quality/useIPQualityCollect'
import { useIPQualityEnrichment } from '../components/ip-quality/useIPQualityEnrichment'
import { PageState } from '../components/PageState'
import { ApiError, getVPSIPQuality, getVPSIPQualityReport } from '../lib/api'
import type { IPQualityCollectRequest, IPQualityIPVersion, VPSIPQualityReport } from '../lib/types'
import './vps-detail/VPSDetailWorkspace.css'

type PageLoadState = {
  requestKey: string | null
  error: string | null
  report: VPSIPQualityReport | null
  refreshState: CollectNoticeView['refreshState']
}

const INITIAL_STATE: PageLoadState = {
  requestKey: null,
  error: null,
  report: null,
  refreshState: 'idle',
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function familySearch(family: IPQualityIPVersion, reportId?: string): string {
  const query = new URLSearchParams()
  query.set('ip_version', String(family))
  if (reportId) query.set('report_id', reportId)
  return `?${query.toString()}`
}

export function VPSIPQualityPage() {
  const { vpsId } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const reportId = searchParams.get('report_id')?.trim() || ''
  const viewingHistory = reportId !== ''
  const familyParam = parseIPVersionParam(searchParams.getAll('ip_version'))
  const explicitFamily = familyParam.kind === 'family' ? familyParam.ipVersion : null
  // 有效历史 ID 以报告自身族为准，冲突/非法的族参数随后按响应补正；无历史 ID 时非法族不发请求。
  const invalidFamily = !viewingHistory && familyParam.kind === 'invalid'
  const selection = viewingHistory
    ? `report:${reportId}`
    : explicitFamily != null ? `family:${explicitFamily}` : invalidFamily ? null : 'default'
  const requestKey = vpsId && selection ? `${vpsId}|${selection}` : null
  // 请求目标只由 requestKey 决定：历史视图里补正 ip_version 不改变它，也不触发重复请求。
  const requestFamily = viewingHistory ? null : explicitFamily
  const [reloadKey, setReloadKey] = useState(0)
  const [state, setState] = useState<PageLoadState>(INITIAL_STATE)
  // 默认请求的响应被迁移到规范化后的族 requestKey；URL 补正后对应 effect 不再重复请求。
  const adoptedKeyRef = useRef<string | null>(null)
  // 采集完成回调与异步补正读取的“当前选择”，避免闭包里的旧路由。
  const currentFamily = state.report && state.requestKey === requestKey ? selectedReportIPVersion(state.report) : explicitFamily
  const currentRef = useRef({ viewingHistory, family: currentFamily, search: location.search, routeState: location.state as unknown })

  useEffect(() => {
    currentRef.current = { viewingHistory, family: currentFamily, search: location.search, routeState: location.state as unknown }
  }, [viewingHistory, currentFamily, location.search, location.state])

  useEffect(() => {
    if (!vpsId || !requestKey) return
    if (adoptedKeyRef.current === requestKey) {
      adoptedKeyRef.current = null
      return
    }
    adoptedKeyRef.current = null
    let cancelled = false

    const request = reportId
      ? getVPSIPQualityReport(vpsId, reportId)
      : requestFamily != null ? getVPSIPQuality(vpsId, requestFamily) : getVPSIPQuality(vpsId)
    request
      .then((report) => {
        if (cancelled) return
        const family = selectedReportIPVersion(report)
        const { search, routeState } = currentRef.current
        if (!reportId && requestFamily == null && family != null) {
          // 规范化 URL：先把已加载响应迁移到族 requestKey，再 replace，不发第二次请求、不闪空态。
          const canonicalKey = `${vpsId}|family:${family}`
          adoptedKeyRef.current = canonicalKey
          setState({ requestKey: canonicalKey, error: null, report, refreshState: 'idle' })
          navigate({ search: familySearch(family) }, { replace: true, state: routeState })
          return
        }
        setState({ requestKey, error: null, report, refreshState: 'idle' })
        if (reportId && family != null) {
          // 历史 ID 的族优先：缺失或冲突的 ip_version 按报告自身族补正（requestKey 不变，不重复请求）。
          const params = new URLSearchParams(search)
          const current = params.getAll('ip_version')
          if (current.length !== 1 || current[0] !== String(family)) {
            navigate({ search: familySearch(family, reportId) }, { replace: true, state: routeState })
          }
        }
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((prev) => {
          // 后台刷新失败时保留同一选择的旧报告并标记刷新失败，只有首次加载失败才进入错误页。
          if (prev.requestKey === requestKey && prev.report) return { ...prev, refreshState: 'failed' }
          return { requestKey, error: describeError(error, '加载 IP 质量报告失败'), report: null, refreshState: 'idle' }
        })
      })

    return () => { cancelled = true }
  }, [vpsId, reportId, requestFamily, requestKey, reloadKey, navigate])

  // 立即采集完成后在原地刷新当前所选族；历史视图保持历史，不跳回最新；只采了另一族时不刷新、不切族。
  const refreshLatest = useCallback((request: IPQualityCollectRequest) => {
    const { viewingHistory: inHistory, family } = currentRef.current
    if (inHistory || !collectRequestCoversFamily(request, family)) return
    setState((prev) => (prev.report ? { ...prev, refreshState: 'refreshing' } : prev))
    setReloadKey((key) => key + 1)
  }, [])
  const reportReady = requestKey != null && state.requestKey === requestKey && !state.error
  // 采集状态只在该 VPS 首次成功加载报告后读取一次；之后切族/进入历史的加载不再重读，
  // 避免进行中的请求被一次重新读取覆盖，也不会重复 POST。首次加载失败的错误页仍不读取。
  const [collectReadyVps, setCollectReadyVps] = useState<string | null>(null)
  if (reportReady && vpsId && collectReadyVps !== vpsId) setCollectReadyVps(vpsId)
  const collectEnabled = vpsId != null && (reportReady || collectReadyVps === vpsId)
  const collect = useIPQualityCollect(vpsId, refreshLatest, collectEnabled)

  // 手动切族：写入 ip_version 并清除 report_id；不发起采集。
  const selectFamily = useCallback((next: IPQualityIPVersion) => {
    if (!viewingHistory && explicitFamily === next) return
    navigate({ search: familySearch(next) }, { state: location.state })
  }, [viewingHistory, explicitFamily, navigate, location.state])

  // Center 补充资料：同一所选报告在 pending/running 时可见轮询；写回前核对请求身份与报告 ID，迟到结果不覆盖新选择。
  const loadedProduct = state.report && state.requestKey === requestKey && !state.error ? selectedProduct(state.report) : null
  const pollReportId = loadedProduct && productEnriching(loadedProduct) ? loadedProduct.report_id : null
  const adoptEnrichment = useCallback((key: string, next: VPSIPQualityReport) => {
    setState((prev) => {
      if (prev.requestKey !== key || !prev.report) return prev
      const current = selectedProduct(prev.report)
      const updated = selectedProduct(next)
      if (!current || !updated || updated.report_id !== current.report_id) return prev
      return { ...prev, report: next }
    })
  }, [])
  const enrichment = useIPQualityEnrichment({ vpsId, reportId: pollReportId, requestKey, onReport: adoptEnrichment })

  const detailPath = vpsId ? `/vps/${encodeURIComponent(vpsId)}` : '/vps'
  const returnLink = <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>
  const shell = (view: IPQualityDashboardView) => (
    <IPQualityDashboard
      view={view}
      detailPath={detailPath}
      collect={collect}
      {...(vpsId ? { vpsId } : {})}
      viewingHistory={viewingHistory}
      refreshState={state.refreshState}
      onSelectFamily={selectFamily}
      enrichment={loadedProduct && (productEnriching(loadedProduct) || enrichment.failed) ? { active: productEnriching(loadedProduct), failed: enrichment.failed, retry: enrichment.retry } : null}
    />
  )

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

  if (invalidFamily) return shell({ kind: 'invalid_family' })

  // 切族/首次加载期间保留页头与族选择器，只把正文换成加载态，不展示另一族的旧事实。
  if (state.requestKey !== requestKey) return shell({ kind: 'loading', ipVersion: explicitFamily })

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
    return shell({ kind: 'empty', report, ipVersion: report ? selectedReportIPVersion(report) : explicitFamily })
  }
  return shell({ kind: 'report', report, summary })
}
