import { useSearchParams } from 'react-router-dom'

import { Badge, type BadgeTone } from '../atoms'
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
  deriveQualityScore,
  negativeRiskSignalCount,
  providerCoverage,
  providerSucceeded,
  qualityVerdict,
  riskEvidenceComplete,
  riskLevelLabel,
  riskTone,
  serviceCoverage,
  serviceUnlockCounts,
  strongestRiskFlags,
} from './ipQualityPresentation'
import type { IPQualityCollectController } from './useIPQualityCollect'

type IPQualityDashboardProps = {
  report: VPSIPQualityReport
  summary: IPQualitySummary
  detailPath: string
  collect: IPQualityCollectController
}

function scoreTone(score: number | null): BadgeTone {
  if (score == null) return 'neutral'
  if (score >= 82) return 'normal'
  if (score >= 68) return 'notice'
  return 'alert'
}

function IPQualityVerdict({ report, summary }: { report: VPSIPQualityReport, summary: IPQualitySummary }) {
  const score = deriveQualityScore(report)
  const negativeSignals = negativeRiskSignalCount(report.provider_results)
  const evidenceComplete = riskEvidenceComplete(report.provider_results)
  const flagLabels = strongestRiskFlags(report).map((flag) => flag.label)
  const services = serviceUnlockCounts(report.service_unlocks)
  const determinate = services.unlocked + services.partial + services.blocked
  const consistency = databaseConsistency(report.provider_results)
  const successfulProviders = report.provider_results.filter(providerSucceeded).length
  const providerCoveragePct = providerCoverage(report)
  const serviceCoveragePct = serviceCoverage(report)
  const providerRiskLevel = report.provider_results
    .filter(providerSucceeded)
    .map((provider) => (provider.risk_level ?? '').trim().toLowerCase())
    .find((risk) => risk === 'high' || risk === 'critical')
  let riskSignalDetail = '风险证据不足，未形成完整结论'
  if (negativeSignals > 0) {
    const knownFacts = flagLabels.length > 0
      ? flagLabels.join(' · ')
      : `风险等级：${riskLevelLabel(providerRiskLevel ?? summary.risk_level)}`
    riskSignalDetail = evidenceComplete ? knownFacts : `${knownFacts} · ${riskSignalDetail}`
  } else if (evidenceComplete) {
    riskSignalDetail = '已返回的风险字段未命中'
  }

  return (
    <section className={`vps-detail-workspace__section ipq-verdict ipq-verdict--${scoreTone(score)}`} aria-label="质量结论">
      <div className="ipq-verdict__score">
        <span className="ipq-verdict__label">质量分</span>
        <p className="ipq-verdict__value">
          <strong className="mono">{score ?? '—'}</strong>
          {score != null ? <span className="mono">/100</span> : null}
        </p>
        <p className="ipq-verdict__text">{qualityVerdict(score, summary)}</p>
        <div className="ipq-verdict__badges">
          <Badge variant="info" tone={riskTone(summary.risk_level)}>{riskLevelLabel(summary.risk_level)}</Badge>
          {summary.stale ? <Badge variant="info" tone="notice">报告过期</Badge> : null}
          {summary.ambiguous ? <Badge variant="info" tone="notice">归属需复核</Badge> : null}
          {summary.status === 'partial' ? <Badge variant="info" tone="neutral">部分采集</Badge> : null}
        </div>
      </div>
      <dl className="ipq-verdict__metrics" aria-label="IP 质量摘要指标">
        <div className={negativeSignals > 0 ? 'ipq-metric ipq-metric--alert' : 'ipq-metric'}>
          <dt>风险信号</dt>
          <dd>
            <strong className="mono">{negativeSignals > 0 ? `${negativeSignals} 项` : '未命中'}</strong>
            <span>{riskSignalDetail}</span>
          </dd>
        </div>
        <div className={services.blocked > 0 ? 'ipq-metric ipq-metric--alert' : 'ipq-metric'}>
          <dt>服务解锁</dt>
          <dd>
            <strong className="mono">{determinate > 0 ? `${services.unlocked}/${determinate}` : '—'}</strong>
            <span>{determinate > 0 ? `${services.blocked} 受阻 · ${services.unknown} 未知` : '暂无可靠结果'}</span>
          </dd>
        </div>
        <div className="ipq-metric">
          <dt>数据库一致性</dt>
          <dd>
            <strong className="mono">{consistency == null ? '—' : `${consistency}%`}</strong>
            <span>{consistency == null ? '样本不足' : `${successfulProviders} 个数据库`}</span>
          </dd>
        </div>
        <div className="ipq-metric">
          <dt>采集完整性</dt>
          <dd>
            <strong className="mono">{providerCoveragePct == null ? '—' : formatPercent(providerCoveragePct, 0)}</strong>
            <span>服务 {serviceCoveragePct == null ? '—' : formatPercent(serviceCoveragePct, 0)}</span>
          </dd>
        </div>
      </dl>
    </section>
  )
}

export function IPQualityDashboard({ report, summary, detailPath, collect }: IPQualityDashboardProps) {
  const [searchParams] = useSearchParams()
  const selectedReportId = searchParams.get('report_id')?.trim() || ''
  const viewingHistory = selectedReportId !== ''
  const currentReportId = selectedReportId || summary.report_id || report.latest_report?.report_id || ''

  return (
    <div className="page vps-detail-workspace ipq-page">
      <IPQualityHeader
        detailPath={detailPath}
        collect={collect}
        summary={summary}
        latest={report.latest_report ?? null}
        viewingHistory={viewingHistory}
      />
      {viewingHistory ? null : <IPQualityCollectNotice collect={collect} />}
      <IPQualityVerdict report={report} summary={summary} />
      <IPQualityServiceGrid unlocks={report.service_unlocks} />
      <IPQualityProviderTable results={report.provider_results} />
      <IPQualityHistoryList history={report.history} currentReportId={currentReportId} />
      <IPQualityDiagnostics report={report} summary={summary} />
    </div>
  )
}
