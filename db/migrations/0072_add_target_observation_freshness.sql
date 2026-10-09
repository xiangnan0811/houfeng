alter table public.targets
  add column if not exists freshness_reset_at timestamptz not null default now();

alter table public.probe_items
  add column if not exists freshness_reset_at timestamptz not null default now(),
  add column if not exists last_live_observed_at timestamptz;

update public.probe_items as items
set last_live_observed_at = (
  select max(least(observations.observed_at, observations.received_at))
  from public.probe_observations as observations
  where observations.target_id = items.target_id
    and observations.probe_item_id = items.probe_item_id
    and not observations.maintenance_context
    and not observations.is_backfilled
    and observations.result_kind in ('success', 'failure')
);
