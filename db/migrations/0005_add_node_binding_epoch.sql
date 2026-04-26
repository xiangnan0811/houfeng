alter table if exists nodes
  add column if not exists binding_epoch_started_at timestamptz;

update nodes
set binding_epoch_started_at = created_at
where coalesce(binding_fingerprint, '') <> ''
  and binding_epoch_started_at is null;
