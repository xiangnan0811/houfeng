import type { TargetRecord } from '../../lib/types'

type TargetStatusSummaryProps = {
  target: TargetRecord
  probeItemCount: number
}

export function TargetStatusSummary({ target, probeItemCount }: TargetStatusSummaryProps) {
  return (
    <div className="summary-grid">
      <article className="summary-card">
        <p className="summary-card__label">健康状态</p>
        <p className="summary-card__value">{target.current_health_status}</p>
      </article>
      <article className="summary-card">
        <p className="summary-card__label">ProbeItem 数量</p>
        <p className="summary-card__value">{probeItemCount}</p>
      </article>
      <article className="summary-card">
        <p className="summary-card__label">当前主问题</p>
        <p className="summary-card__value summary-card__value--text">
          {target.current_primary_issue_summary || '暂无明显异常'}
        </p>
      </article>
    </div>
  )
}
