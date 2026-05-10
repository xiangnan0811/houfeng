export type NodeRecord = {
  node_id: string
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
  created_at: string
  updated_at: string
}

export type LastAction = {
  action_id: string
  command_id: string
  status: 'pending' | 'done'
  stdout?: string
  stderr?: string
  exit_code?: number
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

export type NodeEnrollmentTokenIssue = {
  token: string
  issued_at: string
}

export type UpdateNodeMetadataInput = {
  labels: string[]
  group?: string
  note: string
}

export type CreateNodeInput = {
  display_name: string
  group: string
  region: string
  city: string
  provider: string
  labels: string[]
  note: string
}

export type NodeOnboardingState = NodeRecord & {
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
  node_id: string
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
  swap_used_pct: number
  disk_used_pct: number
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

export type NodeRuntimeFacts = {
  node_id: string
  latest_host_sample: HostSample | null
  recent_host_samples: HostSample[]
}

export type TargetType = 'service' | 'china_reference'

export type TargetRunStatus = '启用' | '维护中' | '暂停' | '已归档'

export type TargetRecord = {
  target_id: string
  name: string
  target_type: TargetType
  host: string
  base_port?: number
  execution_node_labels: string[]
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
  execution_node_labels: string[]
  run_status: TargetRunStatus
  group: string
  labels: string[]
  note: string
}

export type ProbeKind = 'tcp' | 'http' | 'tls'

export type FrequencyTier = '1m' | '5m' | '15m' | '6h'

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
  node_id: string
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

export type ObservabilityObjectType = 'node' | 'target'

export type IncidentSeverity = '正常' | '关注' | '告警' | '严重'

export type StateChangeEventType =
  | 'incident_started'
  | 'incident_escalated'
  | 'incident_recovered'
  | 'node_binding_rebind_confirmed'
  | 'node_binding_pending_rejected'
  | 'node_binding_reset'
  | 'node_retired'
  | 'node_restored_to_observing'
  | 'node_monitoring_maintenance_entered'
  | 'node_monitoring_maintenance_exited'
  | 'node_monitoring_paused'
  | 'node_monitoring_resumed'
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
  node_binding_rebind_confirmed: '确认重新绑定',
  node_binding_pending_rejected: '拒绝待确认指纹',
  node_binding_reset: '绑定已重置',
  node_retired: '节点已退役',
  node_restored_to_observing: '节点恢复到观察中',
  node_monitoring_maintenance_entered: '节点进入维护',
  node_monitoring_maintenance_exited: '节点退出维护',
  node_monitoring_paused: '节点暂停监控',
  node_monitoring_resumed: '节点恢复监控',
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

export type DashboardOverview = {
  snapshot_generated_at: string
  total_node_count: number
  total_target_count: number
  abnormal_node_count: number
  abnormal_target_count: number
  severe_node_count: number
  severe_target_count: number
  maintenance_node_count: number
  maintenance_target_count: number
  pending_onboarding_node_count: number
  paused_node_count: number
  retired_node_count: number
  paused_target_count: number
  archived_target_count: number
  recent_new_incident_count: number
  recent_recovery_count: number
  group_summaries: DashboardGroupSummary[]
  notification_status: DashboardNotificationStatus
  asset_summary: DashboardAssetSummary
  abnormal_nodes: DashboardNodeSummary[]
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
  node_count: number
  target_count: number
  abnormal_node_count: number
  abnormal_target_count: number
  severe_node_count: number
  severe_target_count: number
  maintenance_node_count: number
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
  to_migrate_vps_count: number
  unlinked_vps_count: number
  abnormal_linked_vps_count: number
  cost_by_currency: DashboardAssetCostByCurrency[]
}

export type DashboardAssetCostByCurrency = {
  currency: string
  monthly_total: number
  yearly_total: number
}

export type DashboardNodeSummary = {
  node_id: string
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
  webhook_url: string
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

export type NodeLabelOverrideRule = {
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
  node_labels: NodeLabelOverrideRule[]
  target_types: TargetTypeOverrideRule[]
  target_labels: TargetLabelOverrideRule[]
}

export type RetentionPolicy = {
  raw_layer_days: number
  aggregate_layer_days: number
  event_layer_days: number
  notification_layer_days: number
}

export type SettingsRecord = {
  telegram: SettingsTelegramResponse
  feishu: FeishuSettingsResponse
  host_sample_frequency_tier: string
  probe_frequency_defaults: ProbeFrequencyDefaults
  incident_defaults: IncidentDefaults
  override_rules: OverrideRules
  retention_policy: RetentionPolicy
}

export type NodeSparklinesResponse = {
  nodes: Record<string, Record<string, (number | null)[]>>
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

export type SubscriptionStatus = 'active' | 'paused' | 'cancelled' | 'expired' | 'unknown'

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
  active_node_link_count: number
  created_at: string
  updated_at: string
  archived_at?: string | null
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
  lifecycle_status: VPSLifecycleStatus
  usage_status: VPSUsageStatus
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
}

export type VPSNodeSummary = {
  node_id: string
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

export type VPSNodeLinkRecord = {
  link_id: string
  vps_id: string
  node_id: string
  linked_at: string
  unlinked_at?: string | null
  note: string
}

export type LinkVPSNodeInput = {
  node_id: string
  note?: string
}

export type UnlinkVPSNodeInput = {
  node_id: string
  note?: string
}

export type VPSAssetDetail = VPSAssetRecord & {
  node_links: VPSNodeSummary[]
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
  from_monthly_price: number
  to_monthly_price: number
  from_renew_at?: string | null
  to_renew_at?: string | null
  from_auto_renew: boolean
  to_auto_renew: boolean
  from_auto_renew_cancelled: boolean
  to_auto_renew_cancelled: boolean
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
  monthly_price: number
  started_at?: string | null
  renew_at?: string | null
  auto_renew: boolean
  auto_renew_cancelled: boolean
  status: SubscriptionStatus
  payment_method: string
  note: string
  created_at: string
  updated_at: string
}

export type CreateSubscriptionInput = {
  vps_id: string
  price: number
  currency: string
  billing_cycle: string
  billing_months: number
  started_at?: string | null
  renew_at?: string | null
  auto_renew: boolean
  auto_renew_cancelled: boolean
  status?: SubscriptionStatus
  payment_method: string
  note: string
}

export type UpdateSubscriptionInput = Partial<CreateSubscriptionInput>

export type SubscriptionListFilter = {
  vps_id?: string | null
  status?: SubscriptionStatus | '' | null
  renew_before?: string | null
  renew_after?: string | null
  renew_within_days?: number | null
  sort?: 'renew_at' | '' | null
  order?: 'asc' | 'desc' | '' | null
}

export type SettingsUpdateInput = {
  telegram: SettingsTelegramInput
  feishu: FeishuSettingsInput
  host_sample_frequency_tier: string
  probe_frequency_defaults: ProbeFrequencyDefaults
  incident_defaults: IncidentDefaults
  override_rules: OverrideRules
  retention_policy: RetentionPolicy
}
