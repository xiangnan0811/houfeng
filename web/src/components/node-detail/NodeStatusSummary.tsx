import { MonoDigits } from '../atoms'
import type { NodeRecord } from '../../lib/types'

type NodeStatusSummaryProps = {
  node: NodeRecord
}

export function NodeStatusSummary({ node }: NodeStatusSummaryProps) {
  return (
    <div className="summary-grid">
      <article className="summary-card">
        <p className="summary-card__label">健康状态</p>
        <p className="summary-card__value">{node.current_health_status}</p>
      </article>
      <article className="summary-card">
        <p className="summary-card__label">活跃异常数</p>
        <p className="summary-card__value">
          <MonoDigits>{node.current_active_incident_count}</MonoDigits>
        </p>
      </article>
      <article className="summary-card">
        <p className="summary-card__label">当前主问题</p>
        <p className="summary-card__value summary-card__value--text">
          {node.current_primary_issue_summary || '暂无明显异常'}
        </p>
      </article>
    </div>
  )
}
