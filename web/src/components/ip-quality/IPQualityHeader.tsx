import { Link, useLocation } from 'react-router-dom'

import { Badge, Timestamp } from '../atoms'
import type { IPQualityReport, IPQualitySummary } from '../../lib/types'
import { IPQualityCollectButton } from './IPQualityCollectControls'
import type { IPQualityCollectController } from './useIPQualityCollect'

type IPQualityHeaderProps = {
  detailPath: string
  collect: IPQualityCollectController
  summary?: IPQualitySummary | null
  latest?: IPQualityReport | null
  viewingHistory?: boolean
  // 空态由正文承载主操作，页头不再重复按钮。
  showCollect?: boolean
}

function IPQualityMark() {
  return (
    <span className="vps-overview-identity__mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        <circle cx="12" cy="12" r="8.5" />
        <path d="M3.5 12h17M12 3.5c2.4 2.3 3.6 5.1 3.6 8.5s-1.2 6.2-3.6 8.5c-2.4-2.3-3.6-5.1-3.6-8.5s1.2-6.2 3.6-8.5z" />
      </svg>
    </span>
  )
}

function regionText(code?: string, name?: string): string {
  const normalizedCode = (code ?? '').trim()
  const normalizedName = (name ?? '').trim()
  if (normalizedName && normalizedCode && normalizedName !== normalizedCode) return `${normalizedName} (${normalizedCode})`
  return normalizedName || normalizedCode
}

export function IPQualityHeader({
  detailPath,
  collect,
  summary = null,
  latest = null,
  viewingHistory = false,
  showCollect = true,
}: IPQualityHeaderProps) {
  const location = useLocation()
  const useRegion = summary ? regionText(summary.use_region_code, summary.use_region_name) : ''
  const registeredRegion = regionText(latest?.registered_region_code, latest?.registered_region_name)
  const network = [summary?.asn, summary?.organization].map((value) => (value ?? '').trim()).filter(Boolean).join(' ')

  return (
    <header className="page__head vps-overview-identity ipq-header">
      <div className="vps-overview-identity__lead">
        <IPQualityMark />
        <div className="vps-overview-identity__copy">
          <div className="vps-overview-identity__title-row">
            <h1 className="page__title">IP 质量报告</h1>
            {viewingHistory ? <Badge variant="info" className="ipq-header__history">历史报告</Badge> : null}
          </div>
          {summary ? (
            <dl className="vps-overview-identity__meta" aria-label="报告身份">
              <div className="vps-overview-identity__meta-item">
                <dt>出口 IP</dt>
                <dd className="mono">{summary.ip_address} · IPv{summary.ip_version}</dd>
              </div>
              {network ? (
                <div className="vps-overview-identity__meta-item"><dt>网络</dt><dd>{network}</dd></div>
              ) : null}
              {useRegion ? (
                <div className="vps-overview-identity__meta-item"><dt>使用地</dt><dd>{useRegion}</dd></div>
              ) : null}
              {registeredRegion && registeredRegion !== useRegion ? (
                <div className="vps-overview-identity__meta-item"><dt>注册地</dt><dd>{registeredRegion}</dd></div>
              ) : null}
              <div className="vps-overview-identity__meta-item">
                <dt>报告时间</dt>
                <dd><Timestamp value={summary.observed_at} mode="both" /></dd>
              </div>
            </dl>
          ) : null}
        </div>
      </div>
      <div className="page__actions">
        {viewingHistory ? (
          <Link className="btn md secondary" to={location.pathname} state={location.state}>查看最新报告</Link>
        ) : showCollect ? (
          <IPQualityCollectButton collect={collect} />
        ) : null}
        <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>
      </div>
    </header>
  )
}
