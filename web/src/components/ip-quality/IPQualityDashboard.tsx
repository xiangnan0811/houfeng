import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Button, Timestamp } from '../atoms'
import { PageState } from '../PageState'
import type { IPQualityIPVersion, IPQualityProductView, IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import { IPQualityBlacklistTable } from './IPQualityBlacklistTable'
import { IPQualityCollectButton, IPQualityCollectNotice } from './IPQualityCollectControls'
import {
  collectNotice,
  collectRequestCoversFamily,
  isCollectRequestActive,
  type CollectNotice,
  type CollectNoticeView,
  type CollectReportContext,
} from './ipQualityCollectPresentation'
import { IPQualityFamilySelector } from './IPQualityFamily'
import { IPQualityDiagnostics } from './IPQualityDiagnostics'
import { IPQualityHeader, type IPQualityEnrichmentView, type IPQualityHeaderMark, type IPQualityIdentityState } from './IPQualityHeader'
import { IPQualityHistoryList } from './IPQualityHistoryList'
import { IPQualityIdentityPanel } from './IPQualityIdentityPanel'
import { IPQualityLocationTable } from './IPQualityLocationTable'
import { IPQualityMailTable } from './IPQualityMailTable'
import { IPQualityProviderTable } from './IPQualityProviderTable'
import { IPQualityServiceGrid } from './IPQualityServiceGrid'
import {
  familyStateNotice,
  isAnonymousServiceProbeRevision,
  reportAttributionBlocker,
  reportCollectionIdentity,
  selectedReportIPVersion,
  serviceProbeRevision,
  serviceRuleMark,
  type FamilyStateNotice,
} from './ipQualityPresentation'
import { selectedProduct } from './ipQualityProductPresentation'
import type { IPQualityCollectController } from './useIPQualityCollect'

// 页面主体的几种形态共用同一页头与报告上下文：切族加载时选择器不卸载，键盘焦点留在按钮上。
export type IPQualityDashboardView =
  | { kind: 'loading', ipVersion: IPQualityIPVersion | null }
  | { kind: 'invalid_family' }
  | { kind: 'error', ipVersion: IPQualityIPVersion | null, error: string, onRetry: () => void }
  | { kind: 'empty', report: VPSIPQualityReport | null, ipVersion: IPQualityIPVersion | null }
  | { kind: 'report', report: VPSIPQualityReport, summary: IPQualitySummary }

type IPQualityDashboardProps = {
  view: IPQualityDashboardView
  detailPath: string
  collect: IPQualityCollectController
  vpsId?: string
  // 历史视图：URL 带 report_id；事实全部来自该历史报告本身。
  viewingHistory: boolean
  // 最近一次同族刷新状态：区分“轮次结束”和“当前报告已更新”。
  refreshState: CollectNoticeView['refreshState']
  onSelectFamily: (next: IPQualityIPVersion) => void
  // 所选报告的 Center 补充资料轮询状态；无 product 或已终结时为 null。
  enrichment?: IPQualityEnrichmentView | null
}

// 为什么没有结果（fact）与可直接采集时的恢复建议（recovery）：只按族状态的 kind 说明，不解析标题文案；
// 缺族错误码本身不升级为缺族事实。
function emptyFact(notice: FamilyStateNotice | null, label: string): { fact: string, recovery: string } {
  switch (notice?.kind) {
    case 'never': return { fact: `${label} 尚无采集结果。`, recovery: '可以首次采集。' }
    case 'unavailable': return { fact: `最近一次采集未检测到 ${label} 可用本地地址。`, recovery: '检查网络配置后可以重新检查。' }
    case 'stale_unavailable': return { fact: `已过期的记录显示当时缺少 ${label} 本地地址，不代表现在。`, recovery: '可以重新确认。' }
    case 'failed': return { fact: `${label} 出口探测未形成报告。`, recovery: '可以重试。' }
    case 'stale_failed': return { fact: `已过期的记录显示当时 ${label} 出口探测未形成报告，不代表当前故障。`, recovery: '可以重试。' }
    case 'stale_attempt': return { fact: `${label} 最近一次记录已过期，不能证明当前状态。`, recovery: '可以重新采集。' }
    default: return { fact: `${label} 还没有有效结果。`, recovery: '可以立即采集一次。' }
  }
}

// 空态正文：事实 + 最近尝试时间 + 唯一恢复口径。不可用原因与恢复动作只由采集状态条给出，这里不承诺自动上报。
function emptyDescription(collect: IPQualityCollectController, family: IPQualityIPVersion | null, notice: FamilyStateNotice | null, viewingHistory: boolean): ReactNode {
  const label = family == null ? '该地址族' : `IPv${family}`
  const { fact, recovery } = emptyFact(notice, label)
  if (viewingHistory) return fact
  const status = collect.status
  const request = status?.request ?? null
  let next = recovery
  // 读取失败（status 为 null 且不在检查中）不谎称仍在检查，保留可直接尝试的恢复口径，结果以采集请求为准。
  if (collect.checking) {
    next = '正在检查采集条件。'
  } else if (status && !status.available) {
    next = '当前无法发起采集，见上方采集状态。'
  } else if (request && isCollectRequestActive(request)) {
    next = collectRequestCoversFamily(request, family)
      ? '正在采集，完成后将刷新本族结果；是否形成报告以采集结果为准。'
      : '本轮未包含当前地址族，结束后可选择本族采集。'
  }
  return (
    <>
      {fact}
      {notice?.attemptedAt ? <>最近尝试 <Timestamp value={notice.attemptedAt} mode="both" />。</> : null}
      {next}
    </>
  )
}

// hasReport：正文是否展示该族的一份真实报告；空态没有可“保留”的旧结果。
function noticeView(report: VPSIPQualityReport | null, ipVersion: IPQualityIPVersion | null, viewingHistory: boolean, refreshState: CollectNoticeView['refreshState'], hasReport: boolean): CollectNoticeView {
  const identity = reportCollectionIdentity(report)
  return { ipVersion, viewingHistory, reportCollectionID: identity.collectionID, reportCollectRequestID: identity.collectRequestID, refreshState, hasReport }
}

// 页头短标记：旧版、部分采集与归属复核；过期/历史等身份标记在报告上下文中。
function reportMarks(report: VPSIPQualityReport, summary: IPQualitySummary, product: IPQualityProductView | null): IPQualityHeaderMark[] {
  const marks: IPQualityHeaderMark[] = []
  const blocker = reportAttributionBlocker(report)
  if (blocker === 'ambiguous') marks.push({ label: '归属需复核', tone: 'notice', title: '出口 IP 同时匹配多台 VPS' })
  if (blocker === 'ip_conflict') marks.push({ label: '来源 IP 冲突', tone: 'notice', title: '来源返回了不同的 IP' })
  if (summary.status === 'partial') marks.push({ label: '部分采集', tone: 'neutral' })
  if (!product) marks.push({ label: '旧版报告', tone: 'neutral', title: '该报告早于完整报告格式，部分模块无数据' })
  return marks
}

function viewIPVersion(view: IPQualityDashboardView): IPQualityIPVersion | null {
  if (view.kind === 'report') return selectedReportIPVersion(view.report)
  if (view.kind === 'invalid_family') return null
  return view.ipVersion
}

function identityState(view: IPQualityDashboardView): IPQualityIdentityState {
  if (view.kind === 'report') return 'ready'
  if (view.kind === 'invalid_family') return 'invalid'
  return view.kind
}

export function IPQualityDashboard({ view, detailPath, collect, vpsId, viewingHistory, refreshState, onSelectFamily, enrichment = null }: IPQualityDashboardProps) {
  const location = useLocation()
  const history = viewingHistory && view.kind !== 'invalid_family'
  // 历史视图只信所选报告自身的族：未展示报告（加载/失败/空）时选择器、标题与身份一律“待确定”，
  // 不用 query 或响应选择族冒充。
  const ipVersion = history && view.kind !== 'report' ? null : viewIPVersion(view)
  // 本次响应：只有 report/empty 才有；loading/error/invalid 不派生完成通知或族状态。
  const response = view.kind === 'report' || view.kind === 'empty' ? view.report : null
  const summary = view.kind === 'report' ? view.summary : null
  const product = view.kind === 'report' ? selectedProduct(view.report) : null
  const hasReport = view.kind === 'report'
  const familyNotice = response && !history && ipVersion != null ? familyStateNotice(response, ipVersion) : null
  let notice: CollectNotice | null = null
  if (view.kind === 'report' || view.kind === 'empty') {
    notice = collect.error
      ? { tone: 'warning', title: collect.error }
      // 返回上下文沿用原口径：路由 vpsId 优先，报告视图回退到所选报告自身的 vps_id；空态不猜。
      : collectNotice(collect.status, collect.watchedRequestId, new Date(), noticeView(response, ipVersion, viewingHistory, refreshState, hasReport), vpsId ?? summary?.vps_id)
  }
  // 有采集结果条时只认它给出的 reportContext（包括“没有”）；只有完全没有采集结果条时，
  // 同选择后台刷新失败才由外壳标明正文是刷新前的报告。
  const reportContext: CollectReportContext | undefined = notice
    ? notice.reportContext
    : hasReport && !history && refreshState === 'failed' ? 'before_refresh' : undefined
  const returnLink = <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>

  return (
    <div className="page vps-detail-workspace ipq-page">
      <IPQualityHeader
        detailPath={detailPath}
        collect={collect}
        identityState={identityState(view)}
        summary={summary}
        targetIPVersion={ipVersion}
        viewingHistory={history}
        showCollect={view.kind === 'report'}
        familyControl={<IPQualityFamilySelector value={ipVersion} onChange={onSelectFamily} />}
        marks={view.kind === 'report' ? reportMarks(view.report, view.summary, product) : []}
        enrichment={product ? enrichment : null}
        familyNotice={familyNotice}
        reportContext={reportContext}
      />
      {view.kind === 'report' || view.kind === 'empty' ? <IPQualityCollectNotice notice={notice} /> : null}
      {view.kind === 'loading' ? (
        <PageState kind="loading" surface="empty" title={ipVersion == null ? '正在加载 IP 质量报告' : `正在加载 IPv${ipVersion} 报告`} />
      ) : null}
      {view.kind === 'error' ? (
        <PageState
          kind="error"
          surface="empty"
          title="IP 质量报告加载失败"
          technicalSummary={view.error}
          action={(
            <>
              <Button size="sm" onClick={view.onRetry}>重试</Button>
              {returnLink}
            </>
          )}
        />
      ) : null}
      {view.kind === 'invalid_family' ? (
        <PageState
          kind="empty"
          surface="empty"
          title="地址族参数无效"
          description="链接中的 ip_version 只能是 4 或 6，且只能出现一次。"
          action={<Link className="btn sm secondary" to={location.pathname} state={location.state}>查看默认报告</Link>}
        />
      ) : null}
      {view.kind === 'empty' ? (
        <PageState
          kind="empty"
          surface="empty"
          title={ipVersion == null ? '暂无 IP 质量报告' : `暂无 IPv${ipVersion} 质量报告`}
          description={emptyDescription(collect, ipVersion, familyNotice, history)}
          action={history ? returnLink : <IPQualityCollectButton collect={collect} />}
        />
      ) : null}
      {view.kind === 'report' ? (
        <IPQualityReportBody report={view.report} summary={view.summary} product={product} ipVersion={ipVersion} />
      ) : null}
    </div>
  )
}

type IPQualityReportBodyProps = {
  report: VPSIPQualityReport
  summary: IPQualitySummary
  product: IPQualityProductView | null
  ipVersion: IPQualityIPVersion | null
}

// 所有事实只来自所选报告本身（及其一次性 product 快照），不拼另一族数据。
function IPQualityReportBody({ report, summary, product, ipVersion }: IPQualityReportBodyProps) {
  const currentReportId = summary.report_id || report.latest_report?.report_id || ''
  const anonymousProbe = isAnonymousServiceProbeRevision(serviceProbeRevision(report))
  const providers = product ? product.providers : report.provider_results
  return (
    <>
      <IPQualityIdentityPanel report={report} summary={summary} product={product} />
      <IPQualityServiceGrid
        unlocks={report.service_unlocks}
        anonymousProbe={anonymousProbe}
        ipVersion={ipVersion}
        productSummary={product?.summary ?? null}
        ruleMark={serviceRuleMark(report)}
      />
      <IPQualityProviderTable
        results={providers}
        productSummary={product?.summary ?? null}
        {...(summary.risk_level ? { legacyRiskLevel: summary.risk_level } : {})}
      />
      <IPQualityLocationTable results={providers} />
      <div className="ipq-pair">
        <IPQualityMailTable product={product} />
        <IPQualityBlacklistTable product={product} />
      </div>
      <IPQualityHistoryList history={report.history} currentReportId={currentReportId} ipVersion={ipVersion} />
      <IPQualityDiagnostics report={report} summary={summary} product={product} />
    </>
  )
}
