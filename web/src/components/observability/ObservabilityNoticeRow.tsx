import type { ReactNode } from 'react'

import { Timestamp } from '../atoms'
import '../../pages/monitoring-detail/MonitoringDetailWorkspace.css'
import type { ObservabilityTone } from './ObservabilityNotice'

type ObservabilityNoticeRowProps = {
  tone: ObservabilityTone
  mark: string
  title: ReactNode
  detail?: ReactNode | undefined
  meta?: ReactNode | undefined
  time?: string | undefined
  timeLabel?: string | undefined
}

export function ObservabilityNoticeRow({
  tone,
  mark,
  title,
  detail,
  meta,
  time,
  timeLabel,
}: ObservabilityNoticeRowProps) {
  const showMeta = (meta != null && meta !== '') || Boolean(time) || Boolean(timeLabel)
  return (
    <li
      className={`observability-notice-row observability-notice-row--${tone}`}
    >
      <div className="observability-notice-row__copy">
        <span className="observability-notice-row__mark">{mark}</span>
        <span className="observability-notice-row__title">{title}</span>
        {detail ? (
          <span className="observability-notice-row__detail">
            {detail}
          </span>
        ) : null}
      </div>
      {showMeta ? (
        <p className="observability-notice-row__meta">
          {meta ? <>{meta}{time || timeLabel ? ' · ' : null}</> : null}
          {timeLabel ? <>{timeLabel} </> : null}
          {time ? <Timestamp value={time} mode="absolute" /> : null}
        </p>
      ) : null}
    </li>
  )
}
