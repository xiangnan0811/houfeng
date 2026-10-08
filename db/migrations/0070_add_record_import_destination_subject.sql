alter table public.record_import_jobs
  add column if not exists destination_subject_kind text,
  add column if not exists destination_subject_source_id text;

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conrelid = 'public.record_import_jobs'::regclass
      and conname = 'record_import_jobs_destination_subject_shape'
  ) then
    alter table public.record_import_jobs
      add constraint record_import_jobs_destination_subject_shape
      check (
        (destination_subject_kind is null and destination_subject_source_id is null)
        or (
          destination_subject_kind is not null
          and destination_subject_source_id is not null
          and (
            (destination_subject_kind = 'vps' and destination_subject_source_id ~ '^vps_[0-9a-f]{16}$')
            or (destination_subject_kind = 'monitoring_instance' and destination_subject_source_id ~ '^mi_[0-9a-f]{16}$')
            or (destination_subject_kind = 'target' and destination_subject_source_id ~ '^tg_[0-9a-f]{16}$')
          )
        )
      );
  end if;
end
$$;
