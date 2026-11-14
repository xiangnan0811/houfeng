-- W2 is a separate, forward-only source authority. W1 bytes and heads are untouched.
create table public.source_authority_state (
  singleton boolean primary key default true check (singleton),
  generation bigint not null check (generation > 0),
  mode text not null check (mode in ('active', 'recovering')),
  recovery_id text,
  coverage_seal bytea check (octet_length(coverage_seal) = 32),
  check (mode <> 'recovering' or recovery_id is not null)
);

-- Provisioning binds a real direct-login principal, never a caller-supplied epoch.
create table public.source_authority_principals (
  principal name primary key,
  purpose text not null check (purpose in ('reader', 'producer', 'recovery', 'provisioner')),
  generation bigint not null check (generation > 0),
  recovery_id text,
  check ((purpose = 'recovery') = (recovery_id is not null))
);
create trigger source_authority_principals_immutable
before update or delete or truncate on public.source_authority_principals
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table public.source_retirement_prepares (
  retirement_id text primary key check (retirement_id ~ '^[a-z0-9_-]{1,128}$'),
  prepare_sequence bigint generated always as identity unique,
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  source_kind text not null check (source_kind in ('vps', 'monitoring_instance', 'target')),
  source_id text not null,
  origin_generation bigint not null check (origin_generation > 0),
  request_fingerprint bytea not null check (octet_length(request_fingerprint) = 32
    and request_fingerprint <> decode(repeat('00', 32), 'hex')),
  check ((source_kind = 'vps' and source_id ~ '^vps_[0-9a-f]{16}$')
    or (source_kind = 'monitoring_instance' and source_id ~ '^mi_[0-9a-f]{16}$')
    or (source_kind = 'target' and source_id ~ '^tg_[0-9a-f]{16}$'))
);
create trigger source_retirement_prepares_immutable
before update or delete or truncate on public.source_retirement_prepares
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table public.source_retirement_terminals (
  retirement_id text primary key references public.source_retirement_prepares(retirement_id),
  deployment_id text not null,
  project_id text not null,
  source_kind text not null,
  source_id text not null,
  origin_generation bigint not null check (origin_generation > 0),
  barrier_digest bytea not null check (octet_length(barrier_digest) = 32
    and barrier_digest <> decode(repeat('00', 32), 'hex')),
  last_live bytea not null check (octet_length(last_live) between 1 and 4096),
  final_floor bytea not null check (octet_length(final_floor) between 1 and 4096),
  sequence bigint not null unique check (sequence > 0),
  previous_hash bytea not null check (octet_length(previous_hash) = 32),
  canonical_entry bytea not null check (octet_length(canonical_entry) between 1 and 65536),
  entry_hash bytea not null unique check (octet_length(entry_hash) = 32),
  unique (deployment_id, project_id, source_kind, source_id),
  check (entry_hash = record_platform_internal.digest(canonical_entry, 'sha256'))
);
create trigger source_retirement_terminals_immutable
before update or delete or truncate on public.source_retirement_terminals
for each statement execute function record_platform_internal.reject_immutable_mutation();
create table public.source_retirement_head (
  singleton boolean primary key default true check (singleton),
  sequence bigint not null default 0 check (sequence >= 0),
  entry_hash bytea not null default decode(repeat('00', 32), 'hex')
    check (octet_length(entry_hash) = 32)
);
insert into public.source_retirement_head(singleton) values (true);

-- Unknown is durable evidence, not an aborted-prepare inference or a lease.
create table public.source_retirement_unknown (
  retirement_id text primary key references public.source_retirement_prepares(retirement_id),
  recovery_id text not null,
  installed_generation bigint not null check (installed_generation > 0)
);
create trigger source_retirement_unknown_immutable
before update or delete or truncate on public.source_retirement_unknown
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table public.source_authority_recoveries (
  recovery_id text primary key check (recovery_id ~ '^[a-z0-9_-]{1,128}$'),
  generation bigint not null unique check (generation > 1),
  endpoint_digest bytea not null check (octet_length(endpoint_digest) = 32),
  phase text not null check (phase in ('recovering', 'coverage_sealed',
    'credentials_staged', 'witness_activated', 'app_active_login_closed',
    'credentials_published', 'logins_open', 'completed')),
  coverage_seal bytea check (octet_length(coverage_seal) = 32),
  prepare_high_water bigint,
  prepare_count bigint,
  prepare_digest bytea check (octet_length(prepare_digest) = 32),
  terminal_sequence bigint,
  terminal_hash bytea check (octet_length(terminal_hash) = 32),
  guard_digest bytea check (octet_length(guard_digest) = 32),
  credential_manifest_digest bytea check (octet_length(credential_manifest_digest) = 32),
  namespace_handoff_digest bytea check (octet_length(namespace_handoff_digest) = 32)
);
create table public.source_authority_credentials (
  generation bigint not null check (generation > 0),
  purpose text not null check (purpose in ('runtime', 'admin', 'worker', 'migrator', 'producer')),
  principal name not null,
  commitment bytea not null unique check (octet_length(commitment) = 32),
  secret_fingerprint bytea not null check (octet_length(secret_fingerprint) = 32
    and secret_fingerprint <> decode(repeat('00', 32), 'hex')),
  primary key (generation, purpose)
);
create trigger source_authority_credentials_immutable
before update or delete or truncate on public.source_authority_credentials
for each statement execute function record_platform_internal.reject_immutable_mutation();

create function record_platform_internal.source_bytes(value bytea)
returns bytea language sql immutable strict parallel safe
set search_path = pg_catalog
as $$ select int4send(octet_length(value)) || value $$;

-- Every mutator serializes generation before prepare/source and then the head.
-- Holding this row through the effect prevents a queued old producer from passing.
create function record_platform_internal.source_authority_lock(required_purpose text, expected_recovery text)
returns public.source_authority_state
language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; p public.source_authority_principals;
begin
  select * into strict s from public.source_authority_state where singleton for update;
  select * into strict p from public.source_authority_principals where principal = session_user;
  if session_user = current_user or p.purpose <> required_purpose
    or (p.purpose <> 'provisioner' and p.generation <> s.generation)
    or not exists (select 1 from pg_roles where rolname = session_user and rolcanlogin
      and not rolsuper and not rolcreaterole and not rolcreatedb and not rolreplication
      and not rolbypassrls and not rolinherit)
    or exists (select 1 from pg_auth_members where member = (select oid from pg_roles where rolname = session_user))
    or (required_purpose = 'producer' and (s.mode <> 'active' or exists (
      select 1 from public.source_authority_recoveries where recovery_id = s.recovery_id and phase <> 'completed')))
    or (required_purpose = 'recovery' and (s.recovery_id is distinct from expected_recovery
      or p.recovery_id is distinct from expected_recovery)) then
    raise exception using errcode = '42501', message = 'source authority principal or generation rejected';
  end if;
  if (select count(*) from public.record_platform_domain_identity) <> 1 or not exists (
    select 1 from public.record_platform_domain_identity d
    where d.domain_kind = 'deletion_witness' and d.identity_mode = 'postgres_system'
      and d.postgres_system_identifier = (select system_identifier::text from pg_control_system())
      and d.database_oid = (select oid from pg_database where datname = current_database())
      and d.database_name = current_database()) then
    raise exception using errcode = '55000', message = 'source authority physical domain mismatch';
  end if;
  return s;
end
$$;

-- W1 has no verified source codec. Preserve its record-only route, but never
-- infer source absence from an unlocatable legacy projection. A malformed
-- source key blocks its project; an unlocatable project/domain blocks the domain.
create function public.source_retirement_legacy_blocked(p_deployment text, p_project text,
  p_kind text, p_source text)
returns boolean language sql stable security definer set search_path = pg_catalog
as $$
  select exists (
    select 1 from public.deletion_witness_entries w
    where (w.route, w.object_kind) is distinct from ('record_permanent_delete', 'record')
      and (
        (w.deployment_id ~ '^dp-[0-9a-f]{64}$' and w.project_id = 'default') is not true
        or (w.deployment_id = p_deployment and w.project_id = p_project and (
          (case w.object_kind
            when 'vps' then w.object_id ~ '^vps_[0-9a-f]{16}$'
            when 'monitoring_instance' then w.object_id ~ '^mi_[0-9a-f]{16}$'
            when 'target' then w.object_id ~ '^tg_[0-9a-f]{16}$'
            else false
          end) is not true
          or (w.object_kind = p_kind and w.object_id = p_source)
        ))
      )
  )
$$;
revoke all on function public.source_retirement_legacy_blocked(text, text, text, text) from public;

create function public.source_retirement_prepare(p_id text, p_deployment text, p_project text,
  p_kind text, p_source text, p_fingerprint bytea)
returns public.source_retirement_prepares
language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; p public.source_retirement_prepares;
begin
  s := record_platform_internal.source_authority_lock('producer', null);
  if not exists (select 1 from public.source_application_enrollments e
    join public.source_application_activations a using (generation)
    where e.generation = s.generation and e.deployment_id = p_deployment) then
    raise exception using errcode = '42501', message = 'source application enrollment is absent or inactive';
  end if;
  if public.source_retirement_legacy_blocked(p_deployment, p_project, p_kind, p_source) then
    raise exception using errcode = '55000', message = 'legacy source witness requires verified reconciliation';
  end if;
  select * into p from public.source_retirement_prepares where retirement_id = p_id;
  if found then
    if (p.deployment_id, p.project_id, p.source_kind, p.source_id, p.request_fingerprint)
      is distinct from (p_deployment, p_project, p_kind, p_source, p_fingerprint) then
      raise exception using errcode = '23505', message = 'source prepare identity conflict';
    end if;
    return p;
  end if;
  if exists (select 1 from public.source_retirement_terminals where
      (deployment_id, project_id, source_kind, source_id) = (p_deployment, p_project, p_kind, p_source))
    or exists (select 1 from public.source_retirement_unknown u join public.source_retirement_prepares q using(retirement_id)
      where (q.deployment_id, q.project_id, q.source_kind, q.source_id) = (p_deployment, p_project, p_kind, p_source)) then
    raise exception using errcode = '55000', message = 'source is permanently retired or coverage unknown';
  end if;
  insert into public.source_retirement_prepares(retirement_id, deployment_id, project_id,
    source_kind, source_id, origin_generation, request_fingerprint)
  values (p_id, p_deployment, p_project, p_kind, p_source, s.generation, p_fingerprint)
  returning * into p;
  return p;
end
$$;

-- The issuer supplies only committed APP facts; SQL constructs the canonical
-- envelope itself, so no independently mutable projection can masquerade as it.
create function record_platform_internal.source_retirement_append(p_id text, p_barrier bytea,
  p_last_live bytea, p_floor bytea, p_recovery text)
returns public.source_retirement_terminals
language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; p public.source_retirement_prepares;
  t public.source_retirement_terminals; h public.source_retirement_head;
  d public.record_platform_domain_identity; canonical bytea;
begin
  s := record_platform_internal.source_authority_lock(
    case when p_recovery is null then 'producer' else 'recovery' end, p_recovery);
  if p_recovery is not null and (s.mode <> 'recovering' or s.coverage_seal is not null) then
    raise exception using errcode = '55000', message = 'source recovery coverage is sealed or inactive';
  end if;
  select * into strict p from public.source_retirement_prepares where retirement_id = p_id;
  if p_recovery is null and not exists (select 1 from public.source_application_enrollments e
    join public.source_application_activations a using (generation)
    where e.generation = s.generation and e.deployment_id = p.deployment_id) then
    raise exception using errcode = '42501', message = 'source application enrollment is absent or inactive';
  end if;
  if public.source_retirement_legacy_blocked(p.deployment_id, p.project_id, p.source_kind, p.source_id) then
    raise exception using errcode = '55000', message = 'legacy source witness requires verified reconciliation';
  end if;
  select * into t from public.source_retirement_terminals where retirement_id = p_id;
  if found then
    if (t.barrier_digest, t.last_live, t.final_floor) is distinct from (p_barrier, p_last_live, p_floor) then
      raise exception using errcode = '23505', message = 'source terminal exact replay conflict';
    end if;
    return t;
  end if;
  if p_recovery is null and p.origin_generation <> s.generation then
    raise exception using errcode = '42501', message = 'old prepare requires recovery finalization';
  end if;
  if exists (select 1 from public.source_retirement_unknown u join public.source_retirement_prepares q using(retirement_id)
    where (q.deployment_id, q.project_id, q.source_kind, q.source_id) =
      (p.deployment_id, p.project_id, p.source_kind, p.source_id)) then
    raise exception using errcode = '55000', message = 'source coverage permanently unknown';
  end if;
  select * into strict h from public.source_retirement_head where singleton for update;
  select * into strict d from public.record_platform_domain_identity;
  canonical := record_platform_internal.source_bytes(convert_to('HOUFENG-SOURCE-TERMINAL-V2', 'UTF8')) || int4send(2)
    || record_platform_internal.source_bytes(convert_to(d.domain_id, 'UTF8'))
    || record_platform_internal.source_bytes(convert_to(d.postgres_system_identifier, 'UTF8'))
    || oidsend(d.database_oid)
    || record_platform_internal.source_bytes(convert_to(d.database_name, 'UTF8')) || int8send(d.identity_epoch)
    || record_platform_internal.source_bytes(convert_to(p.deployment_id, 'UTF8'))
    || record_platform_internal.source_bytes(convert_to(p.project_id, 'UTF8'))
    || record_platform_internal.source_bytes(convert_to(p.source_kind, 'UTF8'))
    || record_platform_internal.source_bytes(convert_to(p.source_id, 'UTF8'))
    || record_platform_internal.source_bytes(convert_to(p.retirement_id, 'UTF8')) || int8send(p.origin_generation)
    || record_platform_internal.source_bytes(p_barrier)
    || record_platform_internal.source_bytes(p_last_live) || record_platform_internal.source_bytes(p_floor)
    || int8send(h.sequence + 1) || record_platform_internal.source_bytes(h.entry_hash);
  insert into public.source_retirement_terminals(retirement_id, deployment_id, project_id,
    source_kind, source_id, origin_generation, barrier_digest, last_live, final_floor,
    sequence, previous_hash, canonical_entry, entry_hash)
  values (p.retirement_id, p.deployment_id, p.project_id, p.source_kind, p.source_id,
    p.origin_generation, p_barrier, p_last_live, p_floor, h.sequence + 1, h.entry_hash,
    canonical, record_platform_internal.digest(canonical, 'sha256')) returning * into t;
  update public.source_retirement_head set sequence = t.sequence, entry_hash = t.entry_hash where singleton;
  return t;
end
$$;
create function public.source_retirement_append(p_id text, p_barrier bytea, p_last_live bytea, p_floor bytea)
returns public.source_retirement_terminals language sql security definer set search_path = pg_catalog
as $$ select record_platform_internal.source_retirement_append(p_id, p_barrier, p_last_live, p_floor, null) $$;
create function public.source_retirement_finalize(p_recovery text, p_id text, p_barrier bytea, p_last_live bytea, p_floor bytea)
returns public.source_retirement_terminals language sql security definer set search_path = pg_catalog
as $$ select record_platform_internal.source_retirement_append(p_id, p_barrier, p_last_live, p_floor, p_recovery) $$;

create function public.source_recovery_unknown(p_recovery text, p_id text)
returns void language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; p public.source_retirement_prepares;
begin
  s := record_platform_internal.source_authority_lock('recovery', p_recovery);
  if s.mode <> 'recovering' or s.coverage_seal is not null then
    raise exception using errcode = '55000', message = 'source coverage is sealed or inactive';
  end if;
  select * into strict p from public.source_retirement_prepares where retirement_id = p_id;
  if exists (select 1 from public.source_retirement_terminals t where
    (t.deployment_id, t.project_id, t.source_kind, t.source_id) =
    (p.deployment_id, p.project_id, p.source_kind, p.source_id)) then
    return;
  end if;
  insert into public.source_retirement_unknown(retirement_id, recovery_id, installed_generation)
  values (p_id, p_recovery, s.generation) on conflict (retirement_id) do nothing;
end
$$;

create function public.source_recovery_seal(p_recovery text, p_namespace_digest bytea)
returns public.source_authority_recoveries
language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; r public.source_authority_recoveries;
  h public.source_retirement_head; high_water bigint; n bigint; pd bytea; gd bytea; seal bytea;
begin
  s := record_platform_internal.source_authority_lock('recovery', p_recovery);
  select * into strict r from public.source_authority_recoveries where recovery_id = p_recovery;
  if octet_length(p_namespace_digest) is distinct from 32 then
    raise exception using errcode = '55000', message = 'source recovery seal unavailable';
  end if;
  if r.phase <> 'recovering' then
    if r.namespace_handoff_digest is distinct from p_namespace_digest or r.coverage_seal is distinct from s.coverage_seal then
      raise exception using errcode = '55000', message = 'source seal replay mismatch';
    end if;
    return r;
  end if;
  if s.mode <> 'recovering' then
    raise exception using errcode = '55000', message = 'source recovery seal unavailable';
  end if;
  -- A verified terminal dominates extra prepares for the same source.
  if exists (select 1 from public.source_retirement_prepares p where not exists (
    select 1 from public.source_retirement_terminals t where
      (t.deployment_id, t.project_id, t.source_kind, t.source_id) =
      (p.deployment_id, p.project_id, p.source_kind, p.source_id))
    and not exists (select 1 from public.source_retirement_unknown u where u.retirement_id = p.retirement_id)) then
    raise exception using errcode = '55000', message = 'source retirement coverage incomplete';
  end if;
  select coalesce(max(prepare_sequence), 0), count(*), record_platform_internal.digest(
    coalesce(string_agg(int8send(prepare_sequence) || record_platform_internal.source_bytes(convert_to(retirement_id, 'UTF8'))
      || request_fingerprint, ''::bytea order by prepare_sequence), ''::bytea), 'sha256')
    into high_water, n, pd from public.source_retirement_prepares;
  select record_platform_internal.digest(coalesce(string_agg(
    record_platform_internal.source_bytes(convert_to(retirement_id, 'UTF8')) || int8send(installed_generation),
    ''::bytea order by retirement_id collate "C"), ''::bytea), 'sha256') into gd from public.source_retirement_unknown;
  select * into strict h from public.source_retirement_head where singleton for update;
  seal := record_platform_internal.digest(convert_to('HOUFENG-SOURCE-COVERAGE-V2', 'UTF8')
    || record_platform_internal.source_bytes(convert_to((select domain_id from public.record_platform_domain_identity), 'UTF8'))
    || int8send(s.generation) || record_platform_internal.source_bytes(convert_to(p_recovery, 'UTF8'))
    || int8send(high_water) || int8send(n) || pd || int8send(h.sequence) || h.entry_hash || gd || p_namespace_digest, 'sha256');
  update public.source_authority_recoveries set phase = 'coverage_sealed', coverage_seal = seal,
    prepare_high_water = high_water, prepare_count = n, prepare_digest = pd,
    terminal_sequence = h.sequence, terminal_hash = h.entry_hash, guard_digest = gd,
    namespace_handoff_digest = p_namespace_digest
    where recovery_id = p_recovery returning * into r;
  update public.source_authority_state set coverage_seal = seal where singleton;
  return r;
end
$$;

-- No W2 DML or callable mutation is inherited from PUBLIC. Provisioning grants
-- only purpose-specific entrypoints to separately bound direct-login roles.
revoke all on table public.source_authority_state, public.source_authority_principals,
  public.source_retirement_prepares, public.source_retirement_terminals,
  public.source_retirement_head, public.source_retirement_unknown,
  public.source_authority_recoveries, public.source_authority_credentials from public;
revoke all on sequence public.source_retirement_prepares_prepare_sequence_seq from public;
revoke all on function record_platform_internal.source_bytes(bytea),
  record_platform_internal.source_authority_lock(text, text),
  record_platform_internal.source_retirement_append(text, bytea, bytea, bytea, text),
  public.source_retirement_prepare(text, text, text, text, text, bytea),
  public.source_retirement_append(text, bytea, bytea, bytea),
  public.source_retirement_finalize(text, text, bytea, bytea, bytea),
  public.source_recovery_unknown(text, text), public.source_recovery_seal(text, bytea) from public;

-- A provisioner is an explicit TCB coordinator, not an application producer.
-- It can bind fresh principals but cannot alter or recycle an old binding.
create function public.source_authority_bind(p_principal name, p_purpose text,
  p_generation bigint, p_recovery text)
returns void language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; old public.source_authority_principals;
begin
  s := record_platform_internal.source_authority_lock('provisioner', null);
  select * into old from public.source_authority_principals where principal = p_principal;
  if found then
    if (old.purpose, old.generation, old.recovery_id) is distinct from (p_purpose, p_generation, p_recovery) then
      raise exception using errcode = '23505', message = 'source principal cannot be reused';
    end if;
    return;
  end if;
  if p_purpose not in ('reader', 'producer', 'recovery')
    or not exists (select 1 from pg_roles where rolname = p_principal
      and (rolcanlogin or (p_purpose = 'producer' and p_generation = s.generation
        and s.mode = 'recovering' and exists (
          select 1 from public.source_authority_recoveries r
          where r.recovery_id = s.recovery_id and r.generation = s.generation
            and r.phase <> 'completed')))
      and not rolsuper and not rolcreaterole and not rolcreatedb and not rolreplication
      and not rolbypassrls and not rolinherit)
    or exists (select 1 from pg_auth_members where member = (select oid from pg_roles where rolname = p_principal))
    or not ((p_generation = s.generation and p_purpose in ('reader', 'producer'))
      or (p_purpose = 'recovery' and p_generation = s.generation + 1
        and p_recovery ~ '^[a-z0-9_-]{1,128}$')) then
    raise exception using errcode = '42501', message = 'invalid source principal binding';
  end if;
  insert into public.source_authority_principals(principal, purpose, generation, recovery_id)
    values (p_principal, p_purpose, p_generation, p_recovery);
end
$$;

create function public.source_recovery_begin(p_recovery text, p_endpoint bytea, p_principal name)
returns public.source_authority_recoveries
language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; r public.source_authority_recoveries;
begin
  s := record_platform_internal.source_authority_lock('provisioner', null);
  select * into r from public.source_authority_recoveries where recovery_id = p_recovery;
  if found then
    if r.endpoint_digest is distinct from p_endpoint or r.generation <> s.generation
      or s.recovery_id is distinct from p_recovery or not exists (
        select 1 from public.source_authority_principals where principal = p_principal
          and purpose = 'recovery' and generation = r.generation and recovery_id = p_recovery) then
      raise exception using errcode = '23505', message = 'source recovery exact identity conflict';
    end if;
    return r;
  end if;
  if s.mode <> 'active' or exists (select 1 from public.source_authority_recoveries
    where recovery_id = s.recovery_id and phase <> 'completed') or not exists (
      select 1 from public.source_authority_principals where principal = p_principal
        and purpose = 'recovery' and generation = s.generation + 1 and recovery_id = p_recovery) then
    raise exception using errcode = '55000', message = 'source recovery interlock is occupied';
  end if;
  if (select count(*) from public.source_authority_credentials where generation = s.generation) <> 5 then
    raise exception using errcode = '55000', message = 'current credential commitments are not enrolled';
  end if;
  insert into public.source_authority_recoveries(recovery_id, generation, endpoint_digest, phase)
    values (p_recovery, s.generation + 1, p_endpoint, 'recovering') returning * into r;
  update public.source_authority_state set generation = r.generation, mode = 'recovering',
    recovery_id = p_recovery, coverage_seal = null where singleton;
  return r;
end
$$;

create table public.source_authority_handoff_events (
  recovery_id text not null references public.source_authority_recoveries(recovery_id),
  phase text not null check (phase in ('credentials_staged', 'witness_activated',
    'app_active_login_closed', 'credentials_published', 'logins_open', 'completed')),
  evidence_digest bytea not null check (octet_length(evidence_digest) = 32
    and evidence_digest <> decode(repeat('00', 32), 'hex')),
  primary key (recovery_id, phase)
);
create trigger source_authority_handoff_events_immutable
before update or delete or truncate on public.source_authority_handoff_events
for each statement execute function record_platform_internal.reject_immutable_mutation();

create function public.source_recovery_credentials(p_recovery text, p_purposes text[],
  p_principals text[], p_commitments bytea[], p_fingerprints bytea[], p_manifest bytea)
returns void language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; r public.source_authority_recoveries; i integer;
  c public.source_authority_credentials;
begin
  s := record_platform_internal.source_authority_lock('recovery', p_recovery);
  select * into strict r from public.source_authority_recoveries where recovery_id = p_recovery;
  if r.phase = 'recovering'
    or p_purposes is distinct from array['runtime', 'admin', 'worker', 'migrator', 'producer']::text[]
    or cardinality(p_principals) is distinct from 5 or array_ndims(p_principals) is distinct from 1 or array_lower(p_principals, 1) is distinct from 1
    or cardinality(p_commitments) is distinct from 5 or array_ndims(p_commitments) is distinct from 1 or array_lower(p_commitments, 1) is distinct from 1
    or cardinality(p_fingerprints) is distinct from 5 or array_ndims(p_fingerprints) is distinct from 1 or array_lower(p_fingerprints, 1) is distinct from 1
    or octet_length(p_manifest) is distinct from 32 or p_manifest = decode(repeat('00', 32), 'hex') then
    raise exception using errcode = '55000', message = 'source credential staging rejected';
  end if;
  for i in 1..5 loop
    if p_principals[i] is null or p_principals[i] !~ '^[a-z][a-z0-9_]{0,62}$'
      or octet_length(p_commitments[i]) is distinct from 32
      or p_commitments[i] = decode(repeat('00', 32), 'hex')
      or octet_length(p_fingerprints[i]) is distinct from 32
      or p_fingerprints[i] = decode(repeat('00', 32), 'hex') then
      raise exception using errcode = '22023', message = 'source credential identity invalid';
    end if;
    if exists (select 1 from public.source_authority_credentials
      where generation < s.generation and secret_fingerprint = p_fingerprints[i]) then
      raise exception using errcode = '23505', message = 'source credential secret was previously used';
    end if;
    select * into c from public.source_authority_credentials
      where generation = s.generation and purpose = p_purposes[i];
    if found then
      if (c.principal::text, c.commitment, c.secret_fingerprint) is distinct from
        (p_principals[i], p_commitments[i], p_fingerprints[i]) then
        raise exception using errcode = '23505', message = 'source credential replay mismatch';
      end if;
    else
      if r.phase <> 'coverage_sealed' then
        raise exception using errcode = '55000', message = 'source staged credential is missing';
      end if;
      insert into public.source_authority_credentials(generation, purpose, principal, commitment, secret_fingerprint)
        values (s.generation, p_purposes[i], p_principals[i]::name, p_commitments[i], p_fingerprints[i]);
    end if;
  end loop;
  if not exists (select 1 from public.source_authority_principals where principal = p_principals[5]::name
    and purpose = 'producer' and generation = s.generation) then
    raise exception using errcode = '42501', message = 'fresh source producer not bound';
  end if;
  if r.credential_manifest_digest is not null and r.credential_manifest_digest <> p_manifest then
    raise exception using errcode = '23505', message = 'source credential manifest replay mismatch';
  end if;
  if r.phase <> 'coverage_sealed' then
    if r.credential_manifest_digest is distinct from p_manifest then
      raise exception using errcode = '23505', message = 'source credential manifest replay mismatch';
    end if;
    return;
  end if;
  update public.source_authority_recoveries set phase = 'credentials_staged',
    credential_manifest_digest = p_manifest where recovery_id = p_recovery;
  insert into public.source_authority_handoff_events(recovery_id, phase, evidence_digest)
    values (p_recovery, 'credentials_staged', p_manifest) on conflict do nothing;
end
$$;

create function public.source_recovery_advance(p_recovery text, p_expected text,
  p_next text, p_seal bytea, p_manifest bytea, p_evidence bytea)
returns public.source_authority_recoveries
language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; r public.source_authority_recoveries; prior bytea;
begin
  s := record_platform_internal.source_authority_lock('recovery', p_recovery);
  select * into strict r from public.source_authority_recoveries where recovery_id = p_recovery;
  if r.coverage_seal is distinct from p_seal or s.coverage_seal is distinct from p_seal
    or r.credential_manifest_digest is distinct from p_manifest or p_seal is null or p_manifest is null
    or not ((p_expected = 'credentials_staged' and p_next = 'witness_activated')
      or (p_expected = 'witness_activated' and p_next = 'app_active_login_closed')
      or (p_expected = 'app_active_login_closed' and p_next = 'credentials_published')
      or (p_expected = 'credentials_published' and p_next = 'logins_open')
      or (p_expected = 'logins_open' and p_next = 'completed')) then
    raise exception using errcode = '55000', message = 'source handoff identity or transition rejected';
  end if;
  select evidence_digest into prior from public.source_authority_handoff_events
    where recovery_id = p_recovery and phase = p_next;
  if found then
    if prior is distinct from p_evidence then
      raise exception using errcode = '23505', message = 'source handoff evidence replay mismatch';
    end if;
    return r;
  end if;
  if r.phase <> p_expected then
    raise exception using errcode = '55000', message = 'source handoff compare-and-set failed';
  end if;
  insert into public.source_authority_handoff_events(recovery_id, phase, evidence_digest)
    values (p_recovery, p_next, p_evidence);
  update public.source_authority_recoveries set phase = p_next where recovery_id = p_recovery returning * into r;
  if p_next = 'witness_activated' then
    update public.source_authority_state set mode = 'active' where singleton;
  end if;
  -- recovery_id intentionally survives activation and completion.
  return r;
end
$$;

create function public.source_authority_snapshot()
returns table (domain_id text, system_identifier text, database_oid oid, database_name text,
  identity_epoch bigint, generation bigint, mode text, recovery_id text,
  handoff_completed boolean, head_sequence bigint, head_hash bytea)
language plpgsql stable security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; p public.source_authority_principals;
  d public.record_platform_domain_identity; h public.source_retirement_head;
begin
  select * into strict s from public.source_authority_state where singleton;
  select * into strict p from public.source_authority_principals where principal = session_user;
  select * into strict d from public.record_platform_domain_identity;
  select * into strict h from public.source_retirement_head where singleton;
  if session_user = current_user or p.purpose not in ('reader', 'producer', 'recovery', 'provisioner')
    or (p.purpose in ('producer', 'recovery') and p.generation <> s.generation)
    or not exists (select 1 from pg_roles where rolname = session_user and rolcanlogin
      and not rolsuper and not rolcreaterole and not rolcreatedb and not rolreplication
      and not rolbypassrls and not rolinherit)
    or exists (select 1 from pg_auth_members where member = (select oid from pg_roles where rolname = session_user))
    or d.domain_kind <> 'deletion_witness' or d.identity_mode <> 'postgres_system'
    or d.postgres_system_identifier <> (select control.system_identifier::text from pg_control_system() control)
    or d.database_oid <> (select oid from pg_database where datname = current_database())
    or d.database_name <> current_database() then
    raise exception using errcode = '42501', message = 'source authority reader or physical identity rejected';
  end if;
  return query select d.domain_id, d.postgres_system_identifier, d.database_oid, d.database_name,
    d.identity_epoch, s.generation, s.mode, s.recovery_id,
    s.recovery_id is null or exists (select 1 from public.source_authority_recoveries r
      where r.recovery_id = s.recovery_id and r.phase = 'completed'), h.sequence, h.entry_hash;
end
$$;

revoke all on public.source_authority_handoff_events from public;
revoke all on function public.source_authority_bind(name, text, bigint, text),
  public.source_recovery_begin(text, bytea, name),
  public.source_recovery_credentials(text, text[], text[], bytea[], bytea[], bytea),
  public.source_recovery_advance(text, text, text, bytea, bytea, bytea),
  public.source_authority_snapshot() from public;

create function public.source_enrollment_credentials(p_purposes text[], p_principals text[], p_commitments bytea[], p_fingerprints bytea[])
returns void language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; c public.source_authority_credentials; i integer;
begin
  s := record_platform_internal.source_authority_lock('provisioner', null);
  if s.generation <> 1 or s.mode <> 'active' or s.recovery_id is not null
    or p_purposes is distinct from array['runtime', 'admin', 'worker', 'migrator', 'producer']::text[]
    or cardinality(p_principals) is distinct from 5 or array_ndims(p_principals) is distinct from 1 or array_lower(p_principals, 1) is distinct from 1
    or cardinality(p_commitments) is distinct from 5 or array_ndims(p_commitments) is distinct from 1 or array_lower(p_commitments, 1) is distinct from 1
    or cardinality(p_fingerprints) is distinct from 5 or array_ndims(p_fingerprints) is distinct from 1 or array_lower(p_fingerprints, 1) is distinct from 1 then
    raise exception using errcode = '55000', message = 'initial source credential enrollment rejected';
  end if;
  for i in 1..5 loop
    if p_principals[i] is null or p_principals[i] !~ '^[a-z][a-z0-9_]{0,62}$'
      or octet_length(p_commitments[i]) is distinct from 32
      or p_commitments[i] = decode(repeat('00', 32), 'hex')
      or octet_length(p_fingerprints[i]) is distinct from 32
      or p_fingerprints[i] = decode(repeat('00', 32), 'hex') then
      raise exception using errcode = '22023', message = 'initial source credential identity invalid';
    end if;
    select * into c from public.source_authority_credentials where generation = 1 and purpose = p_purposes[i];
    if found then
      if (c.principal::text, c.commitment, c.secret_fingerprint) is distinct from
        (p_principals[i], p_commitments[i], p_fingerprints[i]) then
        raise exception using errcode = '23505', message = 'initial source credential replay mismatch';
      end if;
    else
      insert into public.source_authority_credentials(generation, purpose, principal, commitment, secret_fingerprint)
        values (1, p_purposes[i], p_principals[i]::name, p_commitments[i], p_fingerprints[i]);
    end if;
  end loop;
  if not exists (select 1 from public.source_authority_principals where principal = p_principals[5]::name
    and purpose = 'producer' and generation = 1) then
    raise exception using errcode = '42501', message = 'initial source producer not bound';
  end if;
end
$$;
revoke all on function public.source_enrollment_credentials(text[], text[], bytea[], bytea[]) from public;

-- Expected APP identity is held outside the APP rollback unit. A self-consistent
-- identity read from an arbitrary APP endpoint is not an enrollment proof.
create table public.source_application_enrollments (
  generation bigint primary key check (generation > 0),
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  application_domain_id text not null check (application_domain_id ~ '^rd-[0-9a-f]{64}$'),
  application_system_identifier text not null check (
    application_system_identifier ~ '^[1-9][0-9]{0,19}$'
    and application_system_identifier::numeric <= 18446744073709551615),
  application_database_oid oid not null check (application_database_oid <> 0),
  application_database_name text not null check (octet_length(application_database_name) between 1 and 63),
  application_identity_epoch bigint not null check (application_identity_epoch > 0)
);
create table public.source_application_activations (
  generation bigint primary key references public.source_application_enrollments(generation),
  activation_digest bytea not null check (octet_length(activation_digest) = 32
    and activation_digest <> decode(repeat('00', 32), 'hex'))
);
create trigger source_application_enrollments_immutable
before update or delete or truncate on public.source_application_enrollments
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger source_application_activations_immutable
before update or delete or truncate on public.source_application_activations
for each statement execute function record_platform_internal.reject_immutable_mutation();

create function public.source_application_enroll(p_deployment text, p_domain text, p_system text,
  p_oid oid, p_database text, p_epoch bigint)
returns void language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; e public.source_application_enrollments;
begin
  s := record_platform_internal.source_authority_lock('provisioner', null);
  select * into e from public.source_application_enrollments where generation = s.generation;
  if found then
    if (e.deployment_id, e.application_domain_id, e.application_system_identifier,
        e.application_database_oid, e.application_database_name, e.application_identity_epoch)
      is distinct from (p_deployment, p_domain, p_system, p_oid, p_database, p_epoch) then
      raise exception using errcode = '23505', message = 'source application enrollment cannot be rebound';
    end if;
    return;
  end if;
  if s.generation <> 1 or s.mode <> 'active' or s.recovery_id is not null
    or exists (select 1 from public.source_retirement_prepares)
    or exists (select 1 from public.source_retirement_terminals)
    or exists (select 1 from public.record_platform_domain_identity
      where postgres_system_identifier = p_system or domain_id = p_domain) then
    raise exception using errcode = '55000', message = 'initial independent source application enrollment rejected';
  end if;
  insert into public.source_application_enrollments(generation, deployment_id,
    application_domain_id, application_system_identifier, application_database_oid,
    application_database_name, application_identity_epoch)
  values (s.generation, p_deployment, p_domain, p_system, p_oid, p_database, p_epoch);
end
$$;

-- This is the trusted provisioner's acknowledgement of a confirmed APP commit,
-- not an acknowledgement an ordinary producer, runtime, or worker can invent.
create function public.source_application_activate(p_activation bytea)
returns void language plpgsql security definer set search_path = pg_catalog
as $$
declare s public.source_authority_state; old bytea;
begin
  s := record_platform_internal.source_authority_lock('provisioner', null);
  select activation_digest into old from public.source_application_activations where generation = s.generation;
  if found then
    if old is distinct from p_activation then
      raise exception using errcode = '23505', message = 'source application activation replay mismatch';
    end if;
    return;
  end if;
  if s.generation <> 1 or s.mode <> 'active' or s.recovery_id is not null
    or not exists (select 1 from public.source_application_enrollments where generation = s.generation)
    or (select count(*) from public.source_authority_credentials where generation = s.generation) <> 5 then
    raise exception using errcode = '55000', message = 'initial source application activation rejected';
  end if;
  insert into public.source_application_activations(generation, activation_digest)
    values (s.generation, p_activation);
end
$$;
revoke all on table public.source_application_enrollments, public.source_application_activations from public;
revoke all on function public.source_application_enroll(text, text, text, oid, text, bigint),
  public.source_application_activate(bytea) from public;
