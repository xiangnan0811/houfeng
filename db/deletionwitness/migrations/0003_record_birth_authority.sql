-- W2 record birth is independent of the W1/source terminal chain. Installation
-- deliberately creates no key, principal, binding, or enabled binding. A trusted
-- offline provisioner owns those rows and must establish the APP/W2 fence first.
create table public.record_birth_keys (
  key_id text primary key check (key_id ~ '^bk-[0-9a-f]{64}$'),
  key_bytes bytea not null check (octet_length(key_bytes) = 32
    and key_bytes <> decode(repeat('00', 32), 'hex')),
  key_commitment bytea not null unique check (octet_length(key_commitment) = 32
    and key_commitment = record_platform_internal.digest(key_bytes, 'sha256'))
);
create table public.record_birth_lineage (
  singleton boolean primary key default true check (singleton),
  authority_id text not null check (authority_id ~ '^rd-[0-9a-f]{64}$'),
  lineage_id text not null check (lineage_id ~ '^bl-[0-9a-f]{64}$'),
  unique (authority_id, lineage_id)
);
create table public.record_birth_bindings (
  generation bigint not null check (generation > 0),
  authority_id text not null check (authority_id ~ '^rd-[0-9a-f]{64}$'),
  lineage_id text not null check (lineage_id ~ '^bl-[0-9a-f]{64}$'),
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  key_id text not null references public.record_birth_keys(key_id),
  cutover_digest bytea not null check (octet_length(cutover_digest) = 32
    and cutover_digest <> decode(repeat('00', 32), 'hex')),
  primary key (generation, key_id, cutover_digest),
  unique (authority_id, lineage_id, deployment_id, project_id, generation, key_id, cutover_digest),
  foreign key (authority_id, lineage_id) references public.record_birth_lineage(authority_id, lineage_id)
);
-- This singleton is the explicit enabled binding. Its absence means disabled;
-- migration and ordinary provisioning never populate it.
create table public.record_birth_enabled (
  singleton boolean primary key default true check (singleton),
  generation bigint not null,
  key_id text not null,
  cutover_digest bytea not null,
  foreign key (generation,key_id,cutover_digest)
    references public.record_birth_bindings(generation,key_id,cutover_digest)
);
create table public.record_birth_principals (
  principal name primary key,
  purpose text not null check (purpose in ('producer', 'reader')),
  generation bigint not null check (generation > 0),
  unique (generation, purpose)
);
create table public.record_birth_requests (
  reservation_id text primary key check (reservation_id ~ '^rba_[0-9a-f]{32}$'),
  record_id text not null unique check (record_id ~ '^rec2_[89a-f][0-9a-f]{31}$'),
  authority_id text not null check (authority_id ~ '^rd-[0-9a-f]{64}$'),
  lineage_id text not null check (lineage_id ~ '^bl-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  actor_id text not null check (actor_id ~ '^[a-z0-9_]{1,128}$'),
  actor_scope_digest bytea not null check (octet_length(actor_scope_digest) = 32
    and actor_scope_digest <> decode(repeat('00', 32), 'hex')),
  purpose text not null check (purpose in ('record_create','evidence_preview','comparison_save','import')),
  idempotency_key text not null check (idempotency_key ~ '^[a-zA-Z0-9._~-]{1,200}$'
    and idempotency_key !~ 'drt1_[a-zA-Z0-9_-]{43}'),
  request_fingerprint bytea not null check (octet_length(request_fingerprint) = 32
    and request_fingerprint <> decode(repeat('00', 32), 'hex')),
  unique (lineage_id, project_id, actor_id, purpose, idempotency_key)
);
create table public.record_birth_head (
  singleton boolean primary key default true check (singleton),
  sequence bigint not null check (sequence >= 0),
  receipt_hash bytea not null check (octet_length(receipt_hash) = 32),
  check ((sequence = 0) = (receipt_hash = decode(repeat('00', 32), 'hex')))
);
insert into public.record_birth_head values (true, 0, decode(repeat('00', 32), 'hex'));
create table public.record_birth_events (
  sequence bigint primary key check (sequence > 0),
  reservation_id text not null references public.record_birth_requests(reservation_id),
  generation bigint not null,
  key_id text not null,
  cutover_digest bytea not null,
  kind text not null check (kind in ('reserved','prepared','created')),
  previous_hash bytea not null check (octet_length(previous_hash) = 32),
  initial_commitment bytea not null check (octet_length(initial_commitment) = 32),
  receipt bytea not null check (octet_length(receipt) between 33 and 4096),
  receipt_hash bytea not null unique check (octet_length(receipt_hash) = 32
    and receipt_hash = record_platform_internal.digest(receipt, 'sha256')),
  unique (reservation_id, kind),
  foreign key (generation,key_id,cutover_digest)
    references public.record_birth_bindings(generation,key_id,cutover_digest),
  check ((sequence = 1) = (previous_hash = decode(repeat('00', 32), 'hex'))),
  check ((kind = 'reserved') = (initial_commitment = decode(repeat('00', 32), 'hex')))
);
create trigger record_birth_keys_immutable before update or delete or truncate on public.record_birth_keys
  for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger record_birth_lineage_immutable before update or delete or truncate on public.record_birth_lineage
  for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger record_birth_bindings_immutable before update or delete or truncate on public.record_birth_bindings
  for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger record_birth_principals_immutable before update or delete or truncate on public.record_birth_principals
  for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger record_birth_requests_immutable before update or delete or truncate on public.record_birth_requests
  for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger record_birth_events_immutable before update or delete or truncate on public.record_birth_events
  for each statement execute function record_platform_internal.reject_immutable_mutation();

create table public.record_birth_checkpoint_anchor (
  singleton boolean primary key default true check (singleton),
  sequence bigint not null check (sequence>=0),
  checkpoint_hash bytea not null check (octet_length(checkpoint_hash)=32),
  check ((sequence=0)=(checkpoint_hash=decode(repeat('00',32),'hex')))
);
insert into public.record_birth_checkpoint_anchor values(true,0,decode(repeat('00',32),'hex'));
create table public.record_birth_request_coverage (
  lineage_id text not null,
  project_id text not null check (project_id='default'),
  actor_id text not null,
  purpose text not null,
  idempotency_key text not null,
  allocation_id text not null unique check (allocation_id ~ '^rqa_[0-9a-f]{32}$'),
  checkpoint_sequence bigint not null check (checkpoint_sequence>0),
  checkpoint_hash bytea not null check (octet_length(checkpoint_hash)=32),
  checkpoint_bytes bytea not null check (octet_length(checkpoint_bytes) between 1 and 4096
    and record_platform_internal.digest(checkpoint_bytes,'sha256')=checkpoint_hash),
  primary key(lineage_id,project_id,actor_id,purpose,idempotency_key),
  unique(lineage_id,checkpoint_sequence)
);
create trigger record_birth_request_coverage_immutable before update or delete or truncate
  on public.record_birth_request_coverage for each statement
  execute function record_platform_internal.reject_immutable_mutation();
revoke all on public.record_birth_checkpoint_anchor,public.record_birth_request_coverage from public;

-- Decode one bounded canonical envelope. Authentication and current admission
-- are checked separately before these projections can authorize coverage.
create function record_platform_internal.record_birth_checkpoint_decode(raw bytea)
returns jsonb language plpgsql immutable security definer set search_path=pg_catalog as $$
declare fields text[]:=array['proof_domain','authority_id','lineage_id','deployment_id','project_id','generation','key_id','cutover_digest',
    'expected_sequence','expected_hash','head_sequence','head_hash','checkpoint_domain',
    'original_authority_id','original_lineage_id','original_deployment_id','original_project_id','original_generation',
    'original_key_id','original_cutover_digest','checkpoint_sequence','previous_hash','allocation_id','actor_id',
    'actor_scope_digest','purpose','idempotency_key','request_fingerprint','checkpoint_hash'];
  field text; pos integer:=0; size integer; i integer; number bigint; value text;
  result jsonb:='{}'; entry_start integer; entry_end integer;
begin
  if raw is null or octet_length(raw) not between 33 and 4096 then
    raise exception using errcode='22023',message='invalid checkpoint proof length';
  end if;
  foreach field in array fields loop
    if field='checkpoint_domain' then entry_start:=pos; end if;
    if field='checkpoint_hash' then entry_end:=pos; end if;
    if field in ('generation','original_generation','expected_sequence','head_sequence','checkpoint_sequence') then
      if pos+8>octet_length(raw)-32 or get_byte(raw,pos)>127 then
        raise exception using errcode='22023',message='invalid checkpoint integer';
      end if;
      number:=0;
      for i in 0..7 loop number:=number*256+get_byte(raw,pos+i); end loop;
      pos:=pos+8; result:=result || jsonb_build_object(field,number);
    elsif field in ('cutover_digest','original_cutover_digest','expected_hash','head_hash','previous_hash',
      'actor_scope_digest','request_fingerprint','checkpoint_hash') then
      if pos+32>octet_length(raw)-32 then
        raise exception using errcode='22023',message='truncated checkpoint digest';
      end if;
      result:=result || jsonb_build_object(field,encode(substring(raw from pos+1 for 32),'hex')); pos:=pos+32;
    else
      if pos+4>octet_length(raw)-32 or get_byte(raw,pos)<>0 or get_byte(raw,pos+1)<>0 then
        raise exception using errcode='22023',message='invalid checkpoint text length';
      end if;
      size:=get_byte(raw,pos+2)*256+get_byte(raw,pos+3); pos:=pos+4;
      if size>200 or pos+size>octet_length(raw)-32 then
        raise exception using errcode='22023',message='invalid checkpoint text size';
      end if;
      value:=convert_from(substring(raw from pos+1 for size),'UTF8'); pos:=pos+size;
      result:=result || jsonb_build_object(field,value);
    end if;
  end loop;
  if pos<>octet_length(raw)-32 or result->>'proof_domain'<>'houfeng.record-birth.app-checkpoint.v1'
    or result->>'checkpoint_domain'<>'houfeng.record-birth.app-checkpoint-entry.v1'
    or result->>'checkpoint_hash'<>encode(record_platform_internal.digest(substring(raw from entry_start+1 for entry_end-entry_start),'sha256'),'hex') then
    raise exception using errcode='22023',message='invalid checkpoint canonical framing';
  end if;
  return result || jsonb_build_object('checkpoint_bytes',encode(substring(raw from entry_start+1 for entry_end-entry_start),'hex'));
end $$;

create function record_platform_internal.record_birth_checkpoint_accept(p jsonb,b public.record_birth_bindings)
returns void language plpgsql security definer set search_path=pg_catalog as $$
declare raw bytea; proof jsonb; secret bytea; h public.record_birth_checkpoint_anchor;
  coverage public.record_birth_request_coverage; expected_seq bigint; head_seq bigint; checkpoint_seq bigint;
begin
  if p->>'checkpoint_proof' is null or length(p->>'checkpoint_proof') not between 66 and 8192
    or p->>'checkpoint_proof' !~ '^([0-9a-f]{2})+$' then
    raise exception using errcode='22023',message='checkpoint proof required';
  end if;
  raw:=decode(p->>'checkpoint_proof','hex'); proof:=record_platform_internal.record_birth_checkpoint_decode(raw);
  select key_bytes into strict secret from public.record_birth_keys where key_id=b.key_id;
  if substring(raw from octet_length(raw)-31) is distinct from
      record_platform_internal.hmac(substring(raw from 1 for octet_length(raw)-32),secret,'sha256')
    or (proof->>'authority_id',proof->>'lineage_id',proof->>'deployment_id',proof->>'project_id',
      (proof->>'generation')::bigint,proof->>'key_id',decode(proof->>'cutover_digest','hex')) is distinct from
      (b.authority_id,b.lineage_id,b.deployment_id,b.project_id,b.generation,b.key_id,b.cutover_digest)
    or (proof->>'actor_id',proof->>'actor_scope_digest',proof->>'purpose',proof->>'idempotency_key',proof->>'request_fingerprint')
      is distinct from (p->>'actor_id',p->>'actor_scope_digest',p->>'purpose',p->>'idempotency_key',p->>'request_fingerprint')
    or proof->>'allocation_id' !~ '^rqa_[0-9a-f]{32}$'
    or not exists (select 1 from public.record_birth_bindings original where original.generation=(proof->>'original_generation')::bigint
      and original.key_id=proof->>'original_key_id' and original.cutover_digest=decode(proof->>'original_cutover_digest','hex')
      and (original.authority_id,original.lineage_id,original.deployment_id,original.project_id)=
        (proof->>'original_authority_id',proof->>'original_lineage_id',proof->>'original_deployment_id',proof->>'original_project_id')
      and (original.authority_id,original.lineage_id,original.deployment_id,original.project_id)=
        (b.authority_id,b.lineage_id,b.deployment_id,b.project_id) and original.generation<=b.generation) then
    raise exception using errcode='42501',message='checkpoint proof authority rejected';
  end if;
  expected_seq:=(proof->>'expected_sequence')::bigint; head_seq:=(proof->>'head_sequence')::bigint;
  checkpoint_seq:=(proof->>'checkpoint_sequence')::bigint;
  if head_seq<=0 or checkpoint_seq<=0 or expected_seq>head_seq or checkpoint_seq>head_seq
    or ((expected_seq=0) is distinct from (proof->>'expected_hash'=repeat('00',32)))
    or ((checkpoint_seq=1) is distinct from (proof->>'previous_hash'=repeat('00',32)))
    or proof->>'head_hash'=repeat('00',32) or proof->>'checkpoint_hash'=repeat('00',32)
    or (expected_seq=head_seq and proof->>'expected_hash'<>proof->>'head_hash')
    or (checkpoint_seq=head_seq and proof->>'checkpoint_hash'<>proof->>'head_hash')
    or (checkpoint_seq=expected_seq and proof->>'checkpoint_hash'<>proof->>'expected_hash') then
    raise exception using errcode='22023',message='checkpoint proof endpoints inconsistent';
  end if;
  select * into strict h from public.record_birth_checkpoint_anchor where singleton for update;
  if (h.sequence,h.checkpoint_hash) is distinct from (expected_seq,decode(proof->>'expected_hash','hex')) then
    raise exception using errcode='40001',message='checkpoint anchor changed';
  end if;
  select * into coverage from public.record_birth_request_coverage c where
    (c.lineage_id,c.project_id,c.actor_id,c.purpose,c.idempotency_key)=
    (b.lineage_id,b.project_id,p->>'actor_id',p->>'purpose',p->>'idempotency_key');
  if found then
    if (coverage.allocation_id,coverage.checkpoint_sequence,coverage.checkpoint_hash,coverage.checkpoint_bytes)
      is distinct from (proof->>'allocation_id',checkpoint_seq,decode(proof->>'checkpoint_hash','hex'),decode(proof->>'checkpoint_bytes','hex')) then
      raise exception using errcode='23505',message='immutable checkpoint coverage conflict';
    end if;
  else
    insert into public.record_birth_request_coverage values(b.lineage_id,b.project_id,p->>'actor_id',p->>'purpose',
      p->>'idempotency_key',proof->>'allocation_id',checkpoint_seq,decode(proof->>'checkpoint_hash','hex'),decode(proof->>'checkpoint_bytes','hex'));
  end if;
  update public.record_birth_checkpoint_anchor set sequence=head_seq,checkpoint_hash=decode(proof->>'head_hash','hex') where singleton;
end $$;
revoke all on function record_platform_internal.record_birth_checkpoint_decode(bytea),
  record_platform_internal.record_birth_checkpoint_accept(jsonb,public.record_birth_bindings) from public;

-- Parse JSON before jsonb so duplicate members cannot be normalized away.
create function record_platform_internal.record_birth_command(p_command bytea, p_extra text[])
returns jsonb language plpgsql security definer set search_path = pg_catalog as $$
declare j json; pair record; expected text[] := array['authority_id','lineage_id','deployment_id',
  'project_id','generation','key_id','cutover_digest']::text[] || p_extra;
  seen text[] := array[]::text[]; value jsonb; field text; digest text;
begin
  j := convert_from(p_command, 'UTF8')::json;
  if json_typeof(j) <> 'object' then
    raise exception using errcode = '22023', message = 'birth command must be an object';
  end if;
  for pair in select x.key, x.value from json_each(j) as x loop
    if pair.key = 'generation' then
      if json_typeof(pair.value) <> 'number' or pair.value::text !~ '^[1-9][0-9]*$' then
        raise exception using errcode = '22023', message = 'noncanonical birth generation';
      end if;
      if (pair.value::text)::numeric > 9223372036854775807 then
        raise exception using errcode = '22023', message = 'birth generation exceeds bigint';
      end if;
    end if;
    if not (pair.key = any(expected)) or pair.key = any(seen)
      or (pair.key = 'generation' and json_typeof(pair.value) <> 'number')
      or (pair.key <> 'generation' and json_typeof(pair.value) <> 'string') then
      raise exception using errcode = '22023', message = 'invalid birth command field';
    end if;
    seen := array_append(seen, pair.key);
  end loop;
  if cardinality(seen) <> cardinality(expected) then
    raise exception using errcode = '22023', message = 'missing birth command field';
  end if;
  value := j::jsonb;
  if value->>'authority_id' !~ '^rd-[0-9a-f]{64}$'
    or value->>'lineage_id' !~ '^bl-[0-9a-f]{64}$'
    or value->>'deployment_id' !~ '^dp-[0-9a-f]{64}$'
    or value->>'project_id' <> 'default'
    or value->>'key_id' !~ '^bk-[0-9a-f]{64}$'
    or value->>'generation' !~ '^[1-9][0-9]*$'
    or (value->>'generation')::numeric > 9223372036854775807 then
    raise exception using errcode = '22023', message = 'invalid birth binding';
  end if;
  foreach field in array array['cutover_digest','actor_scope_digest','request_fingerprint','initial_commitment'] loop
    if field = any(expected) then
      digest := value->>field;
      if digest !~ '^[0-9a-f]{64}$' or digest = repeat('00', 32) then
        raise exception using errcode = '22023', message = 'invalid birth digest';
      end if;
    end if;
  end loop;
  if 'actor_id' = any(expected) and (value->>'actor_id' !~ '^[a-z0-9_]{1,128}$'
      or value->>'purpose' not in ('record_create','evidence_preview','comparison_save','import')
      or value->>'idempotency_key' !~ '^[a-zA-Z0-9._~-]{1,200}$'
      or value->>'idempotency_key' ~ 'drt1_[a-zA-Z0-9_-]{43}') then
    raise exception using errcode = '22023', message = 'invalid birth request';
  end if;
  if 'reservation_id' = any(expected) and value->>'reservation_id' !~ '^rba_[0-9a-f]{32}$' then
    raise exception using errcode = '22023', message = 'invalid birth reservation';
  end if;
  if 'record_id' = any(expected) and value->>'record_id' <> ''
    and value->>'record_id' !~ '^rec2_[89a-f][0-9a-f]{31}$' then
    raise exception using errcode = '22023', message = 'invalid birth record';
  end if;
  return value;
exception when invalid_text_representation or numeric_value_out_of_range or character_not_in_repertoire or untranslatable_character then
  raise exception using errcode = '22023', message = 'invalid birth command encoding';
end $$;

-- Serializes admission with source generation/handoff changes. No caller-set
-- flag or caller-provided physical identity participates in admission.
create function record_platform_internal.record_birth_admit(p jsonb, p_write boolean)
returns public.record_birth_bindings language plpgsql security definer set search_path = pg_catalog as $$
declare s public.source_authority_state; b public.record_birth_bindings;
begin
  select * into strict s from public.source_authority_state where singleton for update;
  select b0.* into strict b from public.record_birth_bindings b0 where b0.generation=s.generation
    and (exists (select 1 from public.record_birth_enabled e where e.singleton
      and (e.generation,e.key_id,e.cutover_digest)=(b0.generation,b0.key_id,b0.cutover_digest))
      or (not p_write and not exists (select 1 from public.record_birth_enabled)
        and (select count(*) from public.record_birth_bindings current_binding where current_binding.generation=s.generation)=1));
  if session_user = current_user or s.mode <> 'active' or s.generation <> b.generation
    or (s.generation <> 1 and s.recovery_id is null)
    or (s.recovery_id is not null and not exists (select 1 from public.source_authority_recoveries r
      where r.recovery_id = s.recovery_id and r.generation = s.generation and r.phase = 'completed'
        and s.coverage_seal is not null and r.coverage_seal = s.coverage_seal))
    or not exists (select 1 from public.record_birth_principals p0
      where p0.principal = session_user and p0.generation = s.generation
        and (p0.purpose = 'producer' or (not p_write and p0.purpose = 'reader')))
    or not exists (select 1 from pg_roles where rolname = session_user and rolcanlogin
      and not rolsuper and not rolcreaterole and not rolcreatedb and not rolreplication
      and not rolbypassrls and not rolinherit)
    or exists (select 1 from pg_auth_members where member = (select oid from pg_roles where rolname = session_user)
      or roleid = (select oid from pg_roles where rolname = session_user))
    or exists (select 1 from public.source_authority_principals where principal = session_user)
    or (select count(*) from public.record_platform_domain_identity) <> 1
    or not exists (select 1 from public.record_platform_domain_identity d
      where d.domain_id = b.authority_id and d.domain_kind = 'deletion_witness'
        and d.identity_mode = 'postgres_system' and d.identity_epoch = 1
        and d.postgres_system_identifier = (select system_identifier::text from pg_control_system())
        and d.database_oid = (select oid from pg_database where datname = current_database())
        and d.database_name = current_database())
    or not exists (select 1 from public.source_application_enrollments e
      join public.source_application_activations a using (generation)
      where e.generation = s.generation and e.deployment_id = b.deployment_id)
    or (b.authority_id,b.lineage_id,b.deployment_id,b.project_id,b.generation,b.key_id,b.cutover_digest)
      is distinct from (p->>'authority_id',p->>'lineage_id',p->>'deployment_id',p->>'project_id',
        (p->>'generation')::bigint,p->>'key_id',decode(p->>'cutover_digest','hex')) then
    raise exception using errcode = '42501', message = 'birth authority admission rejected';
  end if;
  return b;
exception when no_data_found then
  raise exception using errcode = '42501', message = 'birth authority is disabled';
end $$;

create function record_platform_internal.record_birth_encode(b public.record_birth_bindings,
  r public.record_birth_requests, p_kind text, p_sequence bigint, p_previous bytea, p_initial bytea)
returns bytea language sql security definer set search_path = pg_catalog as $$
  select record_platform_internal.source_bytes(convert_to('houfeng.record-birth.receipt.v1','UTF8'))
    || record_platform_internal.source_bytes(convert_to(b.authority_id,'UTF8'))
    || record_platform_internal.source_bytes(convert_to(b.lineage_id,'UTF8'))
    || record_platform_internal.source_bytes(convert_to(b.deployment_id,'UTF8'))
    || record_platform_internal.source_bytes(convert_to(b.project_id,'UTF8')) || int8send(b.generation)
    || record_platform_internal.source_bytes(convert_to(b.key_id,'UTF8')) || b.cutover_digest
    || int8send(p_sequence) || p_previous
    || record_platform_internal.source_bytes(convert_to(p_kind,'UTF8'))
    || record_platform_internal.source_bytes(convert_to(r.reservation_id,'UTF8'))
    || record_platform_internal.source_bytes(convert_to(r.record_id,'UTF8'))
    || record_platform_internal.source_bytes(convert_to(r.actor_id,'UTF8')) || r.actor_scope_digest
    || record_platform_internal.source_bytes(convert_to(r.purpose,'UTF8'))
    || record_platform_internal.source_bytes(convert_to(r.idempotency_key,'UTF8'))
    || r.request_fingerprint || p_initial
$$;

create function record_platform_internal.record_birth_verify(e public.record_birth_events)
returns bytea language plpgsql security definer set search_path = pg_catalog as $$
declare r public.record_birth_requests; b public.record_birth_bindings; k bytea; canonical bytea;
begin
  select * into strict r from public.record_birth_requests where reservation_id = e.reservation_id;
  select * into strict b from public.record_birth_bindings
    where (generation,key_id,cutover_digest) = (e.generation,e.key_id,e.cutover_digest);
  select key_bytes into strict k from public.record_birth_keys where key_id = b.key_id;
  if (b.authority_id,b.lineage_id,b.project_id) is distinct from (r.authority_id,r.lineage_id,r.project_id) then
    raise exception using errcode = '55000', message = 'birth event lineage mismatch';
  end if;
  canonical := record_platform_internal.record_birth_encode(b,r,e.kind,e.sequence,e.previous_hash,e.initial_commitment);
  if e.receipt is distinct from canonical || record_platform_internal.hmac(canonical,k,'sha256')
    or e.receipt_hash is distinct from record_platform_internal.digest(e.receipt,'sha256') then
    raise exception using errcode = '55000', message = 'birth receipt verification failed';
  end if;
  return e.receipt;
end $$;

create function record_platform_internal.record_birth_append(b public.record_birth_bindings,
  r public.record_birth_requests, p_kind text, p_initial bytea)
returns bytea language plpgsql security definer set search_path = pg_catalog as $$
declare h public.record_birth_head; last_event public.record_birth_events;
  canonical bytea; receipt bytea; next_hash bytea; k bytea;
begin
  select * into strict h from public.record_birth_head where singleton for update;
  if h.sequence = 9223372036854775807 then
    raise exception using errcode = '54000', message = 'birth event sequence exhausted';
  end if;
  if h.sequence > 0 then
    select * into strict last_event from public.record_birth_events where sequence = h.sequence;
    if last_event.receipt_hash is distinct from h.receipt_hash then
      raise exception using errcode = '55000', message = 'birth head hash mismatch';
    end if;
    perform record_platform_internal.record_birth_verify(last_event);
  end if;
  if (b.authority_id,b.lineage_id,b.project_id) is distinct from (r.authority_id,r.lineage_id,r.project_id) then
    raise exception using errcode = '23505', message = 'birth request lineage conflict';
  end if;
  select key_bytes into strict k from public.record_birth_keys where key_id = b.key_id;
  canonical := record_platform_internal.record_birth_encode(b,r,p_kind,h.sequence+1,h.receipt_hash,p_initial);
  receipt := canonical || record_platform_internal.hmac(canonical,k,'sha256');
  next_hash := record_platform_internal.digest(receipt,'sha256');
  insert into public.record_birth_events(sequence,reservation_id,generation,key_id,cutover_digest,kind,previous_hash,initial_commitment,receipt,receipt_hash)
    values (h.sequence+1,r.reservation_id,b.generation,b.key_id,b.cutover_digest,p_kind,h.receipt_hash,p_initial,receipt,next_hash);
  update public.record_birth_head set sequence=h.sequence+1, receipt_hash=next_hash where singleton;
  return receipt;
end $$;

create function public.record_birth_reserve(p_command bytea)
returns bytea language plpgsql security definer set search_path = pg_catalog as $$
declare p jsonb; b public.record_birth_bindings; r public.record_birth_requests; e public.record_birth_events;
  entropy bytea;
begin
  p := record_platform_internal.record_birth_command(p_command,
    array['actor_id','actor_scope_digest','purpose','idempotency_key','request_fingerprint','checkpoint_proof']);
  b := record_platform_internal.record_birth_admit(p,true);
  perform record_platform_internal.record_birth_checkpoint_accept(p,b);
  select * into r from public.record_birth_requests where lineage_id=b.lineage_id and project_id=b.project_id
    and actor_id=p->>'actor_id' and purpose=p->>'purpose' and idempotency_key=p->>'idempotency_key';
  if found then
    if (r.authority_id,r.actor_scope_digest,r.request_fingerprint) is distinct from
      (b.authority_id,decode(p->>'actor_scope_digest','hex'),decode(p->>'request_fingerprint','hex')) then
      raise exception using errcode = '23505', message = 'birth request binding conflict';
    end if;
    select * into strict e from public.record_birth_events where reservation_id=r.reservation_id and kind='reserved';
    return record_platform_internal.record_birth_verify(e);
  end if;
  loop
    entropy := record_platform_internal.gen_random_bytes(16);
    begin
      insert into public.record_birth_requests(reservation_id,record_id,authority_id,lineage_id,project_id,
        actor_id,actor_scope_digest,purpose,idempotency_key,request_fingerprint)
      values ('rba_'||encode(record_platform_internal.gen_random_bytes(16),'hex'),
        'rec2_'||encode(set_byte(entropy,0,get_byte(entropy,0)|128),'hex'),
        b.authority_id,b.lineage_id,b.project_id,p->>'actor_id',decode(p->>'actor_scope_digest','hex'),
        p->>'purpose',p->>'idempotency_key',decode(p->>'request_fingerprint','hex')) returning * into r;
      exit;
    exception when unique_violation then
      -- Admission serializes the stable key; only random ID collisions can retry.
      if exists (select 1 from public.record_birth_requests where lineage_id=b.lineage_id and project_id=b.project_id
          and actor_id=p->>'actor_id' and purpose=p->>'purpose' and idempotency_key=p->>'idempotency_key') then
        raise;
      end if;
    end;
  end loop;
  return record_platform_internal.record_birth_append(b,r,'reserved',decode(repeat('00',32),'hex'));
end $$;

create function record_platform_internal.record_birth_transition(p_command bytea, p_kind text)
returns bytea language plpgsql security definer set search_path = pg_catalog as $$
declare p jsonb; b public.record_birth_bindings; r public.record_birth_requests;
  e public.record_birth_events; prepared public.record_birth_events; initial bytea;
begin
  p := record_platform_internal.record_birth_command(p_command,array['reservation_id','initial_commitment']);
  b := record_platform_internal.record_birth_admit(p,true);
  initial := decode(p->>'initial_commitment','hex');
  select * into strict r from public.record_birth_requests where reservation_id=p->>'reservation_id';
  if (r.authority_id,r.lineage_id,r.project_id) is distinct from (b.authority_id,b.lineage_id,b.project_id) then
    raise exception using errcode = '42501', message = 'birth reservation lineage rejected';
  end if;
  select * into e from public.record_birth_events where reservation_id=r.reservation_id and kind=p_kind;
  if found then
    if e.initial_commitment is distinct from initial then
      raise exception using errcode = '23505', message = 'birth commitment conflict';
    end if;
    return record_platform_internal.record_birth_verify(e);
  end if;
  select * into prepared from public.record_birth_events where reservation_id=r.reservation_id and kind='prepared';
  if p_kind = 'created' and (prepared.sequence is null or prepared.initial_commitment is distinct from initial) then
    raise exception using errcode = '23505', message = 'birth confirm requires exact prepare';
  end if;
  if p_kind not in ('prepared','created') then
    raise exception using errcode = '22023', message = 'invalid birth transition';
  end if;
  return record_platform_internal.record_birth_append(b,r,p_kind,initial);
exception when no_data_found then
  raise exception using errcode = '22023', message = 'unknown birth reservation';
end $$;
create function public.record_birth_prepare(p_command bytea) returns bytea
language sql security definer set search_path = pg_catalog as $$
  select record_platform_internal.record_birth_transition(p_command,'prepared') $$;
create function public.record_birth_confirm(p_command bytea) returns bytea
language sql security definer set search_path = pg_catalog as $$
  select record_platform_internal.record_birth_transition(p_command,'created') $$;

create function public.record_birth_observe(p_command bytea)
returns table(authority_id text,lineage_id text,deployment_id text,project_id text,generation bigint,key_id text,
  cutover_digest bytea,reserved bytea,prepared bytea,created bytea,head_sequence bigint,head_hash bytea,
  system_identifier text,database_oid oid,database_name text,identity_epoch bigint)
language plpgsql security definer set search_path = pg_catalog as $$
declare p jsonb; b public.record_birth_bindings; r public.record_birth_requests; e public.record_birth_events;
  h public.record_birth_head; current_hash bytea; current_seq bigint; first_seq bigint;
begin
  p := record_platform_internal.record_birth_command(p_command,array['record_id']);
  b := record_platform_internal.record_birth_admit(p,false);
  select * into strict h from public.record_birth_head where singleton;
  if h.sequence > 0 then
    select * into strict e from public.record_birth_events where sequence=h.sequence;
    if e.receipt_hash is distinct from h.receipt_hash then
      raise exception using errcode = '55000', message = 'birth head hash mismatch';
    end if;
    perform record_platform_internal.record_birth_verify(e);
  end if;
  reserved := null; prepared := null; created := null;
  if p->>'record_id' <> '' then
    select * into r from public.record_birth_requests q where q.record_id=p->>'record_id'
      and (q.authority_id,q.lineage_id,q.project_id)=(b.authority_id,b.lineage_id,b.project_id);
    if found then
      select min(sequence) into first_seq from public.record_birth_events where reservation_id=r.reservation_id;
      if first_seq is null then
        raise exception using errcode = '55000', message = 'birth reservation has no event';
      end if;
      current_seq := first_seq-1;
      if current_seq = 0 then current_hash := decode(repeat('00',32),'hex');
      else select receipt_hash into strict current_hash from public.record_birth_events where sequence=current_seq; end if;
      for e in select * from public.record_birth_events where sequence >= first_seq order by sequence loop
        if e.sequence <> current_seq+1 or e.previous_hash is distinct from current_hash then
          raise exception using errcode = '55000', message = 'birth event chain mismatch';
        end if;
        perform record_platform_internal.record_birth_verify(e);
        if e.reservation_id=r.reservation_id then
          if e.kind='reserved' then reserved := e.receipt;
          elsif e.kind='prepared' then prepared := e.receipt;
          elsif e.kind='created' then created := e.receipt; end if;
        end if;
        current_seq := e.sequence; current_hash := e.receipt_hash;
      end loop;
      if current_seq <> h.sequence or current_hash is distinct from h.receipt_hash
        or reserved is null or (created is not null and prepared is null) then
        raise exception using errcode = '55000', message = 'birth event head or transitions mismatch';
      end if;
    end if;
  end if;
  return query select b.authority_id,b.lineage_id,b.deployment_id,b.project_id,b.generation,b.key_id,
    b.cutover_digest,reserved,prepared,created,h.sequence,h.receipt_hash,
    (select control.system_identifier::text from pg_catalog.pg_control_system() control),
    (select d.oid from pg_catalog.pg_database d where d.datname=pg_catalog.current_database()),
    pg_catalog.current_database()::text,
    (select d.identity_epoch from public.record_platform_domain_identity d where d.domain_id=b.authority_id);
end $$;

create function record_platform_internal.record_birth_binding_bytes(b public.record_birth_bindings)
returns bytea language sql immutable strict security definer set search_path=pg_catalog as $$
  select record_platform_internal.source_bytes(convert_to((b).authority_id,'UTF8')) ||
    record_platform_internal.source_bytes(convert_to((b).lineage_id,'UTF8')) ||
    record_platform_internal.source_bytes(convert_to((b).deployment_id,'UTF8')) ||
    record_platform_internal.source_bytes(convert_to((b).project_id,'UTF8')) || int8send((b).generation) ||
    record_platform_internal.source_bytes(convert_to((b).key_id,'UTF8')) || (b).cutover_digest
$$;

create function public.record_birth_continuity(p_command bytea)
returns table(proof bytea,system_identifier text,database_oid oid,database_name text,identity_epoch bigint)
language plpgsql security definer set search_path=pg_catalog as $$
declare p jsonb; b public.record_birth_bindings; h public.record_birth_checkpoint_anchor;
  canonical bytea; secret bytea; enabled boolean;
begin
  p:=record_platform_internal.record_birth_command(p_command,'{}');
  b:=record_platform_internal.record_birth_admit(p,false);
  if not exists(select 1 from public.record_birth_principals p0 where p0.principal=session_user
      and p0.generation=b.generation and p0.purpose='reader')
    or has_function_privilege(session_user,'public.record_birth_reserve(bytea)','EXECUTE')
    or has_function_privilege(session_user,'public.record_birth_prepare(bytea)','EXECUTE')
    or has_function_privilege(session_user,'public.record_birth_confirm(bytea)','EXECUTE') then
    raise exception using errcode='42501',message='birth continuity requires an independent read-only principal';
  end if;
  select * into strict h from public.record_birth_checkpoint_anchor where singleton;
  select key_bytes into strict secret from public.record_birth_keys where key_id=b.key_id;
  enabled:=exists(select 1 from public.record_birth_enabled e where e.singleton
    and (e.generation,e.key_id,e.cutover_digest)=(b.generation,b.key_id,b.cutover_digest));
  canonical:=record_platform_internal.source_bytes(convert_to('houfeng.record-birth.continuity.v1','UTF8')) ||
    record_platform_internal.record_birth_binding_bytes(b) || int8send(h.sequence) || h.checkpoint_hash ||
    case when enabled then decode('01','hex') else decode('00','hex') end;
  return query select canonical || record_platform_internal.hmac(canonical,secret,'sha256'),
    (select control.system_identifier::text from pg_catalog.pg_control_system() control),
    (select d.oid from pg_catalog.pg_database d where d.datname=pg_catalog.current_database()),
    pg_catalog.current_database()::text,
    (select d.identity_epoch from public.record_platform_domain_identity d where d.domain_id=b.authority_id);
end $$;

-- Request identity remains observable even if APP lost the assigned record ID.
create function public.record_birth_observe_request(p_command bytea)
returns table(checkpoint_bytes bytea,record_id text,reserved bytea,prepared bytea,created bytea,
  system_identifier text,database_oid oid,database_name text,identity_epoch bigint)
language plpgsql security definer set search_path=pg_catalog as $$
declare p jsonb; b public.record_birth_bindings; r public.record_birth_requests;
  coverage public.record_birth_request_coverage; e public.record_birth_events;
begin
  p:=record_platform_internal.record_birth_command(p_command,
    array['actor_id','actor_scope_digest','purpose','idempotency_key','request_fingerprint']);
  b:=record_platform_internal.record_birth_admit(p,false);
  select * into coverage from public.record_birth_request_coverage c where
    (c.lineage_id,c.project_id,c.actor_id,c.purpose,c.idempotency_key)=
      (b.lineage_id,b.project_id,p->>'actor_id',p->>'purpose',p->>'idempotency_key');
  select * into r from public.record_birth_requests q where
    (q.lineage_id,q.project_id,q.actor_id,q.purpose,q.idempotency_key)=
      (b.lineage_id,b.project_id,p->>'actor_id',p->>'purpose',p->>'idempotency_key');
  if (r.reservation_id is null) is distinct from (coverage.allocation_id is null) then
    raise exception using errcode='55000',message='birth request coverage incomplete';
  end if;
  if r.reservation_id is not null then
    if (r.actor_scope_digest,r.request_fingerprint) is distinct from
      (decode(p->>'actor_scope_digest','hex'),decode(p->>'request_fingerprint','hex')) then
      raise exception using errcode='23505',message='birth request binding conflict';
    end if;
    for e in select * from public.record_birth_events where reservation_id=r.reservation_id order by sequence loop
      if e.kind='reserved' then reserved:=record_platform_internal.record_birth_verify(e);
      elsif e.kind='prepared' then prepared:=record_platform_internal.record_birth_verify(e);
      elsif e.kind='created' then created:=record_platform_internal.record_birth_verify(e); end if;
    end loop;
    if reserved is null or (created is not null and prepared is null) then
      raise exception using errcode='55000',message='birth request events incomplete';
    end if;
  end if;
  return query select coverage.checkpoint_bytes,r.record_id,reserved,prepared,created,
    (select control.system_identifier::text from pg_catalog.pg_control_system() control),
    (select d.oid from pg_catalog.pg_database d where d.datname=pg_catalog.current_database()),
    pg_catalog.current_database()::text,
    (select d.identity_epoch from public.record_platform_domain_identity d where d.domain_id=b.authority_id);
end $$;
revoke all on function record_platform_internal.record_birth_binding_bytes(public.record_birth_bindings),
  public.record_birth_continuity(bytea),public.record_birth_observe_request(bytea) from public;

revoke all on public.record_birth_keys,public.record_birth_lineage,public.record_birth_bindings,public.record_birth_enabled,
  public.record_birth_principals,public.record_birth_requests,public.record_birth_head,public.record_birth_events from public;
revoke all on function record_platform_internal.record_birth_command(bytea,text[]),
  record_platform_internal.record_birth_admit(jsonb,boolean),
  record_platform_internal.record_birth_encode(public.record_birth_bindings,public.record_birth_requests,text,bigint,bytea,bytea),
  record_platform_internal.record_birth_verify(public.record_birth_events),
  record_platform_internal.record_birth_append(public.record_birth_bindings,public.record_birth_requests,text,bytea),
  record_platform_internal.record_birth_transition(bytea,text),
  public.record_birth_reserve(bytea),public.record_birth_prepare(bytea),
  public.record_birth_confirm(bytea),public.record_birth_observe(bytea) from public;
