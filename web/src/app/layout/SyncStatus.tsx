import { Sparkline } from '../../components/atoms'

export interface SyncStatusProps {
  state: 'ok' | 'degraded' | 'down'
  version: string
  lastSync: string
  /** Recent heartbeat sample values (e.g. last 7 sync intervals in seconds).
   *  Optional — when missing or empty, the sparkline track is hidden. */
  recentHeartbeats?: number[]
}

const LABEL = { ok: '中心运行正常', degraded: '中心运行降级', down: '中心不可达' } as const

const TONE = { ok: 'normal', degraded: 'notice', down: 'critical' } as const

export function SyncStatus({ state, version, lastSync, recentHeartbeats }: SyncStatusProps) {
  const time = formatTime(lastSync)
  const showSpark = Array.isArray(recentHeartbeats) && recentHeartbeats.length > 0
  return (
    <div className={`sync-status sync-status--${state}`}>
      <div className="sync-status__line">
        <span className="sync-status__dot" />
        <span className="sync-status__label">{LABEL[state]}</span>
      </div>
      {showSpark && (
        <div className="sync-status__spark" aria-hidden>
          <Sparkline values={recentHeartbeats!} tone={TONE[state]} width={140} height={14} />
        </div>
      )}
      <div className="sync-status__meta">
        {version} · sync {time}
      </div>
    </div>
  )
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    const hh = String(d.getHours()).padStart(2, '0')
    const mm = String(d.getMinutes()).padStart(2, '0')
    const ss = String(d.getSeconds()).padStart(2, '0')
    return `${hh}:${mm}:${ss}`
  } catch {
    return iso
  }
}
