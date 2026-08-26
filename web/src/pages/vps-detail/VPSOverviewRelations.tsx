import { Link } from 'react-router-dom'

import { Button } from '../../components/atoms'
import type { VPSOverviewRelation } from '../../lib/types'
import { overviewRelationStatusLabel } from '../../lib/vpsOverviewPresentation'
import { VPSOverviewFreshness } from './VPSOverviewFreshness'
import {
  resolveVPSOverviewRelationDestination,
  type VPSOverviewCommand,
} from './vpsOverviewDestination'

type Props = {
  vpsId: string
  relations: VPSOverviewRelation[]
  onCommand: (command: VPSOverviewCommand) => void
  onRefresh: () => void
  retrying: boolean
}

export function VPSOverviewRelations({ vpsId, relations, onCommand, onRefresh, retrying }: Props) {
  return (
    <section className="vps-overview-relations" aria-labelledby="vps-overview-relations-title">
      <h2 id="vps-overview-relations-title">关联资源</h2>
      {relations.length === 0 ? (
        <p className="vps-overview-relations__empty">暂无关联资源</p>
      ) : (
        <ul className="vps-overview-relations__list">
          {relations.map((relation, index) => {
            const destination = resolveVPSOverviewRelationDestination(vpsId, relation)
            const content = <RelationContent relation={relation} />
            return (
              <li key={`${relation.kind}:${index}`} className="vps-overview-relations__item">
                {destination?.kind === 'route' ? (
                  <Link className="vps-overview-relations__link" to={destination.to}>{content}</Link>
                ) : destination?.kind === 'command' ? (
                  <Button
                    variant="ghost"
                    className="vps-overview-relations__link"
                    onClick={() => onCommand(destination.command)}
                  >
                    {content}
                  </Button>
                ) : (
                  <div className="vps-overview-relations__link">{content}</div>
                )}
                <VPSOverviewFreshness
                  section={relation.section}
                  sourceLabel={relation.label}
                  onRetry={onRefresh}
                  retrying={retrying}
                />
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

function RelationContent({ relation }: { relation: VPSOverviewRelation }) {
  return (
    <>
      <span className="vps-overview-relations__label">{relation.label}</span>
      <span className="vps-overview-relations__count">
        {relation.section.state === 'unavailable' ? '—' : relation.count}
      </span>
      {relation.status ? (
        <span className="vps-overview-relations__status">{overviewRelationStatusLabel(relation.status)}</span>
      ) : null}
    </>
  )
}
