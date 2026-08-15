create table if not exists public.evidence_payloads (
  payload_digest bytea primary key
    check (octet_length(payload_digest) = 32),
  payload_encoding text not null default 'canonical_json_gzip_v1'
    check (payload_encoding = 'canonical_json_gzip_v1'),
  canonical_size_bytes bigint not null
    check (canonical_size_bytes between 1 and 5242880),
  compressed_size_bytes bigint not null
    check (compressed_size_bytes between 1 and 6291456),
  compressed_payload bytea not null,
  created_at timestamptz not null default now(),
  check (octet_length(compressed_payload) = compressed_size_bytes),
  unique (payload_digest, canonical_size_bytes)
);

create table if not exists public.evidence_snapshots (
  snapshot_id text primary key
    check (snapshot_id ~ '^evs_[a-z0-9]{1,64}$'),
  record_id text not null,
  kind text not null check (kind ~ '^[a-z0-9_.]{1,128}$'),
  schema_version bigint not null check (schema_version > 0),
  source_kind text not null
    check (source_kind in
      ('vps', 'monitoring_instance', 'target', 'subscription',
       'monitoring_event', 'command_audit', 'record_revision')),
  source_id text not null check (source_id ~ '^[a-z0-9_-]{1,128}$'),
  subject_identity_snapshot jsonb not null
    check (jsonb_typeof(subject_identity_snapshot) = 'object'),
  source_identity_snapshot jsonb not null
    check (jsonb_typeof(source_identity_snapshot) = 'object'),
  capture_authorization jsonb not null
    check (jsonb_typeof(capture_authorization) = 'object'),
  capture_authorization_digest bytea not null
    check (octet_length(capture_authorization_digest) = 32),
  requested_started_at timestamptz not null,
  requested_ended_at timestamptz not null,
  actual_started_at timestamptz not null,
  actual_ended_at timestamptz not null,
  observed_at timestamptz not null,
  captured_at timestamptz not null,
  referenced_at timestamptz not null,
  source_revision text not null default '',
  source_watermark text not null default '',
  source_digest bytea not null check (octet_length(source_digest) = 32),
  producer_version text not null,
  calculation_version text not null,
  actual_precision jsonb not null
    check (jsonb_typeof(actual_precision) = 'object'),
  bucket_width jsonb not null
    check (jsonb_typeof(bucket_width) = 'object'),
  unit_semantics jsonb not null
    check (jsonb_typeof(unit_semantics) = 'object'),
  quality jsonb not null check (jsonb_typeof(quality) = 'object'),
  quota_outcome jsonb not null
    check (jsonb_typeof(quota_outcome) = 'object'),
  retention jsonb not null check (jsonb_typeof(retention) = 'object'),
  sensitivity_level text not null
    check (sensitivity_level in ('normal', 'sensitive_topology')),
  redaction jsonb not null check (jsonb_typeof(redaction) = 'array'),
  canonical_hash bytea not null
    check (octet_length(canonical_hash) = 32),
  logical_size_bytes bigint not null
    check (logical_size_bytes between 1 and 5242880),
  payload_digest bytea not null,
  created_at timestamptz not null default now(),
  check (requested_started_at <= requested_ended_at),
  check (actual_started_at <= actual_ended_at
    and actual_started_at >= requested_started_at
    and actual_ended_at <= requested_ended_at),
  check (captured_at >= observed_at and referenced_at >= captured_at),
  check (source_revision <> '' or source_watermark <> ''),
  check (canonical_hash = payload_digest),
  unique (record_id, snapshot_id),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (payload_digest, logical_size_bytes)
    references public.evidence_payloads(payload_digest, canonical_size_bytes)
    on delete restrict
);

create index if not exists idx_evidence_snapshots_source
  on public.evidence_snapshots(source_kind, source_id, captured_at desc, snapshot_id);
create index if not exists idx_evidence_snapshots_record
  on public.evidence_snapshots(record_id, captured_at desc, snapshot_id);
create index if not exists idx_evidence_snapshots_payload
  on public.evidence_snapshots(payload_digest, snapshot_id);

create table if not exists public.evidence_capture_intents (
  intent_id text primary key
    check (intent_id ~ '^evi_[0-9a-f]{24}$'),
  record_id text not null
    check (record_id ~ '^rec_[a-z0-9]{1,64}$'),
  kind text not null check (kind ~ '^[a-z0-9_.]{1,128}$'),
  schema_version bigint not null check (schema_version > 0),
  preview_digest bytea not null unique
    check (octet_length(preview_digest) = 32),
  source_digest bytea not null check (octet_length(source_digest) = 32),
  selection jsonb not null check (jsonb_typeof(selection) = 'object'),
  preview jsonb not null check (jsonb_typeof(preview) = 'object'),
  snapshot_id text not null
    check (snapshot_id ~ '^evs_[a-z0-9]{1,64}$'),
  estimated_size_bytes bigint not null
    check (estimated_size_bytes between 1 and 5242880),
  created_at timestamptz not null default now(),
  valid_until timestamptz not null,
  check (valid_until = created_at + interval '15 minutes'),
  unique (snapshot_id)
);

create index if not exists idx_evidence_capture_intents_expiry
  on public.evidence_capture_intents(valid_until, intent_id);

create table if not exists public.record_revision_evidence (
  record_id text not null,
  revision_id text not null,
  ordinal bigint not null check (ordinal >= 0),
  snapshot_id text not null,
  caption text not null default '',
  reference_role text not null default 'evidence'
    check (reference_role in ('evidence', 'context', 'decision_support')),
  created_at timestamptz not null default now(),
  primary key (revision_id, ordinal),
  unique (revision_id, snapshot_id),
  foreign key (record_id, revision_id)
    references public.record_revisions(record_id, revision_id)
    on delete restrict,
  foreign key (record_id, snapshot_id)
    references public.evidence_snapshots(record_id, snapshot_id)
    on delete restrict deferrable initially deferred
);

create index if not exists idx_record_revision_evidence_record
  on public.record_revision_evidence(record_id, revision_id, ordinal);
create index if not exists idx_record_revision_evidence_snapshot
  on public.record_revision_evidence(snapshot_id, record_id, revision_id, ordinal);

create table if not exists public.evidence_copy_lineage (
  snapshot_id text primary key,
  copied_from_snapshot_id text not null
    check (copied_from_snapshot_id ~ '^evs_[a-z0-9]{1,64}$'),
  copy_reason text not null default '',
  created_at timestamptz not null default now(),
  check (snapshot_id <> copied_from_snapshot_id),
  foreign key (snapshot_id) references public.evidence_snapshots(snapshot_id)
    on delete restrict
);

create index if not exists idx_evidence_copy_lineage_source
  on public.evidence_copy_lineage(copied_from_snapshot_id, snapshot_id);

create table if not exists public.evidence_purge_receipts (
  operation_id text not null check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  surface_kind text not null check (surface_kind ~ '^[a-z0-9_.-]{1,128}$'),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  completed_at timestamptz not null,
  created_at timestamptz not null default now(),
  primary key (operation_id, surface_kind),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict
);

create table if not exists public.evidence_payload_gc_receipts (
  payload_version_digest bytea not null
    check (octet_length(payload_version_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  deleted_at timestamptz not null,
  created_at timestamptz not null default now(),
  primary key (payload_version_digest)
);

create index if not exists idx_evidence_payloads_gc
  on public.evidence_payloads(created_at, payload_digest);

drop trigger if exists evidence_payloads_reject_update on public.evidence_payloads;
create trigger evidence_payloads_reject_update before update on public.evidence_payloads
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists evidence_snapshots_reject_update on public.evidence_snapshots;
create trigger evidence_snapshots_reject_update before update on public.evidence_snapshots
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists record_revision_evidence_reject_update on public.record_revision_evidence;
create trigger record_revision_evidence_reject_update before update on public.record_revision_evidence
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists evidence_copy_lineage_reject_update on public.evidence_copy_lineage;
create trigger evidence_copy_lineage_reject_update before update on public.evidence_copy_lineage
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists evidence_purge_receipts_reject_update on public.evidence_purge_receipts;
create trigger evidence_purge_receipts_reject_update before update on public.evidence_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists evidence_payload_gc_receipts_reject_update on public.evidence_payload_gc_receipts;
create trigger evidence_payload_gc_receipts_reject_update before update on public.evidence_payload_gc_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
