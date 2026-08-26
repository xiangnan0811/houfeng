import { Link } from 'react-router-dom'

import { Button } from '../../components/atoms'
import type { VPSOverviewAnomaly, VPSOverviewAnomalyAction } from '../../lib/types'
import {
  overviewAnomalyDetailLabel,
  overviewAnomalySeverityClass,
  overviewAnomalySourceLabel,
} from '../../lib/vpsOverviewPresentation'
import {
  resolveVPSOverviewAnomalyDestination,
  type VPSOverviewCommand,
} from './vpsOverviewDestination'

type Props = {
  vpsId: string
  anomalies: VPSOverviewAnomaly[]
  onCommand: (command: VPSOverviewCommand) => void
}

/**
 * Healthy overviews must not mount this section at all — the parent gates on
 * `anomalies.length > 0` so query counts for anomaly chrome stay at zero.
 */
export function VPSOverviewAnomalies({ vpsId, anomalies, onCommand }: Props) {
  if (anomalies.length === 0) return null

  return (
    <section className="vps-overview-anomalies" aria-labelledby="vps-overview-anomalies-title">
      <h2 id="vps-overview-anomalies-title" className="vps-overview-anomalies__title">
        需要关注
      </h2>
      <ul className="vps-overview-anomalies__list">
        {anomalies.map((anomaly) => (
          <li
            key={anomaly.rule_id}
            className={['vps-overview-anomalies__item', overviewAnomalySeverityClass(anomaly.severity)].filter(Boolean).join(' ')}
          >
            <div className="vps-overview-anomalies__body">
              <h3 className="vps-overview-anomalies__item-title">{anomaly.title}</h3>
              {anomaly.detail ? (
                <p className="vps-overview-anomalies__detail">{overviewAnomalyDetailLabel(anomaly.rule_id, anomaly.detail)}</p>
              ) : null}
              <p className="vps-overview-anomalies__source">{overviewAnomalySourceLabel(anomaly.source)}</p>
            </div>
            <div className="vps-overview-anomalies__actions">
              {anomaly.primary_action ? (
                <AnomalyAction
                  vpsId={vpsId}
                  ruleId={anomaly.rule_id}
                  action={anomaly.primary_action}
                  primary
                  onCommand={onCommand}
                />
              ) : null}
              {anomaly.secondary_actions.map((action) => (
                <AnomalyAction
                  key={action.id}
                  vpsId={vpsId}
                  ruleId={anomaly.rule_id}
                  action={action}
                  onCommand={onCommand}
                />
              ))}
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}

function AnomalyAction({
  vpsId,
  ruleId,
  action,
  primary = false,
  onCommand,
}: {
  vpsId: string
  ruleId: string
  action: VPSOverviewAnomalyAction
  primary?: boolean
  onCommand: (command: VPSOverviewCommand) => void
}) {
  const destination = resolveVPSOverviewAnomalyDestination(vpsId, ruleId, action)
  const variant = primary ? 'primary' : 'secondary'

  if (destination?.kind === 'route') {
    return <Link className={`btn sm ${variant}`} to={destination.to}>{action.label}</Link>
  }
  if (destination?.kind === 'command') {
    return (
      <Button size="sm" variant={variant} onClick={() => onCommand(destination.command)}>
        {action.label}
      </Button>
    )
  }
  return <span className={`btn sm ${variant}`} aria-disabled="true">{action.label}</span>
}
