alter table if exists host_samples
  add column if not exists agent_version text not null default '';

alter table if exists host_samples
  add column if not exists fingerprint text not null default '';

alter table if exists probe_observations
  add column if not exists agent_version text not null default '';

alter table if exists probe_observations
  add column if not exists fingerprint text not null default '';
