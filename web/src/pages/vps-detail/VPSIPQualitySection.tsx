import { Link } from 'react-router-dom'

import { Badge } from '../../components/atoms'
import { formatDateTime, formatOptional, formatPercent } from '../../lib/format'
import type { VPSIPQualityReport } from '../../lib/types'
import { IPQualityBadge } from '../assetPageBadges'
import {
  activeRiskFlags,
  deriveQualityScore,
  providerCoverage,
  qualityVerdict,
  riskTone,
  serviceCoverage,
  serviceLabel,
  serviceUnlockCounts,
  unlockStatusLabel,
  unlockTone,
} from '../../components/ip-quality/ipQualityPresentation'

type VPSIPQualitySectionProps = {
  vpsId: string
  report: VPSIPQualityReport | null
  error: string | null
}

function strongestRiskFlags(report: VPSIPQualityReport): string[] {
  const seen = new Set<string>()
  for (const result of report.provider_results) {
    for (const flag of activeRiskFlags(result)) {
      if (!flag.negative) continue
      seen.add(flag.label)
    }
  }
  return Array.from(seen).slice(0, 4)
}

export function VPSIPQualitySection({ vpsId, report, error }: VPSIPQualitySectionProps) {
  const summary = report?.summary ?? null
  const score = report ? deriveQualityScore(report) : null
  const riskFlags = report ? strongestRiskFlags(report) : []
  const unlockCounts = report ? serviceUnlockCounts(report.service_unlocks) : null
  const providerCoveragePct = report ? providerCoverage(report) : null
  const serviceCoveragePct = report ? serviceCoverage(report) : null
  const visibleUnlocks = report?.service_unlocks.slice(0, 4) ?? []

  return (
    <section className="page-panel vps-ip-quality-summary">
      <div className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">IP Quality</p>
          <h2 className="section-heading__title">IP 质量</h2>
          <p className="section-heading__description">
            摘要只保留关键质量结论；完整 provider、服务解锁、覆盖率和诊断请进入报告页查看。
          </p>
        </div>
        <div className="section-heading__actions">
          <IPQualityBadge summary={summary} />
          <Link className="btn sm secondary" to={`/vps/${encodeURIComponent(vpsId)}/ip-quality`}>
            查看完整 IP 质量报告
          </Link>
        </div>
      </div>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p>
      ) : null}
      {summary && report ? (
        <>
          <div className="vps-ip-quality-summary__lead">
            <div className="vps-ip-quality-summary__score">
              <span>质量评分</span>
              <strong>{score ?? '—'}</strong>
              <small>{qualityVerdict(score, summary)}</small>
            </div>
            <div className="vps-ip-quality-summary__facts">
              <div>
                <span>风险信号</span>
                <strong>{riskFlags.length > 0 ? `${riskFlags.length} 项` : '未命中'}</strong>
                <small>{riskFlags.length > 0 ? riskFlags.join(' · ') : '未发现 proxy / VPN / abuse 等负面信号'}</small>
              </div>
              <div>
                <span>服务解锁</span>
                <strong>{unlockCounts ? `${unlockCounts.unlocked} 可用 / ${unlockCounts.blocked} 受阻` : '—'}</strong>
                <small>{unlockCounts ? `${unlockCounts.partial} 部分 · ${unlockCounts.unknown} 未知` : '暂无服务解锁结果'}</small>
              </div>
              <div>
                <span>证据覆盖</span>
                <strong>{formatPercent(providerCoveragePct, 0)}</strong>
                <small>服务 {formatPercent(serviceCoveragePct, 0)} · provider {report.provider_results.length}</small>
              </div>
              <div>
                <span>采集时间</span>
                <strong>{formatDateTime(summary.observed_at)}</strong>
                <small>{summary.stale ? '报告已过期' : summary.ambiguous ? '归属需复核' : '低频报告'}</small>
              </div>
            </div>
          </div>

          <div className="vps-ip-quality-summary__context">
            <span className="asset-context-pill">
              出口 IP <strong>{summary.ip_address}</strong>
            </span>
            <span className="asset-context-pill">
              ASN <strong>{formatOptional(summary.asn)}</strong>
            </span>
            <span className="asset-context-pill">
              组织 <strong>{formatOptional(summary.organization)}</strong>
            </span>
            <span className="asset-context-pill">
              使用地 <strong>{summary.use_region_code || summary.use_region_name || '—'}</strong>
            </span>
          </div>

          {riskFlags.length > 0 ? (
            <div className="asset-context-inline">
              {riskFlags.map((flag) => (
                <Badge key={flag} variant="state" tone="alert">{flag}</Badge>
              ))}
            </div>
          ) : null}

          {visibleUnlocks.length > 0 ? (
            <div className="asset-context-inline">
              {visibleUnlocks.map((unlock) => (
                <span key={unlock.service} className={unlock.status === 'blocked' ? 'asset-context-pill asset-context-pill--attention' : 'asset-context-pill'}>
                  <strong>{serviceLabel(unlock.service)}</strong>
                  <span>{unlockStatusLabel(unlock.status, unlock.region)}</span>
                </span>
              ))}
            </div>
          ) : null}

          <div className="asset-context-inline">
            <Badge variant="info" tone={riskTone(summary.risk_level)}>
              {summary.risk_level || '未评级'}
            </Badge>
            {visibleUnlocks.map((unlock) => (
              <Badge key={`${unlock.service}-${unlock.status}`} variant="info" tone={unlockTone(unlock.status)}>
                {serviceLabel(unlock.service)} {unlockStatusLabel(unlock.status, unlock.region)}
              </Badge>
            ))}
          </div>
        </>
      ) : (
        <div className="vps-cost-card__empty">
          <strong>尚无 IP 质量报告</strong>
          <span>Agent 完成低频采集后会展示质量结论、风险信号和完整报告入口。</span>
        </div>
      )}
    </section>
  )
}
