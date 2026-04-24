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
