import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Badge, Button, Timestamp } from '../atoms'
import { VPSCopyValueButton } from '../../pages/vps-detail/VPSCopyValueButton'
import type { IPQualityIPVersion, IPQualitySummary } from '../../lib/types'
import { IPQualityCollectButton } from './IPQualityCollectControls'
import type { IPQualityCollectController } from './useIPQualityCollect'

export type IPQualityEnrichmentView = {
  // pending/running：Center 资料仍在补充；只显示单一短状态。
  active: boolean
  // 页面可见轮询失败：保留已显示事实，给一次重试入口。
  failed: boolean
  retry: () => void
}

export type IPQualityHeaderMark = { label: string, tone: 'notice' | 'neutral', title?: string }

type IPQualityHeaderProps = {
  detailPath: string
  collect: IPQualityCollectController
  summary?: IPQualitySummary | null
  viewingHistory?: boolean
  // 历史视图“查看最新报告”回到该历史报告所属族的最新报告；未知时交给服务端默认。
  latestIPVersion?: IPQualityIPVersion | null
  // 地址族选择器，位于身份下方；页头、空态与参数错误态都保留。
  familyControl?: ReactNode
  // 空态由正文承载主操作，页头不再重复按钮。
  showCollect?: boolean
  // 过期、旧版、归属复核等短标记；不写解释段落。
  marks?: IPQualityHeaderMark[]
  enrichment?: IPQualityEnrichmentView | null
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

export const IP_QUALITY_HISTORY_ANCHOR = 'ip-quality-history-title'

export function IPQualityHeader({
  detailPath,
  collect,
  summary = null,
  viewingHistory = false,
  latestIPVersion = null,
  familyControl = null,
  showCollect = true,
  marks = [],
  enrichment = null,
}: IPQualityHeaderProps) {
  const location = useLocation()

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
            <dl className="vps-overview-identity__meta ipq-header__meta" aria-label="报告身份">
              <div className="vps-overview-identity__meta-item ipq-header__ip">
                <dt>出口 IP</dt>
                <dd>
                  <span className="mono ipq-header__address">{summary.ip_address}</span>
                  <VPSCopyValueButton value={summary.ip_address} label="出口 IP" />
                </dd>
              </div>
              <div className="vps-overview-identity__meta-item">
                <dt>地址族</dt>
                <dd className="mono">IPv{summary.ip_version}</dd>
              </div>
              <div className="vps-overview-identity__meta-item">
                <dt>采集时间</dt>
                <dd><Timestamp value={summary.observed_at} mode="both" /></dd>
              </div>
            </dl>
          ) : null}
          {marks.length > 0 || enrichment?.active || enrichment?.failed ? (
            <div className="ipq-header__marks">
              {marks.map((mark) => (
                <Badge key={mark.label} variant="info" tone={mark.tone}>
                  <span title={mark.title}>{mark.label}</span>
                </Badge>
              ))}
              {enrichment?.failed ? (
                <span className="ipq-header__enrichment" role="status" aria-label="补充资料状态">
                  <span>资料刷新失败</span>
                  <Button size="sm" variant="ghost" onClick={enrichment.retry}>重试</Button>
                </span>
              ) : enrichment?.active ? (
                <span className="ipq-header__enrichment" role="status" aria-label="补充资料状态">
                  <span className="ipq-pulse" aria-hidden="true" />
                  查询中
                </span>
              ) : null}
            </div>
          ) : null}
          {familyControl ? <div className="ipq-header__family">{familyControl}</div> : null}
        </div>
      </div>
      <div className="page__actions">
        {viewingHistory ? (
          <Link
            className="btn md secondary"
            to={latestIPVersion == null ? location.pathname : `${location.pathname}?ip_version=${latestIPVersion}`}
            state={location.state}
          >
            查看最新报告
          </Link>
        ) : showCollect ? (
          <IPQualityCollectButton collect={collect} />
        ) : null}
        {summary ? <a className="btn md ghost" href={`#${IP_QUALITY_HISTORY_ANCHOR}`}>历史</a> : null}
        <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>
      </div>
    </header>
  )
}
