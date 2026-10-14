import { useSearchParams } from 'react-router-dom'

import { Badge } from '../atoms'
import { formatPercent } from '../../lib/format'
import type { IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import { IPQualityCollectNotice } from './IPQualityCollectControls'
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
  negativeRiskSignals,
  riskAssessmentBlocker,
  riskVerdict,
  serviceCoverage,
  serviceProbeRevision,
  serviceUnlockCounts,
  unlockStatusText,
} from './ipQualityPresentation'
import type { IPQualityCollectController } from './useIPQualityCollect'

type IPQualityDashboardProps = {
  report: VPSIPQualityReport
  summary: IPQualitySummary
  detailPath: string
  collect: IPQualityCollectController
  vpsId?: string
}

function IPQualityVerdict({ report, summary, anonymousProbe }: { report: VPSIPQualityReport, summary: IPQualitySummary, anonymousProbe: boolean }) {
  const verdict = riskVerdict(report)
  const assessment = deriveRiskAssessment(report)
  const blocker = riskAssessmentBlocker(report)
  const signals = negativeRiskSignals(report.provider_results)
  const highRiskSources = highRiskProviders(report.provider_results)
  const services = serviceUnlockCounts(report.service_unlocks)
  const determinate = services.unlocked + services.partial + services.blocked
  const consistency = databaseConsistency(report.provider_results)
  const providerCoverage = enabledProviderCoverage(report)
  const serviceCoveragePct = serviceCoverage(report)
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
        <p className="ipq-verdict__value">
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
            <strong className="mono">{determinate > 0 ? `${services.unlocked}/${determinate}` : '—'}</strong>
            <span>
              {determinate > 0
                ? `${services.blocked} ${unlockStatusText('blocked', anonymousProbe)} · ${services.unknown} 未知`
                : '暂无可靠结果'}
            </span>
          </dd>
        </div>
        <div className="ipq-metric">
          <dt>数据库一致性</dt>
          <dd>
            <strong className="mono">{consistency == null ? '—' : `${consistency.percent}%`}</strong>
            <span>{consistency == null ? '可比较字段不足' : `${consistency.dimensions} 个可比维度`}</span>
          </dd>
        </div>
        <div className="ipq-metric">
          <dt>已启用来源成功率</dt>
          <dd>
            <strong className="mono">{providerCoverage == null ? '—' : formatPercent(providerCoverage.percent, 0)}</strong>
            <span>
              {providerCoverage == null
                ? '暂无有效覆盖数据'
                : `成功 ${providerCoverage.successful}/${providerCoverage.enabled} · 未配置 ${providerCoverage.notConfigured}`}
            </span>
            <span>服务探测 {serviceCoveragePct == null ? '—' : formatPercent(serviceCoveragePct, 0)}</span>
          </dd>
        </div>
      </dl>
    </section>
  )
}

export function IPQualityDashboard({ report, summary, detailPath, collect, vpsId }: IPQualityDashboardProps) {
  const [searchParams] = useSearchParams()
  const selectedReportId = searchParams.get('report_id')?.trim() || ''
  const viewingHistory = selectedReportId !== ''
  const currentReportId = selectedReportId || summary.report_id || report.latest_report?.report_id || ''
  const probeNotice = defaultServiceProbeNotice(report)
  const anonymousProbe = serviceProbeRevision(report) === 2

  return (
    <div className="page vps-detail-workspace ipq-page">
      <IPQualityHeader
        detailPath={detailPath}
        collect={collect}
        summary={summary}
        latest={report.latest_report ?? null}
        viewingHistory={viewingHistory}
      />
      {viewingHistory ? null : <IPQualityCollectNotice collect={collect} vpsId={vpsId ?? summary.vps_id} />}
      {probeNotice ? (
        <aside className={`ipq-notice ipq-notice--${probeNotice.tone}`} role="note" aria-label="服务探测可信度">
          <span className="ipq-notice__dot" aria-hidden="true" />
          <span className="ipq-notice__title">服务探测可信度</span>
          <span className="ipq-notice__detail">{probeNotice.text}</span>
        </aside>
      ) : null}
      <IPQualityVerdict report={report} summary={summary} anonymousProbe={anonymousProbe} />
      <IPQualityServiceGrid unlocks={report.service_unlocks} anonymousProbe={anonymousProbe} />
      <IPQualityProviderTable results={report.provider_results} />
      <IPQualityHistoryList history={report.history} currentReportId={currentReportId} />
      <IPQualityDiagnostics report={report} summary={summary} />
    </div>
  )
}
