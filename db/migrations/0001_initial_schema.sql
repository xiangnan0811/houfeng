create table if not exists nodes (
  node_id text primary key,
  display_name text not null,
  region text not null,
  city text not null,
  provider text not null,
  lifecycle_status text not null,
  monitoring_status text not null default 'enabled',
  binding_status text not null default 'unbound',
  enrollment_token_hash text,
  binding_fingerprint text,
  labels text[] not null default '{}',
  note text not null default '',
  current_health_status text not null default 'normal',
  last_heartbeat_at timestamptz,
  last_sync_at timestamptz,
  current_active_incident_count integer not null default 0,
  current_primary_issue_summary text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists targets (
  target_id text primary key,
  name text not null,
  target_type text not null,
  host text not null,
  base_port integer,
  execution_node_labels text[] not null default '{}',
  run_status text not null,
  labels text[] not null default '{}',
  note text not null default '',
  current_health_status text not null default 'normal',
  current_active_incident_count integer not null default 0,
  last_success_at timestamptz,
  last_failure_at timestamptz,
  current_primary_issue_summary text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists probe_items (
  probe_item_id text primary key,
  target_id text not null references targets(target_id) on delete cascade,
  probe_kind text not null,
  enabled boolean not null default true,
  frequency_tier text not null,
  timeout_seconds integer not null,
  config jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists node_heartbeats (
  id bigserial primary key,
  node_id text not null references nodes(node_id) on delete cascade,
  observed_at timestamptz not null,
  received_at timestamptz not null,
  agent_version text not null,
  fingerprint text not null,
  sync_batch_id text not null,
  is_backfilled boolean not null default false
);

create table if not exists host_samples (
  id bigserial primary key,
  node_id text not null references nodes(node_id) on delete cascade,
  observed_at timestamptz not null,
  received_at timestamptz not null,
  cpu_usage_pct double precision not null,
  load_1 double precision not null,
  load_5 double precision not null,
  load_15 double precision not null,
  mem_used_pct double precision not null,
  mem_available_bytes bigint not null,
  swap_used_pct double precision not null,
  disk_used_pct double precision not null,
  inode_used_pct double precision not null,
  net_in_bytes_per_sec bigint not null,
  net_out_bytes_per_sec bigint not null,
  cpu_iowait_pct double precision not null,
  cpu_steal_pct double precision not null,
  disk_read_bytes_per_sec bigint not null,
  disk_write_bytes_per_sec bigint not null,
  disk_busy_pct double precision not null,
  uptime_seconds bigint not null,
  maintenance_context boolean not null default false,
  is_backfilled boolean not null default false,
  sync_batch_id text not null
);

create table if not exists probe_observations (
  id bigserial primary key,
  node_id text not null references nodes(node_id) on delete cascade,
  target_id text not null references targets(target_id) on delete cascade,
  probe_item_id text not null references probe_items(probe_item_id) on delete cascade,
  observed_at timestamptz not null,
  received_at timestamptz not null,
  result_kind text not null,
  latency_ms integer,
  http_status integer,
  tls_expiry_days integer,
  error_code text,
  error_summary text,
  maintenance_context boolean not null default false,
  is_backfilled boolean not null default false,
  sync_batch_id text not null
);

create table if not exists active_incidents (
  incident_id text primary key,
  object_type text not null,
  object_id text not null,
  incident_class text not null,
  severity text not null,
  started_at timestamptz not null,
  last_evaluated_at timestamptz not null,
  status text not null,
  source_summary text not null
);

create table if not exists state_change_events (
  event_id text primary key,
  object_type text not null,
  object_id text not null,
  event_type text not null,
  severity text,
  summary text not null,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create table if not exists notification_records (
  notification_id text primary key,
  incident_id text,
  object_type text not null,
  object_id text not null,
  channel text not null,
  delivery_status text not null,
  summary text not null,
  sent_at timestamptz,
  created_at timestamptz not null default now()
);

create index if not exists idx_node_heartbeats_node_time on node_heartbeats (node_id, observed_at desc);
create index if not exists idx_host_samples_node_time on host_samples (node_id, observed_at desc);
create index if not exists idx_probe_observations_target_time on probe_observations (target_id, observed_at desc);
create index if not exists idx_probe_observations_node_time on probe_observations (node_id, observed_at desc);
create index if not exists idx_active_incidents_object on active_incidents (object_type, object_id);
create index if not exists idx_state_change_events_object_time on state_change_events (object_type, object_id, created_at desc);
