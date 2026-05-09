create table if not exists providers (
  provider_id text primary key,
  name text not null,
  website text not null default '',
  panel_url text not null default '',
  account_hint text not null default '',
  country text not null default '',
  note text not null default '',
  rating integer,
  labels text[] not null default '{}',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint providers_name_not_blank check (length(btrim(name)) > 0),
  constraint providers_rating_range check (rating is null or rating between 1 and 5)
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'providers_name_not_blank'
      and conrelid = 'providers'::regclass
  ) then
    alter table providers
      add constraint providers_name_not_blank check (length(btrim(name)) > 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'providers_rating_range'
      and conrelid = 'providers'::regclass
  ) then
    alter table providers
      add constraint providers_rating_range check (rating is null or rating between 1 and 5);
  end if;
end $$;

create index if not exists idx_providers_name_lower on providers (lower(name));
