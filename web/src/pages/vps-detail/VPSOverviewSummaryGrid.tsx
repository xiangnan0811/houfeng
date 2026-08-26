import type { VPSOverview } from '../../lib/types'
import { overviewSummaryCellLabel, overviewSummaryDetailLabel } from '../../lib/vpsOverviewPresentation'
import { VPSOverviewFreshness } from './VPSOverviewFreshness'

type Props = {
  summary: VPSOverview['summary']
  onRefresh: () => void
  retrying: boolean
}

const CELLS: Array<{ key: keyof VPSOverview['summary']; label: string; retryable: boolean }> = [
  { key: 'overall', label: '总体', retryable: false },
  { key: 'monitoring', label: '监控', retryable: true },
  { key: 'ip_quality', label: 'IP 质量', retryable: true },
  { key: 'renewal', label: '续费', retryable: true },
]

export function VPSOverviewSummaryGrid({ summary, onRefresh, retrying }: Props) {
  return (
    <section className="vps-overview-summary" aria-label="决策摘要">
      <h2 className="visually-hidden">决策摘要</h2>
      <div className="vps-overview-summary__grid">
        {CELLS.map(({ key, label, retryable }) => {
          const cell = summary[key]
          return (
            <article key={key} className="vps-overview-summary__cell">
              <p className="vps-overview-summary__label">{label}</p>
              <h3 className="vps-overview-summary__status">{overviewSummaryCellLabel(key, cell.status) || '—'}</h3>
              {cell.detail ? (
                <p className="vps-overview-summary__detail">{overviewSummaryDetailLabel(key, cell.detail)}</p>
              ) : null}
              <VPSOverviewFreshness
                section={cell.section}
                sourceLabel={label}
                {...(retryable ? { onRetry: onRefresh } : {})}
                retrying={retrying}
              />
            </article>
          )
        })}
      </div>
    </section>
  )
}
