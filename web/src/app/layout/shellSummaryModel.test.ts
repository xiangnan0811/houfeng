import { describe, expect, it } from 'vitest'

import type { DashboardOverview } from '../../lib/types'
import {
  buildShellSummaryModel,
  INITIAL_DASHBOARD_SUMMARY,
  SHELL_SUMMARY_FRESHNESS_MS,
  type DashboardSummaryState,
} from './shellSummaryModel'

const NOW = Date.parse('2026-07-10T09:30:00Z')

function overview(overrides: Partial<DashboardOverview> = {}): DashboardOverview {
  return {
    snapshot_generated_at: '2026-07-10T09:29:00Z',
    total_monitoring_instance_count: 2,
    total_target_count: 1,
    abnormal_monitoring_instance_count: 0,
    abnormal_target_count: 0,
    unobserved_target_count: 0,
    stale_target_count: 0,
    severe_monitoring_instance_count: 0,
    severe_target_count: 0,
    maintenance_monitoring_instance_count: 0,
    maintenance_target_count: 0,
    pending_onboarding_monitoring_instance_count: 0,
    paused_monitoring_instance_count: 0,
    retired_monitoring_instance_count: 0,
    paused_target_count: 0,
    archived_target_count: 0,
    recent_new_incident_count: 0,
    recent_recovery_count: 0,
    group_summaries: [],
    notification_status: {
      telegram_configured: false,
      telegram_runtime_managed: false,
      telegram_runtime_apply_active: false,
      feishu_configured: false,
    },
    asset_summary: {
      renewal_due_30d_subscription_count: 0,
      renewal_due_30d_vps_count: 0,
      unreviewed_vps_count: 0,
      no_renewal_vps_count: 0,
      pending_followup_count: 0,
      archived_vps_count: 0,
      auto_renew_check_vps_count: 0,
      unlinked_vps_count: 0,
      abnormal_linked_vps_count: 0,
      cost_by_currency: [],
    },
    abnormal_monitoring_instances: [],
    abnormal_targets: [],
    recent_events: [],
    ...overrides,
  }
}

function success(value = overview()): DashboardSummaryState {
  return { status: 'success', overview: value, error: null }
}

describe('buildShellSummaryModel', () => {
  it('keeps loading and initial failure distinct', () => {
    expect(buildShellSummaryModel(INITIAL_DASHBOARD_SUMMARY, NOW)).toMatchObject({
      state: 'loading',
      label: '正在读取系统摘要',
      showAnomalyCounts: false,
    })
    expect(buildShellSummaryModel({ status: 'error', overview: null, error: '503' }, NOW))
      .toMatchObject({ state: 'unavailable', label: '系统摘要不可用', showAnomalyCounts: false })
  })

  it('keeps known abnormalities and unobserved targets as separate fresh facts', () => {
    expect(buildShellSummaryModel(success(), NOW)).toMatchObject({
      state: 'clear',
      label: '运行异常 0',
      spokenLabel: '当前运行异常计数为 0',
      showAnomalyCounts: true,
    })
    expect(buildShellSummaryModel(success(overview({ abnormal_target_count: 2 })), NOW))
      .toMatchObject({
        state: 'anomaly',
        label: '运行异常 2',
        showAnomalyCounts: true,
      })
    expect(buildShellSummaryModel(success(overview({ unobserved_target_count: 4 })), NOW))
      .toMatchObject({
        state: 'unobserved',
        label: '尚有目标无观测',
        showAnomalyCounts: true,
      })
    expect(buildShellSummaryModel(success(overview({
      abnormal_monitoring_instance_count: 1,
      abnormal_target_count: 2,
      unobserved_target_count: 3,
    })), NOW)).toMatchObject({
      state: 'anomaly',
      label: '运行异常 3，尚有目标无观测 3',
      showAnomalyCounts: true,
    })
  })

  it('expires a snapshot at the exact freshness boundary', () => {
    const generatedAt = new Date(NOW - SHELL_SUMMARY_FRESHNESS_MS).toISOString()

    expect(
      buildShellSummaryModel(success(overview({ snapshot_generated_at: generatedAt })), NOW),
    ).toMatchObject({ state: 'stale', label: '系统摘要已过期', showAnomalyCounts: false })
  })

  it('preserves the last generated time but hides counts after refresh failure', () => {
    const lastOverview = overview({ abnormal_monitoring_instance_count: 3 })

    expect(
      buildShellSummaryModel(
        { status: 'error', overview: lastOverview, error: 'dashboard unavailable' },
        NOW,
      ),
    ).toEqual({
      state: 'stale',
      label: '更新失败，显示上次结果',
      generatedAt: lastOverview.snapshot_generated_at,
      showAnomalyCounts: false,
    })
  })

  it('lists abnormal, unobserved, and stale counts separately', () => {
    expect(buildShellSummaryModel(success(overview({ stale_target_count: 2 })), NOW)).toMatchObject({
      state: 'notice',
      label: '观测过期 2',
      showAnomalyCounts: true,
    })
    expect(buildShellSummaryModel(success(overview({
      abnormal_target_count: 2,
      stale_target_count: 2,
    })), NOW)).toMatchObject({
      state: 'anomaly',
      label: '运行异常 2，观测过期 2',
      showAnomalyCounts: true,
    })
    expect(buildShellSummaryModel(success(overview({
      abnormal_monitoring_instance_count: 1,
      abnormal_target_count: 2,
      unobserved_target_count: 3,
      stale_target_count: 4,
    })), NOW)).toMatchObject({
      state: 'anomaly',
      label: '运行异常 3，尚有目标无观测 3，观测过期 4',
    })
    expect(buildShellSummaryModel(success(overview({
      unobserved_target_count: 4,
      stale_target_count: 2,
    })), NOW)).toMatchObject({
      state: 'unobserved',
      label: '尚有目标无观测 4，观测过期 2',
    })
    expect(buildShellSummaryModel(success(), NOW).label).toBe('运行异常 0')
  })

  it('hides target stale counts when the system snapshot itself is stale', () => {
    const generatedAt = new Date(NOW - SHELL_SUMMARY_FRESHNESS_MS).toISOString()
    expect(buildShellSummaryModel(success(overview({
      snapshot_generated_at: generatedAt,
      abnormal_target_count: 2,
      stale_target_count: 5,
    })), NOW)).toMatchObject({
      state: 'stale',
      label: '系统摘要已过期',
      showAnomalyCounts: false,
    })
  })
})
