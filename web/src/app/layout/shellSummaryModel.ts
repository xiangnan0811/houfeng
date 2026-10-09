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
      label: '系统摘要不可用',
      showAnomalyCounts: false,
    }
  }

  const generatedAtMs = Date.parse(overview.snapshot_generated_at)
  const snapshotIsStale =
    !Number.isFinite(generatedAtMs) || now - generatedAtMs >= SHELL_SUMMARY_FRESHNESS_MS

  if (summary.status === 'error') {
    return {
      state: 'stale',
      label: '更新失败，显示上次结果',
      generatedAt: overview.snapshot_generated_at,
      showAnomalyCounts: false,
    }
  }

  if (snapshotIsStale) {
    return {
      state: 'stale',
      label: '系统摘要已过期',
      generatedAt: overview.snapshot_generated_at,
      showAnomalyCounts: false,
    }
  }

  const abnormalCount =
    overview.abnormal_monitoring_instance_count + overview.abnormal_target_count
  const unobservedCount = overview.unobserved_target_count
  const staleCount = overview.stale_target_count
  const label = shellObservationLabel(abnormalCount, unobservedCount, staleCount)
  const generatedAt = overview.snapshot_generated_at

  if (abnormalCount > 0) {
    return { state: 'anomaly', label, generatedAt, showAnomalyCounts: true }
  }
  if (unobservedCount > 0) {
    return { state: 'unobserved', label, generatedAt, showAnomalyCounts: true }
  }
  if (staleCount > 0) {
    return { state: 'notice', label, generatedAt, showAnomalyCounts: true }
  }
  return { state: 'clear', label, generatedAt, showAnomalyCounts: true }
}

function shellObservationLabel(
  abnormalCount: number,
  unobservedCount: number,
  staleCount: number,
): string {
  const parts: string[] = []
  if (abnormalCount > 0) parts.push(`运行异常 ${abnormalCount}`)
  if (unobservedCount > 0) {
    parts.push(abnormalCount > 0 || staleCount > 0
      ? `尚有目标无观测 ${unobservedCount}`
      : '尚有目标无观测')
  }
  if (staleCount > 0) parts.push(`观测过期 ${staleCount}`)
  return parts.length === 0 ? '当前运行异常计数为 0' : parts.join('，')
}
