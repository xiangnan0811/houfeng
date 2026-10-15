import { Link, useLocation } from 'react-router-dom'

import { PageState } from '../PageState'
import type { IPQualityIPVersion, IPQualityProductView, IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import { IPQualityBlacklistTable } from './IPQualityBlacklistTable'
import { IPQualityCollectButton, IPQualityCollectNotice } from './IPQualityCollectControls'
import type { CollectNoticeView } from './ipQualityCollectPresentation'
import { IPQualityFamilySelector, IPQualityFamilyState } from './IPQualityFamily'
import { IPQualityDiagnostics } from './IPQualityDiagnostics'
import { IPQualityHeader, type IPQualityEnrichmentView, type IPQualityHeaderMark } from './IPQualityHeader'
import { IPQualityHistoryList } from './IPQualityHistoryList'
import { IPQualityIdentityPanel } from './IPQualityIdentityPanel'
import { IPQualityLocationTable } from './IPQualityLocationTable'
import { IPQualityMailTable } from './IPQualityMailTable'
import { IPQualityProviderTable } from './IPQualityProviderTable'
import { IPQualityServiceGrid } from './IPQualityServiceGrid'
import {
  isAnonymousServiceProbeRevision,
  reportAttributionBlocker,
  reportCollectionIdentity,
  selectedReportIPVersion,
  serviceProbeRevision,
  serviceRuleMark,
} from './ipQualityPresentation'
import { selectedProduct } from './ipQualityProductPresentation'
import type { IPQualityCollectController } from './useIPQualityCollect'

// 页面主体的几种形态共用同一页头与族选择器：切族加载时选择器不卸载，键盘焦点留在按钮上。
export type IPQualityDashboardView =
  | { kind: 'loading', ipVersion: IPQualityIPVersion | null }
  | { kind: 'invalid_family' }
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

function emptyDescription(collect: IPQualityCollectController, family: IPQualityIPVersion | null): string {
  const status = collect.status
  if (collect.active) return '采集完成后会自动显示报告。'
  if (status && !status.available) return '满足采集条件后，agent 会自动上报。'
  const label = family == null ? '该地址族' : `IPv${family}`
  return `${label} 还没有有效结果，可以立即采集一次。`
}

// hasReport：正文是否展示该族的一份真实报告；空态没有可“保留”的旧结果。
function noticeView(report: VPSIPQualityReport | null, ipVersion: IPQualityIPVersion | null, viewingHistory: boolean, refreshState: CollectNoticeView['refreshState'], hasReport: boolean): CollectNoticeView {
  const identity = reportCollectionIdentity(report)
  return { ipVersion, viewingHistory, reportCollectionID: identity.collectionID, reportCollectRequestID: identity.collectRequestID, refreshState, hasReport }
}

// 页头短标记：过期、旧版、部分采集与归属复核；不写解释段落。
function reportMarks(report: VPSIPQualityReport, summary: IPQualitySummary, product: IPQualityProductView | null): IPQualityHeaderMark[] {
  const marks: IPQualityHeaderMark[] = []
  if (summary.stale) marks.push({ label: '已过期', tone: 'notice', title: '报告超过过期窗口，仅作历史参考' })
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

export function IPQualityDashboard({ view, detailPath, collect, vpsId, viewingHistory, refreshState, onSelectFamily, enrichment = null }: IPQualityDashboardProps) {
  const location = useLocation()
  const ipVersion = viewIPVersion(view)
  const report = view.kind === 'report' ? view.report : null
  const summary = view.kind === 'report' ? view.summary : null
  const product = report ? selectedProduct(report) : null
  const returnLink = <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>

  return (
    <div className="page vps-detail-workspace ipq-page">
      <IPQualityHeader
        detailPath={detailPath}
        collect={collect}
        summary={summary}
        viewingHistory={viewingHistory && view.kind !== 'invalid_family'}
        latestIPVersion={ipVersion}
        showCollect={view.kind === 'report'}
        familyControl={<IPQualityFamilySelector value={ipVersion} onChange={onSelectFamily} />}
        marks={report && summary ? reportMarks(report, summary, product) : []}
        enrichment={product ? enrichment : null}
      />
      {view.kind === 'loading' ? (
        <PageState kind="loading" surface="empty" title={ipVersion == null ? '正在加载 IP 质量报告' : `正在加载 IPv${ipVersion} 报告`} />
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
        <>
          <IPQualityCollectNotice collect={collect} vpsId={vpsId} view={noticeView(view.report, ipVersion, viewingHistory, refreshState, false)} />
          {view.report && ipVersion != null && !viewingHistory ? <IPQualityFamilyState report={view.report} ipVersion={ipVersion} /> : null}
          <PageState
            kind="empty"
            surface="empty"
            title={ipVersion == null ? '暂无 IP 质量报告' : `暂无 IPv${ipVersion} 质量报告`}
            description={emptyDescription(collect, ipVersion)}
            action={viewingHistory ? returnLink : <IPQualityCollectButton collect={collect} />}
          />
        </>
      ) : null}
      {view.kind === 'report' ? (
        <IPQualityReportBody
          report={view.report}
          summary={view.summary}
          product={product}
          ipVersion={ipVersion}
          collect={collect}
          vpsId={vpsId ?? view.summary.vps_id}
          viewingHistory={viewingHistory}
          refreshState={refreshState}
        />
      ) : null}
    </div>
  )
}

type IPQualityReportBodyProps = {
  report: VPSIPQualityReport
  summary: IPQualitySummary
  product: IPQualityProductView | null
  ipVersion: IPQualityIPVersion | null
  collect: IPQualityCollectController
  vpsId: string | undefined
  viewingHistory: boolean
  refreshState: CollectNoticeView['refreshState']
}

// 所有事实只来自所选报告本身（及其一次性 product 快照），不拼另一族数据。
function IPQualityReportBody({ report, summary, product, ipVersion, collect, vpsId, viewingHistory, refreshState }: IPQualityReportBodyProps) {
  const currentReportId = summary.report_id || report.latest_report?.report_id || ''
  const anonymousProbe = isAnonymousServiceProbeRevision(serviceProbeRevision(report))
  const providers = product ? product.providers : report.provider_results
  return (
    <>
      <IPQualityCollectNotice collect={collect} vpsId={vpsId} view={noticeView(report, ipVersion, viewingHistory, refreshState, true)} />
      {!viewingHistory && ipVersion != null ? <IPQualityFamilyState report={report} ipVersion={ipVersion} /> : null}
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
