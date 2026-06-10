import { Link } from 'react-router-dom'

import { Badge, type BadgeTone } from '../atoms'
import { formatDateTime, formatNumber, formatOptional, formatPercent } from '../../lib/format'
import type { IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import {
  activeRiskFlags,
  databaseConsistency,
  deriveQualityScore,
  providerCoverage,
  providerEvidenceSignals,
  providerSucceeded,
  qualityVerdict,
  riskFlags,
  riskLevelLabel,
  riskSignalCounts,
  riskTone,
  serviceCardDescription,
  serviceCoverage,
  serviceLabel,
  serviceUnlockCounts,
  topQualityReasons,
  unlockStatusKind,
  unlockStatusLabel,
  unlockTone,
} from './ipQualityPresentation'

type IPQualityDashboardProps = {
  report: VPSIPQualityReport
  summary: IPQualitySummary
  detailPath: string
}

function boolLabel(value: boolean | null): string {
  if (value === true) return '是'
  if (value === false) return '否'
  return '未知'
}

function boolTone(value: boolean | null, negative: boolean): BadgeTone {
  if (value === true && negative) return 'alert'
  if (value === false && negative) return 'normal'
  if (value === true) return 'notice'
  return 'neutral'
}

function statusToneClass(status: string): string {
  const kind = unlockStatusKind(status)
  if (kind === 'blocked') return 'vps-ip-quality-dashboard__service--blocked'
  if (kind === 'partial') return 'vps-ip-quality-dashboard__service--partial'
  if (kind === 'unlocked') return 'vps-ip-quality-dashboard__service--unlocked'
  return 'vps-ip-quality-dashboard__service--unknown'
}

function metricValue(value: number | null, suffix = ''): string {
  if (value == null) return '样本不足'
  return `${formatNumber(value, 0)}${suffix}`
}

function coverageLabel(value: number | null): string {
  return value == null ? '未采集' : formatPercent(value, 0)
}

function sourceStatusLabel(value?: string): string {
  if (value === 'success' || !value) return '已采集'
  if (value === 'failure') return '失败'
  if (value === 'skipped') return '未检测'
  if (value === 'not_configured') return '未配置'
  return value
}

function sourceStatusTone(value?: string): BadgeTone {
  if (value === 'success' || !value) return 'normal'
  if (value === 'failure') return 'alert'
  if (value === 'skipped') return 'notice'
  if (value === 'not_configured') return 'neutral'
  return 'neutral'
}

function unlockMeta(unlockRegion?: string, unlockType?: string): string {
  const parts: string[] = []
  if (unlockRegion) parts.push(`区域 ${unlockRegion}`)
  if (unlockType && unlockType !== 'none') parts.push(`类型 ${unlockType}`)
  return parts.length > 0 ? parts.join(' · ') : '无可展示区域'
}

function compactJSON(value: unknown): string {
  if (value == null) return '—'
  try {
    return JSON.stringify(value)
  } catch {
    return '无法展示'
  }
}

export function IPQualityDashboard({ report, summary, detailPath }: IPQualityDashboardProps) {
  const score = deriveQualityScore(report)
  const reasons = topQualityReasons(report)
  const providerCoveragePct = providerCoverage(report)
  const serviceCoveragePct = serviceCoverage(report)
  const consistency = databaseConsistency(report.provider_results)
  const serviceCounts = serviceUnlockCounts(report.service_unlocks)
  const coverage = summary.coverage ?? report.latest_report?.coverage
  const negativeSignals = report.provider_results.filter(providerSucceeded).reduce((total, result) => {
    const flags = activeRiskFlags(result).filter((flag) => flag.negative).length
    const risk = (result.risk_level ?? '').trim().toLowerCase()
    return total + flags + (risk === 'high' || risk === 'critical' ? 1 : 0)
  }, 0)

  return (
    <div className="page-stack asset-page vps-ip-quality-dashboard">
      <section className="page-panel vps-ip-quality-dashboard__hero">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">IP Quality</p>
            <h1 className="section-heading__title">IP 质量驾驶舱</h1>
            <p className="section-heading__description">
              完整展示低频 IP 质量报告：风险来源、provider 分歧、服务解锁、覆盖率、历史和诊断。
            </p>
            <p className="section-heading__meta">
              最近采集 {formatDateTime(summary.observed_at)} · Agent {report.latest_report?.agent_version || '—'} · {report.provider_results.length} 个 provider · {report.service_unlocks.length} 个服务
            </p>
          </div>
          <div className="section-heading__actions">
            <Link className="btn sm secondary" to={detailPath}>返回 VPS 详情</Link>
          </div>
        </div>

        <div className="vps-ip-quality-dashboard__lead">
          <div className="vps-ip-quality-dashboard__score">
            <span>Quality Score</span>
            <strong>{score ?? '—'}</strong>
            <small>{qualityVerdict(score, summary)}</small>
            <div className="vps-ip-quality-dashboard__score-badges">
              <Badge variant="state" tone={riskTone(summary.risk_level)}>{riskLevelLabel(summary.risk_level)}</Badge>
              {summary.ambiguous ? <Badge variant="state" tone="notice">归属需复核</Badge> : null}
              {summary.stale ? <Badge variant="state" tone="notice">报告过期</Badge> : null}
            </div>
          </div>
          <div className="vps-ip-quality-dashboard__reasons">
            {reasons.map((reason) => (
              <div key={reason.title} className="vps-ip-quality-dashboard__reason">
                <strong>{reason.title}</strong>
                <span>{reason.detail}</span>
                <em>{reason.impact}</em>
              </div>
            ))}
          </div>
          <div className="vps-ip-quality-dashboard__metrics" aria-label="IP 质量摘要指标">
            <div>
              <span>风险信号</span>
              <strong>{negativeSignals} 项</strong>
              <small>proxy / vpn / abuse / 高风险等级</small>
            </div>
            <div>
              <span>解锁可用</span>
              <strong>{serviceCounts.unlocked} / {report.service_unlocks.length}</strong>
              <small>{serviceCounts.blocked} 受阻 · {serviceCounts.partial} 部分</small>
            </div>
            <div>
              <span>数据库一致性</span>
              <strong>{metricValue(consistency, '%')}</strong>
              <small>基于已归一 provider 字段</small>
            </div>
            <div>
              <span>采集完整性</span>
              <strong>{coverageLabel(providerCoveragePct)}</strong>
              <small>服务 {coverageLabel(serviceCoveragePct)}</small>
              {coverage ? <small>{coverage.failed_provider_count + coverage.skipped_provider_count + coverage.not_configured_provider_count} 个来源未成功</small> : null}
            </div>
          </div>
        </div>
      </section>

      <section className="page-panel vps-ip-quality-dashboard__signal-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">Risk Matrix</p>
            <h2 className="section-heading__title">风险信号矩阵</h2>
          </div>
          <span className="section-heading__meta">Server / Datacenter 本身只作为上下文，不单独构成负面风险。</span>
        </div>
        <div className="vps-ip-quality-dashboard__signal-grid">
          {riskSignalCounts(report.provider_results).map((signal) => (
            <div key={signal.key} className={signal.negative && signal.yes > 0 ? 'vps-ip-quality-dashboard__signal vps-ip-quality-dashboard__signal--attention' : 'vps-ip-quality-dashboard__signal'}>
              <span>{signal.label}</span>
              <strong>{signal.yes} 是 / {signal.no} 否</strong>
              <small>{signal.unknown} 未知</small>
            </div>
          ))}
        </div>
      </section>

      <section className="page-panel page-panel--scroll-x vps-ip-quality-dashboard__provider-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">Provider Evidence</p>
            <h2 className="section-heading__title">各 IP 数据库判断</h2>
          </div>
          <span className="section-heading__meta">逐 provider 展示，不把分歧合并掉。</span>
        </div>
        {report.provider_results.length > 0 ? (
          <table className="data-table data-table--compact asset-table vps-ip-quality-dashboard__provider-table">
            <thead className="data-table__head">
              <tr>
                <th>Provider</th>
                <th>状态</th>
                <th>使用类型</th>
                <th>公司类型</th>
                <th>风险</th>
                <th>地区</th>
                <th>Proxy</th>
                <th>Tor</th>
                <th>VPN</th>
                <th>Server</th>
                <th>Abuse</th>
                <th>Robot</th>
                <th>证据说明</th>
                <th>Extra</th>
              </tr>
            </thead>
            <tbody>
              {report.provider_results.map((result) => {
                const flags = riskFlags(result)
                const evidenceSignals = providerEvidenceSignals(result)
                return (
                  <tr key={result.provider} className="data-table__row">
                    <td className="data-table__cell"><strong>{result.provider}</strong></td>
                    <td className="data-table__cell"><Badge variant="state" tone={sourceStatusTone(result.status)}>{sourceStatusLabel(result.status)}</Badge></td>
                    <td className="data-table__cell">{formatOptional(result.usage_type)}</td>
                    <td className="data-table__cell">{formatOptional(result.company_type)}</td>
                    <td className="data-table__cell">
                      <Badge variant="state" tone={riskTone(result.risk_level)}>{result.risk_level || result.risk_score || '未评级'}</Badge>
                      {result.risk_score ? <small>{result.risk_score}</small> : null}
                    </td>
                    <td className="data-table__cell">{result.region_code || result.region_name || '—'}</td>
                    {flags.map((flag) => (
                      <td key={flag.key} className="data-table__cell">
                        <Badge variant="info" tone={boolTone(flag.active, flag.negative)}>{boolLabel(flag.active)}</Badge>
                      </td>
                    ))}
                    <td className="data-table__cell">
                      <span className="vps-ip-quality-dashboard__evidence-chips">
                        {evidenceSignals.map((signal) => (
                          <Badge key={signal.key} variant="info" tone={signal.tone}>{signal.label}</Badge>
                        ))}
                      </span>
                    </td>
                    <td className="data-table__cell">
                      {result.extra_json ? (
                        <details className="vps-ip-quality-dashboard__json-detail">
                          <summary>查看</summary>
                          <code>{compactJSON(result.extra_json)}</code>
                        </details>
                      ) : '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        ) : (
          <p className="asset-table-empty-state">暂无 provider 结果。</p>
        )}
      </section>

      <section className="page-panel vps-ip-quality-dashboard__service-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">Service Unlock</p>
            <h2 className="section-heading__title">服务解锁矩阵</h2>
          </div>
          <div className="asset-context-inline" aria-label="服务解锁状态统计">
            <Badge variant="state" tone="normal">{serviceCounts.unlocked} 可用</Badge>
            <Badge variant="state" tone="alert">{serviceCounts.blocked} 受阻</Badge>
            <Badge variant="state" tone="notice">{serviceCounts.partial} 部分</Badge>
            <Badge variant="state" tone="neutral">{serviceCounts.unknown} 未知</Badge>
          </div>
        </div>
        {report.service_unlocks.length > 0 ? (
          <div className="vps-ip-quality-dashboard__service-grid">
            {report.service_unlocks.map((unlock) => (
              <article key={unlock.service} className={`vps-ip-quality-dashboard__service ${statusToneClass(unlock.status)}`}>
                <header>
                  <h3>{serviceLabel(unlock.service)}</h3>
                  <Badge variant="state" tone={unlockTone(unlock.status)}>{unlockStatusLabel(unlock.status, unlock.region)}</Badge>
                </header>
                <p>{serviceCardDescription(unlock)}</p>
                <small>{unlockMeta(unlock.region, unlock.unlock_type)}</small>
                {unlock.extra_json ? (
                  <details className="vps-ip-quality-dashboard__json-detail">
                    <summary>查看采集细节</summary>
                    <code>{compactJSON(unlock.extra_json)}</code>
                  </details>
                ) : null}
              </article>
            ))}
          </div>
        ) : (
          <p className="asset-table-empty-state">暂无服务解锁结果。</p>
        )}
      </section>

      <section className="page-panel vps-ip-quality-dashboard__context-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">Context And Coverage</p>
            <h2 className="section-heading__title">证据上下文与采集完整性</h2>
          </div>
          <span className="section-heading__meta">基础 IP 信息作为证据上下文，不作为本页主结论。</span>
        </div>
        <div className="vps-ip-quality-dashboard__context-grid">
          <div className="vps-ip-quality-dashboard__kv-grid">
            <div><span>出口 IP</span><strong>{summary.ip_address} / IPv{summary.ip_version}</strong></div>
            <div><span>ASN</span><strong>{formatOptional(summary.asn)}</strong></div>
            <div><span>组织</span><strong>{formatOptional(summary.organization)}</strong></div>
            <div><span>坐标</span><strong>{report.latest_report?.latitude ?? '—'}, {report.latest_report?.longitude ?? '—'}</strong></div>
            <div><span>使用地</span><strong>{summary.use_region_code || summary.use_region_name || '—'}</strong></div>
            <div><span>注册地</span><strong>{report.latest_report?.registered_region_code || report.latest_report?.registered_region_name || '—'}</strong></div>
          </div>
          <div className="vps-ip-quality-dashboard__coverage">
            <label>
              <span>IP 数据库</span>
              <progress value={providerCoveragePct ?? 0} max={100}>{coverageLabel(providerCoveragePct)}</progress>
              <strong>{coverage ? `${coverage.successful_provider_count} / ${coverage.expected_provider_count}` : `${report.provider_results.length} / ${Math.max(summary.provider_count, report.provider_results.length)}`}</strong>
            </label>
            <label>
              <span>解锁服务</span>
              <progress value={serviceCoveragePct ?? 0} max={100}>{coverageLabel(serviceCoveragePct)}</progress>
              <strong>{coverage ? `${coverage.successful_service_count} / ${coverage.expected_service_count}` : `${report.service_unlocks.length} / ${Math.max(summary.unlockable_count, report.service_unlocks.length)}`}</strong>
            </label>
            <div className="asset-context-inline">
              <span className="asset-context-pill">raw 已脱敏保留</span>
              <span className="asset-context-pill">failure 不进入用户侧 latest</span>
              <span className="asset-context-pill">partial 可展示真实 IP 事实</span>
            </div>
          </div>
        </div>
      </section>

      <section className="page-panel vps-ip-quality-dashboard__history-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">Trend</p>
            <h2 className="section-heading__title">质量变化历史</h2>
          </div>
        </div>
        {report.history.length > 0 ? (
          <div className="vps-ip-quality-dashboard__history">
            {report.history.slice(0, 6).map((item) => (
              <article key={`${item.observed_at}-${item.ip_address}`}>
                <span>{formatDateTime(item.observed_at)}</span>
                <strong>{riskLevelLabel(item.risk_level)}</strong>
                <small>{item.ip_address} · {item.provider_count} provider · {item.unlockable_count} 解锁</small>
                {item.report_id ? <Link to={`?report_id=${encodeURIComponent(item.report_id)}`}>查看详情</Link> : null}
              </article>
            ))}
          </div>
        ) : (
          <p className="asset-table-empty-state">暂无历史变化。</p>
        )}
      </section>

      <section className="page-panel vps-ip-quality-dashboard__diagnostics-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">Diagnostics</p>
            <h2 className="section-heading__title">诊断与异常</h2>
          </div>
          <span className="section-heading__meta">诊断用于排查采集，不把未知当作受阻或高风险。</span>
        </div>
        <div className="vps-ip-quality-dashboard__diagnostics">
          <div>
            <strong>归属方式</strong>
            <span>{summary.assignment_mode || '自动归属'}</span>
            <em>{summary.ambiguous ? '需复核' : '已归属'}</em>
          </div>
          <div>
            <strong>采集状态</strong>
            <span>{report.latest_report?.error_summary || summary.error_summary || '最近一次用户侧报告可用'}</span>
            <em>{report.latest_report?.status || summary.status}</em>
          </div>
          <div>
            <strong>报告标识</strong>
            <span>{report.latest_report?.report_id || '—'}</span>
            <em>{report.latest_report?.is_backfilled ? 'backfilled' : 'live'}</em>
          </div>
          <div>
            <strong>详细诊断</strong>
            <span>{compactJSON(report.latest_report?.diagnostics_json)}</span>
            <em>diagnostics_json</em>
          </div>
        </div>
      </section>
    </div>
  )
}
