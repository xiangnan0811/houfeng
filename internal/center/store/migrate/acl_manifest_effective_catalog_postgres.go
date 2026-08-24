package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyPostgresAppACLEffectiveCatalogR1 takes one repeatable, read-only
// pg_catalog snapshot and proves it equals the compiled r1 privilege contract
// plus the fixed public projector inventory.
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
	snapshot, err = readAppACLEffectiveCatalogSnapshotInTxR1(ctx, tx, input)
	if err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("commit read-only app ACL catalog snapshot: %w", err)
	}
	return snapshot, nil
}

// readAppACLEffectiveCatalogSnapshotInTxR1 reads the complete managed APP
// catalog through a caller-owned snapshot. Runtime admission composes this
// with manifest and ledger reads inside one REPEATABLE READ READ ONLY tx.
func readAppACLEffectiveCatalogSnapshotInTxR1(
	ctx context.Context,
	tx pgx.Tx,
	input AppACLEffectiveCatalogVerifierInputR1,
) (snapshot AppACLEffectiveCatalogSnapshotR1, err error) {
	if err := input.Validate(); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("validate app ACL effective catalog verifier input: %w", err)
	}
	generic, err := appACLEffectiveCatalogVerifierInputFromR1(input)
	if err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("adapt app ACL effective catalog verifier input: %w", err)
	}
	return readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, generic)
}

func readAppACLEffectiveCatalogSnapshotInTx(
	ctx context.Context,
	tx pgx.Tx,
	input appACLEffectiveCatalogVerifierInput,
) (snapshot AppACLEffectiveCatalogSnapshotR1, err error) {
	if err := input.Validate(); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("validate app ACL effective catalog verifier input: %w", err)
	}

	if err := tx.QueryRow(ctx, `select pg_catalog.current_database(), session_user, current_user`).Scan(&snapshot.DatabaseName, &snapshot.SessionUser, &snapshot.CurrentUser); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("read app ACL catalog database name: %w", err)
	}
	if snapshot.DatabaseName != input.Contract.DatabaseName {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("app ACL catalog snapshot database %q does not match expected database %q", snapshot.DatabaseName, input.Contract.DatabaseName)
	}
	scope, err := newAppACLManagedSurfaceScope(input.Contract)
	if err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.PGCryptoExtension, err = readAppACLEffectiveCatalogPGCryptoExtensionR1(ctx, tx); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := verifyAppACLEffectiveCatalogPGCryptoExtensionR1(snapshot.PGCryptoExtension); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, fmt.Errorf("verify app ACL pgcrypto extension placement: %w", err)
	}
	roleNames := []string{
		input.Contract.RoleBindings[0].CatalogRole,
		input.Contract.RoleBindings[1].CatalogRole,
		input.MigratorRole,
	}
	auxiliaryRoleNames := make(map[string]struct{})
	for _, privilege := range input.Contract.AuxiliaryPrivileges {
		if _, seen := auxiliaryRoleNames[privilege.CatalogRole]; seen {
			continue
		}
		auxiliaryRoleNames[privilege.CatalogRole] = struct{}{}
		roleNames = append(roleNames, privilege.CatalogRole)
	}
	if snapshot.Roles, err = readAppACLEffectiveCatalogRolesR1(ctx, tx, snapshot.DatabaseName, roleNames); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Memberships, err = readAppACLEffectiveCatalogMembershipsR1(ctx, tx, roleNames); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Owners, err = readAppACLEffectiveCatalogOwnersR1(ctx, tx, snapshot.DatabaseName, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.DirectPrivileges, err = readAppACLEffectiveCatalogDirectPrivilegesR1(ctx, tx, snapshot.DatabaseName, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	effectiveRoleNames := append([]string(nil), roleNames[:2]...)
	for _, role := range snapshot.Roles {
		if _, auxiliary := auxiliaryRoleNames[role.Name]; auxiliary {
			effectiveRoleNames = append(effectiveRoleNames, role.Name)
		}
	}
	if snapshot.EffectivePrivileges, err = readAppACLEffectiveCatalogEffectivePrivilegesR1(ctx, tx, snapshot.DatabaseName, effectiveRoleNames, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.ColumnACLs, err = readAppACLEffectiveCatalogColumnACLsR1(ctx, tx, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.DefaultACLs, err = readAppACLEffectiveCatalogDefaultACLsForScope(ctx, tx, input.MigratorRole, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if snapshot.Functions, err = readAppACLEffectiveCatalogFunctionsR1(ctx, tx, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := verifyAppACLPublicProjectorStructureR1(ctx, tx, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	if err := verifyAppACLOpaqueExtensionMemberReachabilityForScope(ctx, tx, effectiveRoleNames, scope); err != nil {
		return AppACLEffectiveCatalogSnapshotR1{}, err
	}
	snapshot = scopeAppACLEffectiveCatalogSnapshot(snapshot, scope)
	return snapshot, nil
}

func readAppACLEffectiveCatalogPGCryptoExtensionR1(
	ctx context.Context,
	tx pgx.Tx,
) (AppACLEffectiveCatalogExtensionR1, error) {
	var extension AppACLEffectiveCatalogExtensionR1
	err := tx.QueryRow(ctx, `
		select installed_extension.extname::text,
		       namespace.nspname::text
		from pg_catalog.pg_extension installed_extension
		join pg_catalog.pg_namespace namespace on namespace.oid = installed_extension.extnamespace
		where installed_extension.extname = 'pgcrypto'
	`).Scan(&extension.ExtensionName, &extension.SchemaName)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppACLEffectiveCatalogExtensionR1{}, nil
	}
	if err != nil {
		return AppACLEffectiveCatalogExtensionR1{}, fmt.Errorf("read app ACL pgcrypto extension placement: %w", err)
	}
	return extension, nil
}

func verifyAppACLPublicProjectorStructureR1(
	ctx context.Context,
	tx pgx.Tx,
	scope appACLManagedSurfaceScopeR1,
) error {
	projectorNames := scope.publicProjectorFunctionNames()
	rows, err := tx.Query(ctx, `
		select procedure.proname::text,
		       exists (
			       select 1
			       from pg_catalog.pg_depend dependency
			       join pg_catalog.pg_extension installed_extension on installed_extension.oid = dependency.refobjid
			       where dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
			         and dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
			         and dependency.objid = procedure.oid
			         and dependency.deptype = 'e'
		       )
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		where namespace.nspname = 'public'
		  and procedure.proname = any($1::name[])
		order by procedure.proname, pg_catalog.pg_get_function_identity_arguments(procedure.oid)
	`, projectorNames)
	if err != nil {
		return fmt.Errorf("read app ACL public projector structure: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int, len(projectorNames))
	extensionMembers := make(map[string]bool, len(projectorNames))
	for rows.Next() {
		var name string
		var extensionMember bool
		if err := rows.Scan(&name, &extensionMember); err != nil {
			return fmt.Errorf("scan app ACL public projector structure: %w", err)
		}
		counts[name]++
		extensionMembers[name] = extensionMembers[name] || extensionMember
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate app ACL public projector structure: %w", err)
	}
	for _, name := range projectorNames {
		if counts[name] != 1 {
			return fmt.Errorf("public projector %q has %d overloads, want exactly one non-extension member procedure", "public."+name, counts[name])
		}
		if extensionMembers[name] {
			return fmt.Errorf("public projector %q is an extension member", "public."+name)
		}
	}
	return nil
}

func verifyAppACLOpaqueExtensionMemberReachabilityR1(
	ctx context.Context,
	tx pgx.Tx,
	roleNames []string,
) error {
	scope, err := newAppACLManagedSurfaceScopeR1("app_acl_extension_scope")
	if err != nil {
		return err
	}
	return verifyAppACLOpaqueExtensionMemberReachabilityForScope(ctx, tx, roleNames, scope)
}

func verifyAppACLOpaqueExtensionMemberReachabilityForScope(
	ctx context.Context,
	tx pgx.Tx,
	roleNames []string,
	scope appACLManagedSurfaceScope,
) error {
	rows, err := tx.Query(ctx, `
		with target_roles as (
			select role.oid, role.rolname
			from pg_catalog.pg_roles role
			where role.rolname = any($1::name[])
		)
		select role.rolname,
		       namespace.nspname::text,
		       procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')'
		from target_roles role
		join pg_catalog.pg_namespace namespace
		  on namespace.nspname = any($2::name[])
		join pg_catalog.pg_proc procedure on procedure.pronamespace = namespace.oid
		where pg_catalog.has_schema_privilege(role.oid, namespace.oid, 'USAGE')
		  and pg_catalog.has_function_privilege(role.oid, procedure.oid, 'EXECUTE')
		  and exists (
			select 1
			from pg_catalog.pg_depend dependency
			join pg_catalog.pg_extension installed_extension on installed_extension.oid = dependency.refobjid
			where dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
			  and dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
			  and dependency.objid = procedure.oid
			  and dependency.deptype = 'e'
		  )
		order by role.rolname, namespace.nspname, procedure.proname,
		         pg_catalog.pg_get_function_identity_arguments(procedure.oid)
	`, roleNames, scope.managedSchemaNames())
	if err != nil {
		return fmt.Errorf("read app ACL opaque extension-member reachability: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var roleName, schemaName, identity string
		if err := rows.Scan(&roleName, &schemaName, &identity); err != nil {
			return fmt.Errorf("scan app ACL opaque extension-member reachability: %w", err)
		}
		return fmt.Errorf("opaque extension member %s.%s is reachable by app role %q through schema %q USAGE and function EXECUTE", schemaName, identity, roleName, schemaName)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate app ACL opaque extension-member reachability: %w", err)
	}
	return nil
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
	scope appACLManagedSurfaceScopeR1,
) ([]AppACLEffectiveCatalogObjectOwnerR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname, namespace.nspowner
			from pg_catalog.pg_namespace namespace
			where namespace.nspname = any($2::name[])
		)
		select object_class, schema_name, object_identity, owner_role
		from (
		select 'database'::text as object_class,
		       ''::text as schema_name,
		       database.datname::text as object_identity,
		       owner.rolname as owner_role
			from pg_catalog.pg_database database
			join pg_catalog.pg_roles owner on owner.oid = database.datdba
			where database.datname = $1
		union all
		select 'schema'::text,
		       namespace.nspname::text,
		       namespace.nspname::text,
		       owner.rolname
			from application_namespaces namespace
			join pg_catalog.pg_roles owner on owner.oid = namespace.nspowner
			union all
			select case
			         when relation.relkind = 'S' then 'sequence'
			         when relation.relkind in ('v', 'm') then 'view'
			         else 'table'
		       end,
		       namespace.nspname::text,
		       relation.relname::text,
		       owner.rolname
			from pg_catalog.pg_class relation
			join application_namespaces namespace on namespace.oid = relation.relnamespace
			join pg_catalog.pg_roles owner on owner.oid = relation.relowner
			where relation.relkind in ('r', 'p', 'f', 'v', 'm', 'S')
		union all
		select 'function'::text,
		       namespace.nspname,
		       procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
		       owner.rolname
			from pg_catalog.pg_proc procedure
			join application_namespaces namespace on namespace.oid = procedure.pronamespace
			join pg_catalog.pg_roles owner on owner.oid = procedure.proowner
			where not exists (
				select 1
				from pg_catalog.pg_depend dependency
				join pg_catalog.pg_extension installed_extension on installed_extension.oid = dependency.refobjid
				where dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
				  and dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
				  and dependency.objid = procedure.oid
				  and dependency.deptype = 'e'
			)
		) owners
		order by object_class, schema_name, object_identity, owner_role
	`, databaseName, scope.managedSchemaNames())
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
		if !scope.containsOwner(owner) {
			continue
		}
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
	scope appACLManagedSurfaceScopeR1,
) ([]AppACLEffectiveCatalogPrivilegeObservationR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname, namespace.nspowner, namespace.nspacl
			from pg_catalog.pg_namespace namespace
			where namespace.nspname = any($2::name[])
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
			  and not exists (
					select 1
					from pg_catalog.pg_depend dependency
					join pg_catalog.pg_extension installed_extension on installed_extension.oid = dependency.refobjid
					where dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
					  and dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
					  and dependency.objid = procedure.oid
					  and dependency.deptype = 'e'
				)
		)
		select object_class, schema_name, object_identity, column_name,
		       grantee_name, privilege_type, is_grantable
		from direct_grants
		order by object_class, schema_name, object_identity, column_name,
		         grantee_name, privilege_type, is_grantable
	`, databaseName, scope.managedSchemaNames())
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
		if !scope.containsPrivilege(privilege) {
			continue
		}
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
	scope appACLManagedSurfaceScopeR1,
) ([]AppACLEffectiveCatalogPrivilegeObservationR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname
			from pg_catalog.pg_namespace namespace
			where namespace.nspname = any($3::name[])
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
			  and not exists (
					select 1
					from pg_catalog.pg_depend dependency
					join pg_catalog.pg_extension installed_extension on installed_extension.oid = dependency.refobjid
					where dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
					  and dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
					  and dependency.objid = procedure.oid
					  and dependency.deptype = 'e'
				)
		)
		select grantee_name, object_class, schema_name, object_identity, column_name, privilege_type
		from effective_privileges
		order by grantee_name, object_class, schema_name, object_identity, column_name, privilege_type
	`, roleNames, databaseName, scope.managedSchemaNames())
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
		if !scope.containsPrivilege(privilege) {
			continue
		}
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
	scope appACLManagedSurfaceScopeR1,
) ([]AppACLEffectiveCatalogColumnACLR1, error) {
	rows, err := tx.Query(ctx, `
		with application_namespaces as (
			select namespace.oid, namespace.nspname
			from pg_catalog.pg_namespace namespace
			where namespace.nspname = any($1::name[])
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
	`, scope.managedSchemaNames())
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
		if !scope.containsColumnACL(columnACL) {
			continue
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
	migratorRole string,
) ([]AppACLEffectiveCatalogDefaultACLR1, error) {
	scope, err := newAppACLManagedSurfaceScopeR1("app_acl_default_scope")
	if err != nil {
		return nil, err
	}
	return readAppACLEffectiveCatalogDefaultACLsForScope(ctx, tx, migratorRole, scope)
}

func readAppACLEffectiveCatalogDefaultACLsForScope(
	ctx context.Context,
	tx pgx.Tx,
	migratorRole string,
	scope appACLManagedSurfaceScope,
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
		where default_acl.defaclrole = (select role.oid from pg_catalog.pg_roles role where role.rolname = $1)
		  and (
			default_acl.defaclnamespace = 0
			or namespace.nspname = any($2::name[])
		  )
		order by owner.rolname, namespace.nspname nulls first, default_acl.defaclobjtype,
		         grantee.rolname nulls first, acl_entry.privilege_type, acl_entry.is_grantable
	`, migratorRole, scope.managedSchemaNames())
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
	scope appACLManagedSurfaceScopeR1,
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
		where namespace.nspname = any($1::name[])
		  and not exists (
			select 1
			from pg_catalog.pg_depend dependency
			join pg_catalog.pg_extension installed_extension on installed_extension.oid = dependency.refobjid
			where dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
			  and dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
			  and dependency.objid = procedure.oid
			  and dependency.deptype = 'e'
		  )
		order by namespace.nspname, procedure.proname,
		         pg_catalog.pg_get_function_identity_arguments(procedure.oid)
	`, scope.managedSchemaNames())
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
		if !scope.containsFunction(function) {
			continue
		}
		functions = append(functions, function)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app ACL function catalog: %w", err)
	}
	return functions, nil
}
