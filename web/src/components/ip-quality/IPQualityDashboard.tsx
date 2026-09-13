import { Link, useLocation, useSearchParams } from 'react-router-dom'

import { Badge, Hostname, Timestamp, type BadgeTone } from '../atoms'
import { formatLatency, formatNumber, formatOptional, formatPercent } from '../../lib/format'
import type {
  IPQualityProviderResult,
  IPQualityServiceUnlock,
  IPQualitySummary,
  VPSIPQualityReport,
} from '../../lib/types'
import {
  databaseConsistency,
  deriveQualityScore,
  negativeRiskSignalCount,
  providerCoverage,
  providerEvidenceSignals,
  providerSourceGaps,
  qualityVerdict,
  riskFlags,
  riskLevelLabel,
  riskSignalCounts,
  riskTone,
  serviceCardDescription,
  serviceCoverage,
  serviceLabel,
  serviceUnlockCounts,
  serviceUnlockMeta,
  topQualityReasons,
  unlockStatusKind,
  unlockStatusLabel,
  unlockTone,
  visibleProviderResults,
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

function providerRiskLabel(riskLevel?: string, riskScore?: string): string {
  const normalizedLevel = (riskLevel ?? '').trim()
  if (normalizedLevel) return riskLevelLabel(normalizedLevel)
  return riskScore ? '风险分' : '未评级'
}

function compactJSON(value: unknown): string {
  if (value == null) return '—'
  try {
    return JSON.stringify(value)
  } catch {
    return '无法展示'
  }
}

function providerRowKey(result: IPQualityProviderResult): string {
  return `${result.provider}:${result.source_type ?? ''}:${result.status ?? ''}`
}

function serviceRowKey(unlock: IPQualityServiceUnlock): string {
  return `${unlock.service}:${unlock.source ?? ''}`
}

function reportStatusLabel(status?: string): string {
  const normalized = (status ?? '').trim().toLowerCase()
  if (normalized === 'success') return '采集成功'
  if (normalized === 'partial') return '部分采集'
  if (normalized === 'failure') return '采集失败'
  return status || '未知'
}

function reportStatusTone(status?: string): BadgeTone {
  const normalized = (status ?? '').trim().toLowerCase()
  if (normalized === 'success') return 'normal'
  if (normalized === 'partial') return 'notice'
  if (normalized === 'failure') return 'alert'
  return 'neutral'
}

function assignmentModeLabel(mode?: string): string {
  const normalized = (mode ?? '').trim().toLowerCase()
  if (!normalized) return '自动归属'
  if (normalized === 'link' || normalized === 'monitoring_link') return '监控关联'
  if (normalized === 'ip' || normalized === 'ipv4' || normalized === 'ipv6') return '出口 IP 匹配'
  return '关联事实'
}

export function IPQualityDashboard({ report, summary, detailPath }: IPQualityDashboardProps) {
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const selectedReportId = searchParams.get('report_id')?.trim() || ''
  const viewingHistory = selectedReportId !== ''
  const score = deriveQualityScore(report)
  const reasons = topQualityReasons(report)
  const providerCoveragePct = providerCoverage(report)
  const serviceCoveragePct = serviceCoverage(report)
  const consistency = databaseConsistency(report.provider_results)
  const serviceCounts = serviceUnlockCounts(report.service_unlocks)
  const visibleProviders = visibleProviderResults(report.provider_results)
  const sourceGaps = providerSourceGaps(report.provider_results)
  const coverage = summary.coverage ?? report.latest_report?.coverage
  const negativeSignals = negativeRiskSignalCount(report.provider_results)
  const currentReportId = selectedReportId || summary.report_id || report.latest_report?.report_id || ''

  return (
    <div className="page vps-ip-quality-dashboard">
      <header className="page__head">
        <div>
          <h1 className="page__title">IP 质量报告</h1>
          <p className="page-sub">{viewingHistory ? '历史报告' : '最新报告'}</p>
        </div>
        <div className="page__actions">
          {viewingHistory ? (
            <Link className="btn sm secondary" to={location.pathname} state={location.state}>查看最新报告</Link>
          ) : null}
          <Link className="btn sm secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>
        </div>
      </header>

      <section className="page-panel vps-ip-quality-dashboard__hero">
        <dl className="vps-ip-quality-dashboard__identity" aria-label="报告身份">
          <div>
            <dt>出口 IP</dt>
            <dd><Hostname>{summary.ip_address}</Hostname> / IPv{summary.ip_version}</dd>
          </div>
          <div>
            <dt>报告时间</dt>
            <dd><Timestamp value={summary.observed_at} /></dd>
          </div>
          <div>
            <dt>来源</dt>
            <dd>Agent {report.latest_report?.agent_version || '—'}</dd>
          </div>
          <div>
            <dt>ASN</dt>
            <dd>{formatOptional(summary.asn)}</dd>
          </div>
          <div>
            <dt>组织</dt>
            <dd>{formatOptional(summary.organization)}</dd>
          </div>
          <div>
            <dt>使用地</dt>
            <dd>{summary.use_region_code || summary.use_region_name || '—'}</dd>
          </div>
          <div>
            <dt>注册地</dt>
            <dd>{report.latest_report?.registered_region_code || report.latest_report?.registered_region_name || '—'}</dd>
          </div>
          <div>
            <dt>采集状态</dt>
            <dd><Badge variant="state" tone={reportStatusTone(summary.status)}>{reportStatusLabel(summary.status)}</Badge></dd>
          </div>
          <div>
            <dt>报告标识</dt>
            <dd>{currentReportId || '—'}</dd>
          </div>
        </dl>

        <div className="vps-ip-quality-dashboard__lead">
          <div className="vps-ip-quality-dashboard__score">
            <span>质量分</span>
            <strong>{score ?? '—'}</strong>
            <small>{qualityVerdict(score, summary)}</small>
            <div className="vps-ip-quality-dashboard__score-badges">
              <Badge variant="state" tone={riskTone(summary.risk_level)}>{riskLevelLabel(summary.risk_level)}</Badge>
              {summary.ambiguous ? <Badge variant="state" tone="notice">归属需复核</Badge> : null}
              {summary.stale ? <Badge variant="state" tone="notice">报告过期</Badge> : null}
              {viewingHistory ? <Badge variant="state" tone="neutral">历史报告</Badge> : null}
            </div>
          </div>
          <div className="vps-ip-quality-dashboard__metrics" aria-label="IP 质量摘要指标">
            <div>
              <span>风险信号</span>
              <strong>{negativeSignals} 项</strong>
              <small>proxy / vpn / abuse / 高风险等级</small>
            </div>
            <div>
              <span>解锁可用</span>
              <strong>{serviceCounts.unlocked} / {serviceCounts.unlocked + serviceCounts.partial + serviceCounts.blocked}</strong>
              <small>{serviceCounts.blocked} 受阻 · {serviceCounts.partial} 部分 · {serviceCounts.unknown} 未知</small>
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
          <ul className="vps-ip-quality-dashboard__reasons">
            {reasons.map((reason) => (
              <li key={reason.title} className="vps-ip-quality-dashboard__reason">
                <strong>{reason.title}</strong>
                <span>{reason.detail}</span>
                <em>{reason.impact}</em>
              </li>
            ))}
          </ul>
        </div>
      </section>

      <section className="page-panel vps-ip-quality-dashboard__signal-panel">
        <div className="section-heading section-heading--inline">
          <h2 className="section-heading__title">风险信号矩阵</h2>
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

      <section className="page-panel vps-ip-quality-dashboard__provider-panel">
        <div className="section-heading section-heading--inline">
          <h2 id="ip-quality-provider-table-title" className="section-heading__title">各 IP 数据库判断</h2>
        </div>
        {visibleProviders.length > 0 ? (
          <>
            <p id="ip-quality-provider-table-hint" className="vps-ip-quality-dashboard__table-hint">横向滚动查看完整列</p>
            <div
              className="vps-ip-quality-dashboard__table-scroll"
              role="region"
              aria-labelledby="ip-quality-provider-table-title"
              aria-describedby="ip-quality-provider-table-hint"
              tabIndex={0}
            >
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
                  </tr>
                </thead>
                <tbody>
                  {visibleProviders.map((result) => {
                    const flags = riskFlags(result)
                    const evidenceSignals = providerEvidenceSignals(result)
                    return (
                      <tr key={providerRowKey(result)} className="data-table__row">
                        <td className="data-table__cell"><strong>{result.provider}</strong></td>
                        <td className="data-table__cell"><Badge variant="state" tone={sourceStatusTone(result.status)}>{sourceStatusLabel(result.status)}</Badge></td>
                        <td className="data-table__cell">{formatOptional(result.usage_type)}</td>
                        <td className="data-table__cell">{formatOptional(result.company_type)}</td>
                        <td className="data-table__cell">
                          <span className="vps-ip-quality-dashboard__risk-cell">
                            <Badge variant="state" tone={riskTone(result.risk_level)}>{providerRiskLabel(result.risk_level, result.risk_score)}</Badge>
                            {result.risk_score ? <span className="vps-ip-quality-dashboard__risk-score">{result.risk_score}</span> : null}
                          </span>
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
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <p className="asset-table-empty-state">暂无 provider 结果。</p>
        )}
        {sourceGaps.length > 0 ? (
          <div className="vps-ip-quality-dashboard__source-gaps" aria-label="未配置 IP 数据库来源">
            <span>未配置来源：</span>
            {sourceGaps.map((gap) => (
              <Badge key={gap.provider} variant="info" tone={gap.tone}>{gap.label}</Badge>
            ))}
          </div>
        ) : null}
      </section>

      <section className="page-panel vps-ip-quality-dashboard__service-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <h2 className="section-heading__title">服务检测</h2>
          </div>
          <div className="vps-ip-quality-dashboard__service-stats" aria-label="服务解锁状态统计">
            <Badge variant="state" tone="normal">{serviceCounts.unlocked} 可用</Badge>
            <Badge variant="state" tone="alert">{serviceCounts.blocked} 受阻</Badge>
            <Badge variant="state" tone="notice">{serviceCounts.partial} 部分</Badge>
            <Badge variant="state" tone="neutral">{serviceCounts.unknown} 未知</Badge>
          </div>
        </div>
        {report.service_unlocks.length > 0 ? (
          <div className="vps-ip-quality-dashboard__service-grid">
            {report.service_unlocks.map((unlock) => (
              <article key={serviceRowKey(unlock)} className={`vps-ip-quality-dashboard__service ${statusToneClass(unlock.status)}`}>
                <header>
                  <h3>{serviceLabel(unlock.service)}</h3>
                  <Badge variant="state" tone={unlockTone(unlock.status)}>{unlockStatusLabel(unlock.status, unlock.region)}</Badge>
                </header>
                <p>{serviceCardDescription(unlock)}</p>
                <small>{serviceUnlockMeta(unlock)}</small>
              </article>
            ))}
          </div>
        ) : (
          <p className="asset-table-empty-state">暂无服务解锁结果。</p>
        )}
      </section>

      <section className="page-panel vps-ip-quality-dashboard__history-panel">
        <div className="section-heading section-heading--inline">
          <h2 className="section-heading__title">质量变化历史</h2>
        </div>
        {report.history.length > 0 ? (
          <div className="vps-ip-quality-dashboard__history">
            {report.history.map((item, index) => {
              const isCurrent = Boolean(item.report_id) && item.report_id === currentReportId
              return (
                <article key={item.report_id || `${item.observed_at}-${item.ip_address}-${index}`} aria-current={isCurrent ? 'page' : undefined}>
                  <span><Timestamp value={item.observed_at} /></span>
                  <strong>{riskLevelLabel(item.risk_level)}</strong>
                  <small>{item.ip_address} · {item.provider_count} provider · {item.unlockable_count} 解锁</small>
                  {item.report_id ? (
                    <Link to={`?report_id=${encodeURIComponent(item.report_id)}`} state={location.state}>查看详情</Link>
                  ) : null}
                </article>
              )
            })}
          </div>
        ) : (
          <p className="asset-table-empty-state">暂无历史变化。</p>
        )}
      </section>

      <section className="page-panel vps-ip-quality-dashboard__diagnostics-panel" aria-label="采集诊断">
        <details className="vps-ip-quality-dashboard__diagnostics-fold">
          <summary>
            <h2 className="section-heading__title">诊断与异常</h2>
          </summary>
          <p className="vps-ip-quality-dashboard__table-hint">采集源、探测、时延、错误摘要和原始 JSON 仅用于排障，不改变上方结论。</p>
          <div className="vps-ip-quality-dashboard__diagnostics">
            <div>
              <strong>归属方式</strong>
              <span>{assignmentModeLabel(summary.assignment_mode)}</span>
              <em>{summary.assignment_mode || (summary.ambiguous ? '需复核' : '已归属')}</em>
            </div>
            <div>
              <strong>采集状态</strong>
              <span>{report.latest_report?.error_summary || summary.error_summary || reportStatusLabel(report.latest_report?.status || summary.status)}</span>
              <em>{report.latest_report?.status || summary.status}</em>
            </div>
            <div>
              <strong>报告标识</strong>
              <span>{report.latest_report?.report_id || '—'}</span>
              <em>{report.latest_report?.is_backfilled ? 'backfilled' : 'live'}</em>
            </div>
            <div>
              <strong>坐标</strong>
              <span>{report.latest_report?.latitude ?? '—'}, {report.latest_report?.longitude ?? '—'}</span>
              <em>latitude / longitude</em>
            </div>
            <div>
              <strong>详细诊断</strong>
              <span>{compactJSON(report.latest_report?.diagnostics_json)}</span>
              <em>diagnostics_json</em>
            </div>
            {report.latest_report?.raw_json != null ? (
              <div>
                <strong>原始报告</strong>
                <span>{compactJSON(report.latest_report.raw_json)}</span>
                <em>raw_json</em>
              </div>
            ) : null}
          </div>
          {report.provider_results.length > 0 ? (
            <div className="vps-ip-quality-dashboard__diagnostic-list">
              {report.provider_results.map((result) => (
                <article key={`diag-provider-${providerRowKey(result)}`}>
                  <strong>{result.provider}</strong>
                  <span>
                    {result.source_type || 'default'}
                    {result.latency_ms != null ? ` · ${formatLatency(result.latency_ms)}` : ''}
                    {result.error_code ? ` · ${result.error_code}` : ''}
                    {result.error_summary ? ` · ${result.error_summary}` : ''}
                  </span>
                  {result.extra_json != null ? <code>{compactJSON(result.extra_json)}</code> : null}
                </article>
              ))}
            </div>
          ) : null}
          {report.service_unlocks.length > 0 ? (
            <div className="vps-ip-quality-dashboard__diagnostic-list">
              {report.service_unlocks.map((unlock) => (
                <article key={`diag-service-${serviceRowKey(unlock)}`}>
                  <strong>{serviceLabel(unlock.service)}</strong>
                  <span>
                    {unlock.source || '—'}
                    {unlock.probe_status ? ` · ${unlock.probe_status}` : ''}
                    {unlock.latency_ms != null ? ` · ${formatLatency(unlock.latency_ms)}` : ''}
                    {unlock.error_code ? ` · ${unlock.error_code}` : ''}
                    {unlock.error_summary ? ` · ${unlock.error_summary}` : ''}
                  </span>
                  {unlock.extra_json != null ? <code>{compactJSON(unlock.extra_json)}</code> : null}
                </article>
              ))}
            </div>
          ) : null}
        </details>
      </section>
    </div>
  )
}
