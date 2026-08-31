import type { User } from '../../src/lib/auth-client'
import type {
  AssetDomainRecord,
  AssetDecisionOverview,
  AssetServiceRecord,
  CommandAuditAction,
  CommandAuditEvent,
  CommandAuditListResponse,
  ComparisonCandidateItem,
  ComparisonEvaluateResponse,
  DashboardOverview,
  MonitoringInstanceRecord,
  MonitoringInstanceSparklinesResponse,
  ProviderRecord,
  RecordDraft,
  RecordMutationResult,
  RecordNotification,
  SettingsRecord,
  SubjectActivityListResponse,
  SubscriptionOverview,
  SubscriptionRecord,
  SubscriptionStatistics,
  TargetSparklinesResponse,
  VPSAssetRecord,
  VPSAssetDetail,
  VPSMonitoringInstanceSummary,
  VPSOverview,
} from '../../src/lib/types'
import {
  COMPARISON_URL_VERSION,
  comparisonHref,
  type ComparisonURLState,
} from '../../src/pages/records/compare/comparisonQueryState'
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

const RECORD_NOTIFICATION = {
  notification_id: `rnt_${'a'.repeat(64)}`,
  record_id: 'rec_e2e001',
  event_kind: 'comment_mentioned',
  subject_kind: 'comment',
  subject_id: 'rcm_e2e001',
  source_version: 3,
  reason: 'mention',
  mandatory: true,
  event_at: '2026-08-17T09:30:00Z',
  read_at: null,
  dismissed_at: null,
} satisfies RecordNotification

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
    stale_threshold_intervals: 12,
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
  | '/record-inbox'
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
    [apiRouteKey('GET', '/api/record-notifications/unread-count')]: {
      status: 200,
      body: { unread_count: 1 },
    },
    ...routes,
  }
}

const RECORD_USER_ID = 'usr_0123456789abcdef01234567'
const RECORD_TIMESTAMP = '2026-08-17T09:00:00Z'
const RECORD_EVIDENCE_ID = 'evs_e2ethirdnight'
const RECORD_ATTACHMENT_ID = 'att_e2emtrreport'

const RECORD_BODY_MARKDOWN = [
  '# 第三晚 TCP 观测',
  '',
  '```sh',
  '# 复现丢包',
  'mtr -rw 203.0.113.7',
  '```',
  '',
  '| 主机 | 丢包 |',
  '| --- | --- |',
  '| alpha | 3% |',
  '',
  '<!-- houfeng-ref:v1 evidence evs_e2ethirdnight -->',
  `[系统证据：第三晚 TCP 观测](houfeng-evidence:${RECORD_EVIDENCE_ID})`,
].join('\n')

const RECORD_REVISION = {
  record_id: 'rec_e2e001',
  revision_id: 'rrv_e2e001',
  revision_no: 2,
  title: '第三晚 TCP 观测',
  body_markdown: RECORD_BODY_MARKDOWN,
  // Produced by the Go document parser for RECORD_BODY_MARKDOWN, so the reading
  // surface exercises the server render model rather than the source fallback.
  render_model: {
    version: 'houfeng_markdown/v1',
    nodes: [
      { type: 'heading', level: 1, children: [{ type: 'text', text: '第三晚 TCP 观测' }] },
      { type: 'fenced_code', text: '# 复现丢包\nmtr -rw 203.0.113.7\n' },
      {
        type: 'table',
        header: [[{ type: 'text', text: '主机' }], [{ type: 'text', text: '丢包' }]],
        rows: [[[{ type: 'text', text: 'alpha' }], [{ type: 'text', text: '3%' }]]],
      },
      {
        type: 'reference',
        kind: 'evidence',
        id: RECORD_EVIDENCE_ID,
        children: [{ type: 'text', text: '系统证据：第三晚 TCP 观测' }],
      },
    ],
  },
  render_model_status: 'ready',
  markdown_dialect_version: 1,
  record_type: 'troubleshooting',
  business_status: 'investigating',
  impact_level: 'high',
  visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
  subjects: [{
    registry_version: 1,
    kind: 'vps',
    role: 'affected',
    source_id: 'vps_0123456789abcdef',
    primary: true,
    identity: { display_name: 'VPS Alpha', provider: 'Example Cloud' },
  }],
  tags: ['network'],
  attachment_ids: [RECORD_ATTACHMENT_ID],
  evidence_snapshot_ids: [RECORD_EVIDENCE_ID],
  owner_id: RECORD_USER_ID,
  participants: [],
  author_id: RECORD_USER_ID,
  save_reason: '记录第三晚复现',
  created_at: RECORD_TIMESTAMP,
}

// A body the dialect cannot model. Writes only check UTF-8 and the dialect version,
// so this is a normal record that must stay readable through the client fallback.
const RECORD_UNSUPPORTED_REVISION = {
  ...RECORD_REVISION,
  render_model: undefined,
  render_model_status: 'unsupported',
  body_markdown: '# 排查路径\n\n- 排查\n  - 磁盘\n  - 网络',
}

/**
 * A published record that actually carries materials, a fenced snippet and a table.
 * The `/records/new` profile cannot exercise reading, layout switching or a populated
 * material drawer, so those paths need their own served record.
 */
export function recordDetailProfile(options: { renderModel?: 'ready' | 'unsupported' } = {}): ApiFixtureProfile {
  const revision = options.renderModel === 'unsupported' ? RECORD_UNSUPPORTED_REVISION : RECORD_REVISION
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/records/rec_e2e001')]: {
      status: 200,
      body: {
        record_id: 'rec_e2e001',
        lifecycle: 'active',
        current_revision_id: RECORD_REVISION.revision_id,
        lock_version: 4,
        authorization_epoch: 2,
        current: revision,
        capabilities: {
          read: true,
          update: true,
          archive: true,
          restore: true,
          draft: true,
          permanent_delete: false,
        },
        created_at: RECORD_TIMESTAMP,
        updated_at: RECORD_TIMESTAMP,
      },
    },
    [apiRouteKey('GET', '/api/records/rec_e2e001/actions?limit=50')]: {
      status: 200,
      body: { items: [] },
    },
    [apiRouteKey('GET', '/api/records/rec_e2e001/comments?limit=100')]: {
      status: 200,
      body: { comments: [] },
    },
    [apiRouteKey('GET', '/api/records/rec_e2e001/watch')]: {
      status: 200,
      body: {
        record_id: 'rec_e2e001',
        user_id: RECORD_USER_ID,
        version: 0,
        preference: 'default',
        sources: {
          author: false, owner: false, participant: false, comment: false, mention: false, action: false,
        },
        updated_at: null,
      },
    },
    [apiRouteKey('GET', '/api/record-drafts?limit=100')]: {
      status: 200,
      body: { items: [] },
    },
  })
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
    case '/record-inbox':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/record-notifications?limit=50')]: {
          status: 200,
          body: { items: [RECORD_NOTIFICATION] },
        },
        [apiRouteKey('GET', `/api/record-notifications/${RECORD_NOTIFICATION.notification_id}/target`)]: {
          status: 200,
          body: {
            record_id: RECORD_NOTIFICATION.record_id,
            subject_kind: RECORD_NOTIFICATION.subject_kind,
            subject_id: RECORD_NOTIFICATION.subject_id,
          },
        },
        [apiRouteKey('PUT', `/api/record-notifications/${RECORD_NOTIFICATION.notification_id}/read`)]: {
          status: 200,
          body: { ...RECORD_NOTIFICATION, read_at: '2026-08-17T10:00:00Z' },
          expectNoBody: true as const,
        },
        [apiRouteKey('PUT', `/api/record-notifications/${RECORD_NOTIFICATION.notification_id}/dismiss`)]: {
          status: 200,
          body: { ...RECORD_NOTIFICATION, read_at: '2026-08-17T10:00:00Z', dismissed_at: '2026-08-17T10:01:00Z' },
          expectNoBody: true as const,
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

const EMPTY_SECTION = {
  state: 'ready',
  observed_at: null,
  last_success_at: null,
  reason_code: '',
} as const

const VPS_OVERVIEW_MONITORING = {
  monitoring_instance_id: 'mi_001',
  display_name: 'Tokyo Monitor',
  group: 'edge',
  region: 'Kanto',
  city: 'Tokyo',
  provider: 'Example Cloud',
  lifecycle_status: 'active',
  monitoring_status: 'active',
  binding_status: 'bound',
  current_health_status: '正常',
  last_heartbeat_at: '2026-08-20T08:59:00Z',
  current_active_incident_count: 0,
  current_primary_issue_summary: '',
  linked_at: '2026-08-01T00:00:00Z',
  note: '',
} satisfies VPSMonitoringInstanceSummary

const VPS_OVERVIEW_SERVICE = {
  service_id: 'svc_001',
  vps_id: 'vps_001',
  name: 'Overview Gateway',
  service_type: 'web',
  status: 'active',
  url: 'https://edge.example.invalid',
  labels: ['edge'],
  note: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-20T09:00:00Z',
} satisfies AssetServiceRecord

const VPS_OVERVIEW_DOMAIN = {
  domain_id: 'domain_001',
  vps_id: 'vps_001',
  service_id: 'svc_001',
  domain_name: 'edge.example.com',
  purpose: 'gateway',
  status: 'active',
  registrar: 'Example Registrar',
  auto_renew: true,
  https_enabled: true,
  labels: ['edge'],
  note: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-20T09:00:00Z',
} satisfies AssetDomainRecord

const VPS_OVERVIEW_DETAIL = {
  ...vpsAssetFixture(),
  monitoring_instance_links: [VPS_OVERVIEW_MONITORING],
} satisfies VPSAssetDetail

export function vpsOverviewFixture(overrides: Partial<VPSOverview> = {}): VPSOverview {
  const base: VPSOverview = {
    generated_at: '2026-08-20T09:00:00Z',
    identity: {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_name: 'Example Cloud',
      product_name: 'VPS',
      country: 'JP',
      region: 'Tokyo',
      city: 'Tokyo',
      datacenter: 'TK1',
      ipv4: '192.0.2.10',
      ipv6: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'high',
      labels: ['edge'],
      updated_at: '2026-08-20T09:00:00Z',
    },
    anomalies: [],
    summary: {
      overall: { status: 'healthy', section: { ...EMPTY_SECTION } },
      monitoring: { status: '正常', section: { ...EMPTY_SECTION } },
      ip_quality: { status: 'low', section: { ...EMPTY_SECTION } },
      renewal: { status: 'keep', section: { ...EMPTY_SECTION } },
    },
    recent_activity: {
      section: { ...EMPTY_SECTION },
      items: [{
        activity_id: 'act_e2e_recent',
        event_kind: 'record_created',
        event_at: '2026-08-19T12:00:00Z',
        recorded_at: '2026-08-19T12:00:01Z',
        source_kind: 'record_domain',
        backfilled: false,
        subjects: [],
        presentation: { version: 1, title: 'E2E 最近活动' },
      }],
      snapshot_cursor: 'snap-e2e-opaque',
    },
    facts: [{ key: 'ipv4', label: 'IPv4', value: '192.0.2.10' }],
    relations: [
      {
        kind: 'monitoring_instances', count: 1, status: '正常',
        label: '监控实例', section: { ...EMPTY_SECTION },
      },
      {
        kind: 'subscriptions', count: 1, status: 'keep', route: '/subscriptions?vps_id=vps_001',
        label: '订阅', section: { ...EMPTY_SECTION },
      },
      {
        kind: 'services', count: 1, status: 'active',
        label: '服务', section: { ...EMPTY_SECTION },
      },
      {
        kind: 'domains', count: 1, status: 'active',
        label: '域名', section: { ...EMPTY_SECTION },
      },
    ],
    capabilities: ['records_v2_read'],
  }
  return {
    ...base,
    ...overrides,
    identity: { ...base.identity, ...overrides.identity },
    summary: {
      overall: { ...base.summary.overall, ...overrides.summary?.overall, section: { ...base.summary.overall.section, ...overrides.summary?.overall?.section } },
      monitoring: { ...base.summary.monitoring, ...overrides.summary?.monitoring, section: { ...base.summary.monitoring.section, ...overrides.summary?.monitoring?.section } },
      ip_quality: { ...base.summary.ip_quality, ...overrides.summary?.ip_quality, section: { ...base.summary.ip_quality.section, ...overrides.summary?.ip_quality?.section } },
      renewal: { ...base.summary.renewal, ...overrides.summary?.renewal, section: { ...base.summary.renewal.section, ...overrides.summary?.renewal?.section } },
    },
    recent_activity: {
      ...base.recent_activity,
      ...overrides.recent_activity,
      section: { ...base.recent_activity.section, ...overrides.recent_activity?.section },
      items: overrides.recent_activity?.items ?? base.recent_activity.items,
    },
    anomalies: overrides.anomalies ?? base.anomalies,
    facts: overrides.facts ?? base.facts,
    relations: overrides.relations ?? base.relations,
    capabilities: overrides.capabilities ?? base.capabilities,
  }
}

export function vpsOverviewPartialFixture(): VPSOverview {
  const unavailable = (reasonCode: string) => ({
    state: 'unavailable' as const,
    observed_at: null,
    last_success_at: null,
    reason_code: reasonCode,
  })
  return vpsOverviewFixture({
    summary: {
      overall: { status: 'attention', section: { ...EMPTY_SECTION } },
      monitoring: { status: '正常', section: { ...EMPTY_SECTION } },
      ip_quality: {
        status: '未知',
        section: {
          state: 'stale',
          observed_at: '2026-08-19T08:00:00Z',
          last_success_at: '2026-08-19T08:00:00Z',
          reason_code: 'ip_quality_stale',
        },
      },
      renewal: { status: 'unavailable', section: unavailable('subscription_timeout') },
    },
    recent_activity: {
      section: unavailable('activity_projection_unavailable'),
      items: [],
    },
    relations: [
      {
        kind: 'monitoring_instances', count: 1, status: '正常',
        label: '监控实例', section: { ...EMPTY_SECTION },
      },
      {
        kind: 'subscriptions', count: 0, status: 'unavailable', route: '/subscriptions?vps_id=vps_001',
        label: '订阅', section: unavailable('subscription_timeout'),
      },
      {
        kind: 'services', count: 0, status: 'unavailable',
        label: '服务', section: unavailable('relation_timeout'),
      },
      {
        kind: 'domains', count: 0, status: '',
        label: '域名', section: { ...EMPTY_SECTION },
      },
    ],
  })
}

export function subjectActivityFixture(
  overrides: Partial<SubjectActivityListResponse> = {},
): SubjectActivityListResponse {
  const base: SubjectActivityListResponse = {
    subject: {
      kind: 'vps',
      source_id: 'vps_001',
      identity: { display_name: 'Tokyo Edge' },
      live_route: '/vps/vps_001',
      status: 'live',
    },
    view: 'activity',
    snapshot_cursor: 'snap-e2e-opaque',
    freshness: {
      state: 'ready',
      visible_observed_at: '2026-08-19T12:00:00Z',
      new_items_available: false,
      reason_code: '',
    },
    items: [{
      activity_id: 'act_e2e_1',
      event_kind: 'record_revised',
      event_at: '2026-08-19T12:00:00Z',
      recorded_at: '2026-08-19T12:00:01Z',
      source_kind: 'record_domain',
      backfilled: false,
      subjects: [],
      presentation: { version: 1, title: 'E2E 时间线条目' },
      record_id: 'rec_e2e001',
      revision_id: 'rrv_e2e001',
    }],
    source_statuses: [],
  }
  return {
    ...base,
    ...overrides,
    subject: { ...base.subject, ...overrides.subject, identity: { ...base.subject.identity, ...overrides.subject?.identity } },
    freshness: { ...base.freshness, ...overrides.freshness },
    items: overrides.items ?? base.items,
    source_statuses: overrides.source_statuses ?? base.source_statuses,
  }
}

export function monitoringInstanceDetailProfile(monitoringInstanceId = 'mi_001'): ApiFixtureProfile {
  const record = {
    monitoring_instance_id: monitoringInstanceId,
    display_name: 'Tokyo Monitor',
    group: 'edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Example Cloud',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: [] as string[],
    note: '',
    current_health_status: '正常',
    last_heartbeat_at: '2026-08-20T08:59:00Z',
    last_sync_at: '2026-08-20T09:00:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T09:00:00Z',
  } satisfies MonitoringInstanceRecord
  const runtimeFacts = {
    monitoring_instance_id: monitoringInstanceId,
    latest_host_sample: null,
  }
  const onboarding = {
    ...record,
    phase: '接入完成',
    has_host_sample: false,
    has_accepted_observation: false,
  }
  return authenticatedProfile({
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}`)]: { status: 200, body: record },
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}/runtime-facts?window=realtime`)]: {
      status: 200,
      body: runtimeFacts,
    },
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}/runtime-facts?window=24h`)]: {
      status: 200,
      body: runtimeFacts,
    },
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}/onboarding`)]: {
      status: 200,
      body: onboarding,
    },
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}/vps`)]: { status: 200, body: [] },
    [apiRouteKey('GET', `/api/incidents?object_type=monitoring_instance&object_id=${monitoringInstanceId}`)]: {
      status: 200,
      body: [],
    },
    [apiRouteKey('GET', `/api/events?object_type=monitoring_instance&object_id=${monitoringInstanceId}`)]: {
      status: 200,
      body: { items: [] },
    },
    [apiRouteKey('GET', '/api/settings')]: { status: 200, body: SETTINGS },
  })
}

export function vpsOverviewProfile(options: {
  overview?: VPSOverview
  overviewStatus?: number
  overviewWaitFor?: Promise<void>
  detail?: VPSAssetDetail
} = {}): ApiFixtureProfile {
  const status = options.overviewStatus ?? 200
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/vps/vps_001/overview')]: {
      status,
      body: status >= 400
        ? { error: 'overview unavailable', code: status === 503 ? 'overview_unavailable' : 'resource_not_found' }
        : options.overview ?? vpsOverviewFixture(),
      ...(options.overviewWaitFor ? { waitFor: options.overviewWaitFor } : {}),
    },
    [apiRouteKey('GET', '/api/vps/vps_001')]: {
      status: 200,
      body: options.detail ?? VPS_OVERVIEW_DETAIL,
    },
    [apiRouteKey('GET', '/api/vps/vps_001/monitoring-instances')]: {
      status: 200,
      body: [VPS_OVERVIEW_MONITORING],
    },
    [apiRouteKey('GET', '/api/vps/vps_001/services')]: {
      status: 200,
      body: [VPS_OVERVIEW_SERVICE],
    },
    [apiRouteKey('GET', '/api/vps/vps_001/domains')]: {
      status: 200,
      body: [VPS_OVERVIEW_DOMAIN],
    },
  })
}

export function subjectActivityProfile(options: {
  activity?: SubjectActivityListResponse
  activityStatus?: number
  activityWaitFor?: Promise<void>
  path?: string
} = {}): ApiFixtureProfile {
  const status = options.activityStatus ?? 200
  const path = options.path ?? '/api/subjects/vps/vps_001/activity'
  return authenticatedProfile({
    [apiRouteKey('GET', path)]: {
      status,
      body: status >= 400
        ? { error: 'activity unavailable', code: status === 503 ? 'activity_projection_unavailable' : 'resource_not_found' }
        : options.activity ?? subjectActivityFixture(),
      ...(options.activityWaitFor ? { waitFor: options.activityWaitFor } : {}),
    },
  })
}

export const COMPARISON_E2E_WINDOW = {
  requested_from: '2026-07-01T00:00:00Z',
  requested_to: '2026-07-02T00:00:00Z',
} as const

const COMPARISON_CANDIDATE_KEYS = ['subjects', 'requested_window', 'kinds'] as const
const COMPARISON_EVALUATE_KEYS = [
  'items',
  'baseline_index',
  'alignment',
  'requested_window',
  'tolerance_seconds',
  'detail',
] as const

const COMPARISON_CANDIDATE: ComparisonCandidateItem = {
  subject: { kind: 'vps', id: 'vps_cmpleft' },
  snapshot_id: 'evs_cmpleft',
  record_id: 'rec_cmpleft',
  revision_ids: ['rrv_cmpleft'],
  kind: 'monitoring.host',
  schema_version: 1,
  canonical_hash: 'aa'.repeat(32),
  requested_window: {
    start: COMPARISON_E2E_WINDOW.requested_from,
    end: COMPARISON_E2E_WINDOW.requested_to,
  },
  actual_window: {
    start: COMPARISON_E2E_WINDOW.requested_from,
    end: COMPARISON_E2E_WINDOW.requested_to,
  },
  quality_status: 'complete',
  captured_at: COMPARISON_E2E_WINDOW.requested_to,
  recommendation: 'nearest_window',
}

function comparisonEvaluateFixture(
  overrides: Partial<ComparisonEvaluateResponse> = {},
): ComparisonEvaluateResponse {
  const base: ComparisonEvaluateResponse = {
    digest: 'dd'.repeat(32),
    items: [
      {
        snapshot_id: 'evs_cmpleft',
        canonical_hash: '11'.repeat(32),
        kind: 'monitoring.host',
        schema_version: 1,
        revision_context: 'not_applicable',
      },
      {
        snapshot_id: 'evs_cmpright',
        canonical_hash: '22'.repeat(32),
        kind: 'monitoring.host',
        schema_version: 1,
        revision_context: 'not_applicable',
      },
    ],
    review: [],
    available_kinds: [
      { kind: 'monitoring.host', schema_version: 1 },
      { kind: 'monitoring.probe', schema_version: 2 },
    ],
    pairwise: [],
    series: [],
    save_eligibility: { eligible: true, blockers: [] },
    comparison_intent: {
      token: 'cmp1.e2e.payload.mac',
      key_id: 'cmp_e2e',
      issued_at: '2026-08-20T10:00:00Z',
      expires_at: '2026-08-20T10:15:00Z',
    },
  }
  return {
    ...base,
    ...overrides,
    items: overrides.items ?? base.items,
    review: overrides.review ?? base.review,
    available_kinds: overrides.available_kinds ?? base.available_kinds,
    pairwise: overrides.pairwise ?? base.pairwise,
    series: overrides.series ?? base.series,
    save_eligibility: overrides.save_eligibility ?? base.save_eligibility,
  }
}

export function comparisonWorkbenchHref(
  state: Omit<ComparisonURLState, 'version' | 'requested_from' | 'requested_to'> & {
    requested_from?: string
    requested_to?: string
  },
): string {
  return comparisonHref({
    version: COMPARISON_URL_VERSION,
    requested_from: COMPARISON_E2E_WINDOW.requested_from,
    requested_to: COMPARISON_E2E_WINDOW.requested_to,
    ...state,
  })
}

export type ComparisonWorkbenchMode =
  | 'candidates'
  | 'host-partial'
  | 'metadata-only'
  | 'incompatible'
  | 'revoked'

export function comparisonWorkbenchProfile(options: {
  mode: ComparisonWorkbenchMode
  compareWaitFor?: Promise<void>
  includeSave?: boolean
} = { mode: 'host-partial' }): ApiFixtureProfile {
  const evaluate = options.mode === 'metadata-only'
    ? comparisonEvaluateFixture({
      items: [
        {
          snapshot_id: 'evs_cmpleft',
          canonical_hash: '11'.repeat(32),
          kind: 'command.audit',
          schema_version: 1,
          revision_context: 'bound',
          revision: {
            record_type: 'note',
            business_status: 'open',
            status_group: 'open',
            impact_level: 'low',
            occurred_at: null,
          },
        },
        {
          snapshot_id: 'evs_cmpright',
          canonical_hash: '22'.repeat(32),
          kind: 'command.audit',
          schema_version: 1,
          revision_context: 'bound',
          revision: {
            record_type: 'note',
            business_status: 'open',
            status_group: 'open',
            impact_level: 'low',
            occurred_at: null,
          },
        },
      ],
      review: [{ item_index: 0, kind: 'command.audit', schema_version: 1, reason: 'metadata_only' }],
      available_kinds: [{ kind: 'command.audit', schema_version: 1 }],
      pairwise: [{
        item_index: 1,
        kind: 'command.audit',
        schema_version: 1,
        compatible: true,
        reason: '',
        values: { count: 0 },
      }],
    })
    : options.mode === 'incompatible'
      ? comparisonEvaluateFixture({
        review: [{
          item_index: 1,
          kind: 'monitoring.host',
          schema_version: 1,
          reason: 'schema_incompatible',
        }],
        available_kinds: [],
        pairwise: [{
          item_index: 1,
          kind: 'monitoring.host',
          schema_version: 1,
          compatible: false,
          reason: 'schema_incompatible',
          values: {},
        }],
        series: [],
      })
      : comparisonEvaluateFixture({
        review: [{
          item_index: 0,
          kind: 'monitoring.host',
          schema_version: 1,
          reason: 'coverage_partial',
        }],
        series: [{
          item_index: 0,
          metric_id: 'cpu_usage_pct',
          unit: '%',
          segments: [
            [{ start: '2026-07-01T00:00:00Z', end: '2026-07-01T00:05:00Z', value: 12 }],
            [{ start: '2026-07-01T00:20:00Z', end: '2026-07-01T00:25:00Z', value: 18 }],
          ],
        }],
      })

  const draft: RecordDraft = {
    draft_id: 'rdf_cmp_save',
    etag: 'rdt1_cmp_save',
    payload: {
      title: '',
      body_markdown: '',
      markdown_dialect_version: 1,
      record_type: 'note',
      business_status: '',
      impact_level: 'medium',
      visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
      subjects: [],
      tags: [],
      attachment_ids: [],
      owner_id: AUTHENTICATED_USER.user_id,
      participant_ids: [],
      save_reason: '',
    },
    version: 1,
    warning_at: '2026-10-20T00:00:00Z',
    created_at: '2026-08-20T10:00:00Z',
    updated_at: '2026-08-20T10:00:00Z',
    expires_at: '2026-11-01T10:00:00Z',
  }

  const saved: RecordMutationResult = {
    record_id: 'rec_cmpsaved01',
    revision_id: 'rrv_cmpsaved01',
    revision_no: 1,
    lock_version: 1,
    authorization_epoch: 1,
    lifecycle: 'active',
    created: true,
    replayed: false,
    committed_at: '2026-08-20T10:01:00Z',
  }

  return authenticatedProfile({
    [apiRouteKey('POST', '/api/evidence/comparison-candidates')]: {
      status: 200,
      body: {
        subjects: [
          { kind: 'vps', id: 'vps_cmpleft' },
          { kind: 'vps', id: 'vps_cmpright' },
        ],
        candidates: [
          COMPARISON_CANDIDATE,
          {
            ...COMPARISON_CANDIDATE,
            subject: { kind: 'vps', id: 'vps_cmpright' },
            snapshot_id: 'evs_cmpright',
            record_id: 'rec_cmpright',
            revision_ids: ['rrv_cmpright'],
          },
        ],
      },
      expectedBodyKeys: COMPARISON_CANDIDATE_KEYS,
    },
    [apiRouteKey('POST', '/api/evidence/comparisons')]: options.mode === 'revoked'
      ? {
        status: 404,
        body: { error: 'resource not found', code: 'resource_not_found', snapshot_id: 'evs_restricted' },
        expectedBodyKeys: COMPARISON_EVALUATE_KEYS,
      }
      : {
        status: 200,
        body: evaluate,
        expectedBodyKeys: COMPARISON_EVALUATE_KEYS,
        ...(options.compareWaitFor ? { waitFor: options.compareWaitFor } : {}),
      },
    ...(options.includeSave
      ? {
        [apiRouteKey('POST', '/api/record-drafts')]: {
          status: 200,
          body: draft,
          expectedBodyKeys: ['payload'],
        },
        [apiRouteKey('POST', '/api/records')]: {
          status: 200,
          body: saved,
          expectedBodyKeys: ['record_id', 'draft_id', 'draft_etag', 'comparison_intent'],
        },
      }
      : {}),
  })
}

export function recordSearchProfile(): ApiFixtureProfile {
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/records/search')]: {
      status: 200,
      body: {
        items: [{
          record_id: 'rec_e2e001',
          lifecycle: 'active',
          current_revision_id: RECORD_REVISION.revision_id,
          lock_version: 4,
          authorization_epoch: 2,
          current: RECORD_REVISION,
          capabilities: {
            read: true,
            update: true,
            archive: true,
            restore: true,
            draft: true,
            permanent_delete: false,
          },
          created_at: RECORD_TIMESTAMP,
          updated_at: RECORD_TIMESTAMP,
        }],
        generation: 1,
      },
    },
    [apiRouteKey('POST', '/api/record-export-previews')]: {
      status: 200,
      body: {
        preview_id: 'rej_e2e001',
        preview_token: 'tok',
        export_kind: 'markdown',
        export_mode: 'safe',
        inventory_digest: 'aa',
        expected_files: [{ name: 'record.md', media_type: 'text/markdown', byte_size: 12 }],
        unavailable: [],
        expires_at: '2026-08-21T13:00:00Z',
      },
      expectedBodyKeys: ['record_id', 'export_kind', 'export_mode'],
    },
  })
}
