import { useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Badge, Button, Timestamp } from '../atoms'
import { formatDateTime } from '../../lib/format'
import type { IPQualitySummary } from '../../lib/types'
import { riskLevelLabel, riskTone } from './ipQualityPresentation'

type IPQualityHistoryListProps = {
  history: IPQualitySummary[]
  currentReportId: string
}

const TITLE_ID = 'ip-quality-history-title'
const COLLAPSED_LIMIT = 5

export function IPQualityHistoryList({ history, currentReportId }: IPQualityHistoryListProps) {
  const location = useLocation()
  const [expanded, setExpanded] = useState(false)
  // 当前查看的报告不在前几条时直接展开，避免它被折叠。
  const currentIndex = history.findIndex((item) => Boolean(item.report_id) && item.report_id === currentReportId)
  const showAll = expanded || currentIndex >= COLLAPSED_LIMIT
  const visible = showAll ? history : history.slice(0, COLLAPSED_LIMIT)
  const hiddenCount = history.length - visible.length

  return (
    <section className="vps-detail-workspace__section ipq-history" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>历史报告</h2>
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
                <Badge variant="info" tone={riskTone(item.risk_level)}>{riskLevelLabel(item.risk_level)}</Badge>
                <span className="ipq-history__ip mono">{item.ip_address}</span>
                <span className="ipq-history__action">
                  {isCurrent ? (
                    <span className="ipq-muted">当前查看</span>
                  ) : item.report_id ? (
                    <Link
                      className="text-link"
                      to={`?report_id=${encodeURIComponent(item.report_id)}`}
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
