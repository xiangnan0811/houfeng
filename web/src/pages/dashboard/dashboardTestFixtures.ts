import type {
  DashboardAssetSummary,
  DashboardNotificationStatus,
  DashboardOverview,
  SubscriptionOverview,
  VPSAssetRecord,
} from '../../lib/types'

export const DASHBOARD_FIXTURE_LOADED_AT = '2026-07-10T06:30:00Z'

type DashboardOverviewOverrides = Omit<
  Partial<DashboardOverview>,
  'asset_summary' | 'notification_status'
> & {
  asset_summary?: Partial<DashboardAssetSummary>
  notification_status?: Partial<DashboardNotificationStatus>
}

export function dashboardOverviewFixture(
  overrides: DashboardOverviewOverrides = {},
): DashboardOverview {
  const assetSummary: DashboardAssetSummary = {
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
    ...overrides.asset_summary,
  }
  const notificationStatus: DashboardNotificationStatus = {
    telegram_configured: false,
    telegram_runtime_managed: false,
    telegram_runtime_apply_active: false,
    feishu_configured: false,
    ...overrides.notification_status,
  }

  return {
    snapshot_generated_at: '2026-07-10T06:25:00Z',
    total_monitoring_instance_count: 4,
    total_target_count: 3,
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
    ...overrides,
    notification_status: notificationStatus,
    asset_summary: assetSummary,
    abnormal_monitoring_instances: overrides.abnormal_monitoring_instances ?? [],
    abnormal_targets: overrides.abnormal_targets ?? [],
    recent_events: overrides.recent_events ?? [],
  }
}

export function subscriptionOverviewFixture(
  overrides: Partial<SubscriptionOverview> = {},
): SubscriptionOverview {
  return {
    snapshot_generated_at: '2026-07-10T06:27:00Z',
    base_currency: 'CNY',
    total_monthly_cost: 298,
    total_yearly_cost: 3576,
    active_subscription_count: 4,
    renewal_due_14d_count: 0,
    renewal_due_30d_count: 0,
    budget_risk_count: 0,
    exchange_rate_stale_count: 0,
    decision_attention_count: 0,
    missing_subscription_vps_count: 0,
    upcoming_renewals: [],
    provider_breakdown: [],
    currency_breakdown: [],
    category_breakdown: [],
    budget_risks: [],
    vps_costs: [],
    missing_subscription_assets: [],
    ...overrides,
  }
}

export function vpsAssetFixture(
  overrides: Partial<VPSAssetRecord> = {},
): VPSAssetRecord {
  return {
    vps_id: 'vps_001',
    display_name: 'Tokyo Edge',
    provider_id: 'pv_001',
    provider_name: 'Example Cloud',
    product_name: 'edge-small',
    order_ref: 'order-001',
    country: 'JP',
    region: 'Kanto',
    city: 'Tokyo',
    datacenter: 'nrt-1',
    ipv4: '192.0.2.10',
    ipv6: '',
    ssh_host: '192.0.2.10',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'kvm',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'normal',
    labels: ['edge'],
    note: '',
    active_monitoring_instance_link_count: 1,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-10T06:00:00Z',
    ...overrides,
  }
}
