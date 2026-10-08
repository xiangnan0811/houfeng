alter table public.users
  add column if not exists is_supervisor boolean not null default false,
  add column if not exists disabled_at timestamptz;

do $$
declare
  user_count bigint;
  only_user_id text;
  only_user_role text;
begin
  select count(*) into user_count from public.users;
  if user_count = 0 then
    null;
  elsif user_count = 1 then
    select user_id, role
    into only_user_id, only_user_role
    from public.users;
    if only_user_role <> 'admin' then
      raise exception using
        errcode = '55000',
        message = 'cannot select initial supervisor: the only existing user is not an admin';
    end if;
    update public.users
    set is_supervisor = true
    where user_id = only_user_id;
  else
    raise exception using
      errcode = '55000',
      message = 'cannot select initial supervisor: existing users are ambiguous';
  end if;
end
$$;

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conrelid = 'public.users'::regclass
      and conname = 'users_supervisor_contract'
  ) then
    alter table public.users
      add constraint users_supervisor_contract
      check (not is_supervisor or (role = 'admin' and disabled_at is null));
  end if;
end
$$;

create unique index if not exists users_single_supervisor_idx
  on public.users ((is_supervisor))
  where is_supervisor;

alter table public.record_access_groups
  add column if not exists display_name text;

update public.record_access_groups
set display_name = group_id
where display_name is null;

alter table public.record_access_groups
  alter column display_name set not null;

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conrelid = 'public.record_access_groups'::regclass
      and conname = 'record_access_groups_display_name_shape'
  ) then
    alter table public.record_access_groups
      add constraint record_access_groups_display_name_shape
      check (
        length(btrim(display_name)) between 1 and 100
        and display_name = btrim(display_name)
      );
  end if;
end
$$;

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conrelid = 'public.record_access_groups'::regclass
      and conname = 'record_access_groups_project_display_name_key'
  ) then
    alter table public.record_access_groups
      add constraint record_access_groups_project_display_name_key
      unique (project_id, display_name);
  end if;
end
$$;
