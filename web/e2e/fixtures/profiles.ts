import type { User } from '../../src/lib/auth-client'
import type {
  AssetDecisionOverview,
  CommandAuditAction,
  CommandAuditEvent,
  CommandAuditListResponse,
  DashboardOverview,
  MonitoringInstanceSparklinesResponse,
  ProviderRecord,
  SettingsRecord,
  SubscriptionOverview,
  SubscriptionRecord,
  SubscriptionStatistics,
  TargetSparklinesResponse,
  VPSAssetRecord,
} from '../../src/lib/types'
import {
  dashboardOverviewFixture,
  subscriptionOverviewFixture,
  vpsAssetFixture,
} from '../../src/pages/dashboard/dashboardTestFixtures'

import { apiRouteKey, type ApiFixtureProfile } from './contracts'

const AUTHENTICATED_USER = {
  user_id: 'u_e2e',
  username: 'e2e-admin',
  role: 'admin',
  display_name: 'E2E Admin',
} satisfies User

const PROVIDER = {
  provider_id: 'pv_001',
  name: 'Example Cloud',
  website: 'https://example.invalid',
  panel_url: 'https://console.example.invalid',
  account_hint: 'e2e',
  country: 'JP',
  note: 'Browser contract fixture',
  rating: 5,
  labels: ['edge'],
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-10T06:00:00Z',
} satisfies ProviderRecord

const SUBSCRIPTION = {
  subscription_id: 'sub_001',
  vps_id: 'vps_001',
  price: 12,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  billing_period_unit: 'month',
  billing_period_length: 1,
  monthly_price: 12,
  monthly_price_base: 84,
  yearly_price_base: 1008,
  base_currency: 'CNY',
  exchange_rate: 7,
  exchange_rate_date: '2026-07-10',
  exchange_rate_stale: false,
  budget_status: 'ok',
  next_reminder_at: '2026-07-20T00:00:00Z',
  started_at: '2026-07-01',
  renew_at: '2026-08-01',
  auto_renew: true,
  auto_renew_cancelled: false,
  renewal_mode: 'auto',
  status: 'active',
  payment_method: 'card',
  display_name: 'Tokyo Edge subscription',
  cost_category: 'compute',
  labels: ['edge'],
  note: '',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-10T06:00:00Z',
} satisfies SubscriptionRecord

const SUBSCRIPTION_STATISTICS = {
  window: 'year',
  base_currency: 'CNY',
  total_monthly_cost: 84,
  total_yearly_cost: 1008,
  provider_breakdown: [],
  currency_breakdown: [],
  category_breakdown: [],
  payment_breakdown: [],
  region_breakdown: [],
  cost_month_buckets: [],
  renewal_month_buckets: [],
  budget_statuses: [],
} satisfies SubscriptionStatistics

const SETTINGS = {
  telegram: {
    chat_id: '',
    token_present: false,
    token_masked_summary: '',
    runtime_managed: false,
    runtime_apply_active: false,
  },
  feishu: {
    enabled: false,
    webhook_url_present: false,
    webhook_url_masked_summary: '',
  },
  host_sample_frequency_tier: '5s',
  probe_frequency_defaults: { tcp: '5s', http: '5s', tls: '6h' },
  incident_defaults: {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 3,
    sweep_interval_seconds: 5,
    notify_on_started: true,
    notify_on_escalated: true,
    notify_on_recovered: true,
    cpu_warning_pct: 80,
    cpu_alert_pct: 90,
    cpu_critical_pct: 95,
    mem_warning_pct: 85,
    mem_alert_pct: 92,
    mem_critical_pct: 95,
    disk_warning_pct: 85,
    disk_alert_pct: 92,
    disk_critical_pct: 97,
    inode_warning_pct: 80,
    inode_alert_pct: 90,
    inode_critical_pct: 95,
    iowait_warning_pct: 20,
    iowait_critical_pct: 50,
    load5_warning: 4,
    load5_critical: 8,
  },
  override_rules: {
    monitoring_instance_labels: [],
    target_types: [],
    target_labels: [],
  },
  retention_policy: {
    raw_layer_days: 30,
    aggregate_layer_days: 30,
    event_layer_days: 90,
    notification_layer_days: 180,
  },
  ip_quality_settings: {
    enabled: true,
    frequency_seconds: 86_400,
    stale_after_seconds: 604_800,
    timeout_seconds: 15,
    raw_retention_days: 90,
    history_retention_days: 365,
    services: ['netflix', 'chatgpt'],
  },
  subscription_cost_settings: {
    base_currency: 'CNY',
    exchange_rate_provider: 'frankfurter',
    fixer_configured: false,
    fixer_masked_summary: '',
    default_reminder_offsets_days: [14, 7, 1],
    max_reminder_lead_days: 30,
    exchange_rate_stale_after_hours: 36,
  },
} satisfies SettingsRecord

const ASSET_DECISION_OVERVIEW = {
  snapshot_generated_at: '2026-07-10T06:28:00Z',
  renew_within_days: 30,
  group_count: 0,
  member_vps_count: 0,
  needs_decision_count: 0,
  renewal_group_count: 0,
  region_group_count: 0,
  provider_group_count: 0,
  cost_group_count: 0,
  evidence_group_count: 0,
  top_groups: [],
  type_counts: {},
  view_counts: {},
  source_availability: {
    subscriptions: true,
    services: true,
    domains: true,
    monitoring: true,
    targets: true,
  },
} satisfies AssetDecisionOverview

const MONITORING_SPARKLINES = {
  monitoring_instances: {},
} satisfies MonitoringInstanceSparklinesResponse

const TARGET_SPARKLINES = {
  targets: {},
} satisfies TargetSparklinesResponse

type HostileCommandAuditEvent = CommandAuditEvent & {
  stdout?: string
  stderr?: string
  details?: Record<string, unknown>
}

type HostileCommandAuditAction = Omit<CommandAuditAction, 'events'> & {
  stdout: string
  stderr: string
  details: Record<string, unknown>
  events: HostileCommandAuditEvent[]
}

const COMMAND_AUDIT_RESPONSE: CommandAuditListResponse & {
  items: HostileCommandAuditAction[]
} = {
  items: [{
    id: 'act_e2e_command_audit',
    action_id: 'act_e2e_command_audit',
    monitoring_instance: {
      id: 'mi_001',
      name: 'Tokyo Edge',
      deleted: false,
    },
    command_id: 'systemctl_status',
    sensitivity: 'sensitive',
    outcome: 'succeeded',
    actor: {
      user_id: 'u_e2e',
      username: 'e2e-admin',
      display_name: 'E2E Admin',
    },
    started_at: '2026-07-12T08:00:00Z',
    events: [
      {
        audit_id: 'cmd_aud_e2e_queued',
        event_type: 'queued',
        source: 'web',
        occurred_at: '2026-07-12T08:00:00Z',
        stdout: 'COMMAND_AUDIT_EVENT_OUTPUT_SHOULD_NOT_RENDER',
        details: { stderr: 'COMMAND_AUDIT_EVENT_DETAILS_SHOULD_NOT_RENDER' },
      },
      {
        audit_id: 'cmd_aud_e2e_completed',
        event_type: 'completed',
        source: 'agent_sync',
        occurred_at: '2026-07-12T08:00:02Z',
        exit_code: 0,
      },
    ],
    stdout: 'COMMAND_AUDIT_STDOUT_SHOULD_NOT_RENDER',
    stderr: 'COMMAND_AUDIT_STDERR_SHOULD_NOT_RENDER',
    details: { stdout: 'COMMAND_AUDIT_DETAILS_SHOULD_NOT_RENDER' },
  }],
}

export type CoreRoutePath =
  | '/'
  | '/vps'
  | '/asset-decisions'
  | '/monitoring'
  | '/targets'
  | '/events'
  | '/command-audit'
  | '/providers'
  | '/subscriptions'
  | '/settings'

export const unauthenticatedProfile = {
  [apiRouteKey('GET', '/api/auth/me')]: {
    status: 401,
    body: { error: 'unauthenticated' },
  },
} satisfies ApiFixtureProfile

export function authenticatedProfile(
  routes: ApiFixtureProfile = {},
  dashboard: DashboardOverview = dashboardOverviewFixture(),
): ApiFixtureProfile {
  return {
    [apiRouteKey('GET', '/api/auth/me')]: {
      status: 200,
      body: AUTHENTICATED_USER,
    },
    [apiRouteKey('GET', '/api/dashboard')]: {
      status: 200,
      body: dashboard,
    },
    ...routes,
  }
}

export function dashboardProfile(options: {
  dashboard?: DashboardOverview
  vps?: VPSAssetRecord[]
  subscription?: SubscriptionOverview
} = {}): ApiFixtureProfile {
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/vps')]: {
      status: 200,
      body: options.vps ?? [vpsAssetFixture()],
    },
    [apiRouteKey('GET', '/api/subscriptions/overview')]: {
      status: 200,
      body: options.subscription ?? subscriptionOverviewFixture(),
    },
  }, options.dashboard)
}

export function coreRouteProfile(path: CoreRoutePath): ApiFixtureProfile {
  const vps = vpsAssetFixture()
  switch (path) {
    case '/':
      return dashboardProfile()
    case '/vps':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/vps')]: { status: 200, body: [vps] },
        [apiRouteKey('GET', '/api/providers')]: { status: 200, body: [PROVIDER] },
        [apiRouteKey('GET', '/api/subscriptions?sort=renew_at&order=asc')]: {
          status: 200,
          body: [SUBSCRIPTION],
        },
      })
    case '/asset-decisions':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/asset-decisions/overview?view=needs_decision&renew_within_days=30')]: {
          status: 200,
          body: ASSET_DECISION_OVERVIEW,
        },
        [apiRouteKey('GET', '/api/asset-decisions/groups?view=needs_decision&renew_within_days=30')]: {
          status: 200,
          body: [],
        },
        [apiRouteKey('GET', '/api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30')]: {
          status: 200,
          body: [],
        },
        [apiRouteKey('GET', '/api/asset-decisions/scenario-templates')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/asset-decisions/records?view=needs_decision&renew_within_days=30')]: {
          status: 200,
          body: [],
        },
        [apiRouteKey('GET', '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc')]: {
          status: 200,
          body: [SUBSCRIPTION],
        },
        [apiRouteKey('GET', '/api/subscriptions?sort=renew_at&order=asc')]: {
          status: 200,
          body: [SUBSCRIPTION],
        },
        [apiRouteKey('GET', '/api/vps')]: { status: 200, body: [vps] },
        [apiRouteKey('GET', '/api/vps?renewal_decision=unreviewed')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/vps?renewal_decision=migrate')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/vps?renewal_decision=cancel')]: { status: 200, body: [] },
      })
    case '/monitoring':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/monitoring-instances')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/monitoring-instances/sparklines?metrics=cpu_usage_pct,mem_used_pct,disk_used_pct&window=24h&downsample=24')]: {
          status: 200,
          body: MONITORING_SPARKLINES,
        },
        [apiRouteKey('GET', '/api/settings')]: { status: 200, body: SETTINGS },
      })
    case '/targets':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/targets')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/targets/sparklines?metrics=latency&window=24h&downsample=24')]: {
          status: 200,
          body: TARGET_SPARKLINES,
        },
        [apiRouteKey('GET', '/api/asset-context/targets')]: { status: 200, body: [] },
      })
    case '/events':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/monitoring-instances')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/targets')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/events?limit=200')]: { status: 200, body: { items: [] } },
      })
    case '/command-audit':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/command-audits')]: {
          status: 200,
          body: COMMAND_AUDIT_RESPONSE,
        },
      })
    case '/providers':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/providers')]: { status: 200, body: [PROVIDER] },
        [apiRouteKey('GET', '/api/vps')]: { status: 200, body: [vps] },
        [apiRouteKey('GET', '/api/subscriptions')]: { status: 200, body: [SUBSCRIPTION] },
      })
    case '/subscriptions':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/subscriptions')]: { status: 200, body: [SUBSCRIPTION] },
        [apiRouteKey('GET', '/api/vps')]: { status: 200, body: [vps] },
        [apiRouteKey('GET', '/api/subscriptions/overview')]: {
          status: 200,
          body: subscriptionOverviewFixture({
            active_subscription_count: 1,
            total_monthly_cost: 84,
            total_yearly_cost: 1008,
          }),
        },
        [apiRouteKey('GET', '/api/subscriptions/statistics?window=year')]: {
          status: 200,
          body: SUBSCRIPTION_STATISTICS,
        },
      })
    case '/settings':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/settings')]: { status: 200, body: SETTINGS },
      })
  }
}
