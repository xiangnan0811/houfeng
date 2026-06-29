import { Link } from 'react-router-dom'

import { Badge } from '../../components/atoms'
import { formatPercent } from '../../lib/format'
import type { VPSIPQualityReport } from '../../lib/types'
import {
  deriveQualityScore,
  providerCoverage,
  qualityVerdict,
  serviceCoverage,
  serviceUnlockCounts,
  strongestRiskFlags,
} from '../../components/ip-quality/ipQualityPresentation'

type VPSIPQualitySectionProps = {
  vpsId: string
  report: VPSIPQualityReport | null
  error: string | null
}

export function VPSIPQualitySection({ vpsId, report, error }: VPSIPQualitySectionProps) {
  const summary = report?.summary ?? null
  const score = report ? deriveQualityScore(report) : null
  const riskFlags = report ? strongestRiskFlags(report) : []
  const riskFlagLabels = riskFlags.map((flag) => flag.label)
  const unlockCounts = report ? serviceUnlockCounts(report.service_unlocks) : null
  const providerCoveragePct = report ? providerCoverage(report) : null
  const serviceCoveragePct = report ? serviceCoverage(report) : null

  return (
    <section className="page-panel vps-ip-quality-summary" aria-labelledby="vps-ip-quality-summary-title">
      <div className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">IP Quality</p>
          <h2 id="vps-ip-quality-summary-title" className="section-heading__title">IP 质量概况</h2>
        </div>
        <div className="section-heading__actions">
          <Link className="btn sm secondary" to={`/vps/${encodeURIComponent(vpsId)}/ip-quality`}>
            查看完整 IP 质量报告
          </Link>
        </div>
      </div>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p>
      ) : null}
      {error ? (
        <div className="vps-cost-card__empty">
          <strong>报告暂不可用</strong>
          <span>保留完整报告入口；当前不根据缓存摘要推断评分。</span>
        </div>
      ) : summary && report ? (
        <>
          <div className="vps-ip-quality-summary__facts">
            <div>
              <span>质量评分</span>
              <strong>{score ?? '—'}</strong>
              <small>{qualityVerdict(score, summary)}</small>
            </div>
            <div>
              <span>风险信号</span>
              <strong>{riskFlags.length > 0 ? `${riskFlags.length} 项` : '未命中'}</strong>
              <small>{riskFlagLabels.length > 0 ? riskFlagLabels.join(' · ') : '无明显负面信号'}</small>
            </div>
            <div>
              <span>解锁概览</span>
              <strong>{unlockCounts ? `${unlockCounts.unlocked} 可用` : '—'}</strong>
              <small>{unlockCounts ? `${unlockCounts.blocked} 受阻 · ${unlockCounts.partial} 部分 · ${unlockCounts.unknown} 未知` : '暂无服务解锁结果'}</small>
            </div>
            <div>
              <span>证据覆盖</span>
              <strong>{formatPercent(providerCoveragePct, 0)}</strong>
              <small>服务 {formatPercent(serviceCoveragePct, 0)} · provider {report.provider_results.length}</small>
            </div>
          </div>

          <div className="vps-ip-quality-summary__unlock-overview" aria-label="IP 质量解锁概览">
            <Badge variant="state" tone="normal">{unlockCounts?.unlocked ?? 0} 可用</Badge>
            <Badge variant="state" tone="alert">{unlockCounts?.blocked ?? 0} 受阻</Badge>
            <Badge variant="state" tone="notice">{unlockCounts?.partial ?? 0} 部分</Badge>
            <Badge variant="state" tone="neutral">{unlockCounts?.unknown ?? 0} 未知</Badge>
            <span>{riskFlagLabels.length > 0 ? `重点风险：${riskFlagLabels.join(' · ')}` : '无明显负面信号'}</span>
          </div>
        </>
      ) : (
        <div className="vps-cost-card__empty">
          <strong>尚无 IP 质量报告</strong>
          <span>尚未收到可用质量结论。</span>
        </div>
      )}
    </section>
  )
}
