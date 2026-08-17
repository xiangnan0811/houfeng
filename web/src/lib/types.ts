export type MonitoringInstanceRecord = {
  monitoring_instance_id: string
  display_name: string
  group: string
  region: string
  city: string
  provider: string
  lifecycle_status: string
  monitoring_status: string
  binding_status: string
  labels: string[]
  note: string
  current_health_status: string
  last_heartbeat_at?: string
  last_sync_at?: string
  current_active_incident_count: number
  current_primary_issue_summary: string
  last_action?: LastAction | null
  archived_at?: string | null
  archived_reason?: string
  created_at: string
  updated_at: string
}

export type MonitoringInstanceListScope = 'active' | 'archived' | 'all'

export type MonitoringInstanceManagementVPSLink = {
  link_id: string
  vps_id: string
  display_name: string
  lifecycle_status: string
  usage_status: string
  linked_at: string
  note: string
}

export type MonitoringInstanceManagementCounts = {
  heartbeat_count: number
  host_sample_count: number
  probe_observation_count: number
  host_sample_daily_aggregate_count: number
  ip_quality_report_count: number
  active_incident_count: number
  state_change_event_count: number
  notification_record_count: number
  asset_lifecycle_action_step_count: number
  command_action_audit_count: number
  active_vps_link_count: number
}

export type MonitoringInstanceManagementActions = {
  can_retire: boolean
  can_restore_lifecycle: boolean
  can_archive: boolean
  can_restore_archive: boolean
  can_permanent_cleanup: boolean
}

export type MonitoringInstanceManagementReview = {
  record: MonitoringInstanceRecord
  active_vps_links: MonitoringInstanceManagementVPSLink[]
  counts: MonitoringInstanceManagementCounts
  warnings: string[]
  blockers: string[]
  actions: MonitoringInstanceManagementActions
  empty_mistake_candidate: boolean
}

export type MonitoringInstanceLifecycleManagementInput = {
  reason: string
}

export type MonitoringInstanceArchiveInput = {
  reason: string
  confirmation_name: string
}

export type MonitoringInstancePermanentCleanupInput = {
  reason: string
  confirmation_name: string
}

export type MonitoringInstancePermanentCleanupResult = {
  monitoring_instance_id: string
  counts: MonitoringInstanceManagementCounts
  deleted_reference_count: number
  deleted: boolean
}

export type LastAction = {
  action_id: string
  command_id: string
  status: 'pending' | 'done'
  sensitivity?: 'standard' | 'sensitive'
  queued_at?: string
  stdout?: string
  stderr?: string
  exit_code?: number
  completed_at?: string
  output_expires_at?: string
  output_expired?: boolean
}

export type CommandAuditWindow = '24h' | '7d' | '30d' | 'all' | 'custom'
export type CommandAuditOutcome = 'rejected' | 'queued' | 'dispatched' | 'succeeded' | 'failed'

export type CommandAuditEvent = {
  audit_id: string
  event_type: 'queued' | 'dispatched' | 'completed' | 'rejected'
  source: 'web' | 'agent_sync'
  occurred_at: string
  exit_code?: number
  rejection_reason?: 'sensitive_confirmation_required'
}

export type CommandAuditMonitoringInstance = {
  id: string
  name: string
  deleted: boolean
}

export type CommandAuditActor = {
  user_id: string
  username: string
  display_name: string
}

export type CommandAuditAction = {
  id: string
  action_id?: string
  monitoring_instance: CommandAuditMonitoringInstance
  command_id: string
  sensitivity: 'standard' | 'sensitive'
  outcome: CommandAuditOutcome
  actor: CommandAuditActor | null
  started_at: string
  events: CommandAuditEvent[]
}

export type CommandAuditListResponse = {
  items: CommandAuditAction[]
  next_cursor?: string
}

export type CommandAuditListFilter = {
  cursor?: string
  window?: CommandAuditWindow
  started_from?: string
  started_to?: string
  monitoring_instance?: string
  command_id?: string
  sensitivity?: 'standard' | 'sensitive'
  outcome?: CommandAuditOutcome
  actor?: string
  action_id?: string
  limit?: number
}

export type OnboardingPhase =
  | '未开始接入'
  | '已绑定，等待稳定观测'
  | '接入完成'
  | '绑定冲突待处理'

export type PendingBindingMetadata = {
  fingerprint: string
  first_seen_at?: string
  last_seen_at?: string
  attempt_count: number
}

export type MonitoringInstanceInstallCommandIssue = {
  command: string
  issued_at: string
  expires_at: string
  installer_url: string
  public_base_url: string
  agent_version: string
  release_repo: string
}

export type UpdateMonitoringInstanceMetadataInput = {
  labels: string[]
  group?: string
  note: string
}

export type CreateMonitoringInstanceInput = {
  display_name: string
  group: string
  region: string
  city: string
  provider: string
  labels: string[]
  note: string
}

export type MonitoringInstanceOnboardingState = MonitoringInstanceRecord & {
  phase: OnboardingPhase
  has_host_sample: boolean
  has_accepted_observation: boolean
  enrollment_token_issued_at?: string
  current_binding_fingerprint_summary?: string
  pending_binding?: PendingBindingMetadata
}

export type ContainerInfo = {
  id: string
  name: string
  image: string
  status: string
  cpu_pct?: number
  mem_pct?: number
}

export type HostSample = {
  monitoring_instance_id: string
  observed_at: string
  received_at: string
  agent_version: string
  fingerprint: string
  cpu_usage_pct: number
  load_1: number
  load_5: number
  load_15: number
  mem_used_pct: number
  mem_available_bytes: number
  mem_total_bytes: number
  swap_used_pct: number
  disk_used_pct: number
  disk_total_bytes: number
  inode_used_pct: number
  net_in_bytes_per_sec: number
  net_out_bytes_per_sec: number
  cpu_iowait_pct: number
  cpu_steal_pct: number
  disk_read_bytes_per_sec: number
  disk_write_bytes_per_sec: number
  disk_busy_pct: number
  uptime_seconds: number
  maintenance_context: boolean
  is_backfilled: boolean
  sync_batch_id: string
  containers?: ContainerInfo[]
}

export type MonitoringRuntimeWindow = {
  key: string
  started_at: string
  ended_at: string
  bucket_count: number
  available_started_at: string | null
  available_ended_at: string | null
  sample_count: number
}

export type HostMetricPoint = {
  observed_at: string
  sample_count: number
  cpu_usage_pct: number
  mem_used_pct: number
  disk_used_pct: number
  inode_used_pct: number
  load_5: number
  cpu_iowait_pct: number
  net_in_bytes_per_sec: number
  net_out_bytes_per_sec: number
}

export type HostSampleStreamMessage = {
  type: 'host_sample'
  monitoring_instance_id: string
  sample: HostSample
  received_at: string
}

export type MonitoringInstanceRuntimeFacts = {
  monitoring_instance_id: string
  window?: MonitoringRuntimeWindow
  latest_host_sample: HostSample | null
  host_metric_points?: HostMetricPoint[]
  recent_host_samples?: HostSample[]
}

export type TargetType = 'service' | 'china_reference'

export type TargetRunStatus = '启用' | '维护中' | '暂停' | '已归档'

export type TargetRecord = {
  target_id: string
  name: string
  target_type: TargetType
  host: string
  base_port?: number
  execution_monitoring_instance_labels: string[]
  run_status: TargetRunStatus
  group: string
  labels: string[]
  note: string
  current_health_status: string
  current_active_incident_count: number
  last_success_at?: string
  last_failure_at?: string
  current_primary_issue_summary: string
  created_at: string
  updated_at: string
}

export type UpdateTargetMetadataInput = {
  group?: string
  labels: string[]
  note: string
}

export type CreateTargetInput = {
  name: string
  target_type: TargetType
  host: string
  base_port?: number
  execution_monitoring_instance_labels: string[]
  run_status: TargetRunStatus
  group: string
  labels: string[]
  note: string
}

export type ProbeKind = 'tcp' | 'http' | 'tls'

export type FrequencyTier = '5s' | '1m' | '5m' | '15m' | '6h'

export type ProbeItemRecord = {
  probe_item_id: string
  target_id: string
  probe_kind: ProbeKind
  enabled: boolean
  frequency_tier: FrequencyTier
  timeout_seconds: number
  config: Record<string, unknown>
  created_at: string
  updated_at: string
}

export type CreateProbeItemInput = {
  probe_kind: ProbeKind
  enabled: boolean
  frequency_tier: FrequencyTier
  timeout_seconds: number
  config: Record<string, unknown>
}

export type UpdateProbeItemInput = CreateProbeItemInput

export type ProbeObservation = {
  monitoring_instance_id: string
  target_id: string
  probe_item_id: string
  probe_kind: ProbeKind
  observed_at: string
  received_at: string
  agent_version: string
  fingerprint: string
  result_kind: string
  latency_ms: number | null
  http_status: number | null
  tls_expiry_days: number | null
  error_code?: string
  error_summary?: string
  maintenance_context: boolean
  is_backfilled: boolean
  sync_batch_id: string
}

export type TargetRuntimeFacts = {
  target_id: string
  latest_probe_observations: ProbeObservation[]
  recent_probe_observations: ProbeObservation[]
}

export type ObservabilityObjectType = 'monitoring_instance' | 'target'

export type IncidentSeverity = '正常' | '关注' | '告警' | '严重'

export type StateChangeEventType =
  | 'incident_started'
  | 'incident_escalated'
  | 'incident_recovered'
  | 'monitoring_instance_binding_rebind_confirmed'
  | 'monitoring_instance_binding_pending_rejected'
  | 'monitoring_instance_binding_reset'
  | 'monitoring_instance_lifecycle_updated'
  | 'monitoring_instance_retired'
  | 'monitoring_instance_restored_to_observing'
  | 'monitoring_instance_monitoring_maintenance_entered'
  | 'monitoring_instance_monitoring_maintenance_exited'
  | 'monitoring_instance_monitoring_paused'
  | 'monitoring_instance_monitoring_resumed'
  | 'target_maintenance_entered'
  | 'target_maintenance_exited'
  | 'target_paused'
  | 'target_resumed'
  | 'target_archived'
  | 'target_restored_to_paused'

export const STATE_CHANGE_EVENT_TYPE_LABELS: Record<StateChangeEventType, string> = {
  incident_started: '异常开始',
  incident_escalated: '异常升级',
  incident_recovered: '异常恢复',
  monitoring_instance_binding_rebind_confirmed: '确认重新绑定',
  monitoring_instance_binding_pending_rejected: '拒绝待确认指纹',
  monitoring_instance_binding_reset: '绑定已重置',
  monitoring_instance_lifecycle_updated: '监控实例生命周期已更新',
  monitoring_instance_retired: '监控实例已退役',
  monitoring_instance_restored_to_observing: '监控实例恢复到观察中',
  monitoring_instance_monitoring_maintenance_entered: '监控实例进入维护',
  monitoring_instance_monitoring_maintenance_exited: '监控实例退出维护',
  monitoring_instance_monitoring_paused: '监控实例暂停监控',
  monitoring_instance_monitoring_resumed: '监控实例恢复监控',
  target_maintenance_entered: '目标进入维护',
  target_maintenance_exited: '目标退出维护',
  target_paused: '目标已暂停',
  target_resumed: '目标已恢复',
  target_archived: '目标已归档',
  target_restored_to_paused: '目标已恢复为暂停',
}

export type StateChangeEventRecord = {
  event_id?: string
  incident_id: string
  incident_class: string
  object_type: ObservabilityObjectType
  object_id: string
  event_type: StateChangeEventType
  severity: IncidentSeverity | ''
  summary: string
  created_at: string
}

export type EventListResponse = {
  items: StateChangeEventRecord[]
}

export type DashboardOverview = {
  snapshot_generated_at: string
  total_monitoring_instance_count: number
  total_target_count: number
  abnormal_monitoring_instance_count: number
  abnormal_target_count: number
  severe_monitoring_instance_count: number
  severe_target_count: number
  maintenance_monitoring_instance_count: number
  maintenance_target_count: number
  pending_onboarding_monitoring_instance_count: number
  paused_monitoring_instance_count: number
  retired_monitoring_instance_count: number
  paused_target_count: number
  archived_target_count: number
  recent_new_incident_count: number
  recent_recovery_count: number
  group_summaries: DashboardGroupSummary[]
  notification_status: DashboardNotificationStatus
  asset_summary: DashboardAssetSummary
  abnormal_monitoring_instances: DashboardMonitoringInstanceSummary[]
  abnormal_targets: DashboardTargetSummary[]
  recent_events: StateChangeEventRecord[]
  /**
   * 24-element array of per-hour incident_started counts. Index 0 is 23
   * hours ago, index 23 is the current hour. Optional — older backends
   * (and unit-test fixtures) may omit it; consumers should treat missing
   * as "no trend data, hide sparkline".
   */
  new_incident_trend_24h?: number[]
  /**
   * 24-element array of per-hour incident_recovered counts. Same indexing
   * as new_incident_trend_24h.
   */
  recovery_trend_24h?: number[]
}

export type DashboardGroupSummary = {
  group: string
  monitoring_instance_count: number
  target_count: number
  abnormal_monitoring_instance_count: number
  abnormal_target_count: number
  severe_monitoring_instance_count: number
  severe_target_count: number
  maintenance_monitoring_instance_count: number
  maintenance_target_count: number
}

export type DashboardNotificationStatus = {
  telegram_configured: boolean
  telegram_runtime_managed: boolean
  telegram_runtime_apply_active: boolean
  feishu_configured: boolean
}

export type DashboardAssetSummary = {
  renewal_due_30d_subscription_count: number
  renewal_due_30d_vps_count: number
  unreviewed_vps_count: number
  to_cancel_vps_count: number
  cancelled_vps_count: number
  cancellation_attention_vps_count: number
  running_cancelled_asset_count: number
  to_migrate_vps_count: number
  unlinked_vps_count: number
  abnormal_linked_vps_count: number
  cost_by_currency: DashboardAssetCostByCurrency[]
  base_currency?: string
  monthly_cost_base?: number
  yearly_cost_base?: number
  budget_risk_count?: number
  exchange_rate_stale_count?: number
}

export type DashboardAssetCostByCurrency = {
  currency: string
  monthly_total: number
  yearly_total: number
}

export type DashboardMonitoringInstanceSummary = {
  monitoring_instance_id: string
  display_name: string
  group: string
  region: string
  city: string
  provider: string
  lifecycle_status: string
  monitoring_status: string
  current_health_status: IncidentSeverity
  last_heartbeat_at?: string
  current_active_incident_count: number
  current_primary_issue_summary: string
}

export type DashboardTargetSummary = {
  target_id: string
  name: string
  target_type: string
  host: string
  base_port?: number
  run_status: string
  group: string
  current_health_status: IncidentSeverity
  last_success_at?: string
  last_failure_at?: string
  current_active_incident_count: number
  current_primary_issue_summary: string
}

export type ActiveIncidentRecord = {
  incident_id: string
  incident_class: string
  object_type: ObservabilityObjectType
  object_id: string
  severity: IncidentSeverity
  started_at: string
  last_evaluated_at: string
  source_summary: string
}

export type EventListFilter = {
  object_type?: ObservabilityObjectType | ''
  object_id?: string
  severity?: IncidentSeverity | ''
  event_type?: StateChangeEventType | ''
  created_from?: string
  created_to?: string
  label?: string
  notification_only?: boolean
  recovery_only?: boolean
  maintenance_only?: boolean
  include_backfilled?: boolean
  limit?: number
}

export type IncidentListFilter = {
  object_type?: ObservabilityObjectType | ''
  object_id?: string
  severity?: IncidentSeverity | ''
  limit?: number
}

export type SettingsTelegramResponse = {
  chat_id: string
  token_present: boolean
  token_masked_summary?: string
  runtime_managed: boolean
  runtime_apply_active: boolean
}

export type FeishuSettingsResponse = {
  enabled: boolean
  webhook_url_present: boolean
  webhook_url_masked_summary?: string
}

export type FeishuSettingsInput = {
  enabled?: boolean
  webhook_url?: string
}

export type SettingsTelegramInput = {
  bot_token?: string
  chat_id: string
  runtime_managed?: boolean
}

export type ProbeFrequencyDefaults = {
  tcp: string
  http: string
  tls: string
}

export type IncidentDefaults = {
  heartbeat_interval_seconds: number
  stale_threshold_intervals: number
  sweep_interval_seconds: number
  notify_on_started: boolean
  notify_on_escalated: boolean
  notify_on_recovered: boolean
  cpu_warning_pct: number
  cpu_alert_pct: number
  cpu_critical_pct: number
  mem_warning_pct: number
  mem_alert_pct: number
  mem_critical_pct: number
  disk_warning_pct: number
  disk_alert_pct: number
  disk_critical_pct: number
  inode_warning_pct: number
  inode_alert_pct: number
  inode_critical_pct: number
  iowait_warning_pct: number
  iowait_critical_pct: number
  load5_warning: number
  load5_critical: number
}

export type ProbeFrequencyOverride = {
  tcp?: string
  http?: string
  tls?: string
}

export type IncidentDefaultsOverride = {
  heartbeat_interval_seconds?: number
  stale_threshold_intervals?: number
  sweep_interval_seconds?: number
  notify_on_started?: boolean
  notify_on_escalated?: boolean
  notify_on_recovered?: boolean
  cpu_warning_pct?: number
  cpu_alert_pct?: number
  cpu_critical_pct?: number
  mem_warning_pct?: number
  mem_alert_pct?: number
  mem_critical_pct?: number
  disk_warning_pct?: number
  disk_alert_pct?: number
  disk_critical_pct?: number
  inode_warning_pct?: number
  inode_alert_pct?: number
  inode_critical_pct?: number
  iowait_warning_pct?: number
  iowait_critical_pct?: number
  load5_warning?: number
  load5_critical?: number
}

export type SettingsOverrideFields = {
  host_sample_frequency_tier?: string
  probe_frequency_defaults?: ProbeFrequencyOverride
  incident_defaults?: IncidentDefaultsOverride
}

export type MonitoringInstanceLabelOverrideRule = {
  label: string
  overrides: SettingsOverrideFields
}

export type TargetTypeOverrideRule = {
  target_type: string
  overrides: SettingsOverrideFields
}

export type TargetLabelOverrideRule = {
  label: string
  overrides: SettingsOverrideFields
}

export type OverrideRules = {
  monitoring_instance_labels: MonitoringInstanceLabelOverrideRule[]
  target_types: TargetTypeOverrideRule[]
  target_labels: TargetLabelOverrideRule[]
}

export type RetentionPolicy = {
  raw_layer_days: number
  aggregate_layer_days: number
  event_layer_days: number
  notification_layer_days: number
}

export type SubscriptionCostSettings = {
  base_currency: string
  exchange_rate_provider: 'frankfurter' | 'fixer' | string
  fixer_configured: boolean
  fixer_masked_summary?: string
  default_reminder_offsets_days: number[]
  max_reminder_lead_days: number
  exchange_rate_stale_after_hours: number
}

export type SubscriptionCostSettingsUpdateInput = {
  base_currency?: string
  exchange_rate_provider?: 'frankfurter' | 'fixer' | string
  fixer_api_key?: string
  default_reminder_offsets_days?: number[]
  max_reminder_lead_days?: number
  exchange_rate_stale_after_hours?: number
}

export type SettingsRecord = {
  telegram: SettingsTelegramResponse
  feishu: FeishuSettingsResponse
  host_sample_frequency_tier: string
  probe_frequency_defaults: ProbeFrequencyDefaults
  incident_defaults: IncidentDefaults
  override_rules: OverrideRules
  retention_policy: RetentionPolicy
  ip_quality_settings: IPQualitySettings
  subscription_cost_settings: SubscriptionCostSettings
}

export type IPQualitySettings = {
  enabled: boolean
  frequency_seconds: number
  stale_after_seconds: number
  timeout_seconds: number
  raw_retention_days: number
  history_retention_days: number
  services: string[]
}

export type MonitoringInstanceSparklinesResponse = {
  monitoring_instances: Record<string, Record<string, (number | null)[]>>
}

export type TargetSparklinesResponse = {
  targets: Record<string, { latency: (number | null)[] }>
}

export type ProviderRecord = {
  provider_id: string
  name: string
  website: string
  panel_url: string
  account_hint: string
  country: string
  note: string
  rating: number | null
  labels: string[]
  created_at: string
  updated_at: string
}

export type CreateProviderInput = {
  name: string
  website: string
  panel_url: string
  account_hint: string
  country: string
  note: string
  rating?: number | null
  labels: string[]
}

export type UpdateProviderInput = Partial<CreateProviderInput>

export type VPSLifecycleStatus =
  | 'active'
  | 'idle'
  | 'testing'
  | 'to_migrate'
  | 'to_cancel'
  | 'cancelled'
  | 'archived'

export type VPSUsageStatus = 'in_use' | 'idle' | 'standby' | 'testing' | 'unknown'

export type VPSRenewalDecision =
  | 'unreviewed'
  | 'keep'
  | 'observe'
  | 'migrate'
  | 'cancel'
  | 'auto_renew_cancelled'
  | 'replaced'

export type RenewalSubscriptionLinkageStatus =
  | 'none'
  | 'subscription_updated'
  | 'subscription_already_cancelled'
  | 'no_active_subscription'
  | 'multiple_active_subscriptions'

export type SubscriptionStatus = 'active' | 'paused' | 'cancelled' | 'expired' | 'unknown'

export type BillingPeriodUnit = 'day' | 'week' | 'month' | 'year'

export type RenewalMode = 'auto' | 'manual' | 'auto_cancelled' | 'lottery' | 'gift' | 'bonus' | 'other'

export type AssetServiceType = 'web' | 'api' | 'database' | 'worker' | 'proxy' | 'other'

export type AssetServiceStatus = 'active' | 'paused' | 'retired' | 'unknown'

export type AssetDomainStatus = 'active' | 'paused' | 'retired' | 'unknown'

export type VPSExperienceCategory =
  | 'note'
  | 'stability'
  | 'network'
  | 'support'
  | 'billing'
  | 'migration'
  | 'cancellation'

export type VPSExperienceSeverity = 'info' | 'warning' | 'critical'

export const VPS_LIFECYCLE_STATUS_LABELS: Record<VPSLifecycleStatus, string> = {
  active: '在用',
  idle: '闲置',
  testing: '测试中',
  to_migrate: '待迁移',
  to_cancel: '待取消',
  cancelled: '已取消',
  archived: '已归档',
}

export const VPS_USAGE_STATUS_LABELS: Record<VPSUsageStatus, string> = {
  in_use: '承载业务',
  idle: '暂无用途',
  standby: '备用',
  testing: '测试用途',
  unknown: '未确认',
}

export const VPS_RENEWAL_DECISION_LABELS: Record<VPSRenewalDecision, string> = {
  unreviewed: '未评估',
  keep: '保留',
  observe: '观察',
  migrate: '迁移',
  cancel: '取消',
  auto_renew_cancelled: '已取消自动续费',
  replaced: '已替换',
}

export const SUBSCRIPTION_STATUS_LABELS: Record<SubscriptionStatus, string> = {
  active: '生效中',
  paused: '已暂停',
  cancelled: '已取消',
  expired: '已过期',
  unknown: '未确认',
}

export const VPS_EXPERIENCE_CATEGORY_LABELS: Record<VPSExperienceCategory, string> = {
  note: '备注',
  stability: '稳定性',
  network: '网络',
  support: '服务支持',
  billing: '账单',
  migration: '迁移',
  cancellation: '取消',
}

export const VPS_EXPERIENCE_SEVERITY_LABELS: Record<VPSExperienceSeverity, string> = {
  info: '信息',
  warning: '关注',
  critical: '严重',
}

export const ASSET_SERVICE_TYPE_LABELS: Record<AssetServiceType, string> = {
  web: 'Web',
  api: 'API',
  database: '数据库',
  worker: 'Worker',
  proxy: '代理',
  other: '其他',
}

export const ASSET_SERVICE_STATUS_LABELS: Record<AssetServiceStatus, string> = {
  active: '运行中',
  paused: '已暂停',
  retired: '已退役',
  unknown: '未确认',
}

export const ASSET_DOMAIN_STATUS_LABELS: Record<AssetDomainStatus, string> = {
  active: '使用中',
  paused: '已暂停',
  retired: '已退役',
  unknown: '未确认',
}

export type VPSAssetRecord = {
  vps_id: string
  display_name: string
  provider_id?: string | null
  provider_name: string
  product_name: string
  order_ref: string
  country: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  os_name: string
  virtualization: string
  lifecycle_status: VPSLifecycleStatus
  usage_status: VPSUsageStatus
  renewal_decision: VPSRenewalDecision
  importance: string
  labels: string[]
  note: string
  active_monitoring_instance_link_count: number
  running_monitoring_instance_count?: number
  running_target_count?: number
  ip_quality_summary?: IPQualitySummary | null
  created_at: string
  updated_at: string
  archived_at?: string | null
}

export type IPQualityProviderResult = {
  provider: string
  status?: string
  source_type?: string
  latency_ms?: number | null
  usage_type?: string
  company_type?: string
  risk_level?: string
  risk_score?: string
  region_code?: string
  region_name?: string
  is_proxy?: boolean | null
  is_tor?: boolean | null
  is_vpn?: boolean | null
  is_server?: boolean | null
  is_abuser?: boolean | null
  is_robot?: boolean | null
  error_code?: string
  error_summary?: string
  extra_json?: unknown
}

export type IPQualityServiceUnlock = {
  service: string
  source?: string
  status: string
  probe_status?: string
  latency_ms?: number | null
  region?: string
  unlock_type?: string
  error_code?: string
  error_summary?: string
  extra_json?: unknown
}

export type IPQualityCoverage = {
  expected_provider_count: number
  successful_provider_count: number
  failed_provider_count: number
  skipped_provider_count: number
  not_configured_provider_count: number
  expected_service_count: number
  successful_service_count: number
  failed_service_count: number
  skipped_service_count: number
  not_configured_service_count: number
}

export type IPQualitySummary = {
  report_id?: string
  vps_id?: string
  observed_at: string
  ip_address: string
  ip_version: number
  status: string
  risk_level?: string
  use_region_code?: string
  use_region_name?: string
  asn?: string
  organization?: string
  stale: boolean
  ambiguous: boolean
  assignment_mode?: string
  error_code?: string
  error_summary?: string
  provider_count: number
  unlockable_count: number
  coverage?: IPQualityCoverage | null
}

export type IPQualityReport = {
  report_id: string
  monitoring_instance_id: string
  observed_at: string
  received_at: string
  agent_version: string
  fingerprint: string
  sync_batch_id: string
  ip_address: string
  ip_version: number
  status: string
  asn?: string
  organization?: string
  latitude?: number | null
  longitude?: number | null
  use_region_code?: string
  use_region_name?: string
  registered_region_code?: string
  registered_region_name?: string
  risk_level?: string
  error_code?: string
  error_summary?: string
  is_backfilled: boolean
  created_at: string
  raw_json?: unknown
  coverage?: IPQualityCoverage | null
  diagnostics_json?: unknown
}

export type VPSIPQualityReport = {
  summary?: IPQualitySummary | null
  latest_report?: IPQualityReport | null
  provider_results: IPQualityProviderResult[]
  service_unlocks: IPQualityServiceUnlock[]
  history: IPQualitySummary[]
}

export type RenewalSubscriptionLinkage = {
  status: RenewalSubscriptionLinkageStatus
  candidate_count: number
  subscription_id?: string
  updated: boolean
  message: string
}

export type VPSAssetUpdateResult = VPSAssetRecord & {
  renewal_subscription_linkage?: RenewalSubscriptionLinkage | null
}

export type AssetDecisionGroupType =
  | 'renewal_attention'
  | 'cancellation_attention'
  | 'region_portfolio'
  | 'provider_portfolio'
  | 'cost_pressure'
  | 'evidence_gap'

export type AssetDecisionView =
  | 'needs_decision'
  | 'renewal'
  | 'region'
  | 'provider'
  | 'cost'
  | 'evidence'

export type AssetDecisionSuggestedRole =
  | 'primary_candidate'
  | 'standby_candidate'
  | 'observe_candidate'
  | 'retire_candidate'
  | 'evidence_needed'

export type AssetDecisionSuggestedAction =
  | 'review'
  | 'keep'
  | 'observe'
  | 'migrate'
  | 'cancel'
  | 'open_cancellation_workbench'
  | 'complete_evidence'

export type AssetDecisionEvidenceKind =
  | 'renewal_due'
  | 'idle_paid'
  | 'missing_subscription'
  | 'missing_monitoring'
  | 'carries_service'
  | 'cancellation_linkage'
  | 'budget_risk'
  | 'abnormal_monitoring'
  | 'missing_provider'
  | 'missing_location'
  | 'missing_access'
  | 'exchange_rate_stale'
  | 'no_service_context'
  | 'subscription_unavailable'
  | 'current_fact_missing'
  | 'ip_quality_missing'
  | 'ip_quality_stale'
  | 'ip_quality_risk'
  | 'ip_egress_mismatch'
  | 'media_unlock_blocked'

export type AssetDecisionSourceAvailability = {
  subscriptions: boolean
  services: boolean
  domains: boolean
  monitoring: boolean
  targets: boolean
}

export type AssetDecisionCostByCurrency = {
  currency: string
  monthly_total: number
  yearly_total: number
}

export type AssetDecisionEvidenceChip = {
  kind: AssetDecisionEvidenceKind
  label: string
  tone: 'normal' | 'notice' | 'alert' | 'critical' | string
  details?: string
}

export type AssetDecisionEvidenceQualityTier =
  | 'strong'
  | 'usable'
  | 'weak'
  | 'blocked'

export type AssetDecisionEvidenceDecisionBias =
  | 'keep'
  | 'observe'
  | 'complete_evidence'
  | 'retire'
  | 'migrate'
  | 'review'

export type AssetDecisionEvidenceAssessment = {
  confidence_score: number
  pressure_score: number
  readiness_score: number
  quality_tier: AssetDecisionEvidenceQualityTier
  decision_bias: AssetDecisionEvidenceDecisionBias
  support_signal_count: number
  risk_signal_count: number
  gap_signal_count: number
  summary: string
}

export type AssetDecisionRecommendationReason = {
  kind: string
  label: string
  tone: 'normal' | 'notice' | 'alert' | 'critical' | string
  details?: string
}

export type AssetDecisionRecommendation = {
  summary: string
  next_step: string
  reasons: AssetDecisionRecommendationReason[]
  blockers: AssetDecisionRecommendationReason[]
  priority_vps_ids: string[]
  confidence_label: string
}

export type AssetDecisionComparisonPrimaryAxis =
  | 'renewal'
  | 'cost'
  | 'service_context'
  | 'monitoring'
  | 'evidence'
  | 'lifecycle'
  | 'review'

export type AssetDecisionComparisonLane =
  | 'primary'
  | 'standby'
  | 'observe'
  | 'retire'
  | 'evidence'
  | 'review'

export type AssetDecisionComparisonSignal = {
  kind: string
  label: string
  tone: 'normal' | 'notice' | 'alert' | 'critical' | 'neutral' | string
  details?: string
}

export type AssetDecisionComparisonLaneCount = {
  lane: AssetDecisionComparisonLane
  count: number
}

export type AssetDecisionComparisonInsight = {
  summary: string
  primary_axis: AssetDecisionComparisonPrimaryAxis
  lane_counts: AssetDecisionComparisonLaneCount[]
  priority_vps_ids: string[]
  tradeoffs: AssetDecisionComparisonSignal[]
}

export type AssetDecisionMemberComparisonInsight = {
  rank: number
  lane: AssetDecisionComparisonLane
  summary: string
  strengths: AssetDecisionComparisonSignal[]
  risks: AssetDecisionComparisonSignal[]
  gaps: AssetDecisionComparisonSignal[]
  tradeoffs: AssetDecisionComparisonSignal[]
}

export type AssetDecisionGroupSummary = {
  group_id: string
  group_type: AssetDecisionGroupType
  view: AssetDecisionView
  title: string
  scope_key: string
  scope_label: string
  priority: number
  member_count: number
  lifecycle_counts: Partial<Record<VPSLifecycleStatus, number>>
  usage_counts: Partial<Record<VPSUsageStatus, number>>
  renewal_decision_counts: Partial<Record<VPSRenewalDecision, number>>
  renewal_window_count: number
  unreviewed_count: number
  migrate_count: number
  cancel_count: number
  cancellation_attention_count: number
  idle_count: number
  standby_count: number
  in_use_count: number
  service_count: number
  domain_count: number
  target_count: number
  running_target_count: number
  monitoring_link_count: number
  abnormal_monitoring_count: number
  active_incident_count: number
  primary_issue_summary: string
  monthly_cost_by_currency: AssetDecisionCostByCurrency[]
  monthly_cost_base?: number | null
  yearly_cost_base?: number | null
  base_currency?: string
  evidence_chips: AssetDecisionEvidenceChip[]
  evidence_assessment: AssetDecisionEvidenceAssessment
  decision_recommendation?: AssetDecisionRecommendation
  comparison_insight?: AssetDecisionComparisonInsight
}

export type AssetDecisionGroupMember = {
  vps: VPSAssetRecord
  primary_subscription?: SubscriptionRecord | null
  subscription_count: number
  active_subscription_count: number
  inactive_subscription_count: number
  service_count: number
  domain_count: number
  target_count: number
  running_target_count: number
  monitoring_link_count: number
  running_monitoring_count: number
  abnormal_monitoring_count: number
  active_incident_count: number
  primary_issue_summary: string
  cancellation_attention_reason?: string
  suggested_role: AssetDecisionSuggestedRole
  suggested_action: AssetDecisionSuggestedAction
  evidence_chips: AssetDecisionEvidenceChip[]
  evidence_assessment: AssetDecisionEvidenceAssessment
  decision_recommendation?: AssetDecisionRecommendation
  comparison_insight?: AssetDecisionMemberComparisonInsight
  renewal_within_window: boolean
  source_availability: AssetDecisionSourceAvailability
}

export type AssetDecisionGroupDetail = AssetDecisionGroupSummary & {
  members: AssetDecisionGroupMember[]
}

export type AssetDecisionManualGroupStatus = 'active' | 'archived'

export type AssetDecisionManualGroupScenario =
  | 'general'
  | 'primary_standby'
  | 'budget_reduction'
  | 'provider_review'
  | 'region_review'
  | 'migration_retirement'
  | 'evidence_cleanup'

export type AssetDecisionSourceType = 'auto_group' | 'manual_group'

export type AssetDecisionManualGroupSummary = {
  manual_group_id: string
  status: AssetDecisionManualGroupStatus
  scenario: AssetDecisionManualGroupScenario
  title: string
  goal: string
  note: string
  source_type: 'manual' | 'auto_group' | string
  source_group_id?: string
  source_group_type?: AssetDecisionGroupType
  source_view?: AssetDecisionView
  scope_key?: string
  scope_label?: string
  renew_within_days: number
  member_count: number
  lifecycle_counts: Partial<Record<VPSLifecycleStatus, number>>
  usage_counts: Partial<Record<VPSUsageStatus, number>>
  renewal_decision_counts: Partial<Record<VPSRenewalDecision, number>>
  renewal_window_count: number
  unreviewed_count: number
  migrate_count: number
  cancel_count: number
  cancellation_attention_count: number
  idle_count: number
  standby_count: number
  in_use_count: number
  service_count: number
  domain_count: number
  target_count: number
  running_target_count: number
  monitoring_link_count: number
  abnormal_monitoring_count: number
  active_incident_count: number
  primary_issue_summary: string
  monthly_cost_by_currency: AssetDecisionCostByCurrency[]
  monthly_cost_base?: number | null
  yearly_cost_base?: number | null
  base_currency?: string
  evidence_chips: AssetDecisionEvidenceChip[]
  evidence_assessment: AssetDecisionEvidenceAssessment
  decision_recommendation?: AssetDecisionRecommendation
  comparison_insight?: AssetDecisionComparisonInsight
  source_availability: AssetDecisionSourceAvailability
  created_at: string
  updated_at: string
  archived_at?: string | null
}

export type AssetDecisionManualGroupMember = AssetDecisionGroupMember & {
  manual_group_id: string
  vps_id: string
  intended_role: AssetDecisionSuggestedRole
  intended_action: AssetDecisionSuggestedAction
  reason: string
  note: string
  sort_order: number
  evidence_snapshot: AssetDecisionEvidenceSnapshot
  current_fact_found: boolean
  created_at: string
  updated_at: string
}

export type AssetDecisionManualGroupDetail = AssetDecisionManualGroupSummary & {
  members: AssetDecisionManualGroupMember[]
}

export type AssetDecisionOverview = {
  snapshot_generated_at: string
  renew_within_days: number
  group_count: number
  member_vps_count: number
  needs_decision_count: number
  renewal_group_count: number
  region_group_count: number
  provider_group_count: number
  cost_group_count: number
  evidence_group_count: number
  top_groups: AssetDecisionGroupSummary[]
  type_counts: Partial<Record<AssetDecisionGroupType, number>>
  view_counts: Partial<Record<AssetDecisionView, number>>
  source_availability: AssetDecisionSourceAvailability
}

export type AssetDecisionGroupListFilter = {
  view?: AssetDecisionView | '' | null
  renew_within_days?: number | null
  provider_id?: string | null
  vps_id?: string | null
  country?: string | null
  region?: string | null
  city?: string | null
  scenario?: AssetDecisionManualGroupScenario | '' | null
}

export type AssetDecisionScenarioTemplateStatus = 'active' | 'archived'

export type AssetDecisionScenarioTemplateMember = {
  template_id?: string
  member_id?: string
  vps_id?: string
  intended_role?: AssetDecisionSuggestedRole
  intended_action?: AssetDecisionSuggestedAction
  reason: string
  note: string
  sort_order: number
  created_at: string
  updated_at: string
}

export type AssetDecisionScenarioTemplateSummary = {
  template_id: string
  builtin: boolean
  status: AssetDecisionScenarioTemplateStatus
  scenario: AssetDecisionManualGroupScenario
  title: string
  goal: string
  note: string
  source_manual_group_id?: string
  member_count: number
  created_at: string
  updated_at: string
  archived_at?: string | null
}

export type AssetDecisionScenarioTemplateDetail = AssetDecisionScenarioTemplateSummary & {
  members: AssetDecisionScenarioTemplateMember[]
}

export type CreateAssetDecisionScenarioTemplateMemberInput = {
  vps_id?: string
  intended_role?: AssetDecisionSuggestedRole
  intended_action?: AssetDecisionSuggestedAction
  reason?: string
  note?: string
  sort_order?: number
}

export type CreateAssetDecisionScenarioTemplateInput = {
  status?: AssetDecisionScenarioTemplateStatus
  scenario?: AssetDecisionManualGroupScenario
  title?: string
  goal?: string
  note?: string
  source_manual_group_id?: string
  members?: CreateAssetDecisionScenarioTemplateMemberInput[]
}

export type PatchAssetDecisionScenarioTemplateInput = {
  status?: AssetDecisionScenarioTemplateStatus
  scenario?: AssetDecisionManualGroupScenario
  title?: string
  goal?: string
  note?: string
}

export type CreateManualGroupFromScenarioTemplateInput = {
  renew_within_days?: number
  status?: AssetDecisionManualGroupStatus
  scenario?: AssetDecisionManualGroupScenario
  title?: string
  goal?: string
  note?: string
  members?: CreateAssetDecisionManualGroupMemberInput[]
}

export type AssetDecisionRecordStatus =
  | 'draft'
  | 'decided'
  | 'in_progress'
  | 'completed'
  | 'abandoned'

export type AssetDecisionFollowupStatus =
  | 'todo'
  | 'in_progress'
  | 'blocked'
  | 'done'
  | 'skipped'

export type AssetDecisionEvidenceSnapshot = Record<string, unknown>

export type AssetDecisionExecutionReadbackStatus =
  | 'open'
  | 'aligned'
  | 'drift'
  | 'blocked'
  | 'needs_evidence'
  | 'inactive'

export type AssetDecisionExecutionReadbackIssue = {
  kind: string
  label: string
  tone: 'normal' | 'notice' | 'alert' | 'critical' | string
  details?: string
}

export type AssetDecisionExecutionCurrentFacts = {
  found: boolean
  lifecycle_status?: VPSLifecycleStatus
  usage_status?: VPSUsageStatus
  renewal_decision?: VPSRenewalDecision
  active_subscription_count: number
  service_count: number
  domain_count: number
  target_count: number
  running_target_count: number
  monitoring_link_count: number
  running_monitoring_count: number
  abnormal_monitoring_count: number
  active_incident_count: number
  ip_quality_summary?: IPQualitySummary | null
  ip_quality_provider_risk_signal_count?: number
  ip_quality_blocked_services?: string[]
  source_availability: AssetDecisionSourceAvailability
}

export type AssetDecisionMemberExecutionReadback = {
  status: AssetDecisionExecutionReadbackStatus
  summary: string
  issues: AssetDecisionExecutionReadbackIssue[]
  current_facts: AssetDecisionExecutionCurrentFacts
}

export type AssetDecisionRecordExecutionReadback = {
  status: AssetDecisionExecutionReadbackStatus
  summary: string
  open_count: number
  aligned_count: number
  drift_count: number
  blocked_count: number
  needs_evidence_count: number
}

export type AssetDecisionExecutionPlanLane =
  | 'cancel_retire'
  | 'migration'
  | 'keep_observe'
  | 'evidence'
  | 'review'

export type AssetDecisionExecutionPlanStepKind =
  | 'open_cancellation_workbench'
  | 'open_vps_detail'
  | 'open_subscription_context'
  | 'review_record'

export type AssetDecisionExecutionPlanTone =
  | 'critical'
  | 'alert'
  | 'notice'
  | 'normal'
  | 'neutral'

export type AssetDecisionExecutionPlanLaneCount = {
  lane: AssetDecisionExecutionPlanLane
  count: number
}

export type AssetDecisionRecordExecutionPlan = {
  summary: string
  lane_counts: AssetDecisionExecutionPlanLaneCount[]
  actionable_count: number
  blocked_count: number
}

export type AssetDecisionMemberExecutionPlan = {
  lane: AssetDecisionExecutionPlanLane
  step_kind: AssetDecisionExecutionPlanStepKind
  tone: AssetDecisionExecutionPlanTone
  summary: string
  step_label: string
  issue_count: number
  blocked: boolean
  actionable: boolean
}

export type AssetDecisionRecordSummary = {
  record_id: string
  title: string
  goal: string
  status: AssetDecisionRecordStatus
  source_type: AssetDecisionSourceType | string
  source_group_id: string
  source_group_type: AssetDecisionGroupType
  source_view: AssetDecisionView
  scope_key: string
  scope_label: string
  renew_within_days: number
  member_count: number
  followup_todo_count: number
  followup_in_progress_count: number
  followup_blocked_count: number
  followup_done_count: number
  followup_skipped_count: number
  evidence_snapshot: AssetDecisionEvidenceSnapshot
  execution_readback: AssetDecisionRecordExecutionReadback
  execution_plan: AssetDecisionRecordExecutionPlan
  created_at: string
  updated_at: string
  decided_at?: string | null
  completed_at?: string | null
}

export type AssetDecisionRecordMember = {
  record_id: string
  vps_id: string
  display_name: string
  suggested_role: AssetDecisionSuggestedRole
  decided_role: AssetDecisionSuggestedRole
  suggested_action: AssetDecisionSuggestedAction
  decided_action: AssetDecisionSuggestedAction
  reason: string
  followup_status: AssetDecisionFollowupStatus
  followup_note: string
  followup_updated_at?: string | null
  evidence_snapshot: AssetDecisionEvidenceSnapshot
  execution_readback: AssetDecisionMemberExecutionReadback
  execution_plan: AssetDecisionMemberExecutionPlan
  created_at: string
  updated_at: string
}

export type AssetDecisionRecordDetail = AssetDecisionRecordSummary & {
  members: AssetDecisionRecordMember[]
}

export type CreateAssetDecisionRecordMemberInput = {
  vps_id: string
  decided_role?: AssetDecisionSuggestedRole
  decided_action?: AssetDecisionSuggestedAction
  reason?: string
}

export type CreateAssetDecisionRecordInput = {
  source_type?: AssetDecisionSourceType
  source_group_id: string
  renew_within_days: number
  title?: string
  goal?: string
  status?: AssetDecisionRecordStatus
  members?: CreateAssetDecisionRecordMemberInput[]
}

export type PatchAssetDecisionRecordInput = {
  title?: string
  goal?: string
  status?: AssetDecisionRecordStatus
  members?: Array<{
    vps_id: string
    followup_status?: AssetDecisionFollowupStatus
    followup_note?: string
  }>
}

export type CreateAssetDecisionManualGroupMemberInput = {
  vps_id: string
  intended_role?: AssetDecisionSuggestedRole
  intended_action?: AssetDecisionSuggestedAction
  reason?: string
  note?: string
  sort_order?: number
}

export type CreateAssetDecisionManualGroupInput = {
  source_type?: 'manual' | 'auto_group'
  source_group_id?: string
  renew_within_days?: number
  status?: AssetDecisionManualGroupStatus
  scenario?: AssetDecisionManualGroupScenario
  title?: string
  goal?: string
  note?: string
  members?: CreateAssetDecisionManualGroupMemberInput[]
}

export type PatchAssetDecisionManualGroupInput = {
  status?: AssetDecisionManualGroupStatus
  scenario?: AssetDecisionManualGroupScenario
  title?: string
  goal?: string
  note?: string
}

export type PatchAssetDecisionManualGroupMemberInput = {
  intended_role?: AssetDecisionSuggestedRole
  intended_action?: AssetDecisionSuggestedAction
  reason?: string
  note?: string
  sort_order?: number
}

export type LifecycleActionStepStatus = 'completed' | 'skipped' | 'failed'

export type LifecycleActionStep = {
  step_id: string
  action_id: string
  object_type: 'vps' | 'subscription' | 'monitoring_instance' | 'target' | string
  object_id: string
  step_type:
    | 'vps_lifecycle'
    | 'subscription_status'
    | 'subscription_renew_at'
    | 'monitoring_instance_lifecycle'
    | 'monitoring_instance_monitoring'
    | 'target_run_status'
    | string
  status: LifecycleActionStepStatus
  before_state: Record<string, unknown>
  after_state: Record<string, unknown>
  message: string
  executed_at?: string | null
  created_at: string
}

export type LifecycleActionRecord = {
  action_id: string
  vps_id: string
  action_type: 'cancel_vps' | 'extend_validity' | string
  status: 'completed' | 'failed' | string
  reason: string
  effective_date?: string | null
  created_at: string
  confirmed_at?: string | null
  completed_at?: string | null
  summary?: Record<string, unknown>
}

export type LifecycleActionResult = {
  action: LifecycleActionRecord
  steps: LifecycleActionStep[]
}

export type SubscriptionImpact = {
  record: SubscriptionRecord
  role: 'active' | 'inactive' | 'attention' | string
  recommended_action: string
  message: string
}

export type TargetImpact = {
  target_id: string
  name: string
  run_status: TargetRunStatus | string
  service_ids: string[]
  domain_ids: string[]
  last_linked_at?: string | null
}

export type RecommendedLifecycleStep = {
  object_type: 'vps' | 'subscription' | 'monitoring_instance' | 'target' | string
  object_id: string
  step_type:
    | 'vps_lifecycle'
    | 'subscription_status'
    | 'subscription_renew_at'
    | 'monitoring_instance_lifecycle'
    | 'monitoring_instance_monitoring'
    | 'target_run_status'
    | string
  from_state: string
  to_state: string
  required: boolean
  message: string
}

export type CancellationPreview = {
  vps: VPSAssetRecord
  subscriptions: SubscriptionImpact[]
  monitoring_instance_links: VPSMonitoringInstanceSummary[]
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
  target_links: TargetImpact[]
  recommended_steps: RecommendedLifecycleStep[]
  warnings: string[]
  blockers: string[]
}

export type ArchiveReview = {
  vps: VPSAssetRecord
  subscriptions: SubscriptionImpact[]
  monitoring_instance_links: VPSMonitoringInstanceSummary[]
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
  target_links: TargetImpact[]
  warnings: string[]
  blockers: string[]
  eligible: boolean
}

export type ApplyArchiveInput = {
  confirmation_name: string
}

export type MonitoringInstanceLifecycleActionInput = {
  monitoring_instance_id: string
  lifecycle_status?: string
  monitoring_status?: string
}

export type TargetLifecycleActionInput = {
  target_id: string
  run_status: TargetRunStatus | string
}

export type ApplyCancellationInput = {
  reason: string
  effective_date?: string | null
  subscription_ids: string[]
  vps_lifecycle_status: Extract<VPSLifecycleStatus, 'to_cancel' | 'cancelled'>
  monitoring_instance_actions: MonitoringInstanceLifecycleActionInput[]
  target_actions: TargetLifecycleActionInput[]
}

export type ExtendVPSValidityInput = {
  extend_to: string
  reason: string
  fee: number
  fee_currency: string
  source_type: string
}

export type LinkedVPSContext = {
  vps_id: string
  display_name: string
  lifecycle_status: VPSLifecycleStatus
  renewal_decision: VPSRenewalDecision
  subscription_state: SubscriptionStatus | 'missing' | string
  message: string
}

export type AssetContextForTarget = {
  target_id: string
  linked_vps_count: number
  cancellation_attention: boolean
  summaries: LinkedVPSContext[]
  service_ids: string[]
  domain_ids: string[]
}

export type CreateVPSAssetInput = {
  display_name: string
  provider_id?: string | null
  provider_name: string
  product_name: string
  order_ref: string
  country: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  ssh_host: string
  ssh_port?: number
  ssh_user: string
  os_name: string
  virtualization: string
  lifecycle_status?: VPSLifecycleStatus
  usage_status?: VPSUsageStatus
  renewal_decision?: VPSRenewalDecision
  importance: string
  labels: string[]
  note: string
}

export type UpdateVPSAssetInput = Partial<{
  display_name: string
  provider_id: string | null
  provider_name: string
  product_name: string
  order_ref: string
  country: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  os_name: string
  virtualization: string
  lifecycle_status: VPSLifecycleStatus
  usage_status: VPSUsageStatus
  renewal_decision: VPSRenewalDecision
  renewal_reason: string
  importance: string
  labels: string[]
  note: string
}>

export type VPSAssetListFilter = {
  provider_id?: string | null
  lifecycle_status?: VPSLifecycleStatus | '' | null
  usage_status?: VPSUsageStatus | '' | null
  renewal_decision?: VPSRenewalDecision | '' | null
  asset_scope?: AssetScope | null
}

export type VPSMonitoringInstanceSummary = {
  monitoring_instance_id: string
  display_name: string
  group: string
  region: string
  city: string
  provider: string
  lifecycle_status: string
  monitoring_status: string
  binding_status: string
  current_health_status: IncidentSeverity | string
  last_heartbeat_at?: string | null
  last_sync_at?: string | null
  current_active_incident_count: number
  current_primary_issue_summary: string
  linked_at: string
  note: string
}

export type VPSMonitoringInstanceLinkRecord = {
  link_id: string
  vps_id: string
  monitoring_instance_id: string
  linked_at: string
  unlinked_at?: string | null
  note: string
}

export type CreateVPSMonitoringInstanceInput = {
  display_name?: string
  group?: string
  region?: string
  city?: string
  provider?: string
  labels?: string[]
  note?: string
  link_note?: string
}

export type CreateVPSMonitoringInstanceResponse = MonitoringInstanceRecord & {
  link: VPSMonitoringInstanceLinkRecord
}

export type LinkVPSMonitoringInstanceInput = {
  monitoring_instance_id: string
  note?: string
}

export type UnlinkVPSMonitoringInstanceInput = {
  monitoring_instance_id: string
  note?: string
}

export type VPSAssetDetail = VPSAssetRecord & {
  monitoring_instance_links: VPSMonitoringInstanceSummary[]
}

export type VPSDecisionHistoryRecord = {
  decision_id: string
  vps_id: string
  from_decision?: VPSRenewalDecision | null
  to_decision: VPSRenewalDecision
  reason: string
  decided_at: string
  created_at: string
}

export type VPSPriceHistoryRecord = {
  price_history_id: string
  subscription_id: string
  vps_id: string
  from_price: number
  to_price: number
  from_currency: string
  to_currency: string
  from_billing_cycle: string
  to_billing_cycle: string
  from_billing_months: number
  to_billing_months: number
  from_billing_period_unit?: BillingPeriodUnit | string
  to_billing_period_unit?: BillingPeriodUnit | string
  from_billing_period_length?: number
  to_billing_period_length?: number
  from_monthly_price: number
  to_monthly_price: number
  from_renew_at?: string | null
  to_renew_at?: string | null
  from_auto_renew: boolean
  to_auto_renew: boolean
  from_auto_renew_cancelled: boolean
  to_auto_renew_cancelled: boolean
  from_renewal_mode?: RenewalMode | string
  to_renewal_mode?: RenewalMode | string
  from_status: SubscriptionStatus
  to_status: SubscriptionStatus
  changed_at: string
  created_at: string
}

export type VPSIPHistoryRecord = {
  ip_history_id: string
  vps_id: string
  from_ipv4: string
  to_ipv4: string
  from_ipv6: string
  to_ipv6: string
  changed_at: string
  created_at: string
}

export type VPSSpecSnapshotRecord = {
  snapshot_id: string
  vps_id: string
  product_name: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  os_name: string
  virtualization: string
  captured_at: string
  created_at: string
}

export type VPSExperienceLogRecord = {
  experience_log_id: string
  vps_id: string
  category: VPSExperienceCategory
  severity: VPSExperienceSeverity
  summary: string
  details: string
  occurred_at: string
  created_at: string
}

export type AssetServiceRecord = {
  service_id: string
  vps_id: string
  target_id?: string | null
  name: string
  service_type: AssetServiceType
  status: AssetServiceStatus
  url: string
  port?: number | null
  labels: string[]
  note: string
  created_at: string
  updated_at: string
}

export type CreateAssetServiceInput = {
  vps_id?: string
  target_id?: string | null
  name: string
  service_type?: AssetServiceType
  status?: AssetServiceStatus
  url?: string
  port?: number | null
  labels?: string[]
  note?: string
}

export type AssetServiceListFilter = {
  vps_id?: string | null
  target_id?: string | null
  service_type?: AssetServiceType | '' | null
  status?: AssetServiceStatus | '' | null
}

export type AssetDomainRecord = {
  domain_id: string
  vps_id: string
  service_id?: string | null
  target_id?: string | null
  domain_name: string
  purpose: string
  status: AssetDomainStatus
  registrar: string
  expires_at?: string | null
  auto_renew: boolean
  https_enabled: boolean
  labels: string[]
  note: string
  created_at: string
  updated_at: string
}

export type CreateAssetDomainInput = {
  vps_id?: string
  service_id?: string | null
  target_id?: string | null
  domain_name: string
  purpose?: string
  status?: AssetDomainStatus
  registrar?: string
  expires_at?: string | null
  auto_renew?: boolean
  https_enabled?: boolean
  labels?: string[]
  note?: string
}

export type AssetDomainListFilter = {
  vps_id?: string | null
  service_id?: string | null
  target_id?: string | null
  status?: AssetDomainStatus | '' | null
}

export type CreateVPSExperienceLogInput = {
  category: VPSExperienceCategory
  severity: VPSExperienceSeverity
  summary: string
  details?: string
  occurred_at?: string | null
}

export type VPSTimeline = {
  vps_id: string
  renewal_decisions: VPSDecisionHistoryRecord[]
  price_histories: VPSPriceHistoryRecord[]
  ip_histories: VPSIPHistoryRecord[]
  spec_snapshots: VPSSpecSnapshotRecord[]
  experience_logs: VPSExperienceLogRecord[]
}

export type VPSSummary = {
  vps_id: string
  display_name: string
  provider_id?: string | null
  provider_name: string
  country: string
  region: string
  city: string
  lifecycle_status: VPSLifecycleStatus | string
  usage_status: VPSUsageStatus | string
  renewal_decision: VPSRenewalDecision | string
  importance: string
  labels: string[]
  archived_at?: string | null
  linked_at: string
  note: string
}

export type SubscriptionRecord = {
  subscription_id: string
  vps_id: string
  price: number
  currency: string
  billing_cycle: string
  billing_months: number
  billing_period_unit?: BillingPeriodUnit | string
  billing_period_length?: number
  monthly_price: number
  started_at?: string | null
  renew_at?: string | null
  auto_renew: boolean
  auto_renew_cancelled: boolean
  renewal_mode?: RenewalMode | string
  status: SubscriptionStatus
  payment_method: string
  display_name?: string
  cost_category?: string
  labels?: string[]
  trial_ends_at?: string | null
  ends_at?: string | null
  note: string
  monthly_price_base?: number | null
  yearly_price_base?: number | null
  base_currency?: string
  exchange_rate?: number | null
  exchange_rate_date?: string | null
  exchange_rate_stale?: boolean
  budget_status?: SubscriptionBudgetStatus | string
  next_reminder_at?: string | null
  created_at: string
  updated_at: string
}

export type SubscriptionBudgetStatus = 'disabled' | 'ok' | 'warning' | 'over' | 'unknown'

export type CreateSubscriptionInput = {
  vps_id: string
  price: number
  currency: string
  billing_cycle: string
  billing_months: number
  billing_period_unit?: BillingPeriodUnit | string
  billing_period_length?: number
  started_at?: string | null
  renew_at?: string | null
  auto_renew: boolean
  auto_renew_cancelled: boolean
  renewal_mode?: RenewalMode | string
  status?: SubscriptionStatus
  payment_method: string
  display_name?: string
  cost_category?: string
  labels?: string[]
  trial_ends_at?: string | null
  ends_at?: string | null
  note: string
}

export type CreateVPSSubscriptionInput = Omit<CreateSubscriptionInput, 'vps_id' | 'status'>

export type UpdateSubscriptionInput = Partial<CreateSubscriptionInput>

export type SubscriptionListFilter = {
  vps_id?: string | null
  status?: SubscriptionStatus | '' | null
  renew_before?: string | null
  renew_after?: string | null
  renew_within_days?: number | null
  currency?: string | null
  provider_id?: string | null
  budget_status?: SubscriptionBudgetStatus | '' | null
  auto_renew?: boolean | null
  payment_method?: string | null
  label?: string | null
  renewal_decision?: string | null
  sort?: 'renew_at' | '' | null
  order?: 'asc' | 'desc' | '' | null
  asset_scope?: AssetScope | null
}

export type AssetScope = 'current' | 'archived' | 'historical' | 'all'

export type SubscriptionBreakdownItem = {
  key: string
  label: string
  monthly_cost: number
  yearly_cost: number
  subscription_count: number
}

export type SubscriptionRenewalQueueItem = {
  subscription_id: string
  vps_id: string
  vps_display_name: string
  display_name: string
  provider_name: string
  renew_at?: string | null
  monthly_price_base?: number | null
  yearly_price_base?: number | null
  base_currency: string
  currency: string
  renewal_decision: string
  lifecycle_status: string
  exchange_rate_stale: boolean
}

export type MissingSubscriptionAsset = {
  vps_id: string
  display_name: string
  provider_id?: string
  provider_name: string
  lifecycle_status: string
  renewal_decision: string
}

export type SubscriptionCostRow = {
  subscription_id: string
  vps_id: string
  vps_display_name: string
  provider_id: string
  provider_name: string
  display_name: string
  cost_category: string
  labels: string[]
  price: number
  currency: string
  monthly_price: number
  monthly_price_base?: number | null
  yearly_price_base?: number | null
  base_currency: string
  exchange_rate?: number | null
  exchange_rate_date?: string | null
  exchange_rate_stale: boolean
  renew_at?: string | null
  next_reminder_at?: string | null
  status: string
  payment_method: string
  country: string
  region: string
  lifecycle_status: string
  renewal_decision: string
  budget_status: SubscriptionBudgetStatus | string
}

export type SubscriptionBudgetRecord = {
  budget_id: string
  scope_type: 'global' | 'provider' | 'label' | 'category' | 'vps' | string
  scope_id: string
  name: string
  base_currency: string
  monthly_limit?: number | null
  yearly_limit?: number | null
  warning_pct: number
  enabled: boolean
  note: string
  current_monthly_spend: number
  current_yearly_spend: number
  status: SubscriptionBudgetStatus
  created_at: string
  updated_at: string
}

export type CreateSubscriptionBudgetInput = {
  scope_type: string
  scope_id: string
  name: string
  base_currency: string
  monthly_limit?: number | null
  yearly_limit?: number | null
  warning_pct: number
  enabled: boolean
  note: string
}

export type PatchSubscriptionBudgetInput = Partial<CreateSubscriptionBudgetInput> & {
  budget_id: string
}

export type SubscriptionMonthlyBudgetRecord = {
  budget_month: string
  base_currency: string
  monthly_limit: number
  warning_pct: number
  note: string
  created_at: string
  updated_at: string
}

export type UpsertSubscriptionMonthlyBudgetInput = {
  budget_month?: string
  base_currency: string
  monthly_limit: number
  warning_pct: number
  note: string
}

export type SubscriptionMonthlyBudgetBulkScope = 'all_history' | 'recent_year' | 'current_year'

export type BulkUpsertSubscriptionMonthlyBudgetInput = {
  scope: SubscriptionMonthlyBudgetBulkScope
  base_currency: string
  monthly_limit: number
  warning_pct: number
  note: string
}

export type BulkUpsertSubscriptionMonthlyBudgetResult = {
  scope: SubscriptionMonthlyBudgetBulkScope
  start_month: string
  end_month: string
  records: SubscriptionMonthlyBudgetRecord[]
}

export type SubscriptionOverview = {
  snapshot_generated_at: string
  base_currency: string
  total_monthly_cost: number
  total_yearly_cost: number
  active_subscription_count: number
  renewal_due_14d_count: number
  renewal_due_30d_count: number
  budget_risk_count: number
  exchange_rate_stale_count: number
  decision_attention_count: number
  missing_subscription_vps_count: number
  upcoming_renewals: SubscriptionRenewalQueueItem[]
  provider_breakdown: SubscriptionBreakdownItem[]
  currency_breakdown: SubscriptionBreakdownItem[]
  category_breakdown: SubscriptionBreakdownItem[]
  budget_risks: SubscriptionBudgetRecord[]
  vps_costs: SubscriptionCostRow[]
  missing_subscription_assets: MissingSubscriptionAsset[]
}

export type SubscriptionSeriesPoint = {
  bucket: string
  monthly_cost: number
  renewal_count: number
  budget_limit?: number | null
  budget_currency?: string
  budget_warning_pct?: number
  data_insufficient: boolean
}

export type SubscriptionStatistics = {
  window: 'month' | 'quarter' | 'year' | string
  base_currency: string
  total_monthly_cost: number
  total_yearly_cost: number
  provider_breakdown: SubscriptionBreakdownItem[]
  currency_breakdown: SubscriptionBreakdownItem[]
  category_breakdown: SubscriptionBreakdownItem[]
  payment_breakdown: SubscriptionBreakdownItem[]
  region_breakdown: SubscriptionBreakdownItem[]
  cost_month_buckets: SubscriptionSeriesPoint[]
  renewal_month_buckets: SubscriptionSeriesPoint[]
  budget_statuses: SubscriptionBudgetRecord[]
}

export type ExchangeRateFetchResult = {
  quote_currency: string
  base_currency: string
  rate: number
  rate_date?: string
  error?: string
}

export type ExchangeRateRefreshResult = {
  provider: string
  base_currency: string
  fetched_at: string
  succeeded: ExchangeRateFetchResult[]
  failed: ExchangeRateFetchResult[]
}

export type SettingsUpdateInput = {
  telegram: SettingsTelegramInput
  feishu: FeishuSettingsInput
  host_sample_frequency_tier: string
  probe_frequency_defaults: ProbeFrequencyDefaults
  incident_defaults: IncidentDefaults
  override_rules: OverrideRules
  retention_policy: RetentionPolicy
  ip_quality_settings: IPQualitySettings
  subscription_cost_settings?: SubscriptionCostSettingsUpdateInput
}

// Records v1 transport contracts.
export type EvidenceKindName =
  | 'ip_quality.report'
  | 'monitoring.host'
  | 'monitoring.probe'
  | 'monitoring.event'
  | 'subscription.cost'
  | 'command.audit'

export type EvidenceTimeWindow = {
  start: string
  end: string
}

export type EvidenceIdentitySnapshot = {
  type: string
  id: string
  display_name?: string
  provider?: string
  region?: string
  purpose?: string
  version?: string
  target_type?: string
}

export type EvidenceQuality = {
  status: 'complete' | 'partial' | 'degraded' | 'unknown'
  sample_count: number
  gap_count: number
  maintenance_count: number
  backfilled_count: number
  bucket_count: number
  data_point_count: number
  peak_count: number
  truncated: boolean
  partial: boolean
}

export type EvidenceUnits = {
  status: 'applicable' | 'not_applicable'
  values: Record<string, string>
  reason?: string
}

export type EvidenceQuota = {
  status: 'allowed' | 'warning' | 'exceeded' | 'unavailable'
  reason?: string
}

export type EvidenceRetention = {
  immutable: boolean
  scope: 'record_revision'
  source_deletion: 'snapshot_retained_source_unavailable'
}

export type EvidenceFieldDecision = {
  path: string
  sensitivity: 'normal' | 'sensitive_topology' | 'forbidden'
  action: 'included' | 'stripped' | 'masked' | 'forbidden'
}

export type EvidenceCapturePreviewInput = {
  record_id?: string
  kind: EvidenceKindName
  schema_version: number
  source_type: string
  source_id: string
  requested_window: EvidenceTimeWindow
  metrics: string[]
  precision_seconds: number
  sensitive_topology_fields: string[]
}

type EvidenceEnvelopeTransport = {
  record_id: string
  snapshot_id: string
  kind: EvidenceKindName
  schema_version: number
  subject: EvidenceIdentitySnapshot
  source: EvidenceIdentitySnapshot
  requested_window: EvidenceTimeWindow
  actual_window: EvidenceTimeWindow
  observed_at: string
  source_revision: string
  source_watermark: string
  producer_version: string
  calculation_version: string
  units: EvidenceUnits
  quality: EvidenceQuality
  sensitivity: 'normal' | 'sensitive_topology' | 'forbidden'
  actual_precision_seconds: number
  bucket_width_seconds: number
  quota: EvidenceQuota
  retention: EvidenceRetention
  redaction: EvidenceFieldDecision[]
  renderer_version: string
}

export type EvidenceCapturePreview = EvidenceEnvelopeTransport & {
  capture_intent_id: string
  estimated_canonical_bytes: number
  previewed_at: string
  valid_until: string
}

export type EvidenceSnapshotRead = EvidenceEnvelopeTransport & {
  captured_at: string
  referenced_at: string
  source_available: boolean
  title: string
  read_model: unknown
}

export type EvidenceCaptureReference = {
  record_id: string
  capture_intent_id: string
}

export type RecordLifecycle = 'active' | 'archived'

export type RecordType =
  | 'troubleshooting'
  | 'maintenance'
  | 'migration'
  | 'provider_communication'
  | 'billing'
  | 'important_finding'
  | 'note'

export type RecordBusinessStatus =
  | 'pending_investigation'
  | 'investigating'
  | 'verifying'
  | 'resolved'
  | 'closed'
  | 'cancelled'
  | 'planned'
  | 'executing'
  | 'completed'
  | 'pending_contact'
  | 'waiting_provider'
  | 'waiting_internal'
  | 'pending_review'
  | 'processing'

export type RecordStatusGroup =
  | 'pending'
  | 'in_progress'
  | 'waiting'
  | 'verification'
  | 'completed'
  | 'cancelled'

export type RecordSubjectKind = 'vps' | 'monitoring_instance' | 'target'
export type RecordRelationRole = 'affected' | 'context' | 'evidence_source'
export type RecordVisibilityKind = 'project' | 'restricted'
export type RecordVisibilityRole = 'project_admin' | 'viewer'
export type RecordSort = 'updated_at_desc' | 'updated_at_asc'

export type RecordVisibility = {
  kind: RecordVisibilityKind
  allowed_roles: RecordVisibilityRole[]
  allowed_group_ids: string[]
}

export type RecordSubjectReference = {
  registry_version: number
  kind: RecordSubjectKind
  role: RecordRelationRole
  source_id: string
  primary: boolean
}

export type RecordTemplate = {
  id: string
  version: number
}

export type AttachmentUploadState =
  | 'created'
  | 'uploading'
  | 'quarantined'
  | 'available'
  | 'rejected'
  | 'expired'

export type AttachmentQuota = {
  logical_bytes: number
  reserved_bytes: number
  physical_bytes: number
  effective_record_bytes: number
  project_warning: boolean
}

type AttachmentUploadTargetBase = {
  upload_url: string
  method: 'PUT'
  required_headers: string[]
}

export type AttachmentUploadTarget = AttachmentUploadTargetBase & ({
  transport: 'local'
  temporary_object_key?: never
} | {
  transport: 's3'
  temporary_object_key: string
})

export type CreateAttachmentUploadInput = {
  draft_id: string
  display_name: string
  media_type: string
  declared_size_bytes: number
}

export type AttachmentUploadSession = {
  upload_id: string
  attachment_id: string
  state: 'created' | 'uploading'
  expires_at: string
  quota: AttachmentQuota
  target: AttachmentUploadTarget
}

export type AttachmentUploadCompletion = {
  upload_id: string
  attachment_id: string
  state: 'quarantined' | 'available'
  quota: AttachmentQuota
}

export type AttachmentMetadata = {
  attachment_id: string
  state: AttachmentUploadState
  display_name: string
  media_type: string
  size_bytes: number
  preview_available: boolean
}

export type AttachmentContentVariant = 'original' | 'preview'

export type RecordDraftPayload = {
  title: string
  body_markdown: string
  markdown_dialect_version: 1
  record_type: RecordType
  business_status: RecordBusinessStatus | ''
  impact_level: string
  occurred_at?: string | null
  completed_at?: string | null
  visibility: RecordVisibility
  subjects: RecordSubjectReference[]
  tags: string[]
  attachment_ids: string[]
  owner_id: string
  participant_ids: string[]
  follow_up_at?: string | null
  template?: RecordTemplate | null
  save_reason: string
}

export type RecordSubjectIdentity = {
  display_name: string
  provider?: string
  region?: string
  purpose?: string
  version?: string
  target_type?: string
}

export type RecordSubject = RecordSubjectReference & {
  identity: RecordSubjectIdentity
}

export type RecordParticipant = {
  participant_id: string
  display_name: string
}

export type RecordRevision = {
  record_id: string
  revision_id: string
  base_revision_id?: string
  revision_no: number
  title: string
  body_markdown: string
  markdown_dialect_version: 1
  record_type: RecordType
  business_status?: RecordBusinessStatus
  status_group?: RecordStatusGroup
  impact_level: string
  occurred_at?: string
  completed_at?: string
  visibility: RecordVisibility
  subjects: RecordSubject[]
  tags: string[]
  attachment_ids: string[]
  owner_id?: string
  participants: RecordParticipant[]
  follow_up_at?: string
  template?: RecordTemplate
  author_id: string
  save_reason: string
  created_at: string
}

export type RecordCapabilities = {
  read: boolean
  update: boolean
  archive: boolean
  restore: boolean
  draft: boolean
  permanent_delete: boolean
}

export type RecordDetail = {
  record_id: string
  lifecycle: RecordLifecycle
  current_revision_id: string
  lock_version: number
  authorization_epoch: number
  current: RecordRevision
  capabilities: RecordCapabilities
  created_at: string
  updated_at: string
  archived_at?: string
}

export type RecordListResponse = {
  items: RecordDetail[]
  next_cursor?: string
}

export type RecordRevisionListResponse = {
  items: RecordRevision[]
}

export type RecordMutationResult = {
  record_id: string
  revision_id: string
  revision_no: number
  lock_version: number
  authorization_epoch: number
  lifecycle: RecordLifecycle
  created: boolean
  replayed: boolean
  committed_at: string
}

export type RecordLifecycleResult = {
  record_id: string
  current_revision_id: string
  lock_version: number
  authorization_epoch: number
  lifecycle: RecordLifecycle
  replayed: boolean
  changed_at: string
}

export type RecordDraft = {
  draft_id: string
  record_id?: string
  base_revision_id?: string
  payload: RecordDraftPayload
  version: number
  etag: string
  warning_at: string
  created_at: string
  updated_at: string
  expires_at: string
}

export type RecordDraftListResponse = {
  items: RecordDraft[]
}

export type RecordListFilter = {
  q?: string
  lifecycle?: RecordLifecycle
  record_type?: RecordType
  sort?: RecordSort
  limit?: number
  cursor?: string
}

export type RecordRevisionListFilter = {
  limit?: number
}

export type RecordDraftListFilter = {
  limit?: number
}

export type CreateRecordDraftInput = ({
  record_id?: never
  base_revision_id?: never
} | {
  record_id: string
  base_revision_id: string
}) & {
  payload: RecordDraftPayload
}

export type PatchRecordDraftInput = {
  payload: RecordDraftPayload
}

export type PublishRecordInput = {
  draft_id: string
  draft_etag: string
}

export type PublishRecordRevisionInput = PublishRecordInput & {
  base_revision_id: string
  lock_version: number
  authorization_epoch: number
}

export type RestoreRecordRevisionInput = {
  save_reason: string
}

export type RecordDraftConflictRecovery = {
  server_draft: RecordDraft
  local_payload: RecordDraftPayload
}

export type RecordRevisionConflictRecovery = {
  server_revision_id: string
  server_lock_version: number
  server_authorization_epoch: number
  draft: RecordDraft
}

export type RecordErrorRecovery = RecordDraftConflictRecovery | RecordRevisionConflictRecovery

export type RecordDeletionScope =
  | 'record_core'
  | 'record_attachments'
  | 'record_evidence'
  | 'record_markdown_client'
  | 'record_search'
  | 'record_activity_projection'
  | 'record_comparison'
  | 'record_collaboration'
  | 'record_portability'

export type RecordSurvivingCopyKind =
  | 'other_record'
  | 'delivered_export'
  | 'delivered_notification'
  | 'offline_browser_buffer'
  | 'possible_external_delivery'

export type RecordDeletionSurvivingCopy = {
  scope: RecordDeletionScope
  kind: RecordSurvivingCopyKind
  copy_count: number
}

export type RecordDeletionManagedBackup = {
  retained_copy_count: number
  maximum_retention_days: number
  latest_expires_at: string | null
}

export type RecordDeletionPreview = {
  reservation_id: string
  deletion_request_token: string
  expires_at: string
  online_purge_scopes: RecordDeletionScope[]
  surviving_copies: RecordDeletionSurvivingCopy[]
  managed_backup: RecordDeletionManagedBackup
  ledger_health: 'healthy'
}

export type RecordDeletionExecuteInput = {
  reservation_id: string
  deletion_request_token: string
}

export type RecordDeletionState =
  | 'provisional_fenced'
  | 'ledger_commit_unknown'
  | 'witness_pending'
  | 'delete_requested'
  | 'fence_propagating'
  | 'read_fenced'
  | 'online_purging'
  | 'online_purged'
  | 'release_pending'
  | 'not_committed'
  | 'retry_required'

export type RecordDeletionOperation = {
  operation_id: string
  state: RecordDeletionState
}

export type RecordActionStatus = 'open' | 'completed' | 'cancelled'
export type RecordActionTransition = 'complete' | 'cancel' | 'reopen'

export type RecordAction = {
  action_id: string
  record_id: string
  version: number
  status: RecordActionStatus
  title: string
  assignee_id: string
  due_at: string | null
  completed_at: string | null
  subject_revision_id: string
  created_at: string
  updated_at: string
}

export type RecordActionListResponse = { items: RecordAction[] }

export type RecordActionInput = {
  title: string
  details: string
  assignee_id: string
  due_at: string | null
  subject_revision_id: string
}

export type RecordActionMutation = {
  action_id: string
  record_id: string
  version: number
  status: RecordActionStatus
  event_kind: 'created' | 'updated' | 'completed' | 'cancelled' | 'reopened'
  replayed: boolean
  changed_at: string
}

export type RecordCommentState = 'active' | 'redacted'

export type RecordComment = {
  comment_id: string
  record_id: string
  author_id: string
  version: number
  state: RecordCommentState
  body_markdown: string | null
  render_model: unknown | null
  reply_to_comment_id: string
  mention_user_ids: string[]
  created_at: string
  updated_at: string
  redacted_at: string | null
}

export type RecordCommentListResponse = { comments: RecordComment[] }

export type RecordCommentInput = {
  body_markdown: string
  reply_to_comment_id: string
  mention_user_ids: string[]
}

export type RecordCommentMutation = {
  comment_id: string
  record_id: string
  version: number
  state: RecordCommentState
  event_kind: 'created' | 'edited' | 'redacted'
  replayed: boolean
  changed_at: string
}

export type RecordFollowerPreference = 'default' | 'watching' | 'muted'

export type RecordWatch = {
  record_id: string
  user_id: string
  version: number
  preference: RecordFollowerPreference
  sources: {
    author: boolean
    owner: boolean
    participant: boolean
    comment: boolean
    mention: boolean
    action: boolean
  }
  updated_at: string | null
}

export type RecordNotification = {
  notification_id: string
  record_id: string
  event_kind: 'record_action_assigned' | 'record_action_completed' | 'record_action_cancelled' | 'record_comment_replied' | 'record_comment_mentioned'
  subject_kind: 'action' | 'comment'
  subject_id: string
  source_version: number
  reason: 'owner' | 'participant' | 'assignee' | 'mention' | 'reply' | 'follower' | 'security'
  mandatory: boolean
  event_at: string
  read_at: string | null
  dismissed_at: string | null
}

export type RecordNotificationListResponse = { items: RecordNotification[] }
export type RecordNotificationUnreadResponse = { unread_count: number }
export type RecordNotificationTarget = {
  record_id: string
  subject_kind: 'action' | 'comment'
  subject_id: string
}
