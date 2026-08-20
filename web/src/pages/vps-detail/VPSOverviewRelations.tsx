import { Link } from 'react-router-dom'

import type { VPSOverviewRelation } from '../../lib/types'

type Props = {
  relations: VPSOverviewRelation[]
}

export function VPSOverviewRelations({ relations }: Props) {
  return (
    <section className="vps-overview-relations" aria-labelledby="vps-overview-relations-title">
      <h2 id="vps-overview-relations-title">关联资源</h2>
      {relations.length === 0 ? (
        <p className="vps-overview-relations__empty">暂无关联资源</p>
      ) : (
        <ul className="vps-overview-relations__list">
          {relations.map((relation) => (
            <li key={relation.kind}>
              <Link className="vps-overview-relations__link" to={relation.route}>
                <span className="vps-overview-relations__label">{relation.label}</span>
                <span className="vps-overview-relations__count">{relation.count}</span>
                {relation.status ? (
                  <span className="vps-overview-relations__status">{relation.status}</span>
                ) : null}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
