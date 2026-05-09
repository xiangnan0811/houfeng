create table if not exists renewal_decisions (
  decision_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  from_decision text,
  to_decision text not null,
  reason text not null default '',
  decided_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  constraint renewal_decisions_from_allowed check (
    from_decision is null or from_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')
  ),
  constraint renewal_decisions_to_allowed check (
    to_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')
  )
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'renewal_decisions_from_allowed'
      and conrelid = 'renewal_decisions'::regclass
  ) then
    alter table renewal_decisions
      add constraint renewal_decisions_from_allowed check (
        from_decision is null or from_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'renewal_decisions_to_allowed'
      and conrelid = 'renewal_decisions'::regclass
  ) then
    alter table renewal_decisions
      add constraint renewal_decisions_to_allowed check (
        to_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'renewal_decisions_reason_not_null'
      and conrelid = 'renewal_decisions'::regclass
  ) then
    alter table renewal_decisions
      add constraint renewal_decisions_reason_not_null check (reason is not null);
  end if;
end $$;

create index if not exists idx_renewal_decisions_vps_time
  on renewal_decisions (vps_id, decided_at desc, created_at desc);

create index if not exists idx_renewal_decisions_to_decision
  on renewal_decisions (to_decision);
