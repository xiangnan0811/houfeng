import type { VPSAssetRecord } from '../../lib/types'
import type { RemoteState } from './dashboardRemoteState'

/**
 * 观测证据里的监控覆盖事实：在用 VPS 中有多少台关联了监控实例。
 * 只来自 VPS 清单的关联计数，表达“是否关联”，不代表实例在线或健康。
 */
export type DashboardMonitoringCoverage =
  | { status: 'loading' }
  | { status: 'unavailable' }
  | { status: 'available'; linked: number; total: number; loadedAt: string }

export function buildMonitoringCoverage(vps: RemoteState<VPSAssetRecord[]>): DashboardMonitoringCoverage | null {
  if (vps.status === 'loading') return { status: 'loading' }
  if (vps.status === 'error') return { status: 'unavailable' }
  const active = vps.value.filter((item) => item.lifecycle_status === 'active')
  // 还没有在用 VPS 时没有覆盖可谈，交给首次接入引导。
  if (active.length === 0) return null
  return {
    status: 'available',
    linked: active.filter((item) => item.active_monitoring_instance_link_count > 0).length,
    total: active.length,
    loadedAt: vps.loadedAt,
  }
}
