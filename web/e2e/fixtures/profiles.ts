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
  MonitoringInstanceRuntimeFacts,
  MonitoringInstanceRuntimeSummariesResponse,
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
  },
  ip_quality_settings: {
    enabled: true,
    frequency_seconds: 86_400,
    stale_after_seconds: 604_800,
    timeout_seconds: 15,
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

const MONITORING_RUNTIME_SUMMARIES = {
  read_at: '2026-08-20T09:00:00Z',
  monitoring_instances: {},
} satisfies MonitoringInstanceRuntimeSummariesResponse

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

const RECORD_PEER_ID = 'usr_89abcdef0123456701234567'

const RECORD_RICH_BODY_MARKDOWN = [
  '## 现象',
  '',
  '第三晚 21:40 起，alpha 到上游的 TCP 重传率升高，业务侧出现间歇超时。',
  '',
  '## 排查',
  '',
  '```sh',
  '# 复现丢包',
  'mtr -rw 203.0.113.7',
  '```',
  '',
  '| 主机 | 丢包 | 延迟 | 抖动 | 重传率 | 上游第二跳 | 备注 |',
  '| --- | --- | --- | --- | --- | --- | --- |',
  '| alpha | 3% | 182 ms | 41 ms | 2.4% | 198.51.100.17 | 21:40 起持续 |',
  '| beta | 0% | 41 ms | 3 ms | 0.1% | 198.51.100.29 | 对照组 |',
  '',
  '- [x] 确认监控告警时间线',
  '- [ ] 联系服务商核对上游线路',
  '',
  '## 结论',
  '',
  '丢包集中在服务商第二跳，已提交工单，等待回复。',
  '',
  '<!-- houfeng-ref:v1 evidence evs_e2ethirdnight -->',
  `[系统证据：第三晚 TCP 观测](houfeng-evidence:${RECORD_EVIDENCE_ID})`,
].join('\n')

const inlineText = (value: string) => [{ type: 'text', text: value }]

/** A populated record: sectioned body, named participants and a revision to compare against. */
const RECORD_RICH_REVISION = {
  ...RECORD_REVISION,
  revision_id: 'rrv_e2e002',
  revision_no: 3,
  body_markdown: RECORD_RICH_BODY_MARKDOWN,
  render_model: {
    version: 'houfeng_markdown/v1',
    nodes: [
      { type: 'heading', level: 2, children: inlineText('现象') },
      { type: 'paragraph', children: inlineText('第三晚 21:40 起，alpha 到上游的 TCP 重传率升高，业务侧出现间歇超时。') },
      { type: 'heading', level: 2, children: inlineText('排查') },
      { type: 'fenced_code', text: '# 复现丢包\nmtr -rw 203.0.113.7\n' },
      {
        type: 'table',
        header: ['主机', '丢包', '延迟', '抖动', '重传率', '上游第二跳', '备注'].map(inlineText),
        rows: [
          ['alpha', '3%', '182 ms', '41 ms', '2.4%', '198.51.100.17', '21:40 起持续'].map(inlineText),
          ['beta', '0%', '41 ms', '3 ms', '0.1%', '198.51.100.29', '对照组'].map(inlineText),
        ],
      },
      {
        type: 'task_list',
        items: [
          { checked: true, children: inlineText('确认监控告警时间线') },
          { checked: false, children: inlineText('联系服务商核对上游线路') },
        ],
      },
      { type: 'heading', level: 2, children: inlineText('结论') },
      { type: 'paragraph', children: inlineText('丢包集中在服务商第二跳，已提交工单，等待回复。') },
      {
        type: 'reference',
        kind: 'evidence',
        id: RECORD_EVIDENCE_ID,
        children: inlineText('系统证据：第三晚 TCP 观测'),
      },
    ],
  },
  status_group: 'in_progress',
  participants: [
    { participant_id: RECORD_USER_ID, display_name: '林岚' },
    { participant_id: RECORD_PEER_ID, display_name: '周衡' },
  ],
  save_reason: '补充结论',
  created_at: '2026-08-18T02:30:00Z',
}

const RECORD_POPULATED_ACTIONS = [
  {
    action_id: 'ract_e2e001', record_id: 'rec_e2e001', version: 2, status: 'open',
    title: '联系服务商核对上游线路', details: '附上 mtr 结果与告警时间线，要求确认第二跳丢包原因。',
    assignee_id: RECORD_PEER_ID, due_at: '2026-08-20T09:00:00Z', completed_at: null,
    subject_revision_id: 'rrv_e2e002', created_at: '2026-08-18T02:40:00Z', updated_at: '2026-08-18T03:00:00Z',
  },
  {
    action_id: 'ract_e2e002', record_id: 'rec_e2e001', version: 3, status: 'completed',
    title: '确认监控告警时间线', details: '',
    assignee_id: RECORD_USER_ID, due_at: null, completed_at: '2026-08-18T04:00:00Z',
    subject_revision_id: '', created_at: '2026-08-18T02:35:00Z', updated_at: '2026-08-18T04:00:00Z',
  },
]

function commentModel(value: string) {
  return { version: 'comment_markdown/v1', nodes: [{ type: 'paragraph', children: inlineText(value) }] }
}

const RECORD_POPULATED_COMMENTS = [
  {
    comment_id: 'rcm_e2e001', record_id: 'rec_e2e001', author_id: RECORD_PEER_ID, version: 1, state: 'active',
    body_markdown: '服务商回复第二跳在做线路割接，预计今晚结束。',
    render_model: commentModel('服务商回复第二跳在做线路割接，预计今晚结束。'),
    reply_to_comment_id: '', mention_user_ids: [], created_at: '2026-08-18T05:00:00Z',
    updated_at: '2026-08-18T05:00:00Z', redacted_at: null,
  },
  {
    comment_id: 'rcm_e2e002', record_id: 'rec_e2e001', author_id: RECORD_USER_ID, version: 1, state: 'active',
    body_markdown: '收到，割接后再跑一轮 mtr 对比。',
    render_model: commentModel('收到，割接后再跑一轮 mtr 对比。'),
    reply_to_comment_id: 'rcm_e2e001', mention_user_ids: [], created_at: '2026-08-18T05:20:00Z',
    updated_at: '2026-08-18T05:20:00Z', redacted_at: null,
  },
]

/**
 * A published record that actually carries materials, a fenced snippet and a table.
 * The `/records/new` profile cannot exercise reading, layout switching or a populated
 * material drawer, so those paths need their own served record.
 */
export function recordDetailProfile(options: {
  renderModel?: 'ready' | 'unsupported'
  /** Sectioned body, named participants, actions, comments and an older revision. */
  populated?: boolean
} = {}): ApiFixtureProfile {
  const revision = options.populated
    ? RECORD_RICH_REVISION
    : options.renderModel === 'unsupported' ? RECORD_UNSUPPORTED_REVISION : RECORD_REVISION
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/records/rec_e2e001')]: {
      status: 200,
      body: {
        record_id: 'rec_e2e001',
        lifecycle: 'active',
        current_revision_id: revision.revision_id,
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
      body: { items: options.populated ? RECORD_POPULATED_ACTIONS : [] },
    },
    [apiRouteKey('GET', '/api/records/rec_e2e001/comments?limit=100')]: {
      status: 200,
      body: { comments: options.populated ? RECORD_POPULATED_COMMENTS : [] },
    },
    [apiRouteKey('GET', '/api/records/rec_e2e001/watch')]: {
      status: 200,
      body: options.populated
        ? {
          record_id: 'rec_e2e001',
          user_id: RECORD_USER_ID,
          version: 2,
          preference: 'default',
          sources: {
            author: true, owner: true, participant: false, comment: true, mention: false, action: false,
          },
          updated_at: '2026-08-18T05:20:00Z',
        }
        : {
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
    ...(options.populated ? {
      [apiRouteKey('GET', `/api/records/rec_e2e001/revisions/${RECORD_REVISION.revision_id}`)]: {
        status: 200,
        body: RECORD_REVISION,
      },
    } : {}),
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

/** 带异常对象、趋势、最近事件与续费队列的工作台，用于验证有界面板与桌面布局。 */
export function dashboardPopulatedProfile(now = Date.now()): ApiFixtureProfile {
  const hour = 60 * 60 * 1000
  const day = 24 * hour
  const iso = (offset: number) => new Date(now + offset).toISOString()
  const dashboard = dashboardOverviewFixture({
    snapshot_generated_at: iso(-2 * 60 * 1000),
    total_monitoring_instance_count: 12,
    total_target_count: 6,
    abnormal_monitoring_instance_count: 2,
    severe_monitoring_instance_count: 1,
    abnormal_target_count: 1,
    abnormal_monitoring_instances: [
      {
        monitoring_instance_id: 'mi_fra',
        display_name: 'Frankfurt-02',
        group: 'edge',
        region: 'eu-central',
        city: 'Falkenstein',
        provider: 'Hetzner',
        lifecycle_status: '在用',
        monitoring_status: '启用',
        current_health_status: '严重',
        last_heartbeat_at: iso(-18 * 60 * 1000),
        current_active_incident_count: 1,
        current_primary_issue_summary: '心跳中断 18 分钟',
      },
      {
        monitoring_instance_id: 'mi_hk',
        display_name: 'HK-Relay',
        group: 'relay',
        region: 'ap-east',
        city: 'Hong Kong',
        provider: 'DMIT',
        lifecycle_status: '在用',
        monitoring_status: '启用',
        current_health_status: '告警',
        last_heartbeat_at: iso(-30 * 1000),
        current_active_incident_count: 1,
        current_primary_issue_summary: '磁盘使用率 87%',
      },
    ],
    abnormal_targets: [{
      target_id: 'tg_edge',
      name: 'edge.example.com',
      target_type: 'service',
      host: 'edge.example.com',
      base_port: 443,
      run_status: '启用',
      group: 'prod',
      current_health_status: '告警',
      last_failure_at: iso(-5 * 60 * 1000),
      current_active_incident_count: 1,
      current_primary_issue_summary: 'HTTPS 响应超过 2 秒',
    }],
    new_incident_trend_24h: [0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 1, 0, 1],
    recovery_trend_24h: Array.from({ length: 24 }, (_, index) => (index % 7 === 0 ? 1 : 0)),
    recent_events: Array.from({ length: 7 }, (_, index) => ({
      event_id: `ev_${index}`,
      incident_id: `inc_${index}`,
      incident_class: 'heartbeat',
      object_type: 'monitoring_instance',
      object_id: 'mi_fra',
      event_type: index % 3 === 2 ? 'incident_recovered' : 'incident_started',
      severity: index === 0 ? '严重' : '告警',
      summary: index === 0 ? 'Frankfurt-02 心跳超时' : `事件摘要 ${index}`,
      // 覆盖 24 小时以外的旧事件：最近动态不限时间窗口。
      created_at: iso(-(index + 1) * 11 * hour),
    })),
  })
  const renewals = Array.from({ length: 7 }, (_, index) => ({
    subscription_id: `sub_${index}`,
    vps_id: `vps_${index}`,
    vps_display_name: `VPS ${index}`,
    // 第二条是超长且不可断行的名称，验证可操作标题完整换行而不是被省略。
    display_name: ['LA-Bench', 'tokyo-edge-production-gateway-primary-node-with-a-very-long-unbroken-identifier', 'Frankfurt-02', 'Seoul-Proxy', 'SJC-Build', 'Paris-Git', 'NYC-Monitor'][index] ?? `VPS ${index}`,
    provider_name: ['RackNerd', 'Vultr', 'Hetzner-Online-GmbH-Falkenstein-Datacenter-Park-Region-EU-Central', 'Vultr', 'DigitalOcean', 'Scaleway', 'Linode'][index] ?? '',
    // 生产续费日是 YYYY-MM-DD 日历日期。
    renew_at: iso((index * 6 + 3) * day).slice(0, 10),
    monthly_price_base: index === 3 ? null : 12 + index * 7,
    yearly_price_base: null,
    base_currency: 'CNY',
    currency: 'USD',
    renewal_decision: 'keep',
    lifecycle_status: 'active',
    exchange_rate_stale: false,
  }))
  return dashboardProfile({
    dashboard,
    subscription: subscriptionOverviewFixture({ upcoming_renewals: renewals, renewal_due_30d_count: 5 }),
  })
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
        [apiRouteKey('GET', '/api/vps?renewal_decision=cancel')]: { status: 200, body: [] },
      })
    case '/monitoring':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/monitoring-instances')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/monitoring-instances/sparklines?metrics=cpu_usage_pct,mem_used_pct,disk_used_pct&window=24h&downsample=24')]: {
          status: 200,
          body: MONITORING_SPARKLINES,
        },
        [apiRouteKey('GET', '/api/monitoring-instances/runtime-summaries')]: {
          status: 200,
          body: MONITORING_RUNTIME_SUMMARIES,
        },
        [apiRouteKey('GET', '/api/settings')]: { status: 200, body: SETTINGS },
      })
    case '/targets':
      return authenticatedProfile({
        [apiRouteKey('GET', '/api/targets')]: { status: 200, body: [] },
        [apiRouteKey('GET', '/api/targets?scope=all')]: { status: 200, body: [] },
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
  lifecycle_status: '已接入',
  monitoring_status: '启用',
  binding_status: '已绑定',
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
      usage_tags: ['业务'],
      validity_mode: 'unknown',
      expires_at: null,
      auto_renew_check: 'unchecked',
      auto_renew_checked_at: null,
      renewal_reason: '',
      renewal_review_at: null,
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
      section: {
        state: 'ready',
        observed_at: '2026-08-19T12:00:01Z',
        last_success_at: '2026-08-19T12:00:01Z',
        reason_code: '',
      },
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
        kind: 'subscriptions', count: 1, status: 'keep', route: '/subscriptions?vps_id=vps_001&view=details',
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
        kind: 'subscriptions', count: 0, status: 'unavailable', route: '/subscriptions?vps_id=vps_001&view=details',
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
    lifecycle_status: '已接入',
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
  const runtimeFacts24h = {
    monitoring_instance_id: monitoringInstanceId,
    read_at: '2026-08-20T09:00:00Z',
    window: {
      key: '24h',
      started_at: '2026-08-19T09:00:00Z',
      ended_at: '2026-08-20T09:00:00Z',
      bucket_count: 288,
      available_started_at: null,
      available_ended_at: null,
      sample_count: 0,
    },
    latest_host_sample: null,
    host_metric_points: [],
    recent_host_samples: [],
  } satisfies MonitoringInstanceRuntimeFacts
  const runtimeFactsRealtime = {
    monitoring_instance_id: monitoringInstanceId,
    read_at: '2026-08-20T09:00:00Z',
    window: {
      key: 'realtime',
      started_at: '2026-08-20T08:00:00Z',
      ended_at: '2026-08-20T09:00:00Z',
      bucket_count: 720,
      available_started_at: null,
      available_ended_at: null,
      sample_count: 0,
    },
    latest_host_sample: null,
    host_metric_points: [],
    recent_host_samples: [],
  } satisfies MonitoringInstanceRuntimeFacts
  const onboarding = {
    ...record,
    phase: '接入完成',
    has_host_sample: false,
    has_accepted_observation: false,
  }
  return authenticatedProfile({
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}`)]: { status: 200, body: record },
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}/management-review`)]: {
      status: 200,
      body: {
        record,
        active_vps_links: [],
        counts: {
          heartbeat_count: 1, host_sample_count: 0, probe_observation_count: 0,
          host_sample_daily_aggregate_count: 0, ip_quality_report_count: 0,
          active_incident_count: 0, state_change_event_count: 0, notification_record_count: 0,
          asset_lifecycle_action_step_count: 0, command_action_audit_count: 0, active_vps_link_count: 1,
        },
        action_reviews: { retire: { allowed: true, blockers: [], warnings: [] } },
        dependency_impacts: [],
        preview_digest: `review-${monitoringInstanceId}`,
        empty_mistake_candidate: false,
      },
    },
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}/runtime-facts?window=realtime`)]: {
      status: 200,
      body: runtimeFactsRealtime,
    },
    [apiRouteKey('GET', `/api/monitoring-instances/${monitoringInstanceId}/runtime-facts?window=24h`)]: {
      status: 200,
      body: runtimeFacts24h,
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
  subscriptions?: readonly SubscriptionRecord[]
  subscriptionsStatus?: number
  subscriptionsWaitFor?: Promise<void>
  services?: readonly AssetServiceRecord[]
  servicesStatus?: number
  servicesWaitFor?: Promise<void>
  domains?: readonly AssetDomainRecord[]
  domainsStatus?: number
  domainsWaitFor?: Promise<void>
} = {}): ApiFixtureProfile {
  const overviewStatus = options.overviewStatus ?? 200
  const subscriptionsStatus = options.subscriptionsStatus ?? 200
  const servicesStatus = options.servicesStatus ?? 200
  const domainsStatus = options.domainsStatus ?? 200
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/vps/vps_001/overview')]: {
      status: overviewStatus,
      body: overviewStatus >= 400
        ? { error: 'overview unavailable', code: overviewStatus === 503 ? 'overview_unavailable' : 'resource_not_found' }
        : options.overview ?? vpsOverviewFixture(),
      ...(options.overviewWaitFor ? { waitFor: options.overviewWaitFor } : {}),
    },
    [apiRouteKey('GET', '/api/vps/vps_001')]: {
      status: 200,
      body: options.detail ?? VPS_OVERVIEW_DETAIL,
    },
    [apiRouteKey('GET', '/api/vps/vps_001/monitoring-instances?scope=current')]: {
      status: 200,
      body: [VPS_OVERVIEW_MONITORING],
    },
    [apiRouteKey('GET', '/api/vps/vps_001/monitoring-instances?scope=all')]: {
      status: 200,
      body: [VPS_OVERVIEW_MONITORING],
    },
    [apiRouteKey('GET', '/api/vps/vps_001/service-associations')]: { status: 200, body: [] },
    [apiRouteKey('GET', '/api/vps/vps_001/domain-associations')]: { status: 200, body: [] },
    [apiRouteKey('GET', '/api/services')]: { status: 200, body: options.services ?? [VPS_OVERVIEW_SERVICE] },
    [apiRouteKey('GET', '/api/services?vps_id=vps_001')]: { status: 200, body: options.services ?? [VPS_OVERVIEW_SERVICE] },
    [apiRouteKey('GET', '/api/domains')]: { status: 200, body: options.domains ?? [VPS_OVERVIEW_DOMAIN] },
    [apiRouteKey('GET', '/api/targets')]: { status: 200, body: [] },
    [apiRouteKey('GET', '/api/vps/vps_001/services')]: {
      status: servicesStatus,
      body: servicesStatus >= 400 ? { error: 'services unavailable' } : options.services ?? [VPS_OVERVIEW_SERVICE],
      ...(options.servicesWaitFor ? { waitFor: options.servicesWaitFor } : {}),
    },
    [apiRouteKey('GET', '/api/vps/vps_001/domains')]: {
      status: domainsStatus,
      body: domainsStatus >= 400 ? { error: 'domains unavailable' } : options.domains ?? [VPS_OVERVIEW_DOMAIN],
      ...(options.domainsWaitFor ? { waitFor: options.domainsWaitFor } : {}),
    },
    [apiRouteKey('GET', '/api/subscriptions?vps_id=vps_001&sort=renew_at&order=asc')]: {
      status: subscriptionsStatus,
      body: subscriptionsStatus >= 400 ? { error: 'subscriptions unavailable' } : options.subscriptions ?? [SUBSCRIPTION],
      ...(options.subscriptionsWaitFor ? { waitFor: options.subscriptionsWaitFor } : {}),
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

/** Hourly buckets per segment; a one-hour hole separates consecutive segments. */
function comparisonTrendSeries(itemIndex: number, origin: string, segments: number[][]) {
  const start = Date.parse(origin)
  let hour = 0
  return {
    item_index: itemIndex,
    metric_id: 'cpu_usage_pct',
    unit: '%',
    segments: segments.map((values) => {
      const points = values.map((value) => {
        const from = new Date(start + hour * 3_600_000).toISOString().replace('.000Z', 'Z')
        const to = new Date(start + (hour + 1) * 3_600_000).toISOString().replace('.000Z', 'Z')
        hour += 1
        return { start: from, end: to, value }
      })
      hour += 1
      return points
    }),
  }
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
  | 'host-trend'
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
      : options.mode === 'host-trend'
        ? comparisonEvaluateFixture({
          review: [{
            item_index: 1,
            kind: 'monitoring.host',
            schema_version: 1,
            reason: 'coverage_partial',
          }],
          pairwise: [{
            item_index: 1,
            kind: 'monitoring.host',
            schema_version: 1,
            compatible: true,
            reason: '',
            values: { equal: false, matched: 10, unmatched_baseline: 0, unmatched_item: 2, deltas: [{ delta: 4 }, { delta: 0 }] },
          }],
          series: [
            comparisonTrendSeries(0, '2026-07-01T00:00:00Z', [[12, 14, 13, 18, 22, 19, 17, 15, 16, 14, 13, 12]]),
            comparisonTrendSeries(1, '2026-07-08T00:00:00Z', [[20, 24, 27, 31], [35, 33, 29, 26, 24, 22]]),
          ],
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

export type EvidenceSnapshotFixtureKind =
  | 'monitoring.host'
  | 'monitoring.probe'
  | 'monitoring.event'
  | 'ip_quality.report'
  | 'subscription.cost'
  | 'command.audit'
  | 'unsupported'

const EVIDENCE_QUALITY = {
  status: 'complete',
  partial: false,
  truncated: false,
  sample_count: 12,
  maintenance_count: 0,
  backfilled_count: 0,
  bucket_count: 12,
  gap_count: 0,
  peak_count: 0,
  data_point_count: 24,
}

function evidenceMonitoringReadModel(probe: boolean) {
  const start = Date.parse('2026-08-17T13:00:00Z')
  const iso = (minutes: number) => new Date(start + minutes * 60_000).toISOString().replace('.000Z', 'Z')
  const cpu = [18, 21, 24, 35, 52, 71, 64, 48, 39, 33, 29, 26]
  const mem = [61, 61, 62, 63, 66, 70, 71, 69, 67, 66, 65, 65]
  const latency = [41, 44, 43, 58, 96, 182, 170, 121, 88, 62, 49, 45]
  const buckets = cpu.flatMap((value, index) => {
    if (index === 6) return []
    const metrics = probe
      ? [{ name: 'latency_ms', unit: 'ms', average: latency[index], min: latency[index], max: latency[index] }]
      : [
        { name: 'cpu_usage_pct', unit: 'percent', average: value, min: value, max: value },
        { name: 'mem_used_pct', unit: 'percent', average: mem[index], min: mem[index], max: mem[index] },
      ]
    return [{
      series_id: probe ? 'probe-tcp-443' : 'host-alpha',
      series_kind: probe ? 'tcp' : 'host',
      start: iso(index * 5),
      end: iso(index * 5 + 5),
      source_layer: 'raw',
      source_granularity_seconds: 300,
      sample_count: 1,
      maintenance_count: 0,
      backfilled_count: 0,
      metrics,
    }]
  })
  return {
    version: probe ? 'monitoring_probe_read_model/v1' : 'monitoring_host_read_model/v1',
    requested_start: iso(0),
    requested_end: iso(60),
    coverage_start: iso(0),
    coverage_end: iso(60),
    actual_precision_seconds: 300,
    buckets,
    gaps: [{ series_id: probe ? 'probe-tcp-443' : 'host-alpha', start: iso(30), end: iso(35) }],
    peaks: [{
      series_id: probe ? 'probe-tcp-443' : 'host-alpha',
      metric: probe ? 'latency_ms' : 'cpu_usage_pct',
      at: iso(25),
      value: probe ? 182 : 71,
      source_layer: 'raw',
    }],
    // 质量计数必须与桶逐项一致，否则严格解码器拒绝整个读模型。
    quality: {
      ...EVIDENCE_QUALITY,
      status: 'partial',
      partial: true,
      sample_count: buckets.length,
      bucket_count: buckets.length,
      data_point_count: buckets.reduce((total, bucket) => total + bucket.metrics.length, 0),
      gap_count: 1,
      peak_count: 1,
    },
  }
}

function evidenceReadModelFor(kind: Exclude<EvidenceSnapshotFixtureKind, 'unsupported'>): {
  schema_version: number
  renderer_version: string
  title: string
  read_model: unknown
} {
  switch (kind) {
    case 'monitoring.host':
      return { schema_version: 1, renderer_version: 'monitoring_host_v1', title: '第三晚主机负载', read_model: evidenceMonitoringReadModel(false) }
    case 'monitoring.probe':
      return { schema_version: 2, renderer_version: 'monitoring_probe_v2', title: '443 端口探测延迟', read_model: evidenceMonitoringReadModel(true) }
    case 'monitoring.event':
      return {
        schema_version: 2,
        renderer_version: 'monitoring_event_v2',
        title: '第三晚告警事件',
        read_model: {
          version: 'monitoring_event_read_model/v2',
          quality_status: 'complete',
          event_count: 3,
          backfilled_count: 1,
          events: [
            ['evt_e2e001', 'incident_started', '告警', 'TCP 重传率升高', '2026-08-17T13:25:00Z', false, 'normal', 'alert'],
            ['evt_e2e002', 'incident_escalated', '严重', 'TCP 重传率持续高于 2%', '2026-08-17T13:40:00Z', false, 'alert', 'critical'],
            ['evt_e2e003', 'incident_recovered', '严重', 'TCP 重传率恢复正常', '2026-08-17T14:05:00Z', true, 'critical', 'normal'],
          ].map(([id, type, severity, summary, at, backfilled, prior, resulting]) => ({
            event_id: id,
            object_type: 'monitoring_instance',
            object_id: 'mi_e2ealpha',
            event_type: type,
            severity,
            summary,
            event_at: at,
            recorded_at: at,
            backfilled,
            provenance: 'center',
            producer_version: 'center-monitoring-events/v1',
            rule_version: 'incident-rules/v1',
            prior_state: prior,
            resulting_state: resulting,
            correction_of_event_id: '',
            metrics: [],
          })),
        },
      }
    case 'ip_quality.report': {
      const provider = (name: string, risk: string, proxy: boolean, status = 'success') => ({
        provider: name,
        status,
        source_type: 'default',
        latency_ms: 120,
        usage_type: 'hosting',
        company_type: 'hosting',
        risk_level: status === 'success' ? risk : '',
        risk_score: status === 'success' ? '12' : '',
        is_proxy: proxy,
        is_tor: false,
        is_vpn: false,
        is_server: true,
        is_abuser: false,
        is_robot: false,
        error_code: status === 'success' ? '' : 'http_status',
      })
      const service = (name: string, status: string) => ({
        service: name,
        source: 'default',
        status,
        probe_status: status === 'unknown' ? 'failure' : 'success',
        latency_ms: 240,
        unlock_type: status === 'unlocked' ? 'full' : '',
        error_code: status === 'unknown' ? 'http_status' : '',
      })
      return {
        schema_version: 1,
        renderer_version: 'ip_quality_report_v1',
        title: '出口 IP 质量',
        read_model: {
          version: 'ip_quality_report_read_model/v1',
          report_id: 'ipq_e2ethirdnight',
          observed_at: '2026-08-17T13:50:00Z',
          received_at: '2026-08-17T13:50:02Z',
          ip_version: 4,
          status: 'partial',
          stale: false,
          stale_after_seconds: 604800,
          risk_level: 'medium',
          coverage: {
            expected_provider_count: 4,
            successful_provider_count: 3,
            failed_provider_count: 1,
            skipped_provider_count: 0,
            not_configured_provider_count: 0,
            expected_service_count: 4,
            successful_service_count: 3,
            failed_service_count: 1,
            skipped_service_count: 0,
            not_configured_service_count: 0,
          },
          providers: [
            provider('ipapi.is', 'medium', true),
            provider('proxycheck.io', 'low', false),
            provider('ip2location.io', 'low', false),
            provider('scamalytics', '', false, 'failure'),
          ],
          services: [
            service('Netflix', 'unlocked'),
            service('ChatGPT', 'unlocked'),
            service('Disney+', 'blocked'),
            service('Reddit', 'unknown'),
          ],
          quality: { ...EVIDENCE_QUALITY, status: 'partial', partial: true, sample_count: 1, bucket_count: 1, data_point_count: 9 },
        },
      }
    }
    case 'subscription.cost':
      return {
        schema_version: 1,
        renderer_version: 'subscription_cost_v1',
        title: '八月订阅成本',
        read_model: {
          version: 'subscription_cost_read_model/v1',
          subscription_id: 'sub_e2ealpha',
          vps_id: 'vps_0123456789abcdef',
          original_amount: 18.5,
          original_currency: 'USD',
          billing_period_unit: 'month',
          billing_period_length: 1,
          conversion_rate: 132.5 / 18.5,
          conversion_provider: 'fixer',
          rate_date: '2026-08-01',
          rate_fetched_at: '2026-08-16T00:00:00Z',
          rate_stale: false,
          base_amount: 132.5,
          base_currency: 'CNY',
          budget_source: 'subscription_monthly_budgets',
          budget_currency: 'CNY',
          budget_month: '2026-08',
          budget_monthly_limit: 1000,
          budget_warning_pct: 80,
          budget_status: 'ok',
          budget_actual_spend: 612.4,
          coverage_start: '2026-08-01T00:00:00Z',
          coverage_end: '2026-09-01T00:00:00Z',
          coverage_status: 'complete',
          covered_days: 31,
          total_days: 31,
          converted_subscription_count: 1,
          missing_rate_count: 0,
        },
      }
    case 'command.audit':
      return {
        schema_version: 1,
        renderer_version: 'command_audit_v1',
        title: '第三晚诊断命令',
        read_model: {
          version: 'command_audit_read_model/v1',
          audit_count: 3,
          command_result_retention_seconds: 86400,
          command_result_payload_allowed: false,
          audits: [
            ['audit_e2e001', 'uptime', 'completed', 'succeeded', 0, '2026-08-17T13:45:00Z'],
            ['audit_e2e002', 'df_h', 'completed', 'failed', 2, '2026-08-17T13:46:00Z'],
            ['audit_e2e003', 'free_m', 'completed', 'succeeded', 0, '2026-08-17T13:48:00Z'],
          ].map(([id, command, event, outcome, exit, at]) => ({
            audit_id: id,
            action_id: `action_${String(id).slice(6)}`,
            monitoring_instance_id: 'mi_e2ealpha',
            monitoring_instance_name: 'alpha 主机监控',
            actor_user_id: 'user_e2e',
            actor_username: 'operator',
            actor_display_name: '值班员',
            command_id: command,
            sensitivity: 'standard',
            event_type: event,
            outcome,
            source: 'agent_sync',
            exit_code: exit,
            occurred_at: at,
          })),
        },
      }
  }
}

/** A retained evidence snapshot for `/evidence/evs_e2eview`, one per renderer kind. */
export function evidenceSnapshotProfile(options: {
  kind: EvidenceSnapshotFixtureKind
  sourceUnavailable?: boolean
  redacted?: boolean
  backfilled?: boolean
}): ApiFixtureProfile {
  const readable = options.kind === 'unsupported' ? evidenceReadModelFor('command.audit') : evidenceReadModelFor(options.kind)
  const body = {
    record_id: 'rec_e2e001',
    snapshot_id: 'evs_e2eview',
    kind: options.kind === 'unsupported' ? 'command.audit' : options.kind,
    schema_version: readable.schema_version,
    subject: { type: 'vps', id: 'vps_0123456789abcdef', display_name: 'VPS Alpha' },
    source: { type: 'monitoring_instance', id: 'mi_e2ealpha', display_name: 'alpha 主机监控' },
    requested_window: { start: '2026-08-17T13:00:00Z', end: '2026-08-17T14:00:00Z' },
    actual_window: { start: '2026-08-17T13:00:00Z', end: '2026-08-17T14:00:00Z' },
    observed_at: '2026-08-17T14:00:00Z',
    captured_at: '2026-08-17T14:00:05Z',
    referenced_at: '2026-08-17T14:10:00Z',
    source_revision: 'rev-e2e-7',
    source_watermark: 'wm-e2e-7',
    producer_version: 'center/1.9.0',
    calculation_version: 'calc/v2',
    units: { status: 'not_applicable', values: {}, reason: 'not applicable' },
    // 信封质量与读模型保持一致，避免页头和正文互相矛盾。
    quality: {
      ...((readable.read_model as { quality?: typeof EVIDENCE_QUALITY }).quality ?? EVIDENCE_QUALITY),
      // 事件读模型自带回填条数，信封必须同步；其它类型按选项模拟回填。
      ...(options.kind === 'monitoring.event' ? { backfilled_count: 1 } : {}),
      ...(options.backfilled ? { backfilled_count: 1 } : {}),
    },
    sensitivity: 'normal',
    actual_precision_seconds: 300,
    bucket_width_seconds: 300,
    quota: { status: 'allowed' },
    retention: {
      immutable: true,
      scope: 'record_revision',
      source_deletion: 'snapshot_retained_source_unavailable',
    },
    redaction: options.redacted
      ? [
        { path: 'payload.stdout', sensitivity: 'forbidden', action: 'stripped' },
        { path: 'payload.actor_ip', sensitivity: 'sensitive_topology', action: 'masked' },
      ]
      : [],
    source_available: !options.sourceUnavailable,
    renderer_version: options.kind === 'unsupported' ? 'command_audit_v9' : readable.renderer_version,
    title: readable.title,
    read_model: readable.read_model,
  }
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/evidence/evs_e2eview')]: { status: 200, body },
  })
}

const SUBJECT_ACTIVITY_SUBJECTS = {
  vps: { kind: 'vps', source_id: 'vps_001', identity: { display_name: 'Tokyo Edge' }, live_route: '/vps/vps_001', status: 'live' },
  monitoring_instance: {
    kind: 'monitoring_instance',
    source_id: 'mi_001',
    identity: { display_name: 'alpha 主机监控', hostname: 'alpha.example.net' },
    live_route: '/monitoring/mi_001',
    status: 'live',
  },
  target: { kind: 'target', source_id: 'tg_001', identity: { display_name: 'API 443 入口' }, live_route: '/targets/tg_001', status: 'live' },
} satisfies Record<string, SubjectActivityListResponse['subject']>

function subjectActivityItem(
  id: string,
  eventKind: SubjectActivityListResponse['items'][number]['event_kind'],
  sourceKind: SubjectActivityListResponse['items'][number]['source_kind'],
  eventAt: string,
  title: string,
  extra: Partial<SubjectActivityListResponse['items'][number]> = {},
): SubjectActivityListResponse['items'][number] {
  return {
    activity_id: id,
    event_kind: eventKind,
    event_at: eventAt,
    recorded_at: eventAt,
    source_kind: sourceKind,
    backfilled: false,
    subjects: [],
    presentation: { version: 1, title, ...(extra.presentation ?? {}) },
    ...extra,
  }
}

/** Two local days mixing human, system and evidence items, with summaries, backfill and a lagging source. */
export function subjectActivityPopulatedProfile(options: {
  kind?: keyof typeof SUBJECT_ACTIVITY_SUBJECTS
  view?: 'activity' | 'records' | 'evidence'
  degraded?: boolean
} = {}): ApiFixtureProfile {
  const kind = options.kind ?? 'vps'
  const view = options.view ?? 'activity'
  const subject = SUBJECT_ACTIVITY_SUBJECTS[kind]
  // 记录视图只返回记录生命周期事件（后端谓词排除行动 / 评论），行动完成只出现在活动视图。
  const records = [
    subjectActivityItem('act_pop_1', 'record_revised', 'record_domain', '2026-08-19T13:50:00Z', '第三晚 TCP 观测', {
      presentation: { version: 1, title: '第三晚 TCP 观测', summary: '补充结论：丢包集中在服务商第二跳' },
      record_id: 'rec_e2e001',
      revision_id: 'rrv_e2e002',
    }),
    subjectActivityItem('act_pop_7', 'record_created', 'record_domain', '2026-08-17T15:30:00Z', '第三晚 TCP 观测', {
      record_id: 'rec_e2e001',
    }),
  ]
  const actions = [
    subjectActivityItem('act_pop_5', 'action_completed', 'record_domain', '2026-08-18T09:20:00Z', '确认监控告警时间线', {
      record_id: 'rec_e2e001',
    }),
  ]
  const evidence = [
    subjectActivityItem('act_pop_2', 'evidence_captured', 'evidence_snapshot', '2026-08-19T14:00:05Z', '第三晚主机负载', {
      evidence_snapshot_id: 'evs_e2eview',
      subjects: [{
        kind: subject.kind,
        source_id: subject.source_id,
        role: 'evidence_source',
        primary: true,
        identity: { coverage: '21:00–22:00', bucket: '5 分钟', quality: '部分覆盖' },
        tombstoned: false,
      }],
    }),
  ]
  const system = [
    subjectActivityItem('act_pop_3', 'monitoring_state_changed', 'monitoring_event', '2026-08-19T13:25:00Z', 'TCP 重传率升高', {
      presentation: { version: 1, title: 'TCP 重传率升高', summary: '告警 · 重传率 2.4%，阈值 2%' },
      backfilled: true,
      recorded_at: '2026-08-19T13:40:00Z',
    }),
    subjectActivityItem('act_pop_4', 'command_executed', 'command_audit', '2026-08-19T13:46:00Z', '执行 df_h', {
      presentation: { version: 1, title: '执行 df_h', summary: '失败 · 退出码 2 · 值班员' },
    }),
    subjectActivityItem('act_pop_6', 'asset_fact_changed', 'asset_history', '2026-08-18T02:10:00Z', '续费决策改为继续续费'),
  ]
  const items = (view === 'records' ? records : view === 'evidence' ? evidence : [...records, ...actions, ...evidence, ...system])
    .sort((left, right) => right.event_at.localeCompare(left.event_at))
  const query = view === 'activity' ? '' : `?view=${view}`
  return authenticatedProfile({
    [apiRouteKey('GET', `/api/subjects/${kind}/${subject.source_id}/activity${query}`)]: {
      status: 200,
      body: subjectActivityFixture({
        subject,
        view,
        items,
        source_statuses: options.degraded
          ? [{ source_kind: 'command_audit', state: 'stale', reason_code: 'lagging' }]
          : [],
      }),
    },
  })
}
