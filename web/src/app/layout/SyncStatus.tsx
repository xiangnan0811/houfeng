import { Sparkline } from '../../components/atoms'

export interface SyncStatusProps {
  state: 'ok' | 'degraded' | 'down'
  label: string
  meta: string
  /** Recent heartbeat sample values (e.g. last 7 sync intervals in seconds).
   *  Optional — when missing or empty, the sparkline track is hidden. */
  recentHeartbeats?: number[]
}

const TONE = { ok: 'normal', degraded: 'notice', down: 'critical' } as const

export function SyncStatus({ state, label, meta, recentHeartbeats }: SyncStatusProps) {
  const showSpark = Array.isArray(recentHeartbeats) && recentHeartbeats.length > 0
  return (
    <div className={`sync-status sync-status--${state}`}>
      <div className="sync-status__line">
        <span className="sync-status__dot" />
        <span className="sync-status__label">{label}</span>
      </div>
      {showSpark && (
        <div className="sync-status__spark" aria-hidden>
          <Sparkline values={recentHeartbeats!} tone={TONE[state]} width={140} height={14} />
        </div>
      )}
      <div className="sync-status__meta">{meta}</div>
    </div>
  )
}
