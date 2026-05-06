alter table if exists host_samples
  add column if not exists containers jsonb;
