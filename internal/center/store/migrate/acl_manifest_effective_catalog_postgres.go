package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyPostgresAppACLEffectiveCatalogR1 takes one repeatable, read-only
// pg_catalog snapshot and proves it equals the compiler-derived r1 contract.
func VerifyPostgresAppACLEffectiveCatalogR1(
	ctx context.Context,
	db *pgxpool.Pool,
	input AppACLEffectiveCatalogVerifierInputR1,
) error {
	if db == nil {
		return fmt.Errorf("app ACL effective catalog verifier has no PostgreSQL pool")
	}
	if err := input.Validate(); err != nil {
		return fmt.Errorf("validate app ACL effective catalog verifier input: %w", err)
	}
	reader := postgresAppACLEffectiveCatalogReaderR1{db: db}
	snapshot, err := reader.read(ctx, input)
	if err != nil {
		return fmt.Errorf("read app ACL effective catalog snapshot: %w", err)
	}
	if err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input); err != nil {
		return fmt.Errorf("verify app ACL effective catalog snapshot: %w", err)
	}
	return nil
}

type postgresAppACLEffectiveCatalogReaderR1 struct {
	db *pgxpool.Pool
}

func (reader postgresAppACLEffectiveCatalogReaderR1) read(
	ctx context.Context,
	input AppACLEffectiveCatalogVerifierInputR1,
) (snapshot AppACLEffectiveCatalogSnapshotR1, err error) {
	tx, err := reader.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("begin repeatable read-only app ACL catalog snapshot: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := tx.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&snapshot.DatabaseName); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("read app ACL catalog database name: %w", err)
	}
	roleNames := []string{
		input.Contract.RoleBindings[0].CatalogRole,
		input.Contract.RoleBindings[1].CatalogRole,
		input.MigratorRole,
	}
	if snapshot.Roles, err = readAppACLEffectiveCatalogRolesR1(ctx, tx, snapshot.DatabaseName, roleNames); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Memberships, err = readAppACLEffectiveCatalogMembershipsR1(ctx, tx, roleNames); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Owners, err = readAppACLEffectiveCatalogOwnersR1(ctx, tx, snapshot.DatabaseName); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.DirectPrivileges, err = readAppACLEffectiveCatalogDirectPrivilegesR1(ctx, tx, snapshot.DatabaseName); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.EffectivePrivileges, err = readAppACLEffectiveCatalogEffectivePrivilegesR1(ctx, tx, snapshot.DatabaseName, roleNames[:2]); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.ColumnACLs, err = readAppACLEffectiveCatalogColumnACLsR1(ctx, tx); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.DefaultACLs, err = readAppACLEffectiveCatalogDefaultACLsR1(ctx, tx); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Functions, err = readAppACLEffectiveCatalogFunctionsR1(ctx, tx); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("commit read-only app ACL catalog snapshot: %w", err)
	}
	return snapshot, nil
}

func readAppACLEffectiveCatalogRolesR1(
	ctx context.Context,
	tx pgx.Tx,
	databaseName string,
	roleNames []string,
) ([]AppACLEffectiveCatalogRoleStateR1, error) {
	rows, err := tx.Query(ctx, `
		select role.rolname,
		       role.rolcanlogin,
		       role.rolinherit,
		       role.rolsuper,
		       role.rolcreatedb,
		       role.rolcreaterole,
		       role.rolreplication,
		       role.rolbypassrls,
		       pg_catalog.has_database_privilege(role.oid, database.oid, 'TEMPORARY'),
		       pg_catalog.has_schema_privilege(role.oid, namespace.oid, 'CREATE')
		from pg_catalog.pg_roles role
		cross join pg_catalog.pg_database database
		cross join pg_catalog.pg_namespace namespace
		where role.rolname = any($1::name[])
		  and database.datname = $2
		  and namespace.nspname = 'public'
		order by role.rolname
	`, roleNames, databaseName)
	if err != nil {
		return nil, fmt.Errorf("read app ACL role attributes: %w", err)
	}
	defer rows.Close()

	roles := make([]AppACLEffectiveCatalogRoleStateR1, 0, len(roleNames))
	for rows.Next() {
		var role AppACLEffectiveCatalogRoleStateR1
		if err := rows.Scan(
			&role.Name,
			&role.Login,
			&role.Inherit,
			&role.Superuser,
			&role.CreateDatabase,
			&role.CreateRole,
			&role.Replication,
			&role.BypassRLS,
			&role.TemporaryObjects,
			&role.SchemaCreate,
		); err != nil {
			return nil, fmt.Errorf("scan app ACL role attributes: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL role attributes: %w", err)
	}
	return roles, nil
}

func readAppACLEffectiveCatalogMembershipsR1(
	ctx context.Context,
	tx pgx.Tx,
	roleNames []string,
) ([]AppACLEffectiveCatalogMembershipR1, error) {
	rows, err := tx.Query(ctx, `
		with recursive membership_paths(member_oid, parent_oid, path) as (
			select membership.member,
		       membership.roleid,
		       array[membership.member, membership.roleid]::oid[]
			from pg_catalog.pg_auth_members membership
			union all
			select membership_paths.member_oid,
		       next_membership.roleid,
		       membership_paths.path || next_membership.roleid
			from membership_paths
			join pg_catalog.pg_auth_members next_membership
			  on next_membership.member = membership_paths.parent_oid
			where not next_membership.roleid = any(membership_paths.path)
		)
		select member_role.rolname, parent_role.rolname
		from membership_paths
		join pg_catalog.pg_roles member_role on member_role.oid = membership_paths.member_oid
		join pg_catalog.pg_roles parent_role on parent_role.oid = membership_paths.parent_oid
		where member_role.rolname = any($1::name[])
		   or parent_role.rolname = any($1::name[])
		order by member_role.rolname, parent_role.rolname
	`, roleNames)
	if err != nil {
		return nil, fmt.Errorf("read app ACL role memberships: %w", err)
	}
	defer rows.Close()

	memberships := make([]AppACLEffectiveCatalogMembershipR1, 0)
	for rows.Next() {
		var membership AppACLEffectiveCatalogMembershipR1
		if err := rows.Scan(&membership.MemberRole, &membership.ParentRole); err != nil {
			return nil, fmt.Errorf("scan app ACL role membership: %w", err)
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL role memberships: %w", err)
	}
	return memberships, nil
}

func readAppACLEffectiveCatalogOwnersR1(
	ctx context.Context,
	tx pgx.Tx,
	databaseName string,
) ([]AppACLEffectiveCatalogObjectOwnerR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname, namespace.nspowner
			from pg_catalog.pg_namespace namespace
			where namespace.nspname !~ '^pg_'
			  and namespace.nspname <> 'information_schema'
			  and namespace.oid <> pg_catalog.pg_my_temp_schema()
			  and not pg_catalog.pg_is_other_temp_schema(namespace.oid)
		)
		select object_class, schema_name, object_identity, owner_role
		from (
			select 'database'::text as object_class,
		       ''::text as schema_name,
		       database.datname as object_identity,
		       owner.rolname as owner_role
			from pg_catalog.pg_database database
			join pg_catalog.pg_roles owner on owner.oid = database.datdba
			where database.datname = $1
			union all
			select 'schema'::text,
			       namespace.nspname,
		       namespace.nspname,
		       owner.rolname
			from application_namespaces namespace
			join pg_catalog.pg_roles owner on owner.oid = namespace.nspowner
			union all
			select case
			         when relation.relkind = 'S' then 'sequence'
			         when relation.relkind in ('v', 'm') then 'view'
			         else 'table'
			       end,
		       namespace.nspname,
		       relation.relname,
		       owner.rolname
			from pg_catalog.pg_class relation
			join application_namespaces namespace on namespace.oid = relation.relnamespace
			join pg_catalog.pg_roles owner on owner.oid = relation.relowner
			where relation.relkind in ('r', 'p', 'f', 'v', 'm', 'S')
			union all
			select 'function'::text,
		       namespace.nspname,
		       procedure.proname || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
		       owner.rolname
			from pg_catalog.pg_proc procedure
			join application_namespaces namespace on namespace.oid = procedure.pronamespace
			join pg_catalog.pg_roles owner on owner.oid = procedure.proowner
		) owners
		order by object_class, schema_name, object_identity, owner_role
	`, databaseName)
	if err != nil {
		return nil, fmt.Errorf("read app ACL object owners: %w", err)
	}
	defer rows.Close()

	owners := make([]AppACLEffectiveCatalogObjectOwnerR1, 0)
	for rows.Next() {
		var owner AppACLEffectiveCatalogObjectOwnerR1
		var objectClass string
		if err := rows.Scan(&objectClass, &owner.SchemaName, &owner.ObjectIdentity, &owner.OwnerRole); err != nil {
			return nil, fmt.Errorf("scan app ACL object owner: %w", err)
		}
		owner.ObjectClass = AppACLObjectClass(objectClass)
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL object owners: %w", err)
	}
	return owners, nil
}

func readAppACLEffectiveCatalogDirectPrivilegesR1(
	ctx context.Context,
	tx pgx.Tx,
	databaseName string,
) ([]AppACLEffectiveCatalogPrivilegeObservationR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname, namespace.nspowner, namespace.nspacl
			from pg_catalog.pg_namespace namespace
			where namespace.nspname !~ '^pg_'
			  and namespace.nspname <> 'information_schema'
			  and namespace.oid <> pg_catalog.pg_my_temp_schema()
			  and not pg_catalog.pg_is_other_temp_schema(namespace.oid)
		), direct_grants as (
			select 'database'::text as object_class,
		       ''::text as schema_name,
		       database.datname::text as object_identity,
		       ''::text as column_name,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end as grantee_name,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
			from pg_catalog.pg_database database
			cross join lateral pg_catalog.aclexplode(database.datacl)
			  as acl_entry(grantor, grantee, privilege_type, is_grantable)
			left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
			where database.datname = $1
			  and database.datacl is not null
			  and acl_entry.grantee <> database.datdba
			union all
			select 'schema'::text,
		       ''::text,
		       namespace.nspname::text,
		       ''::text,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
			from application_namespaces namespace
			cross join lateral pg_catalog.aclexplode(namespace.nspacl)
			  as acl_entry(grantor, grantee, privilege_type, is_grantable)
			left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
			where namespace.nspacl is not null
			  and acl_entry.grantee <> namespace.nspowner
			union all
			select case
			         when relation.relkind = 'S' then 'sequence'
			         when relation.relkind in ('v', 'm') then 'view'
			         else 'table'
			       end,
		       namespace.nspname,
			       relation.relname::text,
		       ''::text,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
			from pg_catalog.pg_class relation
			join application_namespaces namespace on namespace.oid = relation.relnamespace
			cross join lateral pg_catalog.aclexplode(relation.relacl)
			  as acl_entry(grantor, grantee, privilege_type, is_grantable)
			left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
			where relation.relkind in ('r', 'p', 'f', 'v', 'm', 'S')
			  and relation.relacl is not null
			  and acl_entry.grantee <> relation.relowner
			union all
			select 'function'::text,
		       ''::text,
			       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
		       ''::text,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
			from pg_catalog.pg_proc procedure
			join application_namespaces namespace on namespace.oid = procedure.pronamespace
			cross join lateral pg_catalog.aclexplode(procedure.proacl)
			  as acl_entry(grantor, grantee, privilege_type, is_grantable)
			left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
			where procedure.proacl is not null
			  and acl_entry.grantee <> procedure.proowner
		)
		select object_class, schema_name, object_identity, column_name,
		       grantee_name, privilege_type, is_grantable
		from direct_grants
		order by object_class, schema_name, object_identity, column_name,
		         grantee_name, privilege_type, is_grantable
	`, databaseName)
	if err != nil {
		return nil, fmt.Errorf("read direct app ACL catalog privileges: %w", err)
	}
	defer rows.Close()

	privileges := make([]AppACLEffectiveCatalogPrivilegeObservationR1, 0)
	for rows.Next() {
		var privilege AppACLEffectiveCatalogPrivilegeObservationR1
		var objectClass string
		if err := rows.Scan(
			&objectClass,
			&privilege.SchemaName,
			&privilege.ObjectIdentity,
			&privilege.ColumnName,
			&privilege.Grantee,
			&privilege.Privilege,
			&privilege.GrantOption,
		); err != nil {
			return nil, fmt.Errorf("scan direct app ACL catalog privilege: %w", err)
		}
		privilege.ObjectClass = AppACLObjectClass(objectClass)
		privileges = append(privileges, privilege)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate direct app ACL catalog privileges: %w", err)
	}
	return privileges, nil
}

func readAppACLEffectiveCatalogEffectivePrivilegesR1(
	ctx context.Context,
	tx pgx.Tx,
	databaseName string,
	roleNames []string,
) ([]AppACLEffectiveCatalogPrivilegeObservationR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname
			from pg_catalog.pg_namespace namespace
			where namespace.nspname !~ '^pg_'
			  and namespace.nspname <> 'information_schema'
			  and namespace.oid <> pg_catalog.pg_my_temp_schema()
			  and not pg_catalog.pg_is_other_temp_schema(namespace.oid)
		), target_roles as (
			select role.oid, role.rolname
			from pg_catalog.pg_roles role
			where role.rolname = any($1::name[])
		), effective_privileges as (
			select role.rolname as grantee_name,
		       'database'::text as object_class,
		       ''::text as schema_name,
		       database.datname::text as object_identity,
		       ''::text as column_name,
		       privilege.privilege_type
			from target_roles role
			cross join pg_catalog.pg_database database
			cross join (values ('CONNECT'::text), ('CREATE'::text), ('TEMPORARY'::text)) as privilege(privilege_type)
			where database.datname = $2
			  and pg_catalog.has_database_privilege(role.oid, database.oid, privilege.privilege_type)
			union all
			select role.rolname,
		       'schema'::text,
		       ''::text,
		       namespace.nspname::text,
		       ''::text,
		       privilege.privilege_type
			from target_roles role
			cross join application_namespaces namespace
			cross join (values ('USAGE'::text), ('CREATE'::text)) as privilege(privilege_type)
			where pg_catalog.has_schema_privilege(role.oid, namespace.oid, privilege.privilege_type)
			union all
			select role.rolname,
		       case when relation.relkind in ('v', 'm') then 'view' else 'table' end,
		       namespace.nspname,
			       relation.relname::text,
		       ''::text,
		       privilege.privilege_type
			from target_roles role
			join pg_catalog.pg_class relation on relation.relkind in ('r', 'p', 'f', 'v', 'm')
			join application_namespaces namespace on namespace.oid = relation.relnamespace
			cross join (values
				('SELECT'::text), ('INSERT'::text), ('UPDATE'::text), ('DELETE'::text),
				('TRUNCATE'::text), ('REFERENCES'::text), ('TRIGGER'::text)
			) as privilege(privilege_type)
			where pg_catalog.has_table_privilege(role.oid, relation.oid, privilege.privilege_type)
			union all
			select role.rolname,
		       'sequence'::text,
		       namespace.nspname,
		       relation.relname::text,
		       ''::text,
		       privilege.privilege_type
			from target_roles role
			join pg_catalog.pg_class relation on relation.relkind = 'S'
			join application_namespaces namespace on namespace.oid = relation.relnamespace
			cross join (values ('USAGE'::text), ('SELECT'::text), ('UPDATE'::text)) as privilege(privilege_type)
			where pg_catalog.has_sequence_privilege(role.oid, relation.oid, privilege.privilege_type)
			union all
			select role.rolname,
		       'function'::text,
		       ''::text,
			       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
		       ''::text,
		       'EXECUTE'::text
			from target_roles role
			join pg_catalog.pg_proc procedure on true
			join application_namespaces namespace on namespace.oid = procedure.pronamespace
			where pg_catalog.has_function_privilege(role.oid, procedure.oid, 'EXECUTE')
		)
		select grantee_name, object_class, schema_name, object_identity, column_name, privilege_type
		from effective_privileges
		order by grantee_name, object_class, schema_name, object_identity, column_name, privilege_type
	`, roleNames, databaseName)
	if err != nil {
		return nil, fmt.Errorf("read effective app ACL catalog privileges: %w", err)
	}
	defer rows.Close()

	privileges := make([]AppACLEffectiveCatalogPrivilegeObservationR1, 0)
	for rows.Next() {
		var privilege AppACLEffectiveCatalogPrivilegeObservationR1
		var objectClass string
		if err := rows.Scan(
			&privilege.Grantee,
			&objectClass,
			&privilege.SchemaName,
			&privilege.ObjectIdentity,
			&privilege.ColumnName,
			&privilege.Privilege,
		); err != nil {
			return nil, fmt.Errorf("scan effective app ACL catalog privilege: %w", err)
		}
		privilege.ObjectClass = AppACLObjectClass(objectClass)
		privileges = append(privileges, privilege)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective app ACL catalog privileges: %w", err)
	}
	return privileges, nil
}

func readAppACLEffectiveCatalogColumnACLsR1(
	ctx context.Context,
	tx pgx.Tx,
) ([]AppACLEffectiveCatalogColumnACLR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname
			from pg_catalog.pg_namespace namespace
			where namespace.nspname !~ '^pg_'
			  and namespace.nspname <> 'information_schema'
			  and namespace.oid <> pg_catalog.pg_my_temp_schema()
			  and not pg_catalog.pg_is_other_temp_schema(namespace.oid)
		)
		select namespace.nspname,
		       relation.relname,
		       attribute.attname,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
			from pg_catalog.pg_attribute attribute
			join pg_catalog.pg_class relation on relation.oid = attribute.attrelid
			join application_namespaces namespace on namespace.oid = relation.relnamespace
		cross join lateral pg_catalog.aclexplode(attribute.attacl)
		  as acl_entry(grantor, grantee, privilege_type, is_grantable)
		left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
			where relation.relkind in ('r', 'p', 'f', 'v', 'm', 'S')
		  and attribute.attnum > 0
		  and not attribute.attisdropped
		order by namespace.nspname, relation.relname, attribute.attname,
		         grantee.rolname nulls first, acl_entry.privilege_type, acl_entry.is_grantable
	`)
	if err != nil {
		return nil, fmt.Errorf("read app ACL column grants: %w", err)
	}
	defer rows.Close()

	columnACLs := make([]AppACLEffectiveCatalogColumnACLR1, 0)
	for rows.Next() {
		var columnACL AppACLEffectiveCatalogColumnACLR1
		if err := rows.Scan(
			&columnACL.SchemaName,
			&columnACL.RelationName,
			&columnACL.ColumnName,
			&columnACL.Grantee,
			&columnACL.Privilege,
			&columnACL.GrantOption,
		); err != nil {
			return nil, fmt.Errorf("scan app ACL column grant: %w", err)
		}
		columnACLs = append(columnACLs, columnACL)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL column grants: %w", err)
	}
	return columnACLs, nil
}

func readAppACLEffectiveCatalogDefaultACLsR1(
	ctx context.Context,
	tx pgx.Tx,
) ([]AppACLEffectiveCatalogDefaultACLR1, error) {
	rows, err := tx.Query(ctx, `
		select owner.rolname,
		       coalesce(namespace.nspname, ''),
		       default_acl.defaclobjtype::text,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
		from pg_catalog.pg_default_acl default_acl
		join pg_catalog.pg_roles owner on owner.oid = default_acl.defaclrole
		left join pg_catalog.pg_namespace namespace on namespace.oid = default_acl.defaclnamespace
		cross join lateral pg_catalog.aclexplode(default_acl.defaclacl)
		  as acl_entry(grantor, grantee, privilege_type, is_grantable)
		left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
		order by owner.rolname, namespace.nspname nulls first, default_acl.defaclobjtype,
		         grantee.rolname nulls first, acl_entry.privilege_type, acl_entry.is_grantable
	`)
	if err != nil {
		return nil, fmt.Errorf("read app ACL default grants: %w", err)
	}
	defer rows.Close()

	defaultACLs := make([]AppACLEffectiveCatalogDefaultACLR1, 0)
	for rows.Next() {
		var defaultACL AppACLEffectiveCatalogDefaultACLR1
		if err := rows.Scan(
			&defaultACL.OwnerRole,
			&defaultACL.SchemaName,
			&defaultACL.ObjectType,
			&defaultACL.Grantee,
			&defaultACL.Privilege,
			&defaultACL.GrantOption,
		); err != nil {
			return nil, fmt.Errorf("scan app ACL default grant: %w", err)
		}
		defaultACLs = append(defaultACLs, defaultACL)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL default grants: %w", err)
	}
	return defaultACLs, nil
}

func readAppACLEffectiveCatalogFunctionsR1(
	ctx context.Context,
	tx pgx.Tx,
) ([]AppACLEffectiveCatalogFunctionR1, error) {
	rows, err := tx.Query(ctx, `
		select namespace.nspname,
		       procedure.proname,
		       pg_catalog.pg_get_function_identity_arguments(procedure.oid),
		       owner.rolname,
		       procedure.prokind::text,
		       procedure.prosecdef,
		       coalesce(procedure.proconfig, array[]::text[])
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		join pg_catalog.pg_roles owner on owner.oid = procedure.proowner
		where namespace.nspname = 'public'
		order by namespace.nspname, procedure.proname,
		         pg_catalog.pg_get_function_identity_arguments(procedure.oid)
	`)
	if err != nil {
		return nil, fmt.Errorf("read app ACL function catalog: %w", err)
	}
	defer rows.Close()

	functions := make([]AppACLEffectiveCatalogFunctionR1, 0)
	for rows.Next() {
		var function AppACLEffectiveCatalogFunctionR1
		if err := rows.Scan(
			&function.SchemaName,
			&function.Name,
			&function.IdentityArguments,
			&function.OwnerRole,
			&function.Kind,
			&function.SecurityDefiner,
			&function.Config,
		); err != nil {
			return nil, fmt.Errorf("scan app ACL function catalog: %w", err)
		}
		function.Identity = function.SchemaName + "." + function.Name + "(" + function.IdentityArguments + ")"
		functions = append(functions, function)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL function catalog: %w", err)
	}
	return functions, nil
}
