import { Link, useLocation } from 'react-router-dom'

import { Badge } from '../atoms'
import { PageState } from '../PageState'
import { formatPercent } from '../../lib/format'
import type { IPQualityIPVersion, IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import { IPQualityCollectButton, IPQualityCollectNotice } from './IPQualityCollectControls'
import type { CollectNoticeView } from './ipQualityCollectPresentation'
import { IPQualityFamilySelector, IPQualityFamilyState } from './IPQualityFamily'
import { IPQualityDiagnostics } from './IPQualityDiagnostics'
import { IPQualityHeader } from './IPQualityHeader'
import { IPQualityHistoryList } from './IPQualityHistoryList'
import { IPQualityProviderTable } from './IPQualityProviderTable'
import { IPQualityServiceGrid } from './IPQualityServiceGrid'
import {
  databaseConsistency,
  defaultServiceProbeNotice,
  deriveRiskAssessment,
  enabledProviderCoverage,
  highRiskProviders,
  isAnonymousServiceProbeRevision,
  negativeRiskSignals,
  riskAssessmentBlocker,
  riskVerdict,
  selectedReportIPVersion,
  serviceAvailability,
  reportCollectionIdentity,
  serviceProbeRevision,
  type ServiceAvailability,
  unlockStatusText,
} from './ipQualityPresentation'
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
}

function emptyDescription(collect: IPQualityCollectController, family: IPQualityIPVersion | null): string {
  const status = collect.status
  if (collect.active) return '采集完成后会自动显示报告。'
  if (status && !status.available) return '满足采集条件后，agent 会自动上报。'
  const label = family == null ? '该地址族' : `IPv${family}`
  return `${label} 还没有有效结果，可以立即采集一次（同时检测 IPv4 / IPv6）。`
}

function noticeView(report: VPSIPQualityReport | null, ipVersion: IPQualityIPVersion | null, viewingHistory: boolean, refreshState: CollectNoticeView['refreshState']): CollectNoticeView {
  const identity = reportCollectionIdentity(report)
  return { ipVersion, viewingHistory, reportCollectionID: identity.collectionID, reportCollectRequestID: identity.collectRequestID, refreshState }
}

// 服务分布：partial 与 unknown 分列，不并入可用或受限。
function serviceDistribution(services: ServiceAvailability, anonymousProbe: boolean): string {
  const parts = (['partial', 'blocked', 'unknown'] as const)
    .filter((kind) => services[kind] > 0)
    .map((kind) => `${services[kind]} ${unlockStatusText(kind, anonymousProbe)}`)
  if (parts.length > 0) return parts.join(' · ')
  return services.unlocked > 0 ? '无部分可用、受限或未知' : '暂无服务结果'
}

function IPQualityVerdict({ report, summary, anonymousProbe }: { report: VPSIPQualityReport, summary: IPQualitySummary, anonymousProbe: boolean }) {
  const verdict = riskVerdict(report)
  const assessment = deriveRiskAssessment(report)
  const blocker = riskAssessmentBlocker(report)
  const signals = negativeRiskSignals(report.provider_results)
  const highRiskSources = highRiskProviders(report.provider_results)
  const services = serviceAvailability(report)
  const consistency = databaseConsistency(report.provider_results)
  const providerCoverage = enabledProviderCoverage(report)
  const verdictTone = verdict.tone === 'critical' ? 'alert' : verdict.tone

  let signalValue = '未知'
  let signalDetail = blocker ? verdict.headline : `风险字段 ${assessment.knownFlags}/${assessment.totalFlags}，结论不完整`
  if (signals.length > 0) {
    signalValue = `${signals.length} 项`
    signalDetail = signals.map((flag) => flag.label).join(' · ')
  } else if (assessment.complete) {
    signalValue = '未命中'
    signalDetail = '已返回风险字段未命中'
  }

  return (
    <section className={`vps-detail-workspace__section ipq-verdict ipq-verdict--${verdictTone}`} aria-label="数据库风险评级">
      <div className="ipq-verdict__score">
        <span className="ipq-verdict__label">数据库风险评级</span>
        <p className={verdict.valueKind === 'gap' ? 'ipq-verdict__value ipq-verdict__value--gap' : 'ipq-verdict__value'}>
          <strong>{verdict.value}</strong>
        </p>
        <p className="ipq-verdict__text">{verdict.headline}</p>
        {verdict.scope ? <span className="ipq-muted ipq-verdict__scope">{verdict.scope}</span> : null}
        {summary.stale && !blocker ? <span className="ipq-muted ipq-verdict__stale">报告已过期，仅作历史风险信息</span> : null}
        <div className="ipq-verdict__badges">
          {summary.stale ? <Badge variant="info" tone="notice">报告过期</Badge> : null}
          {summary.ambiguous ? <Badge variant="info" tone="notice">归属需复核</Badge> : null}
          {blocker === 'ip_conflict' ? <Badge variant="info" tone="notice">来源 IP 冲突</Badge> : null}
          {summary.status === 'partial' ? <Badge variant="info" tone="neutral">部分采集</Badge> : null}
        </div>
      </div>
      <dl className="ipq-verdict__metrics" aria-label="IP 质量摘要指标">
        <div className={signals.length > 0 || highRiskSources.length > 0 ? 'ipq-metric ipq-metric--alert' : 'ipq-metric'}>
          <dt>风险信号</dt>
          <dd>
            <strong className="mono">{signalValue}</strong>
            <span>{signalDetail}</span>
            {highRiskSources.length > 0 ? <span>高风险来源：{highRiskSources.join('、')}</span> : null}
          </dd>
        </div>
        <div className={services.blocked > 0 ? 'ipq-metric ipq-metric--alert' : 'ipq-metric'}>
          <dt>{anonymousProbe ? '服务可用性' : '服务解锁'}</dt>
          <dd>
            <strong className="mono">
              {services.expected == null
                ? `${services.unlocked} ${unlockStatusText('unlocked', anonymousProbe)}`
                : `${services.unlocked}/${services.expected} ${unlockStatusText('unlocked', anonymousProbe)}`}
            </strong>
            <span>{serviceDistribution(services, anonymousProbe)}</span>
          </dd>
        </div>
        <div className="ipq-metric">
          <dt>已知字段一致性</dt>
          <dd>
            <strong className="mono">{consistency == null ? '—' : `${consistency.percent}%`}</strong>
            <span>{consistency == null ? '可比较字段不足' : `${consistency.dimensions} 个可比较维度`}</span>
          </dd>
        </div>
        <div className="ipq-metric">
          <dt>已启用来源响应成功率</dt>
          <dd>
            <strong className="mono">{providerCoverage == null ? '—' : formatPercent(providerCoverage.percent, 0)}</strong>
            <span>
              {providerCoverage == null
                ? '暂无有效覆盖数据'
                : `响应成功 ${providerCoverage.successful}/${providerCoverage.enabled}`}
            </span>
          </dd>
        </div>
      </dl>
    </section>
  )
}

function viewIPVersion(view: IPQualityDashboardView): IPQualityIPVersion | null {
  if (view.kind === 'report') return selectedReportIPVersion(view.report)
  if (view.kind === 'invalid_family') return null
  return view.ipVersion
}

export function IPQualityDashboard({ view, detailPath, collect, vpsId, viewingHistory, refreshState, onSelectFamily }: IPQualityDashboardProps) {
  const location = useLocation()
  const ipVersion = viewIPVersion(view)
  const report = view.kind === 'report' ? view.report : null
  const summary = view.kind === 'report' ? view.summary : null
  const returnLink = <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>

  return (
    <div className="page vps-detail-workspace ipq-page">
      <IPQualityHeader
        detailPath={detailPath}
        collect={collect}
        summary={summary}
        latest={report?.latest_report ?? null}
        viewingHistory={viewingHistory && view.kind !== 'invalid_family'}
        latestIPVersion={ipVersion}
        showCollect={view.kind === 'report'}
        familyControl={<IPQualityFamilySelector value={ipVersion} onChange={onSelectFamily} />}
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
          <IPQualityCollectNotice collect={collect} vpsId={vpsId} view={noticeView(view.report, ipVersion, viewingHistory, refreshState)} />
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
  ipVersion: IPQualityIPVersion | null
  collect: IPQualityCollectController
  vpsId: string | undefined
  viewingHistory: boolean
  refreshState: CollectNoticeView['refreshState']
}

// 所有事实只来自所选报告本身，不拼另一族数据。
function IPQualityReportBody({ report, summary, ipVersion, collect, vpsId, viewingHistory, refreshState }: IPQualityReportBodyProps) {
  const currentReportId = summary.report_id || report.latest_report?.report_id || ''
  const probeNotice = defaultServiceProbeNotice(report)
  const anonymousProbe = isAnonymousServiceProbeRevision(serviceProbeRevision(report))
  return (
    <>
      <IPQualityCollectNotice collect={collect} vpsId={vpsId} view={noticeView(report, ipVersion, viewingHistory, refreshState)} />
      {!viewingHistory && ipVersion != null ? <IPQualityFamilyState report={report} ipVersion={ipVersion} /> : null}
      {probeNotice ? (
        <aside className={`ipq-notice ipq-notice--${probeNotice.tone}`} role="note" aria-label="服务探测可信度">
          <span className="ipq-notice__dot" aria-hidden="true" />
          <span className="ipq-notice__title">服务探测可信度</span>
          <span className="ipq-notice__detail">{probeNotice.text}</span>
        </aside>
      ) : null}
      <IPQualityVerdict report={report} summary={summary} anonymousProbe={anonymousProbe} />
      <IPQualityServiceGrid unlocks={report.service_unlocks} anonymousProbe={anonymousProbe} ipVersion={ipVersion} />
      <IPQualityProviderTable results={report.provider_results} />
      <IPQualityHistoryList history={report.history} currentReportId={currentReportId} ipVersion={ipVersion} />
      <IPQualityDiagnostics report={report} summary={summary} />
    </>
  )
}
