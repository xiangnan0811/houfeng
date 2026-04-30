export interface SyncStatusProps {
  state: 'ok' | 'degraded' | 'down'
  version: string
  lastSync: string
}

const LABEL = { ok: '中心运行正常', degraded: '中心运行降级', down: '中心不可达' } as const

export function SyncStatus({ state, version, lastSync }: SyncStatusProps) {
  const time = formatTime(lastSync)
  return (
    <div className={`sync-status sync-status--${state}`}>
      <div className="sync-status__line">
        <span className="sync-status__dot" />
        <span className="sync-status__label">{LABEL[state]}</span>
      </div>
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
