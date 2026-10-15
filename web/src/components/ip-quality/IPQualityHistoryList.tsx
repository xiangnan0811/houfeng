import { useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Badge, Button, Timestamp } from '../atoms'
import { formatDateTime } from '../../lib/format'
import type { IPQualitySummary } from '../../lib/types'
import { IP_QUALITY_HISTORY_ANCHOR } from './IPQualityHeader'
import { riskLevelLabel, riskTone } from './ipQualityPresentation'
import { productRiskFacts } from './ipQualityProductPresentation'

type IPQualityHistoryListProps = {
  history: IPQualitySummary[]
  currentReportId: string
  // 所选族；历史只属于该族，链接同时携带族与 report_id（detail API 只用 report_id）。
  ipVersion: 4 | 6 | null
}

function historyLink(item: IPQualitySummary, fallback: 4 | 6 | null, reportId: string): string {
  const family = item.ip_version === 4 || item.ip_version === 6 ? item.ip_version : fallback
  const query = new URLSearchParams()
  if (family != null) query.set('ip_version', String(family))
  query.set('report_id', reportId)
  return `?${query.toString()}`
}

const COLLAPSED_LIMIT = 5

// 每条历史的事实摘要：revision1 用服务端事实（风险命中、服务可用数）；旧报告只显示标注的旧版评级。
function HistoryFacts({ item }: { item: IPQualitySummary }) {
  const product = item.product_revision === 1 ? item.product_summary : null
  if (!product) {
    return <Badge variant="info" tone={riskTone(item.risk_level)}>旧版评级 · {riskLevelLabel(item.risk_level)}</Badge>
  }
  const facts = productRiskFacts(product)
  return (
    <span className="ipq-history__facts">
      {facts.length > 0
        ? facts.map((fact) => <Badge key={fact} variant="info" tone="alert">{fact}</Badge>)
        : <span className="ipq-muted">未见风险命中</span>}
      <span className="ipq-muted">服务 {product.service_available_count} 可用</span>
    </span>
  )
}

export function IPQualityHistoryList({ history, currentReportId, ipVersion }: IPQualityHistoryListProps) {
  const location = useLocation()
  const [expanded, setExpanded] = useState(false)
  // 当前查看的报告不在前几条时直接展开，避免它被折叠。
  const currentIndex = history.findIndex((item) => Boolean(item.report_id) && item.report_id === currentReportId)
  const showAll = expanded || currentIndex >= COLLAPSED_LIMIT
  const visible = showAll ? history : history.slice(0, COLLAPSED_LIMIT)
  const hiddenCount = history.length - visible.length

  return (
    <section className="vps-detail-workspace__section ipq-history" aria-labelledby={IP_QUALITY_HISTORY_ANCHOR}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={IP_QUALITY_HISTORY_ANCHOR}>历史报告</h2>
        {history.length > 0 ? <span className="ipq-muted">共 {history.length} 份</span> : null}
      </div>
      {history.length > 0 ? (
        <ol className="ipq-history__list">
          {visible.map((item, index) => {
            const isCurrent = Boolean(item.report_id) && item.report_id === currentReportId
            return (
              <li
                key={item.report_id || `${item.observed_at}-${item.ip_address}-${index}`}
                className={isCurrent ? 'ipq-history__item ipq-history__item--current' : 'ipq-history__item'}
                aria-current={isCurrent ? 'page' : undefined}
              >
                <Timestamp value={item.observed_at} className="ipq-history__time" />
                <span className="ipq-history__ip mono">{item.ip_address}</span>
                <HistoryFacts item={item} />
                <span className="ipq-history__action">
                  {isCurrent ? (
                    <span className="ipq-muted">当前查看</span>
                  ) : item.report_id ? (
                    <Link
                      className="text-link"
                      to={historyLink(item, ipVersion, item.report_id)}
                      state={location.state}
                      aria-label={`查看 ${formatDateTime(item.observed_at)} 的报告`}
                    >
                      查看
                    </Link>
                  ) : null}
                </span>
              </li>
            )
          })}
        </ol>
      ) : (
        <p className="ipq-empty">暂无历史报告。</p>
      )}
      {hiddenCount > 0 || (expanded && currentIndex < COLLAPSED_LIMIT) ? (
        <div className="ipq-history__more">
          <Button size="sm" variant="ghost" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
            {expanded ? '收起' : `显示全部 ${history.length} 份`}
          </Button>
        </div>
      ) : null}
    </section>
  )
}
