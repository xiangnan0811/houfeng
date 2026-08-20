import type { VPSOverview } from '../../lib/types'

type Props = {
  summary: VPSOverview['summary']
}

const CELLS: Array<{ key: keyof VPSOverview['summary']; label: string }> = [
  { key: 'overall', label: '总体' },
  { key: 'monitoring', label: '监控' },
  { key: 'ip_quality', label: 'IP 质量' },
  { key: 'renewal', label: '续费' },
]

export function VPSOverviewSummaryGrid({ summary }: Props) {
  return (
    <section className="vps-overview-summary" aria-label="决策摘要">
      <div className="vps-overview-summary__grid">
        {CELLS.map(({ key, label }) => {
          const cell = summary[key]
          return (
            <article key={key} className="vps-overview-summary__cell">
              <p className="vps-overview-summary__label">{label}</p>
              <h3 className="vps-overview-summary__status">{cell.status || '—'}</h3>
              {cell.detail ? (
                <p className="vps-overview-summary__detail">{cell.detail}</p>
              ) : null}
            </article>
          )
        })}
      </div>
    </section>
  )
}
