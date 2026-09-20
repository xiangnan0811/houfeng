import type { ReactNode } from 'react'

import './ObservabilityNotice.css'

export type ObservabilityTone = 'critical' | 'alert' | 'notice' | 'maintenance' | 'offline'

type ObservabilityNoticeProps = {
  tone: ObservabilityTone
  mark: string
  title: ReactNode
  detail?: ReactNode
  action?: ReactNode
  role?: 'status' | 'alert'
}

export function ObservabilityNotice({
  tone,
  mark,
  title,
  detail,
  action,
  role = 'status',
}: ObservabilityNoticeProps) {
  return (
    <div
      className={`observability-notice observability-notice--${tone} monitoring-detail-notice monitoring-detail-notice--${tone}`}
      role={role}
    >
      <div className="observability-notice__copy monitoring-detail-notice__copy">
        <span className="observability-notice__mark monitoring-detail-notice__mark">{mark}</span>
        <span className="observability-notice__text monitoring-detail-notice__text">{title}</span>
        {detail ? (
          <span className="observability-notice__detail monitoring-detail-notice__detail">{detail}</span>
        ) : null}
      </div>
      {action ? (
        <div className="observability-notice__actions monitoring-detail-notice__actions">{action}</div>
      ) : null}
    </div>
  )
}
