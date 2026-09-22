import type { ReactNode } from 'react'

import '../../pages/monitoring-detail/MonitoringDetailWorkspace.css'

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
      className={`observability-notice observability-notice--${tone}`}
      role={role}
    >
      <div className="observability-notice__copy">
        <span className="observability-notice__mark">{mark}</span>
        <span className="observability-notice__text">{title}</span>
        {detail ? (
          <span className="observability-notice__detail">{detail}</span>
        ) : null}
      </div>
      {action ? (
        <div className="observability-notice__actions">{action}</div>
      ) : null}
    </div>
  )
}
