-- Coordinated v2 cutover requires every old Agent stopped with a verified empty
-- queue. This migration cannot establish that cross-host operational prerequisite.
create table public.agent_replay_states (
  session_id text primary key references public.monitoring_agent_sessions(session_id),
  producer_id uuid not null,
  retired_through bigint not null default 0 check (retired_through >= 0),
  allocated_through bigint not null default 0 check (allocated_through >= retired_through),
  live_sequence_high bigint not null default 0 check (live_sequence_high >= 0)
) with (fillfactor = 80, autovacuum_enabled = true,
        autovacuum_vacuum_threshold = 1000, autovacuum_vacuum_scale_factor = 0.05);

create table public.agent_batch_receipts (
  session_id text not null references public.agent_replay_states(session_id),
  batch_sequence bigint not null check (batch_sequence > 0),
  payload_digest bytea not null check (octet_length(payload_digest) = 32),
  primary key (session_id, batch_sequence)
) with (autovacuum_enabled = true,
        autovacuum_vacuum_threshold = 1000, autovacuum_vacuum_scale_factor = 0.05);

create function public.enforce_agent_replay_state() returns trigger
language plpgsql set search_path = pg_catalog as $$
begin
  if tg_when = 'BEFORE' then
    if new.session_id is distinct from old.session_id
       or new.producer_id is distinct from old.producer_id
       or new.retired_through < old.retired_through
       or new.allocated_through < old.allocated_through
       or new.live_sequence_high < old.live_sequence_high then
      raise exception 'agent replay identity and watermarks are monotonic' using errcode = '23514';
    end if;
  elsif new.retired_through > old.retired_through then
    delete from public.agent_batch_receipts
      where session_id = new.session_id and batch_sequence <= new.retired_through;
  end if;
  return new;
end $$;
revoke all on function public.enforce_agent_replay_state() from public;
create trigger agent_replay_state_monotonic before update on public.agent_replay_states
  for each row execute function public.enforce_agent_replay_state();
create trigger agent_replay_state_retirement after update of retired_through on public.agent_replay_states
  for each row execute function public.enforce_agent_replay_state();

create function public.enforce_agent_batch_receipt() returns trigger
language plpgsql set search_path = pg_catalog as $$
declare
  floor bigint;
  allocated bigint;
begin
  if tg_op = 'INSERT' then
    -- SHARE conflicts with a non-key watermark update; KEY SHARE would permit
    -- a stale insert to race retirement and leave an already retired receipt.
    select retired_through, allocated_through into floor, allocated
      from public.agent_replay_states where session_id = new.session_id for share;
    if not found or new.batch_sequence <= floor or new.batch_sequence > allocated
       or new.batch_sequence - floor > 131072 then
      raise exception 'agent receipt is outside the unretired replay window' using errcode = '23514';
    end if;
    return new;
  elsif tg_op = 'DELETE' then
    select retired_through into floor from public.agent_replay_states
      where session_id = old.session_id for share;
    if not found or old.batch_sequence > floor then
      raise exception 'unretired agent receipt cannot be erased' using errcode = '23514';
    end if;
    return old;
  end if;
  raise exception 'agent receipt identity and digest are immutable' using errcode = '23514';
end $$;
revoke all on function public.enforce_agent_batch_receipt() from public;
create trigger agent_batch_receipt_guard before insert or update or delete on public.agent_batch_receipts
  for each row execute function public.enforce_agent_batch_receipt();

-- No CASCADE: an unknown dependency must abort the entire scoped transition.
drop table public.agent_live_signals;
drop table public.agent_sync_batches;
