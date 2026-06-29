import { Link } from 'react-router-dom'

import { Button } from '../../components/atoms'
import type { VPSContextAction, VPSOverviewAction } from './vpsDetailOverviewModel'
import type { VPSDetailModalMode } from './types'

type VPSContextActionPanelProps = {
  action: VPSContextAction
  onOpenModal: (mode: NonNullable<VPSDetailModalMode>) => void
  onMonitoringAgent: () => void
}

function actionKey(action: VPSOverviewAction): string {
  return `${action.kind}:${action.mode ?? action.to ?? action.label}`
}

export function VPSContextActionPanel({ action, onOpenModal, onMonitoringAgent }: VPSContextActionPanelProps) {
  function renderAction(item: VPSOverviewAction, primary = false) {
    if (item.kind === 'link' && item.to) {
      return (
        <Link key={actionKey(item)} className={['btn', 'sm', primary ? 'primary' : 'secondary'].join(' ')} to={item.to}>
          {item.label}
        </Link>
      )
    }
    return (
      <Button
        key={actionKey(item)}
        variant={primary ? 'primary' : 'secondary'}
        size="sm"
        onClick={() => {
          if (item.mode === 'monitoring-instance-create') {
            onMonitoringAgent()
            return
          }
          if (item.mode) onOpenModal(item.mode)
        }}
      >
        {item.label}
      </Button>
    )
  }

  return (
    <section className={['page-panel', 'vps-detail-context-action', `vps-detail-context-action--${action.tone}`].join(' ')} aria-label="需要处理的状态">
      <div>
        <h2>{action.title}</h2>
        <p>{action.reason}</p>
      </div>
      <div className="vps-detail-context-action__actions">
        {renderAction(action.primaryAction, true)}
        {action.secondaryActions.map((item) => renderAction(item))}
      </div>
    </section>
  )
}
