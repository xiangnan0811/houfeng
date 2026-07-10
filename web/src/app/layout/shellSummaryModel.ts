import type { DashboardOverview } from '../../lib/types'
import type { SyncStatusProps } from './SyncStatus'

export const SHELL_SUMMARY_FRESHNESS_MS = 5 * 60_000

export type DashboardSummaryState =
  | { status: 'loading'; overview: null; error: null }
  | { status: 'success'; overview: DashboardOverview; error: null }
  | { status: 'error'; overview: DashboardOverview | null; error: string }

export type ShellSummaryModel = SyncStatusProps & {
  showAnomalyCounts: boolean
}

export const INITIAL_DASHBOARD_SUMMARY: DashboardSummaryState = {
  status: 'loading',
  overview: null,
  error: null,
}

export function buildShellSummaryModel(
  summary: DashboardSummaryState,
  now: number,
): ShellSummaryModel {
  if (summary.status === 'loading') {
    return {
      state: 'loading',
      label: '正在读取系统摘要',
      showAnomalyCounts: false,
    }
  }

  const overview = summary.overview
  if (!overview) {
    return {
      state: 'unavailable',
      label: '摘要不可用',
      showAnomalyCounts: false,
    }
  }

  const generatedAt = Date.parse(overview.snapshot_generated_at)
  const snapshotIsStale =
    !Number.isFinite(generatedAt) || now - generatedAt >= SHELL_SUMMARY_FRESHNESS_MS

  if (summary.status === 'error' || snapshotIsStale) {
    return {
      state: 'stale',
      label: '摘要已过期',
      generatedAt: overview.snapshot_generated_at,
      showAnomalyCounts: false,
    }
  }

  const abnormalCount =
    overview.abnormal_monitoring_instance_count + overview.abnormal_target_count

  if (abnormalCount > 0) {
    return {
      state: 'anomaly',
      label: '摘要有异常',
      generatedAt: overview.snapshot_generated_at,
      showAnomalyCounts: true,
    }
  }

  return {
    state: 'clear',
    label: '摘要无异常',
    generatedAt: overview.snapshot_generated_at,
    showAnomalyCounts: true,
  }
}
