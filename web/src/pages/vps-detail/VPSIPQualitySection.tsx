import { IPQualityBadge } from '../assetPageBadges'
import { formatDateTime, formatOptional } from '../../lib/format'
import type { VPSIPQualityReport } from '../../lib/types'

type VPSIPQualitySectionProps = {
  report: VPSIPQualityReport | null
  error: string | null
}

function serviceLabel(value: string): string {
  const normalized = value.toLowerCase()
  if (normalized === 'chatgpt' || normalized === 'openai') return 'ChatGPT'
  if (normalized === 'netflix') return 'Netflix'
  if (normalized === 'youtube-premium') return 'YouTube Premium'
  if (normalized === 'amazon-prime-video') return 'Amazon Prime Video'
  if (normalized === 'disney-plus') return 'Disney+'
  if (normalized === 'tiktok') return 'TikTok'
  if (normalized === 'reddit') return 'Reddit'
  return value
}

function unlockStatusLabel(status: string, region?: string): string {
  const normalized = status.toLowerCase()
  const label = normalized === 'unlocked'
    ? '解锁'
    : normalized === 'blocked'
      ? '受阻'
      : normalized === 'partial'
        ? '部分'
        : status || '未知'
  return region ? `${label} · ${region}` : label
}

function riskFlags(result: VPSIPQualityReport['provider_results'][number]): string[] {
  const flags: string[] = []
  if (result.is_proxy) flags.push('Proxy')
  if (result.is_tor) flags.push('Tor')
  if (result.is_vpn) flags.push('VPN')
  if (result.is_server) flags.push('Server')
  if (result.is_abuser) flags.push('Abuse')
  if (result.is_robot) flags.push('Robot')
  return flags
}

export function VPSIPQualitySection({ report, error }: VPSIPQualitySectionProps) {
  const summary = report?.summary ?? null
  const details = report
  const latest = details?.latest_report ?? null

  return (
    <section className="page-panel vps-ip-quality-card">
      <div className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">IP Quality</p>
          <h2 className="section-heading__title">IP 质量</h2>
        </div>
        <IPQualityBadge summary={summary} />
      </div>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p>
      ) : null}
      {summary && details ? (
        <>
          <div className="vps-cost-card__grid">
            <div>
              <span>出口 IP</span>
              <strong>{summary.ip_address}</strong>
              <small>{summary.assignment_mode || '自动归属'}</small>
            </div>
            <div>
              <span>ASN / 组织</span>
              <strong>{formatOptional(summary.asn)}</strong>
              <small>{formatOptional(summary.organization)}</small>
            </div>
            <div>
              <span>使用地</span>
              <strong>{summary.use_region_code || '—'}</strong>
              <small>{formatOptional(summary.use_region_name)}</small>
            </div>
            <div>
              <span>采集时间</span>
              <strong>{formatDateTime(summary.observed_at)}</strong>
              <small>{summary.provider_count} provider · {summary.unlockable_count} 解锁</small>
            </div>
          </div>
          {latest ? (
            <div className="asset-context-inline vps-cost-card__signals">
              <span className="asset-context-pill">
                注册地 {latest.registered_region_code || latest.registered_region_name || '—'}
              </span>
              <span className="asset-context-pill">
                坐标 {latest.latitude ?? '—'}, {latest.longitude ?? '—'}
              </span>
              <span className="asset-context-pill">
                Agent {latest.agent_version}
              </span>
            </div>
          ) : null}
          <div className="vps-ip-quality-card__matrix">
            <div>
              <h3>Provider 判断</h3>
              {details.provider_results.length > 0 ? (
                <table className="data-table data-table--compact asset-table vps-ip-quality-card__table">
                  <thead>
                    <tr>
                      <th>Provider</th>
                      <th>类型</th>
                      <th>风险</th>
                      <th>信号</th>
                    </tr>
                  </thead>
                  <tbody>
                    {details.provider_results.map((result) => (
                      <tr key={result.provider}>
                        <td>{result.provider}</td>
                        <td>{result.usage_type || result.company_type || '—'}</td>
                        <td>{result.risk_level || result.risk_score || '—'}</td>
                        <td>
                          {riskFlags(result).length > 0 ? (
                            <span className="asset-context-inline">
                              {riskFlags(result).map((flag) => (
                                <span key={flag} className="asset-context-pill asset-context-pill--attention">{flag}</span>
                              ))}
                            </span>
                          ) : result.error_summary || '—'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : (
                <p className="asset-table-empty-state">暂无 provider 结果</p>
              )}
            </div>
            <div>
              <h3>服务解锁</h3>
              {details.service_unlocks.length > 0 ? (
                <div className="asset-context-inline">
                  {details.service_unlocks.map((unlock) => (
                    <span key={unlock.service} className={unlock.status === 'blocked' ? 'asset-context-pill asset-context-pill--attention' : 'asset-context-pill'}>
                      <strong>{serviceLabel(unlock.service)}</strong>
                      <span>{unlockStatusLabel(unlock.status, unlock.region)}</span>
                    </span>
                  ))}
                </div>
              ) : (
                <p className="asset-table-empty-state">暂无服务解锁结果</p>
              )}
            </div>
          </div>
        </>
      ) : (
        <div className="vps-cost-card__empty">
          <strong>尚无 IP 质量报告</strong>
          <span>Agent 完成低频采集后会展示出口 IP 归属、provider 风险信号与服务解锁结果。</span>
        </div>
      )}
    </section>
  )
}
