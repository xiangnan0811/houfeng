create table if not exists experience_log_create_idempotency (
  idempotency_key text primary key,
  request_digest text not null,
  experience_log_id text not null references experience_logs(experience_log_id) on delete cascade,
  created_at timestamptz not null default now(),
  constraint experience_log_create_idempotency_key_format
    check (
      char_length(idempotency_key) between 8 and 128
      and idempotency_key ~ '^[A-Za-z0-9._:-]+$'
    ),
  constraint experience_log_create_idempotency_digest_sha256
    check (request_digest ~ '^[0-9a-f]{64}$')
);

create index if not exists idx_experience_log_create_idempotency_result
  on experience_log_create_idempotency (experience_log_id);

create table if not exists asset_service_create_idempotency (
  idempotency_key text primary key,
  request_digest text not null,
  service_id text not null references asset_services(service_id) on delete cascade,
  created_at timestamptz not null default now(),
  constraint asset_service_create_idempotency_key_format
    check (
      char_length(idempotency_key) between 8 and 128
      and idempotency_key ~ '^[A-Za-z0-9._:-]+$'
    ),
  constraint asset_service_create_idempotency_digest_sha256
    check (request_digest ~ '^[0-9a-f]{64}$')
);

create index if not exists idx_asset_service_create_idempotency_result
  on asset_service_create_idempotency (service_id);

create table if not exists asset_domain_create_idempotency (
  idempotency_key text primary key,
  request_digest text not null,
  domain_id text not null references asset_domains(domain_id) on delete cascade,
  created_at timestamptz not null default now(),
  constraint asset_domain_create_idempotency_key_format
    check (
      char_length(idempotency_key) between 8 and 128
      and idempotency_key ~ '^[A-Za-z0-9._:-]+$'
    ),
  constraint asset_domain_create_idempotency_digest_sha256
    check (request_digest ~ '^[0-9a-f]{64}$')
);

create index if not exists idx_asset_domain_create_idempotency_result
  on asset_domain_create_idempotency (domain_id);

create table if not exists vps_monitoring_instance_create_idempotency (
  idempotency_key text primary key,
  request_digest text not null,
  monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade,
  link_id text not null references vps_monitoring_instance_links(link_id) on delete cascade,
  created_at timestamptz not null default now(),
  constraint vps_monitoring_instance_create_idempotency_key_format
    check (
      char_length(idempotency_key) between 8 and 128
      and idempotency_key ~ '^[A-Za-z0-9._:-]+$'
    ),
  constraint vps_monitoring_instance_create_idempotency_digest_sha256
    check (request_digest ~ '^[0-9a-f]{64}$')
);

create index if not exists idx_vps_monitoring_instance_create_idempotency_instance
  on vps_monitoring_instance_create_idempotency (monitoring_instance_id);

create index if not exists idx_vps_monitoring_instance_create_idempotency_link
  on vps_monitoring_instance_create_idempotency (link_id);
