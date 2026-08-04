create table if not exists public.blob_objects (
  blob_key text primary key check (blob_key ~ '^sha256/[0-9a-f]{64}$'),
  sha256_digest bytea not null unique
    check (octet_length(sha256_digest) = 32),
  check (blob_key = 'sha256/' || encode(sha256_digest, 'hex')),
  object_version text not null
    check (char_length(object_version) between 1 and 1024),
  size_bytes bigint not null check (size_bytes > 0),
  backend_kind text not null check (backend_kind in ('local', 's3')),
  created_at timestamptz not null default now(),
  unique (blob_key, object_version)
);

create table if not exists public.attachment_quota_accounts (
  project_id text primary key default 'default' check (project_id = 'default'),
  logical_bytes bigint not null default 0 check (logical_bytes >= 0),
  reserved_bytes bigint not null default 0 check (reserved_bytes >= 0),
  physical_bytes bigint not null default 0 check (physical_bytes >= 0),
  quota_version bigint not null default 0 check (quota_version >= 0),
  updated_at timestamptz not null default now()
);

create table if not exists public.record_attachments (
  attachment_id text primary key
    check (attachment_id ~ '^att_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text,
  draft_id text,
  origin_draft_id text
    check (origin_draft_id is null
      or origin_draft_id ~ '^rdf_[a-z0-9]{1,64}$'),
  copied_from_attachment_id text,
  attachment_state text not null default 'created'
    check (attachment_state in
      ('created', 'uploading', 'quarantined', 'available', 'rejected', 'expired')),
  display_name text not null check (char_length(display_name) between 1 and 255),
  media_type text not null check (char_length(media_type) between 1 and 255),
  logical_size_bytes bigint not null check (logical_size_bytes > 0),
  blob_key text,
  blob_object_version text,
  created_by text not null check (created_by ~ '^usr_[a-z0-9]{1,64}$'),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check ((record_id is null) <> (draft_id is null)),
  check (draft_id is null or origin_draft_id is null
    or draft_id = origin_draft_id),
  check ((blob_key is null) = (blob_object_version is null)),
  check ((attachment_state = 'available') = (blob_key is not null)),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (draft_id) references public.record_drafts(draft_id)
    on delete restrict,
  foreign key (blob_key, blob_object_version)
    references public.blob_objects(blob_key, object_version)
    on delete restrict,
  unique (record_id, attachment_id),
  unique (attachment_id, origin_draft_id)
);

create index if not exists idx_record_attachments_draft_state
  on public.record_attachments(draft_id, attachment_state, attachment_id);

create index if not exists idx_record_attachments_record_state
  on public.record_attachments(record_id, attachment_state, attachment_id);

create index if not exists idx_record_attachments_blob
  on public.record_attachments(blob_key, blob_object_version, attachment_id);

create index if not exists idx_record_attachments_copied_from
  on public.record_attachments(copied_from_attachment_id, attachment_id);

create table if not exists public.attachment_uploads (
  upload_id text primary key check (upload_id ~ '^aup_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  attachment_id text not null,
  origin_draft_id text not null
    check (origin_draft_id ~ '^rdf_[a-z0-9]{1,64}$'),
  author_id text not null check (author_id ~ '^usr_[a-z0-9]{1,64}$'),
  upload_state text not null default 'created'
    check (upload_state in
      ('created', 'uploading', 'quarantined', 'available', 'rejected', 'expired')),
  transport_kind text not null check (transport_kind in ('local', 's3')),
  declared_size_bytes bigint not null check (declared_size_bytes > 0),
  reserved_size_bytes bigint not null check (reserved_size_bytes > 0),
  actual_size_bytes bigint
    check (actual_size_bytes is null or actual_size_bytes >= 0),
  actual_sha256_digest bytea
    check (actual_sha256_digest is null
      or octet_length(actual_sha256_digest) = 32),
  temporary_object_key text
    check (temporary_object_key is null
      or char_length(temporary_object_key) between 1 and 1024),
  temporary_object_version text
    check (temporary_object_version is null
      or char_length(temporary_object_version) between 1 and 1024),
  completion_fingerprint bytea
    check (completion_fingerprint is null
      or octet_length(completion_fingerprint) = 32),
  completed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  expires_at timestamptz not null,
  check ((actual_size_bytes is null) = (actual_sha256_digest is null)),
  check (temporary_object_version is null or temporary_object_key is not null),
  check (expires_at > created_at),
  unique (attachment_id),
  unique (upload_id, attachment_id),
  foreign key (attachment_id, origin_draft_id)
    references public.record_attachments(attachment_id, origin_draft_id)
    on delete restrict
    deferrable initially deferred
);

create index if not exists idx_attachment_uploads_expiry
  on public.attachment_uploads(upload_state, expires_at, upload_id);

create index if not exists idx_attachment_uploads_draft_state
  on public.attachment_uploads(origin_draft_id, upload_state, upload_id);

create table if not exists public.attachment_upload_parts (
  upload_id text not null,
  part_number bigint not null check (part_number > 0),
  size_bytes bigint not null check (size_bytes > 0),
  sha256_digest bytea not null check (octet_length(sha256_digest) = 32),
  object_version text not null
    check (char_length(object_version) between 1 and 1024),
  created_at timestamptz not null default now(),
  primary key (upload_id, part_number),
  foreign key (upload_id) references public.attachment_uploads(upload_id)
    on delete restrict
);

create table if not exists public.record_revision_attachments (
  record_id text not null,
  revision_id text not null,
  ordinal bigint not null check (ordinal >= 0),
  attachment_id text not null,
  created_at timestamptz not null default now(),
  primary key (revision_id, ordinal),
  unique (revision_id, attachment_id),
  foreign key (record_id, revision_id)
    references public.record_revisions(record_id, revision_id)
    on delete restrict,
  foreign key (record_id, attachment_id)
    references public.record_attachments(record_id, attachment_id)
    on delete restrict
    deferrable initially deferred
);

create index if not exists idx_record_revision_attachments_record
  on public.record_revision_attachments(record_id, revision_id, ordinal);

create index if not exists idx_record_revision_attachments_attachment
  on public.record_revision_attachments(attachment_id, record_id,
    revision_id, ordinal);

create table if not exists public.attachment_processor_jobs (
  processor_job_id text primary key
    check (processor_job_id ~ '^apj_[a-z0-9]{1,64}$'),
  upload_id text not null unique,
  attachment_id text not null unique,
  processor_state text not null default 'queued'
    check (processor_state in
      ('queued', 'claimed', 'retry_wait', 'succeeded', 'rejected', 'expired')),
  processor_profile text not null
    check (processor_profile in ('image', 'pdf', 'text', 'archive')),
  attempt bigint not null default 0 check (attempt >= 0),
  max_attempts bigint not null check (max_attempts > 0),
  owner_id text not null default ''
    check (owner_id = '' or owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null default 0 check (owner_generation >= 0),
  lease_expires_at timestamptz,
  retry_at timestamptz,
  result_digest bytea
    check (result_digest is null or octet_length(result_digest) = 32),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  expires_at timestamptz not null,
  check ((processor_state = 'claimed') =
    (owner_id <> '' and owner_generation > 0 and lease_expires_at is not null)),
  check (expires_at > created_at),
  foreign key (upload_id, attachment_id)
    references public.attachment_uploads(upload_id, attachment_id)
    on delete restrict
);

create index if not exists idx_attachment_processor_jobs_claim
  on public.attachment_processor_jobs(processor_state, retry_at,
    lease_expires_at, processor_job_id);

create table if not exists public.content_processor_workspaces (
  workspace_id text primary key
    check (workspace_id ~ '^cpw_[a-z0-9]{1,64}$'),
  processor_job_id text not null,
  attempt bigint not null check (attempt >= 0),
  workspace_state text not null default 'registered'
    check (workspace_state in
      ('registered', 'materialized', 'purging', 'purged')),
  workspace_path_digest bytea not null
    check (octet_length(workspace_path_digest) = 32),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  expires_at timestamptz not null,
  purged_at timestamptz,
  check ((workspace_state = 'purged') = (purged_at is not null)),
  check (expires_at > created_at),
  unique (processor_job_id, attempt),
  foreign key (processor_job_id)
    references public.attachment_processor_jobs(processor_job_id)
    on delete restrict
);

create index if not exists idx_content_processor_workspaces_expiry
  on public.content_processor_workspaces(workspace_state, expires_at,
    workspace_id);

create table if not exists public.blob_gc_pins (
  pin_id text primary key check (pin_id ~ '^bgp_[a-z0-9]{1,64}$'),
  pin_owner_kind text not null
    check (pin_owner_kind in
      ('backup_manifest', 'restore_attempt', 'import_plan', 'revision_transaction')),
  pin_owner_id text not null
    check (pin_owner_id ~ '^[a-z0-9_-]{1,128}$'),
  blob_key text not null,
  blob_object_version text not null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  check (expires_at > created_at),
  unique (pin_owner_kind, pin_owner_id, blob_key, blob_object_version),
  foreign key (blob_key, blob_object_version)
    references public.blob_objects(blob_key, object_version)
    on delete restrict
);

create index if not exists idx_blob_gc_pins_expiry
  on public.blob_gc_pins(expires_at, pin_id);

create index if not exists idx_blob_gc_pins_blob
  on public.blob_gc_pins(blob_key, blob_object_version, expires_at, pin_id);

create table if not exists public.attachment_purge_receipts (
  operation_id text not null check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  surface_kind text not null
    check (surface_kind in ('logical_attachment', 'upload_part', 'blob_object')),
  object_version_digest bytea not null
    check (octet_length(object_version_digest) = 32),
  adapter_name text not null default 'record_attachments'
    check (adapter_name = 'record_attachments'),
  removed_surface_digest bytea not null
    check (octet_length(removed_surface_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  removed_row_count bigint not null check (removed_row_count >= 0),
  verified_absent_at timestamptz not null,
  created_at timestamptz not null default now(),
  primary key (operation_id, surface_kind, object_version_digest),
  unique (receipt_digest),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict
);

create table if not exists public.content_workspace_purge_receipts (
  workspace_id text primary key check (workspace_id ~ '^cpw_[a-z0-9]{1,64}$'),
  removed_surface_digest bytea not null
    check (octet_length(removed_surface_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  removed_row_count bigint not null check (removed_row_count >= 0),
  verified_absent_at timestamptz not null,
  created_at timestamptz not null default now()
);

drop trigger if exists blob_objects_reject_update on public.blob_objects;
create trigger blob_objects_reject_update
before update on public.blob_objects
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_attachments_origin_draft_reject_update
  on public.record_attachments;
create trigger record_attachments_origin_draft_reject_update
before update of origin_draft_id on public.record_attachments
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists attachment_upload_parts_reject_update
  on public.attachment_upload_parts;
create trigger attachment_upload_parts_reject_update
before update on public.attachment_upload_parts
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_revision_attachments_reject_update
  on public.record_revision_attachments;
create trigger record_revision_attachments_reject_update
before update on public.record_revision_attachments
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists blob_gc_pins_reject_update on public.blob_gc_pins;
create trigger blob_gc_pins_reject_update
before update on public.blob_gc_pins
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists attachment_purge_receipts_reject_update
  on public.attachment_purge_receipts;
create trigger attachment_purge_receipts_reject_update
before update on public.attachment_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists content_workspace_purge_receipts_reject_update
  on public.content_workspace_purge_receipts;
create trigger content_workspace_purge_receipts_reject_update
before update on public.content_workspace_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
