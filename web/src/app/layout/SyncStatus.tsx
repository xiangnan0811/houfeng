export interface SyncStatusProps {
  state: 'ok' | 'degraded' | 'down'
  label: string
  meta?: string
  recentHeartbeats?: number[]
}
