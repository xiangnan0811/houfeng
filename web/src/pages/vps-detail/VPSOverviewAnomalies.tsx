import { Link, useLocation } from 'react-router-dom'

import { Button } from '../../components/atoms'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import type { VPSOverviewAnomaly, VPSOverviewAnomalyAction } from '../../lib/types'
import {
  overviewAnomalyDetailLabel,
  overviewAnomalySeverityClass,
  overviewAnomalySourceLabel,
  overviewUnlinkedAnomalyCopy,
} from '../../lib/vpsOverviewPresentation'
import {
  isVPSOverviewWriteCommand,
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
  const soleUnlinked = anomalies.length === 1 && anomalies[0]?.rule_id === 'monitoring.unlinked.v1'
  const soleTitle = anomalies[0]?.title ?? '需要关注'

  return (
    <section
      className="vps-overview-anomalies"
      {...(soleUnlinked
        ? { 'aria-label': soleTitle }
        : { 'aria-labelledby': 'vps-overview-anomalies-title' })}
    >
      {soleUnlinked ? null : (
        <h2 id="vps-overview-anomalies-title" className="vps-overview-anomalies__title">
          需要关注
        </h2>
      )}
      <ul className="vps-overview-anomalies__list">
        {anomalies.map((anomaly) => {
          const unlinked = anomaly.rule_id === 'monitoring.unlinked.v1'
          const unlinkedCopy = unlinked ? overviewUnlinkedAnomalyCopy(anomaly) : null
          const sourceLabel = overviewAnomalySourceLabel(anomaly.source)
          const showSource = !unlinked && Boolean(sourceLabel) && !anomaly.title.includes(sourceLabel)
          const detail = !unlinked && anomaly.detail
            ? overviewAnomalyDetailLabel(anomaly.rule_id, anomaly.detail)
            : null
          const showFallbackReason = Boolean(unlinkedCopy) && !(soleUnlinked && unlinkedCopy?.impact)
          return (
          <li
            key={anomaly.rule_id}
            className={['vps-overview-anomalies__item', overviewAnomalySeverityClass(anomaly.severity)].filter(Boolean).join(' ')}
          >
            <div className="vps-overview-anomalies__body">
              <h3 className="vps-overview-anomalies__item-title">{anomaly.title}</h3>
              {unlinkedCopy ? (
                <>
                  {showFallbackReason ? <p className="vps-overview-anomalies__detail">{unlinkedCopy.reason}</p> : null}
                  {unlinkedCopy.impact ? <p className="vps-overview-anomalies__detail">{unlinkedCopy.impact}</p> : null}
                </>
              ) : detail ? (
                <p className="vps-overview-anomalies__detail">{detail}</p>
              ) : null}
              {showSource ? <p className="vps-overview-anomalies__source">{sourceLabel}</p> : null}
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
          )
        })}
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
  const location = useLocation()
  const destination = resolveVPSOverviewAnomalyDestination(vpsId, ruleId, action)
  const variant = primary ? 'primary' : 'secondary'

  if (destination?.kind === 'route') {
    return (
      <Link
        className={`btn sm ${variant}`}
        to={destination.to}
        {...(destination.to.startsWith('/vps/') ? { state: location.state } : {})}
      >
        {action.label}
      </Link>
    )
  }

  if (destination?.kind === 'command') {
    if (READ_ONLY_PREVIEW && isVPSOverviewWriteCommand(destination.command)) {
      const explanation = ruleId === 'monitoring.unlinked.v1'
        ? '只读预览不能接入监控实例。'
        : '只读预览不可写入。'
      return (
        <>
          <Button size="sm" variant={variant} disabled aria-disabled="true">
            {action.label}
          </Button>
          <p className="vps-overview-anomalies__detail">{explanation}</p>
        </>
      )
    }
    return (
      <Button size="sm" variant={variant} onClick={() => onCommand(destination.command)}>
        {action.label}
      </Button>
    )
  }

  return <span className={`btn sm ${variant}`} aria-disabled="true">{action.label}</span>
}
