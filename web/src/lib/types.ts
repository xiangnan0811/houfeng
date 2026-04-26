export type NodeRecord = {
  node_id: string
  display_name: string
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
  created_at: string
  updated_at: string
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

export type NodeOnboardingState = NodeRecord & {
  phase: OnboardingPhase
  has_host_sample: boolean
  has_accepted_observation: boolean
  enrollment_token_issued_at?: string
  current_binding_fingerprint_summary?: string
  pending_binding?: PendingBindingMetadata
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
}

export type NodeRuntimeFacts = {
  node_id: string
  latest_host_sample: HostSample | null
}

export type TargetRecord = {
  target_id: string
  name: string
  target_type: string
  host: string
  base_port?: number
  execution_node_labels: string[]
  run_status: string
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

export type ProbeItemRecord = {
  probe_item_id: string
  target_id: string
  probe_kind: string
  enabled: boolean
  frequency_tier: string
  timeout_seconds: number
  config: Record<string, unknown>
  created_at: string
  updated_at: string
}

export type ProbeObservation = {
  node_id: string
  target_id: string
  probe_item_id: string
  probe_kind: string
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
  abnormal_node_count: number
  abnormal_target_count: number
  severe_node_count: number
  severe_target_count: number
  maintenance_node_count: number
  maintenance_target_count: number
  recent_new_incident_count: number
  recent_recovery_count: number
  recent_events: StateChangeEventRecord[]
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
  limit?: number
}

export type IncidentListFilter = {
  object_type?: ObservabilityObjectType | ''
  object_id?: string
  severity?: IncidentSeverity | ''
  limit?: number
}
