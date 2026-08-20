-- The projector scans its sources by recorded time, and record_domain_activities
-- was indexed only for per-record reads (record_id, event_at desc). Without this
-- index every projection pass sequentially scans the whole log. The index lives
-- here rather than with the table because the projector is what needs it.
create index if not exists idx_record_domain_activities_recorded
  on public.record_domain_activities(project_id, recorded_at, activity_id);

-- Same reason for evidence: its indexes all lead with source or record, and the
-- projection scan filters on neither, so it would read the whole table each pass.
create index if not exists idx_evidence_snapshots_created
  on public.evidence_snapshots(created_at, snapshot_id);

-- The four asset history tables are read as one source, and every index they
-- have leads with vps_id. A scan ordered by write time across all four needs one
-- index per table or it degrades into four sequential scans per pass.
create index if not exists idx_renewal_decisions_created
  on public.renewal_decisions(created_at, decision_id);
create index if not exists idx_price_histories_created
  on public.price_histories(created_at, price_history_id);
create index if not exists idx_ip_histories_created
  on public.ip_histories(created_at, ip_history_id);
create index if not exists idx_vps_spec_snapshots_created
  on public.vps_spec_snapshots(created_at, snapshot_id);

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
-- Freshness max(recorded_at) / top-1 observed_at must stop early; the timeline
-- index alone still forces a full subject scan under max().
create index if not exists idx_record_activity_subjects_observed
  on public.record_activity_subjects
    (subject_kind, subject_source_id, recorded_at desc, activity_id)
  include (ingest_sequence, auth_scope_digest, event_kind, source_kind);
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
-- Tombstone identity lookup must not walk the live timeline when no
-- tombstoned relation exists for the subject.
create index if not exists idx_record_activity_subjects_tombstone
  on public.record_activity_subjects
    (subject_kind, subject_source_id, event_at desc, recorded_at desc, source_kind, activity_id)
  include (identity_snapshot, ingest_sequence, auth_scope_digest)
  where tombstoned;

create table if not exists public.record_activity_projection_checkpoints (
  project_id text not null default 'default' check (project_id = 'default'),
  projection_generation bigint not null check (projection_generation > 0),
  source_kind text not null
    check (source_kind in ('record_domain', 'evidence_snapshot', 'asset_history',
      'monitoring_event', 'command_audit')),
  -- The position is the source's own recorded time, not an ingest sequence: a
  -- sequence maximum would let a transaction that commits out of order be
  -- skipped forever. Null means no pass has completed yet, so the next scan must
  -- cover all history.
  recorded_through timestamptz,
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
  -- The revision's own order, carried so publication can tell a newer revision
  -- from one that arrived late. Arrival order cannot answer that, and joining the
  -- record tables to ask would put a mutable read in the publish transaction.
  revision_no bigint not null check (revision_no > 0),
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

-- Per-record purge proof. Heads and checkpoints stay generation-wide and are
-- never owned by a single record's deletion adapter.
create table if not exists public.record_activity_purge_receipts (
  operation_id text primary key check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  adapter_name text not null default 'record_activity_projection'
    check (adapter_name = 'record_activity_projection'),
  removed_surface_digest bytea not null
    check (octet_length(removed_surface_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  removed_row_count bigint not null check (removed_row_count >= 0),
  verified_absent_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict
);

create or replace function record_platform_internal.purge_record_activity(
  text, text, text, text, bigint, bigint, bytea
)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  p_operation_id alias for $1;
  p_reservation_id alias for $2;
  p_project_id alias for $3;
  p_record_id alias for $4;
  p_fence_epoch alias for $5;
  p_ledger_sequence alias for $6;
  p_ledger_entry_hash alias for $7;
  v_removed bigint := 0;
  v_rows bigint;
  v_remaining bigint;
begin
  if p_operation_id is null or p_reservation_id is null or p_project_id <> 'default'
    or p_record_id is null or p_fence_epoch < 0 or p_ledger_sequence <= 0
    or octet_length(p_ledger_entry_hash) <> 32 then
    raise exception using errcode = '55000', message = 'invalid activity purge authority';
  end if;

  perform 1
  from public.record_purge_operations as operation
  join public.deletion_reservations as reservation
    on reservation.reservation_id = operation.reservation_id
  where operation.operation_id = p_operation_id
    and operation.reservation_id = p_reservation_id
    and operation.project_id = p_project_id
    and operation.operation_state = 'online_purging'
    and operation.ledger_sequence = p_ledger_sequence
    and operation.ledger_entry_hash = p_ledger_entry_hash
    and reservation.project_id = p_project_id
    and reservation.object_kind = 'record'
    and reservation.object_id = p_record_id
    and reservation.state = 'committed'
    and reservation.fence_epoch = p_fence_epoch
  for update of operation, reservation;
  if not found then
    raise exception using errcode = '55000', message = 'activity purge authority unavailable';
  end if;

  -- Subjects reference projection rows, so they go first. Clear both the
  -- activity-linked rows and any denormalized record_id matches so a relation
  -- that somehow lost its parent cannot survive as an orphan timeline hit.
  delete from public.record_activity_subjects
  where record_id = p_record_id
     or activity_id in (
          select activity_id from public.record_activity_projection where record_id = p_record_id
        );
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  delete from public.record_activity_projection where record_id = p_record_id;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  delete from public.record_activity_revision_intervals where record_id = p_record_id;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  select
    (select count(*) from public.record_activity_subjects where record_id = p_record_id) +
    (select count(*) from public.record_activity_projection where record_id = p_record_id) +
    (select count(*) from public.record_activity_revision_intervals where record_id = p_record_id)
  into v_remaining;
  if v_remaining <> 0 then
    raise exception using errcode = '55000', message = 'activity purge left owned rows';
  end if;
  return v_removed;
end
$$;

revoke all on function record_platform_internal.purge_record_activity(text,text,text,text,bigint,bigint,bytea) from public;

create or replace function public.record_activity_purge(bytea)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare p_command alias for $1; v jsonb;
begin
  if p_command is null or octet_length(p_command) not between 1 and 4096 then
    raise exception using errcode = '55000', message = 'invalid activity purge command';
  end if;
  v := convert_from(p_command, 'UTF8')::jsonb;
  if jsonb_typeof(v) <> 'object' or
    array(select key from jsonb_object_keys(v) as key order by key) <>
    array['fence_epoch','ledger_entry_hash','ledger_sequence','operation_id','project_id','record_id','reservation_id']::text[] then
    raise exception using errcode = '55000', message = 'invalid activity purge command';
  end if;
  return record_platform_internal.purge_record_activity(
    v->>'operation_id', v->>'reservation_id', v->>'project_id', v->>'record_id',
    (v->>'fence_epoch')::bigint, (v->>'ledger_sequence')::bigint,
    decode(v->>'ledger_entry_hash', 'hex')
  );
end
$$;
revoke all on function public.record_activity_purge(bytea) from public;

drop trigger if exists record_activity_purge_receipts_reject_update on public.record_activity_purge_receipts;
create trigger record_activity_purge_receipts_reject_update before update on public.record_activity_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
