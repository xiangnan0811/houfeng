import type { ReactNode } from 'react'

import { Timestamp } from '../atoms'
import './ObservabilityNotice.css'
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
      className={`observability-notice-row observability-notice-row--${tone} monitoring-detail-history__row monitoring-detail-history__row--${tone} events-stream__row events-stream__row--${tone}`}
    >
      <div className="observability-notice-row__copy monitoring-detail-history__copy events-stream__copy">
        <span className="observability-notice-row__mark monitoring-detail-history__mark events-stream__mark">{mark}</span>
        <span className="observability-notice-row__title monitoring-detail-history__title events-stream__title">{title}</span>
        {detail ? (
          <span className="observability-notice-row__detail monitoring-detail-history__detail events-stream__detail">
            {detail}
          </span>
        ) : null}
      </div>
      {showMeta ? (
        <p className="observability-notice-row__meta monitoring-detail-history__meta events-stream__meta">
          {meta ? <>{meta}{time || timeLabel ? ' · ' : null}</> : null}
          {timeLabel ? <>{timeLabel} </> : null}
          {time ? <Timestamp value={time} mode="absolute" /> : null}
        </p>
      ) : null}
    </li>
  )
}
