create table if not exists node_host_sample_daily_aggregates (
  node_id text not null references nodes(node_id) on delete cascade,
  bucket_date date not null,
  sample_count integer not null,
  avg_cpu_usage_pct double precision not null,
  max_cpu_usage_pct double precision not null,
  avg_load_5 double precision not null,
  max_load_5 double precision not null,
  avg_mem_used_pct double precision not null,
  max_mem_used_pct double precision not null,
  avg_cpu_iowait_pct double precision not null,
  max_cpu_iowait_pct double precision not null,
  avg_cpu_steal_pct double precision not null,
  max_cpu_steal_pct double precision not null,
  avg_disk_busy_pct double precision not null,
  max_disk_busy_pct double precision not null,
  backfilled_sample_count integer not null,
  maintenance_sample_count integer not null,
  updated_at timestamptz not null default now(),
  primary key (node_id, bucket_date)
);

create index if not exists idx_node_host_sample_daily_aggregates_bucket
  on node_host_sample_daily_aggregates (bucket_date desc);

create table if not exists target_probe_daily_aggregates (
  target_id text not null references targets(target_id) on delete cascade,
  probe_item_id text not null references probe_items(probe_item_id) on delete cascade,
  bucket_date date not null,
  observation_count integer not null,
  success_count integer not null,
  failure_count integer not null,
  avg_latency_ms double precision,
  p95_latency_ms double precision,
  min_tls_expiry_days integer,
  backfilled_observation_count integer not null,
  maintenance_observation_count integer not null,
  updated_at timestamptz not null default now(),
  primary key (target_id, probe_item_id, bucket_date)
);

create index if not exists idx_target_probe_daily_aggregates_bucket
  on target_probe_daily_aggregates (bucket_date desc);
