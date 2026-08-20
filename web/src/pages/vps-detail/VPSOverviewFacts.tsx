import type { VPSOverviewFact } from '../../lib/types'

type Props = {
  facts: VPSOverviewFact[]
}

export function VPSOverviewFacts({ facts }: Props) {
  return (
    <section className="vps-overview-facts" aria-labelledby="vps-overview-facts-title">
      <h2 id="vps-overview-facts-title">稳定事实</h2>
      {facts.length === 0 ? (
        <p className="vps-overview-facts__empty">暂无稳定事实</p>
      ) : (
        <dl className="vps-overview-facts__list">
          {facts.map((fact) => (
            <div key={fact.key} className="vps-overview-facts__row">
              <dt>{fact.label}</dt>
              <dd>{fact.value || '—'}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}
