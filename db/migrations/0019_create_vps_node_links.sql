create table if not exists vps_node_links (
  link_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  node_id text not null references nodes(node_id) on delete cascade,
  linked_at timestamptz not null default now(),
  unlinked_at timestamptz,
  note text not null default ''
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'vps_node_links_note_not_null'
      and conrelid = 'vps_node_links'::regclass
  ) then
    alter table vps_node_links
      add constraint vps_node_links_note_not_null check (note is not null);
  end if;
end $$;

create unique index if not exists idx_vps_node_links_pair_active
  on vps_node_links (vps_id, node_id)
  where unlinked_at is null;

create index if not exists idx_vps_node_links_vps_active
  on vps_node_links (vps_id)
  where unlinked_at is null;

create index if not exists idx_vps_node_links_node_active
  on vps_node_links (node_id)
  where unlinked_at is null;

