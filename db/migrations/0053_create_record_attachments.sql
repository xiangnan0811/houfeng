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
  unique (blob_key, object_version),
  unique (blob_key, object_version, size_bytes)
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
  preview_blob_key text,
  preview_blob_object_version text,
  preview_media_type text,
  preview_size_bytes bigint,
  created_by text not null check (created_by ~ '^usr_[a-z0-9]{1,64}$'),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check ((record_id is null) <> (draft_id is null)),
  check (draft_id is null or origin_draft_id is null
    or draft_id = origin_draft_id),
  check ((blob_key is null) = (blob_object_version is null)),
  check ((preview_blob_key is null
      and preview_blob_object_version is null
      and preview_media_type is null
      and preview_size_bytes is null)
    or (preview_blob_key is not null
      and preview_blob_object_version is not null
      and preview_media_type is not null
      and preview_size_bytes is not null)),
  check (preview_media_type is null
    or (char_length(preview_media_type) between 1 and 255
      and preview_media_type in
        ('image/png', 'text/plain; charset=utf-8'))),
  check (preview_size_bytes is null or preview_size_bytes > 0),
  check (preview_blob_key is null or attachment_state = 'available'),
  check ((attachment_state = 'available') = (blob_key is not null)),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (draft_id) references public.record_drafts(draft_id)
    on delete restrict,
  foreign key (blob_key, blob_object_version)
    references public.blob_objects(blob_key, object_version)
    on delete restrict,
  foreign key (preview_blob_key, preview_blob_object_version, preview_size_bytes)
    references public.blob_objects(blob_key, object_version, size_bytes)
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

create index if not exists idx_record_attachments_preview_blob
  on public.record_attachments(preview_blob_key, preview_blob_object_version,
    attachment_id);

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
  temporary_object_cleanup_retry_at timestamptz,
  temporary_object_deleted_at timestamptz,
  completion_fingerprint bytea
    check (completion_fingerprint is null
      or octet_length(completion_fingerprint) = 32),
  completed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  expires_at timestamptz not null,
  check ((actual_size_bytes is null) = (actual_sha256_digest is null)),
  check (temporary_object_version is null or temporary_object_key is not null),
  check (temporary_object_cleanup_retry_at is null or temporary_object_key is not null),
  check (temporary_object_deleted_at is null
    or (temporary_object_key is not null and temporary_object_version is not null)),
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

create index if not exists idx_attachment_uploads_temporary_cleanup
  on public.attachment_uploads(temporary_object_cleanup_retry_at, expires_at, upload_id)
  where transport_kind = 's3'
    and temporary_object_key is not null
    and temporary_object_deleted_at is null;

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
  result_code text
    check (result_code is null or result_code in
      ('clean', 'malware', 'unsafe_content', 'scanner_unavailable', 'timeout',
        'processing_error')),
  result_digest bytea
    check (result_digest is null or octet_length(result_digest) = 32),
  result_owner_id text not null default ''
    check (result_owner_id = '' or result_owner_id ~ '^[a-z0-9_-]{1,128}$'),
  result_lease_expires_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  expires_at timestamptz not null,
  check ((processor_state = 'claimed') =
    (owner_id <> '' and owner_generation > 0 and lease_expires_at is not null)),
  check ((processor_state = 'retry_wait') = (retry_at is not null)),
  check (((processor_state in ('queued', 'claimed'))
      and result_code is null
      and result_digest is null)
    or (result_code is not null
      and result_digest is not null
      and ((processor_state = 'retry_wait'
          and result_code in
            ('scanner_unavailable', 'timeout', 'processing_error'))
        or (processor_state = 'succeeded' and result_code = 'clean')
        or (processor_state = 'rejected'
          and result_code in ('malware', 'unsafe_content'))
        or (processor_state = 'expired'
          and result_code in
            ('scanner_unavailable', 'timeout', 'processing_error'))))),
  check (((processor_state in ('queued', 'claimed'))
      and result_owner_id = ''
      and result_lease_expires_at is null)
    or ((processor_state in ('retry_wait', 'succeeded', 'rejected', 'expired'))
      and result_owner_id <> ''
      and result_lease_expires_at is not null)),
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

create table if not exists public.blob_gc_deletions (
  deletion_id text primary key
    check (deletion_id ~ '^bgd_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  purge_mode text not null check (purge_mode in ('ordinary', 'permanent')),
  blob_key text not null check (blob_key ~ '^sha256/[0-9a-f]{64}$'),
  sha256_digest bytea not null
    check (octet_length(sha256_digest) = 32),
  check (blob_key = 'sha256/' || encode(sha256_digest, 'hex')),
  object_version text not null
    check (char_length(object_version) between 1 and 1024),
  size_bytes bigint not null check (size_bytes > 0),
  backend_kind text not null check (backend_kind in ('local', 's3')),
  blob_created_at timestamptz not null,
  deletion_state text not null
    check (deletion_state in ('claimed', 'retry_wait', 'completed')),
  owner_id text not null
    check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null check (owner_generation > 0),
  attempt bigint not null check (attempt > 0),
  lease_expires_at timestamptz not null,
  retry_at timestamptz,
  physical_delete_result text
    check (physical_delete_result is null
      or physical_delete_result in ('deleted', 'already_absent')),
  receipt_digest bytea
    check (receipt_digest is null or octet_length(receipt_digest) = 32),
  completed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check ((deletion_state = 'claimed'
      and retry_at is null
      and physical_delete_result is null
      and receipt_digest is null
      and completed_at is null)
    or (deletion_state = 'retry_wait'
      and retry_at is not null
      and physical_delete_result is null
      and receipt_digest is null
      and completed_at is null)
    or (deletion_state = 'completed'
      and retry_at is null
      and physical_delete_result is not null
      and receipt_digest is not null
      and completed_at is not null))
);

create unique index if not exists uq_blob_gc_deletions_active
  on public.blob_gc_deletions(blob_key, object_version)
  where deletion_state <> 'completed';

create index if not exists idx_blob_gc_deletions_claim
  on public.blob_gc_deletions(deletion_state, retry_at, lease_expires_at,
    backend_kind, blob_created_at, deletion_id);

create table if not exists public.blob_publication_intents (
  publication_id text primary key
    check (publication_id ~ '^bpi_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  owner_kind text not null check (owner_kind in ('upload', 'processor_preview')),
  owner_id text not null,
  owner_generation bigint not null check (owner_generation > 0),
  check ((owner_kind = 'upload'
      and owner_id ~ '^aup_[a-z0-9]{1,64}$'
      and owner_generation = 1)
    or (owner_kind = 'processor_preview'
      and owner_id ~ '^apj_[a-z0-9]{1,64}$')),
  blob_key text not null check (blob_key ~ '^sha256/[0-9a-f]{64}$'),
  sha256_digest bytea not null check (octet_length(sha256_digest) = 32),
  check (blob_key = 'sha256/' || encode(sha256_digest, 'hex')),
  size_bytes bigint not null check (size_bytes > 0),
  backend_kind text not null check (backend_kind in ('local', 's3')),
  object_version text
    check (object_version is null or char_length(object_version) between 1 and 1024),
  publication_state text not null
    check (publication_state in ('prepared', 'published', 'cleanup_claimed',
      'retry_wait', 'completed')),
  publish_expires_at timestamptz not null,
  cleanup_owner_id text not null default ''
    check (cleanup_owner_id = '' or cleanup_owner_id ~ '^[a-z0-9_-]{1,128}$'),
  cleanup_generation bigint not null default 0 check (cleanup_generation >= 0),
  attempt bigint not null default 0 check (attempt >= 0),
  cleanup_lease_expires_at timestamptz,
  retry_at timestamptz,
  completion_outcome text
    check (completion_outcome is null or completion_outcome in
      ('consumed', 'deleted', 'already_absent')),
  receipt_digest bytea
    check (receipt_digest is null or octet_length(receipt_digest) = 32),
  completed_at timestamptz,
  check ((cleanup_owner_id = '' and cleanup_generation = 0 and attempt = 0
      and cleanup_lease_expires_at is null)
    or (cleanup_owner_id <> '' and cleanup_generation > 0 and attempt > 0
      and cleanup_lease_expires_at is not null)),
  check ((publication_state in ('prepared', 'published') and cleanup_owner_id = '')
    or (publication_state in ('cleanup_claimed', 'retry_wait')
      and cleanup_owner_id <> '')
    or publication_state = 'completed'),
  check ((publication_state = 'prepared' and object_version is null)
    or (publication_state = 'published' and object_version is not null)
    or publication_state in ('cleanup_claimed', 'retry_wait')
    or (publication_state = 'completed'
      and (completion_outcome = 'already_absent' or object_version is not null))),
  check ((publication_state = 'retry_wait') = (retry_at is not null)),
  check ((publication_state = 'completed') =
    (completion_outcome is not null and receipt_digest is not null
      and completed_at is not null)),
  check (publication_state <> 'completed'
    or (completion_outcome = 'consumed' and cleanup_owner_id = '')
    or (completion_outcome in ('deleted', 'already_absent')
      and cleanup_owner_id <> '')),
  unique (owner_kind, owner_id, owner_generation, blob_key)
);

create unique index if not exists uq_blob_publication_intents_active_key
  on public.blob_publication_intents(blob_key)
  where publication_state <> 'completed';

create index if not exists idx_blob_publication_intents_claim
  on public.blob_publication_intents(publication_state, retry_at,
    cleanup_lease_expires_at, publish_expires_at, backend_kind, publication_id);

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
