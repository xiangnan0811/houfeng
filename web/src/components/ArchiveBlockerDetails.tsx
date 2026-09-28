import { Link } from 'react-router-dom'

import { blockerHandling } from '../lib/assetLifecycle'
import type { ArchiveBlockerDetail } from '../lib/types'

type ArchiveBlockerDetailsProps = {
  details: readonly ArchiveBlockerDetail[]
  vpsId: string
  onInline: (detail: ArchiveBlockerDetail, kind: 'service-status' | 'domain-status' | 'residual' | 'restore') => void
}

// Safety blockers carry no object name/state; describe them in plain language instead of
// exposing the internal code or linking to a non-existent object.
const BLOCKER_REASON: Record<string, string> = {
  vps_lifecycle_not_archivable: '只有管理中的 VPS 可以结束使用并归档。',
  receiver_observation_unhealthy: 'Center 接收链路尚未建立连续健康观察，请等待接收服务恢复。',
  continuous_offline_window_incomplete: '所有接入会话无可信在线信号的时间尚未达到 180 分钟。',
  recent_trusted_online_signal: '最近 180 分钟内仍有可信在线信号',
}

export function ArchiveBlockerDetails({ details, vpsId, onInline }: ArchiveBlockerDetailsProps) {
  if (details.length === 0) return null
  return (
    <ul className="asset-lifecycle-confirm__blockers">
      {details.map((detail) => {
        const handling = blockerHandling(detail, vpsId)
        const reason = BLOCKER_REASON[detail.code]
        const name = detail.display_name || detail.object_id
        const state = detail.current_state || (name ? reason : '')
        return (
          <li key={`${detail.code}:${detail.object_type}:${detail.object_id}`}>
            {name ? <strong>{name}</strong> : <span>{reason ?? '归档条件尚未满足。'}</span>}
            {name && state ? ` · ${state}` : null}
            {detail.object_id || handling.inline ? (
              <div>
                {handling.href ? (
                  <Link to={handling.href}>{handling.label}</Link>
                ) : handling.inline ? (
                  <button type="button" className="btn sm ghost" onClick={() => onInline(detail, handling.inline!)}>
                    {handling.label}
                  </button>
                ) : (
                  <span>{handling.label}</span>
                )}
              </div>
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}
