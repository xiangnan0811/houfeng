import { Link } from 'react-router-dom'

import type { VPSOverviewAnomaly } from '../../lib/types'

type Props = {
  anomalies: VPSOverviewAnomaly[]
}

/**
 * Healthy overviews must not mount this section at all — the parent gates on
 * `anomalies.length > 0` so query counts for anomaly chrome stay at zero.
 */
export function VPSOverviewAnomalies({ anomalies }: Props) {
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
            className={`vps-overview-anomalies__item vps-overview-anomalies__item--${anomaly.severity}`}
          >
            <div className="vps-overview-anomalies__body">
              <h3 className="vps-overview-anomalies__item-title">{anomaly.title}</h3>
              {anomaly.detail ? (
                <p className="vps-overview-anomalies__detail">{anomaly.detail}</p>
              ) : null}
              <p className="vps-overview-anomalies__source">{anomaly.source}</p>
            </div>
            <div className="vps-overview-anomalies__actions">
              {anomaly.primary_action ? (
                anomaly.primary_action.route ? (
                  <Link className="btn sm primary" to={anomaly.primary_action.route}>
                    {anomaly.primary_action.label}
                  </Link>
                ) : (
                  <span className="btn sm primary" aria-disabled="true">
                    {anomaly.primary_action.label}
                  </span>
                )
              ) : null}
              {anomaly.secondary_actions.map((action) => (
                action.route ? (
                  <Link key={action.id} className="btn sm secondary" to={action.route}>
                    {action.label}
                  </Link>
                ) : (
                  <span key={action.id} className="btn sm secondary" aria-disabled="true">
                    {action.label}
                  </span>
                )
              ))}
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}
