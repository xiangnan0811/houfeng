package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The installed authority must remain immutable, and recovery must defer only
// its receipt's operation FK while rebuilding a missing terminal projection.
func verifyRecordDeletionAuthorityCatalog(ctx context.Context, tx pgx.Tx, owner string) error {
	var valid bool
	err := tx.QueryRow(ctx, `select exists (
		select 1 from pg_catalog.pg_trigger t
		join pg_catalog.pg_class c on c.oid=t.tgrelid
		join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		join pg_catalog.pg_proc f on f.oid=t.tgfoid
		join pg_catalog.pg_namespace fn on fn.oid=f.pronamespace
		where n.nspname='record_platform_internal' and c.relname='record_deletion_verified_entries'
		  and c.relkind='r' and pg_get_userbyid(c.relowner)=$1
		  and t.tgname='record_deletion_verified_entry_immutable' and t.tgtype=27
		  and not t.tgisinternal and t.tgenabled='O' and t.tgconstraint=0
		  and t.tgparentid=0 and not t.tgdeferrable and not t.tginitdeferred
		  and t.tgqual is null and t.tgnargs=0 and octet_length(t.tgargs)=0
		  and t.tgattr=''::int2vector and t.tgoldtable is null and t.tgnewtable is null
		  and fn.nspname='record_platform_internal' and f.proname='enforce_client_purge_receipt_immutable'
		  and f.pronargs=0 and f.prokind='f' and not f.prosecdef and pg_get_userbyid(f.proowner)=$1
	) and exists (
		select 1 from pg_catalog.pg_trigger t
		join pg_catalog.pg_class c on c.oid=t.tgrelid
		join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		join pg_catalog.pg_proc f on f.oid=t.tgfoid
		join pg_catalog.pg_namespace fn on fn.oid=f.pronamespace
		join pg_catalog.pg_constraint k on k.oid=t.tgconstraint
		where n.nspname='record_platform_internal' and c.relname='record_deletion_owner_commit_checks'
		  and c.relkind='r' and pg_get_userbyid(c.relowner)=$1
		  and t.tgname='record_deletion_owner_commit_check' and t.tgtype=5
		  and not t.tgisinternal and t.tgenabled='O' and t.tgparentid=0
		  and t.tgdeferrable and t.tginitdeferred and t.tgqual is null
		  and t.tgnargs=0 and octet_length(t.tgargs)=0 and t.tgattr=''::int2vector
		  and t.tgoldtable is null and t.tgnewtable is null
		  and k.contype='t' and k.conrelid=c.oid and k.condeferrable and k.condeferred
		  and fn.nspname='record_platform_internal' and f.proname='record_deletion_owner_commit_trigger'
		  and f.pronargs=0 and f.prokind='f' and f.prosecdef and pg_get_userbyid(f.proowner)=$1
	) and exists (
		select 1 from pg_catalog.pg_trigger t
		join pg_catalog.pg_class c on c.oid=t.tgrelid
		join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		join pg_catalog.pg_proc f on f.oid=t.tgfoid
		join pg_catalog.pg_namespace fn on fn.oid=f.pronamespace
		where n.nspname='public' and c.relname='attachment_purge_workspace_targets'
		  and c.relkind='r' and pg_get_userbyid(c.relowner)=$1
		  and t.tgname='attachment_purge_workspace_target_immutable' and t.tgtype=27
		  and not t.tgisinternal and t.tgenabled='O' and t.tgconstraint=0
		  and t.tgparentid=0 and not t.tgdeferrable and not t.tginitdeferred
		  and t.tgqual is null and t.tgnargs=0 and octet_length(t.tgargs)=0
		  and t.tgattr=''::int2vector and t.tgoldtable is null and t.tgnewtable is null
		  and fn.nspname='record_platform_internal' and f.proname='local_blob_immutable_identity'
		  and f.pronargs=0 and f.prokind='f' and not f.prosecdef and pg_get_userbyid(f.proowner)=$1
	) and exists (
		select 1 from pg_catalog.pg_proc f
		join pg_catalog.pg_namespace n on n.oid=f.pronamespace
		where n.nspname='record_platform_internal' and f.proname='record_deletion_completed_obligation_v2'
		  and f.pronargs=1 and f.proargtypes='25'::oidvector and f.prokind='f'
		  and f.prorettype='pg_catalog.bool'::regtype and not f.proretset and f.prosecdef
		  and pg_get_userbyid(f.proowner)=$1 and f.proconfig=array['search_path=pg_catalog']::text[]
		  and not exists(select 1 from aclexplode(coalesce(f.proacl,acldefault('f',f.proowner))) a
		    where a.privilege_type='EXECUTE' and a.grantee<>f.proowner)
	) and exists (
		select 1 from pg_catalog.pg_constraint fk
		where fk.conrelid='public.record_core_purge_receipts'::regclass
		  and fk.conname='record_core_purge_receipts_operation_id_fkey' and fk.contype='f'
		  and fk.confrelid='public.record_purge_operations'::regclass
		  and fk.condeferrable and not fk.condeferred and fk.convalidated
		  and fk.confupdtype='a' and fk.confdeltype='r' and fk.confmatchtype='s'
		  and fk.conkey=array[(select attnum from pg_attribute where attrelid=fk.conrelid and attname='operation_id')]::smallint[]
		  and fk.confkey=array[(select attnum from pg_attribute where attrelid=fk.confrelid and attname='operation_id')]::smallint[]
	) and exists (
		select 1 from pg_catalog.pg_proc f
		join pg_catalog.pg_namespace n on n.oid=f.pronamespace
		where n.nspname='record_platform_internal' and f.proname='validate_record_revision_primary_subject'
		  and f.pronargs=0 and f.prokind='f' and f.prorettype='pg_catalog.trigger'::regtype
		  and f.prosecdef and pg_get_userbyid(f.proowner)=$1
		  and f.proconfig=array['search_path=pg_catalog']::text[]
		  and not exists(select 1 from aclexplode(coalesce(f.proacl,acldefault('f',f.proowner))) a
		    where a.privilege_type='EXECUTE' and a.grantee<>f.proowner)
		  and (select count(*) from pg_trigger t where t.tgfoid=f.oid)=2
		  and not exists (
		    select 1 from (values
		      ('record_revisions','record_revisions_require_primary_subject',5),
		      ('record_revision_subjects','record_revision_subjects_require_primary_subject',13)
		    ) expected(relation_name,trigger_name,event_type)
		    where not exists(select 1 from pg_trigger t
		      join pg_class c on c.oid=t.tgrelid join pg_namespace cn on cn.oid=c.relnamespace
		      join pg_constraint k on k.oid=t.tgconstraint
		      where cn.nspname='public' and c.relname=expected.relation_name
		        and c.relkind='r' and pg_get_userbyid(c.relowner)=$1
		        and not c.relrowsecurity and not c.relforcerowsecurity
		        and t.tgname=expected.trigger_name and t.tgtype=expected.event_type and t.tgfoid=f.oid
		        and not t.tgisinternal and t.tgenabled='O' and t.tgparentid=0
		        and t.tgdeferrable and t.tginitdeferred and t.tgqual is null
		        and t.tgnargs=0 and octet_length(t.tgargs)=0 and t.tgattr=''::int2vector
		        and t.tgoldtable is null and t.tgnewtable is null
		        and k.contype='t' and k.conrelid=c.oid and k.condeferrable and k.condeferred)
		  )
	) and exists (
		select 1 from pg_catalog.pg_trigger t
		join pg_catalog.pg_class c on c.oid=t.tgrelid
		join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		join pg_catalog.pg_proc f on f.oid=t.tgfoid
		join pg_catalog.pg_namespace fn on fn.oid=f.pronamespace
		where n.nspname='record_platform_internal' and c.relname='compose_membership_transition'
		  and c.relkind='r' and pg_get_userbyid(c.relowner)=$1
		  and not c.relrowsecurity and not c.relforcerowsecurity
		  and t.tgname='compose_membership_transition_immutable' and t.tgtype=58
		  and not t.tgisinternal and t.tgenabled='O' and t.tgconstraint=0
		  and t.tgparentid=0 and not t.tgdeferrable and not t.tginitdeferred
		  and t.tgqual is null and t.tgnargs=0 and octet_length(t.tgargs)=0
		  and t.tgattr=''::int2vector and t.tgoldtable is null and t.tgnewtable is null
		  and fn.nspname='record_platform_internal' and f.proname='reject_immutable_mutation'
		  and f.pronargs=0 and f.prokind='f' and pg_get_userbyid(f.proowner)=$1
	) and exists (
		select 1 from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		where n.nspname='record_platform_internal' and c.relname='compose_membership_transition'
		  and (select count(*) from pg_attribute a where a.attrelid=c.oid and a.attnum>0 and not a.attisdropped)=6
		  and not exists(select 1 from (values
		    ('singleton','pg_catalog.bool'::regtype), ('activation_command','pg_catalog.bytea'::regtype),
		    ('rotation_command','pg_catalog.bytea'::regtype), ('rotation_receipt','pg_catalog.bytea'::regtype),
		    ('manifest_digest','pg_catalog.bytea'::regtype), ('migration_set_digest','pg_catalog.bytea'::regtype)
		  ) expected(name,typ) where not exists(select 1 from pg_attribute a where a.attrelid=c.oid
		    and a.attname=expected.name and a.atttypid=expected.typ and a.attnotnull and not a.attisdropped))
		  and exists(select 1 from pg_constraint k where k.conrelid=c.oid and k.contype='p'
		    and k.convalidated and not k.condeferrable and k.conkey=array[
		      (select a.attnum from pg_attribute a where a.attrelid=c.oid and a.attname='singleton')]::smallint[])
		  and (select count(*) from pg_constraint k where k.conrelid=c.oid and k.contype='c' and k.convalidated)=6
	) and exists (
		select 1 from pg_catalog.pg_proc f join pg_catalog.pg_namespace n on n.oid=f.pronamespace
		where n.nspname='public' and f.proname='record_platform_compose_membership_heartbeat'
		  and f.proargtypes='17'::oidvector and f.prorettype='pg_catalog.timestamptz'::regtype
		  and f.prokind='f' and not f.proretset and f.prosecdef and pg_get_userbyid(f.proowner)=$1
		  and f.proconfig=array['search_path=pg_catalog']::text[]
		  and strpos(f.prosrc, 'record_platform_projection_read_uint64_v1(command,143)<>1')>0
		  and strpos(f.prosrc, 'contract.active_domain_identity_epoch is distinct from 1')>0
	) and exists (
		select 1 from pg_catalog.pg_trigger t
		join pg_catalog.pg_class c on c.oid=t.tgrelid
		join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		join pg_catalog.pg_proc f on f.oid=t.tgfoid
		join pg_catalog.pg_namespace fn on fn.oid=f.pronamespace
		where n.nspname='public' and c.relname='deployment_membership' and c.relkind='r'
		  and pg_get_userbyid(c.relowner)=$1 and t.tgname='source_membership_effect_guard'
		  and t.tgtype=23 and not t.tgisinternal and t.tgenabled='O' and t.tgconstraint=0
		  and t.tgparentid=0 and not t.tgdeferrable and not t.tginitdeferred
		  and t.tgqual is null and t.tgnargs=0 and octet_length(t.tgargs)=0
		  and t.tgattr=''::int2vector and t.tgoldtable is null and t.tgnewtable is null
		  and fn.nspname='record_platform_internal' and f.proname='source_membership_effect_guard'
		  and f.pronargs=0 and f.prokind='f' and f.prorettype='pg_catalog.trigger'::regtype
		  and f.prosecdef and pg_get_userbyid(f.proowner)=$1
		  and f.proconfig=array['search_path=pg_catalog']::text[]
	) and exists (
		select 1 from pg_catalog.pg_proc f join pg_catalog.pg_namespace n on n.oid=f.pronamespace
		where n.nspname='public' and f.proname='record_processor_worker_admit'
		  and f.proargtypes='25 25 25 25'::oidvector and f.prorettype='pg_catalog.void'::regtype
		  and f.prokind='f' and not f.proretset and f.prosecdef and pg_get_userbyid(f.proowner)=$1
		  and f.proconfig=array['search_path=pg_catalog']::text[]
		  and not exists(select 1 from aclexplode(coalesce(f.proacl,acldefault('f',f.proowner))) a
		    where a.privilege_type='EXECUTE' and a.grantee=0)
	) and not exists (
		select 1 from (values
		  ('record_platform_compose_inventory_upgrade', '17 17'::oidvector, 'pg_catalog.bytea'::regtype, false),
		  ('record_platform_compose_membership_heartbeat_v2', '17'::oidvector, 'pg_catalog.timestamptz'::regtype, true)
		) expected(function_name, argument_types, result_type, authority_execute)
		where not exists (
		  select 1 from pg_catalog.pg_proc f
		  join pg_catalog.pg_namespace n on n.oid=f.pronamespace
		  where n.nspname='public' and f.proname=expected.function_name
		    and f.proargtypes=expected.argument_types and f.prorettype=expected.result_type
		    and f.prokind='f' and not f.proretset and f.prosecdef
		    and pg_get_userbyid(f.proowner)=$1 and f.proconfig=array['search_path=pg_catalog']::text[]
		    and not exists(select 1 from aclexplode(coalesce(f.proacl,acldefault('f',f.proowner))) a
		      where a.privilege_type='EXECUTE' and a.grantee<>f.proowner
		        and (not expected.authority_execute or a.grantee is distinct from
		          (select oid from pg_catalog.pg_roles where rolname='houfeng_records_authority')
		          or a.is_grantable))
		    and (not expected.authority_execute or not exists(select 1 from pg_catalog.pg_roles
		      where rolname='houfeng_records_authority') or exists (
		        select 1 from aclexplode(coalesce(f.proacl,acldefault('f',f.proowner))) a
		        join pg_catalog.pg_roles r on r.oid=a.grantee
		        where r.rolname='houfeng_records_authority' and a.privilege_type='EXECUTE'
		          and not a.is_grantable))
		)
	)`, owner).Scan(&valid)
	if err != nil {
		return fmt.Errorf("read record deletion authority catalog: %w", err)
	}
	if !valid {
		return fmt.Errorf("record deletion authority, deferred primary-subject validator or recovery receipt constraint differs")
	}
	return nil
}
