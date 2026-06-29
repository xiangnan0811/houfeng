import { Link } from 'react-router-dom'

import { Button } from '../../components/atoms'
import type { VPSOverviewAction, VPSRelatedOverviewItem } from './vpsDetailOverviewModel'
import type { VPSDetailModalMode } from './types'

type VPSRelatedOverviewProps = {
  items: VPSRelatedOverviewItem[]
  onOpenModal: (mode: NonNullable<VPSDetailModalMode>) => void
  onMonitoringAgent: () => void
}

function actionKey(action: VPSOverviewAction): string {
  return `${action.kind}:${action.mode ?? action.to ?? action.label}`
}

export function VPSRelatedOverview({ items, onOpenModal, onMonitoringAgent }: VPSRelatedOverviewProps) {
  function renderTitle(item: VPSRelatedOverviewItem) {
    const titleAction = item.titleAction
    if (titleAction.kind === 'link') {
      return (
        <Link className="vps-related-overview__title" to={titleAction.to}>
          {item.title}
        </Link>
      )
    }
    return (
      <button className="vps-related-overview__title" type="button" onClick={() => onOpenModal(titleAction.mode)}>
        {item.title}
      </button>
    )
  }

  function handleQuickAction(action: VPSOverviewAction) {
    if (action.mode === 'monitoring-instance-create') {
      onMonitoringAgent()
      return
    }
    if (action.mode) {
      onOpenModal(action.mode)
    }
  }

  return (
    <section className="page-panel vps-related-overview" aria-labelledby="vps-related-overview-title">
      <div className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">Related</p>
          <h2 id="vps-related-overview-title" className="section-heading__title">关联概览</h2>
        </div>
      </div>
      <ul className="vps-related-overview__list">
        {items.map((item) => (
          <li key={item.key} className={['vps-related-overview__item', `vps-related-overview__item--${item.tone}`].join(' ')}>
            <div className="vps-related-overview__main">
              {renderTitle(item)}
              <strong>{item.primary}</strong>
              {item.secondary ? <small>{item.secondary}</small> : null}
            </div>
            {item.quickActions.length > 0 ? (
              <div className="vps-related-overview__actions">
                {item.quickActions.map((action) => (
                  action.to ? (
                    <Link key={actionKey(action)} className="btn sm ghost" to={action.to}>
                      {action.label}
                    </Link>
                  ) : (
                    <Button
                      key={actionKey(action)}
                      variant="ghost"
                      size="sm"
                      onClick={() => handleQuickAction(action)}
                    >
                      {action.label}
                    </Button>
                  )
                ))}
              </div>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  )
}
