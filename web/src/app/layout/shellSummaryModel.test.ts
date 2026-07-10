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
      to_cancel_vps_count: 0,
      cancelled_vps_count: 0,
      cancellation_attention_vps_count: 0,
      running_cancelled_asset_count: 0,
      to_migrate_vps_count: 0,
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
      showAnomalyCounts: false,
    })
    expect(buildShellSummaryModel({ status: 'error', overview: null, error: '503' }, NOW))
      .toMatchObject({ state: 'unavailable', showAnomalyCounts: false })
  })

  it('derives clear and anomaly from a fresh snapshot', () => {
    expect(buildShellSummaryModel(success(), NOW)).toMatchObject({
      state: 'clear',
      label: '摘要无异常',
      showAnomalyCounts: true,
    })
    expect(buildShellSummaryModel(success(overview({ abnormal_target_count: 2 })), NOW))
      .toMatchObject({
        state: 'anomaly',
        label: '摘要有异常',
        showAnomalyCounts: true,
      })
  })

  it('expires a snapshot at the exact freshness boundary', () => {
    const generatedAt = new Date(NOW - SHELL_SUMMARY_FRESHNESS_MS).toISOString()

    expect(
      buildShellSummaryModel(success(overview({ snapshot_generated_at: generatedAt })), NOW),
    ).toMatchObject({ state: 'stale', showAnomalyCounts: false })
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
      label: '摘要已过期',
      generatedAt: lastOverview.snapshot_generated_at,
      showAnomalyCounts: false,
    })
  })
})
