-- Alpine/musl PostgreSQL rejects POSIX repetition counts above RE_DUP_MAX
-- (commonly 255). The 0058 blob_key CHECKs used a 512-bounded class repeat
-- and therefore fail on insert with SQLSTATE 2201B. Keep the same 1..512
-- length and charset without a bounded repetition.

alter table public.record_export_artifacts
  drop constraint if exists record_export_artifacts_blob_key_check;

alter table public.record_export_artifacts
  add constraint record_export_artifacts_blob_key_check
  check (
    char_length(blob_key) between 1 and 512
    and blob_key ~ '^[a-z0-9/._-]+$'
    and blob_key not like '%..%'
  );

alter table public.record_import_artifacts
  drop constraint if exists record_import_artifacts_blob_key_check;

alter table public.record_import_artifacts
  add constraint record_import_artifacts_blob_key_check
  check (
    char_length(blob_key) between 1 and 512
    and blob_key ~ '^[a-z0-9/._-]+$'
    and blob_key not like '%..%'
  );
