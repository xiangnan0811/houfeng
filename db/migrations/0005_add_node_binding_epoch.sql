alter table if exists nodes
  add column if not exists binding_epoch_started_at timestamptz;
