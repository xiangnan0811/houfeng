create table if not exists public.record_activity_projection_heads (
  project_id text not null default 'default' check (project_id = 'default'),
  projection_generation bigint not null check (projection_generation > 0),
  head_state text not null default 'active'
    check (head_state in ('active', 'retired')),
  published_ingest_sequence bigint not null default 0
    check (published_ingest_sequence >= 0),
  allocated_ingest_sequence bigint not null default 0
    check (allocated_ingest_sequence >= 0),
  readiness_digest bytea
    check (readiness_digest is null or octet_length(readiness_digest) = 32),
  started_at timestamptz not null default now(),
  retired_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (project_id, projection_generation),
  check (allocated_ingest_sequence >= published_ingest_sequence),
  check ((head_state = 'retired') = (retired_at is not null)),
  check (retired_at is null or retired_at >= started_at),
  check (updated_at >= created_at)
);

create unique index if not exists uq_record_activity_projection_heads_active
  on public.record_activity_projection_heads(project_id)
  where head_state = 'active';

create table if not exists public.record_activity_projection (
  activity_id text primary key check (activity_id ~ '^act_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  projection_generation bigint not null check (projection_generation > 0),
  ingest_sequence bigint not null check (ingest_sequence > 0),
  event_kind text not null check (event_kind ~ '^[a-z][a-z0-9_]{1,62}$'),
  event_at timestamptz not null,
  recorded_at timestamptz not null,
  source_kind text not null
    check (source_kind in ('record_domain', 'evidence_snapshot', 'asset_history',
      'monitoring_event', 'command_audit')),
  source_event_id text not null check (source_event_id ~ '^[a-z0-9_-]{1,128}$'),
  source_version bigint not null check (source_version > 0),
  record_id text check (record_id is null or record_id ~ '^rec_[a-z0-9]{1,64}$'),
  revision_id text check (revision_id is null or revision_id ~ '^rrv_[a-z0-9]{1,64}$'),
  evidence_snapshot_id text
    check (evidence_snapshot_id is null or evidence_snapshot_id ~ '^evs_[a-z0-9]{1,64}$'),
  backfilled boolean not null default false,
  actor_id text check (actor_id is null or actor_id ~ '^usr_[a-z0-9]{1,64}$'),
  severity text not null default 'info'
    check (severity in ('info', 'notice', 'warning', 'critical')),
  presentation_version bigint not null check (presentation_version = 1),
  presentation_json jsonb not null
    check (jsonb_typeof(presentation_json) = 'object'
      and pg_column_size(presentation_json) <= 4096),
  auth_scope_digest bytea not null check (octet_length(auth_scope_digest) = 32),
  canonical_hash bytea not null check (octet_length(canonical_hash) = 32),
  corrects_activity_id text
    check (corrects_activity_id is null or corrects_activity_id ~ '^act_[a-z0-9]{1,64}$'),
  projected_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  unique (source_kind, source_event_id, source_version, event_kind),
  unique (projection_generation, ingest_sequence),
  check (corrects_activity_id is null or corrects_activity_id <> activity_id),
  check (revision_id is null or record_id is not null),
  foreign key (project_id, projection_generation)
    references public.record_activity_projection_heads(project_id, projection_generation)
    on delete restrict
);

create index if not exists idx_record_activity_projection_source
  on public.record_activity_projection(source_kind, source_event_id, source_version);
create index if not exists idx_record_activity_projection_watermark
  on public.record_activity_projection(projection_generation, ingest_sequence);
create index if not exists idx_record_activity_projection_corrections
  on public.record_activity_projection(corrects_activity_id)
  where corrects_activity_id is not null;
create index if not exists idx_record_activity_projection_record
  on public.record_activity_projection(record_id, ingest_sequence)
  where record_id is not null;

create table if not exists public.record_activity_subjects (
  activity_id text not null,
  subject_kind text not null
    check (subject_kind in ('vps', 'monitoring_instance', 'target')),
  subject_source_id text not null check (subject_source_id ~ '^[a-z0-9_-]{1,128}$'),
  relation_role text not null
    check (relation_role in ('affected', 'context', 'evidence_source')),
  is_primary boolean not null,
  relation_order integer not null default 0 check (relation_order >= 0),
  identity_snapshot jsonb not null
    check (jsonb_typeof(identity_snapshot) = 'object'
      and pg_column_size(identity_snapshot) <= 2048),
  live_route text check (live_route is null or live_route ~ '^/[a-z0-9/_-]{1,255}$'),
  tombstoned boolean not null default false,
  projection_generation bigint not null check (projection_generation > 0),
  ingest_sequence bigint not null check (ingest_sequence > 0),
  event_kind text not null check (event_kind ~ '^[a-z][a-z0-9_]{1,62}$'),
  source_kind text not null
    check (source_kind in ('record_domain', 'evidence_snapshot', 'asset_history',
      'monitoring_event', 'command_audit')),
  event_at timestamptz not null,
  recorded_at timestamptz not null,
  record_id text check (record_id is null or record_id ~ '^rec_[a-z0-9]{1,64}$'),
  revision_id text check (revision_id is null or revision_id ~ '^rrv_[a-z0-9]{1,64}$'),
  evidence_snapshot_id text
    check (evidence_snapshot_id is null or evidence_snapshot_id ~ '^evs_[a-z0-9]{1,64}$'),
  auth_scope_digest bytea not null check (octet_length(auth_scope_digest) = 32),
  relation_hash bytea not null check (octet_length(relation_hash) = 32),
  created_at timestamptz not null default now(),
  primary key (activity_id, subject_kind, subject_source_id, relation_role),
  check (live_route is null or not tombstoned),
  foreign key (activity_id) references public.record_activity_projection(activity_id)
    on delete cascade
);

create index if not exists idx_record_activity_subjects_timeline
  on public.record_activity_subjects
    (subject_kind, subject_source_id, event_at desc, recorded_at desc, source_kind, activity_id)
  include (ingest_sequence, event_kind, record_id, revision_id, evidence_snapshot_id,
    auth_scope_digest, relation_role);
create index if not exists idx_record_activity_subjects_event_kind
  on public.record_activity_subjects
    (subject_kind, subject_source_id, event_kind, event_at desc, recorded_at desc, source_kind, activity_id)
  include (ingest_sequence, record_id, revision_id, auth_scope_digest);
create index if not exists idx_record_activity_subjects_source_kind
  on public.record_activity_subjects
    (subject_kind, subject_source_id, source_kind, event_at desc, recorded_at desc, activity_id)
  include (ingest_sequence, event_kind, record_id, revision_id, auth_scope_digest);
create index if not exists idx_record_activity_subjects_watermark
  on public.record_activity_subjects(subject_kind, subject_source_id, ingest_sequence);
create index if not exists idx_record_activity_subjects_record
  on public.record_activity_subjects(record_id, activity_id)
  where record_id is not null;
create index if not exists idx_record_activity_subjects_evidence
  on public.record_activity_subjects(subject_kind, subject_source_id, evidence_snapshot_id, event_at desc)
  where evidence_snapshot_id is not null;

create table if not exists public.record_activity_projection_checkpoints (
  project_id text not null default 'default' check (project_id = 'default'),
  projection_generation bigint not null check (projection_generation > 0),
  source_kind text not null
    check (source_kind in ('record_domain', 'evidence_snapshot', 'asset_history',
      'monitoring_event', 'command_audit')),
  source_head_digest bytea not null check (octet_length(source_head_digest) = 32),
  source_cursor text not null default '' check (octet_length(source_cursor) <= 512),
  caught_up boolean not null default false,
  lease_owner_id text
    check (lease_owner_id is null or lease_owner_id ~ '^[a-z0-9_-]{1,128}$'),
  lease_expires_at timestamptz,
  last_success_at timestamptz,
  last_error_code text not null default ''
    check (last_error_code = '' or last_error_code ~ '^[a-z0-9_]{1,64}$'),
  attempt bigint not null default 0 check (attempt >= 0),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (project_id, projection_generation, source_kind),
  check ((lease_owner_id is null) = (lease_expires_at is null)),
  check (updated_at >= created_at),
  foreign key (project_id, projection_generation)
    references public.record_activity_projection_heads(project_id, projection_generation)
    on delete restrict
);

create index if not exists idx_record_activity_projection_checkpoints_lease
  on public.record_activity_projection_checkpoints(projection_generation, lease_expires_at)
  where lease_owner_id is not null;

create table if not exists public.record_activity_revision_intervals (
  project_id text not null default 'default' check (project_id = 'default'),
  projection_generation bigint not null check (projection_generation > 0),
  record_id text not null check (record_id ~ '^rec_[a-z0-9]{1,64}$'),
  revision_id text not null check (revision_id ~ '^rrv_[a-z0-9]{1,64}$'),
  valid_from_ingest_sequence bigint not null check (valid_from_ingest_sequence > 0),
  valid_to_ingest_sequence bigint check (valid_to_ingest_sequence is null or valid_to_ingest_sequence > 0),
  source_kind text not null
    check (source_kind in ('record_domain', 'evidence_snapshot', 'asset_history',
      'monitoring_event', 'command_audit')),
  source_event_id text not null check (source_event_id ~ '^[a-z0-9_-]{1,128}$'),
  source_version bigint not null check (source_version > 0),
  created_at timestamptz not null default now(),
  primary key (project_id, projection_generation, record_id, revision_id),
  check (valid_to_ingest_sequence is null or valid_to_ingest_sequence > valid_from_ingest_sequence),
  foreign key (project_id, projection_generation)
    references public.record_activity_projection_heads(project_id, projection_generation)
    on delete restrict
);

create index if not exists idx_record_activity_revision_intervals_validity
  on public.record_activity_revision_intervals
    (projection_generation, record_id, valid_from_ingest_sequence, valid_to_ingest_sequence)
  include (revision_id);

create unique index if not exists uq_record_activity_revision_intervals_open
  on public.record_activity_revision_intervals(projection_generation, record_id)
  where valid_to_ingest_sequence is null;
