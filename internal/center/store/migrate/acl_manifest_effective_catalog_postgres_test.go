package migrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsPublicDatabaseConnect(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	if _, err := fixture.db.Exec(ctx, `grant connect on database `+quotePostgresIdentifier(fixture.databaseName)+` to public`); err != nil {
		t.Fatalf("grant PUBLIC database CONNECT drift: %v", err)
	}
	fixture.requireRejects(t, ctx, "PUBLIC")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsRecursiveMembership(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	parentRole := "houfeng_catalog_parent_" + fixture.roles.suffix
	if _, err := fixture.db.Exec(ctx, `create role `+quotePostgresIdentifier(parentRole)+` noinherit`); err != nil {
		t.Fatalf("create temporary parent role %q: %v", parentRole, err)
	}
	fixture.dropRole(t, parentRole)
	if _, err := fixture.db.Exec(ctx, `grant `+quotePostgresIdentifier(parentRole)+` to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant recursive-membership drift: %v", err)
	}
	fixture.requireRejects(t, ctx, "membership")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1AcceptsMigratorOwnedDatabaseWithBootstrapPublicSchema(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)

	var databaseOwner, publicSchemaOwner string
	if err := fixture.db.QueryRow(ctx, `
		select pg_catalog.pg_get_userbyid(database.datdba)
		from pg_catalog.pg_database database
		where database.datname = $1
	`, fixture.databaseName).Scan(&databaseOwner); err != nil {
		t.Fatalf("read database owner: %v", err)
	}
	if err := fixture.db.QueryRow(ctx, `
		select pg_catalog.pg_get_userbyid(namespace.nspowner)
		from pg_catalog.pg_namespace namespace
		where namespace.nspname = 'public'
	`).Scan(&publicSchemaOwner); err != nil {
		t.Fatalf("read public schema owner: %v", err)
	}
	if databaseOwner != fixture.roles.migrator {
		t.Fatalf("database owner = %q, want migrator %q", databaseOwner, fixture.roles.migrator)
	}
	if publicSchemaOwner != appACLEffectiveCatalogPublicSchemaDatabaseOwnerRoleR1 {
		t.Fatalf("public schema owner = %q, want bootstrap %q", publicSchemaOwner, appACLEffectiveCatalogPublicSchemaDatabaseOwnerRoleR1)
	}
	snapshot, err := (postgresAppACLEffectiveCatalogReaderR1{db: fixture.db}).read(ctx, fixture.input)
	if err != nil {
		t.Fatalf("read real migrated app ACL catalog snapshot: %v", err)
	}
	wantReadBytesIdentity := "record_platform_projection_read_bytes_v1(p_command bytea, p_offset integer, p_length integer)"
	foundReadBytesIdentity := false
	for _, owner := range snapshot.Owners {
		if owner.ObjectClass == AppACLObjectClassFunction && strings.HasPrefix(owner.ObjectIdentity, "record_platform_projection_read_bytes_v1(") {
			if owner.ObjectIdentity != wantReadBytesIdentity {
				t.Fatalf("real function owner identity = %q, want %q", owner.ObjectIdentity, wantReadBytesIdentity)
			}
			foundReadBytesIdentity = true
		}
	}
	if !foundReadBytesIdentity {
		t.Fatalf("real migrated catalog is missing function %q", wantReadBytesIdentity)
	}
	fixture.requireAccepts(t, ctx)
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsDatabaseOwnerDriftWithBootstrapPublicSchema(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	driftOwner := "houfeng_catalog_database_owner_drift_" + fixture.roles.suffix
	if _, err := fixture.db.Exec(ctx, `create role `+quotePostgresIdentifier(driftOwner)+` nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`); err != nil {
		t.Fatalf("create database owner drift role %q: %v", driftOwner, err)
	}
	fixture.dropRole(t, driftOwner)
	if _, err := fixture.db.Exec(ctx, `alter database `+quotePostgresIdentifier(fixture.databaseName)+` owner to `+quotePostgresIdentifier(driftOwner)); err != nil {
		t.Fatalf("assign database owner drift: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fixture.db.Exec(cleanupCtx, `alter database `+quotePostgresIdentifier(fixture.databaseName)+` owner to `+quotePostgresIdentifier(fixture.roles.migrator)); err != nil {
			t.Errorf("restore temporary database owner %q: %v", fixture.roles.migrator, err)
		}
	})

	fixture.requireRejects(t, ctx, "database owner")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsRuntimeSchemaOwner(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	var originalOwner string
	if err := fixture.db.QueryRow(ctx, `
		select pg_catalog.pg_get_userbyid(namespace.nspowner)
		from pg_catalog.pg_namespace namespace
		where namespace.nspname = 'public'
	`).Scan(&originalOwner); err != nil {
		t.Fatalf("read public schema owner: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `alter schema public owner to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("assign public schema ownership drift: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fixture.db.Exec(cleanupCtx, `alter schema public owner to `+quotePostgresIdentifier(originalOwner)); err != nil {
			t.Errorf("restore public schema owner %q: %v", originalOwner, err)
		}
	})
	fixture.requireRejects(t, ctx, "owner")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1AcceptsUnrelatedPublicObjectAndOwner(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	fixture.createUnrelatedPublicTableAndRuntimeGrant(t, ctx)

	fixture.requireAccepts(t, ctx)
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsUnknownNonExtensionInternalObjects(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name   string
		create func(t *testing.T, fixture appACLEffectiveCatalogPostgresFixture)
	}{
		{
			name: "table",
			create: func(t *testing.T, fixture appACLEffectiveCatalogPostgresFixture) {
				fixture.exec(t, ctx, `create table record_platform_internal.unexpected_internal_catalog_table (id integer primary key)`)
				fixture.exec(t, ctx, `alter table record_platform_internal.unexpected_internal_catalog_table owner to `+quotePostgresIdentifier(fixture.roles.migrator))
			},
		},
		{
			name: "function",
			create: func(t *testing.T, fixture appACLEffectiveCatalogPostgresFixture) {
				fixture.exec(t, ctx, `
					create function record_platform_internal.unexpected_internal_catalog_function()
					returns integer
					language sql
					immutable
					return 1
				`)
				fixture.exec(t, ctx, `alter function record_platform_internal.unexpected_internal_catalog_function() owner to `+quotePostgresIdentifier(fixture.roles.migrator))
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
			fixture.requireAccepts(t, ctx)
			tt.create(t, fixture)

			fixture.requireRejects(t, ctx, "unexpected managed object owner")
		})
	}
}

func TestPostgresIntegrationAppACLEffectiveCatalogReadersR1ExcludeUnrelatedPublicObjectAndGrant(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	fixture.createUnrelatedPublicTableAndRuntimeGrant(t, ctx)

	tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin reader-boundary snapshot: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	scope, err := newAppACLManagedSurfaceScopeR1(fixture.databaseName)
	if err != nil {
		t.Fatalf("compile reader managed scope: %v", err)
	}

	owners, err := readAppACLEffectiveCatalogOwnersR1(ctx, tx, fixture.databaseName, scope)
	if err != nil {
		t.Fatalf("read managed object owners: %v", err)
	}
	for _, owner := range owners {
		if owner.ObjectClass == AppACLObjectClassTable && owner.SchemaName == "public" && owner.ObjectIdentity == "unrelated_catalog_fixture" {
			t.Fatalf("owner reader leaked unrelated public table: %#v", owner)
		}
	}

	directPrivileges, err := readAppACLEffectiveCatalogDirectPrivilegesR1(ctx, tx, fixture.databaseName, scope)
	if err != nil {
		t.Fatalf("read managed direct privileges: %v", err)
	}
	for _, privilege := range directPrivileges {
		if privilege.ObjectClass == AppACLObjectClassTable && privilege.SchemaName == "public" && privilege.ObjectIdentity == "unrelated_catalog_fixture" {
			t.Fatalf("direct-privilege reader leaked unrelated public table grant: %#v", privilege)
		}
	}

	effectivePrivileges, err := readAppACLEffectiveCatalogEffectivePrivilegesR1(ctx, tx, fixture.databaseName, []string{fixture.roles.centerRuntime, fixture.roles.platformAdmin}, scope)
	if err != nil {
		t.Fatalf("read managed effective privileges: %v", err)
	}
	for _, privilege := range effectivePrivileges {
		if privilege.ObjectClass == AppACLObjectClassTable && privilege.SchemaName == "public" && privilege.ObjectIdentity == "unrelated_catalog_fixture" {
			t.Fatalf("effective-privilege reader leaked unrelated public table grant: %#v", privilege)
		}
	}

	columnACLs, err := readAppACLEffectiveCatalogColumnACLsR1(ctx, tx, scope)
	if err != nil {
		t.Fatalf("read managed column ACLs: %v", err)
	}
	for _, columnACL := range columnACLs {
		if columnACL.SchemaName == "public" && columnACL.RelationName == "unrelated_catalog_fixture" {
			t.Fatalf("column-ACL reader leaked unrelated public table grant: %#v", columnACL)
		}
	}

	functions, err := readAppACLEffectiveCatalogFunctionsR1(ctx, tx, scope)
	if err != nil {
		t.Fatalf("read managed function definitions: %v", err)
	}
	for _, function := range functions {
		if function.SchemaName == "public" && function.Name == "unrelated_catalog_fixture_function" {
			t.Fatalf("function-definition reader leaked unrelated public function: %#v", function)
		}
	}
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1AcceptsUnrelatedSchemaOwnerDefaultACL(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	unrelatedRole := "houfeng_catalog_unrelated_default_" + fixture.roles.suffix
	unrelatedSchema := "houfeng_catalog_u_schema_" + fixture.roles.suffix
	fixture.exec(t, ctx, `create role `+quotePostgresIdentifier(unrelatedRole)+` nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`)
	fixture.dropRole(t, unrelatedRole)
	fixture.exec(t, ctx, `create schema `+quotePostgresIdentifier(unrelatedSchema)+` authorization `+quotePostgresIdentifier(unrelatedRole))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fixture.db.Exec(cleanupCtx, `drop schema if exists `+quotePostgresIdentifier(unrelatedSchema)+` cascade`); err != nil {
			t.Errorf("drop unrelated schema %q: %v", unrelatedSchema, err)
		}
	})
	fixture.exec(t, ctx, `alter default privileges for role `+quotePostgresIdentifier(unrelatedRole)+` in schema `+quotePostgresIdentifier(unrelatedSchema)+` grant select on tables to `+quotePostgresIdentifier(fixture.roles.centerRuntime))

	var defaultACLCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from pg_catalog.pg_default_acl default_acl
		join pg_catalog.pg_roles owner on owner.oid = default_acl.defaclrole
		join pg_catalog.pg_namespace namespace on namespace.oid = default_acl.defaclnamespace
		where owner.rolname = $1
		  and namespace.nspname = $2
		  and default_acl.defaclobjtype = 'r'
	`, unrelatedRole, unrelatedSchema).Scan(&defaultACLCount); err != nil {
		t.Fatalf("read unrelated schema default ACL: %v", err)
	}
	if defaultACLCount != 1 {
		t.Fatalf("unrelated schema default ACL rows = %d, want 1", defaultACLCount)
	}

	snapshot, err := (postgresAppACLEffectiveCatalogReaderR1{db: fixture.db}).read(ctx, fixture.input)
	if err != nil {
		t.Fatalf("read catalog snapshot with unrelated schema default ACL: %v", err)
	}
	for _, defaultACL := range snapshot.DefaultACLs {
		if defaultACL.OwnerRole == unrelatedRole || defaultACL.SchemaName == unrelatedSchema {
			t.Fatalf("default-ACL reader leaked unrelated schema state: %#v", defaultACL)
		}
	}
	if err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, fixture.input); err != nil {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want unrelated default ACL acceptance", err)
	}
	fixture.requireAccepts(t, ctx)
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsRuntimeAdminProjectorExecuteGrants(t *testing.T) {
	ctx := context.Background()
	for _, role := range []struct {
		name    string
		subject AppACLSubject
	}{
		{name: "runtime", subject: AppACLSubjectCenterRuntime},
		{name: "admin", subject: AppACLSubjectPlatformAdmin},
	} {
		for _, projector := range appACLProjectorFunctionsR1() {
			role := role
			projector := projector
			projectorName, _, found := strings.Cut(projector.identity, "(")
			if !found {
				t.Fatalf("projector identity %q has no argument list", projector.identity)
			}
			t.Run(role.name+"/"+projectorName, func(t *testing.T) {
				fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
				fixture.requireAccepts(t, ctx)

				roleName := ""
				for _, binding := range fixture.input.Contract.RoleBindings {
					if binding.Subject == role.subject {
						roleName = binding.CatalogRole
						break
					}
				}
				if roleName == "" {
					t.Fatalf("catalog contract has no %s role binding", role.name)
				}

				functionIdentity := fixture.functionIdentity(t, projectorName)
				fixture.exec(t, ctx, `grant execute on function `+functionIdentity+` to `+quotePostgresIdentifier(roleName))

				snapshot, err := (postgresAppACLEffectiveCatalogReaderR1{db: fixture.db}).read(ctx, fixture.input)
				if err != nil {
					t.Fatalf("read catalog snapshot with %s projector EXECUTE grant: %v", role.name, err)
				}
				catalogIdentity := projector.schemaName + "." + projector.identity
				assertProjectorPrivilegeObserved(t, snapshot.DirectPrivileges, roleName, catalogIdentity, "direct")
				assertProjectorPrivilegeObserved(t, snapshot.EffectivePrivileges, roleName, catalogIdentity, "effective")
				fixture.requireRejects(t, ctx, "unexpected direct app ACL privilege")
			})
		}
	}
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsProjectorTextOverload(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	fixture.createProjectorTextOverload(t, ctx, "record_platform_cas_contract_activation_projection")
	fixture.requireRejects(t, ctx, "has 2 overloads")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsProjectorTextOverloadAddedToExtension(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	functionIdentity := fixture.createProjectorTextOverload(t, ctx, "record_platform_cas_contract_activation_projection")
	if _, err := fixture.db.Exec(ctx, `alter extension pgcrypto add function `+functionIdentity); err != nil {
		t.Fatalf("add projector text overload to pgcrypto extension: %v", err)
	}
	fixture.revokeFunctionExecuteFromPublicAndAppRoles(t, ctx, functionIdentity)
	fixture.requireRejects(t, ctx, "has 2 overloads")
}

func TestPostgresIntegrationAppACLEffectiveCatalogNormalReadersExcludeReservedExtensionMember(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	functionName := "record_platform_cas_contract_activation_projection"
	functionIdentity := fixture.createProjectorTextOverload(t, ctx, functionName)
	if _, err := fixture.db.Exec(ctx, `alter extension pgcrypto add function `+functionIdentity); err != nil {
		t.Fatalf("add projector text overload to pgcrypto extension: %v", err)
	}
	fixture.exec(t, ctx, `grant execute on function `+functionIdentity+` to `+quotePostgresIdentifier(fixture.roles.centerRuntime))

	tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin normal-reader opacity transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	scope, err := newAppACLManagedSurfaceScopeR1(fixture.databaseName)
	if err != nil {
		t.Fatalf("newAppACLManagedSurfaceScopeR1() error = %v", err)
	}
	owners, err := readAppACLEffectiveCatalogOwnersR1(ctx, tx, fixture.databaseName, scope)
	if err != nil {
		t.Fatalf("read normal owner facts: %v", err)
	}
	directPrivileges, err := readAppACLEffectiveCatalogDirectPrivilegesR1(ctx, tx, fixture.databaseName, scope)
	if err != nil {
		t.Fatalf("read normal direct privilege facts: %v", err)
	}
	effectivePrivileges, err := readAppACLEffectiveCatalogEffectivePrivilegesR1(ctx, tx, fixture.databaseName, []string{fixture.roles.centerRuntime, fixture.roles.platformAdmin}, scope)
	if err != nil {
		t.Fatalf("read normal effective privilege facts: %v", err)
	}
	functions, err := readAppACLEffectiveCatalogFunctionsR1(ctx, tx, scope)
	if err != nil {
		t.Fatalf("read normal function facts: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit normal-reader opacity transaction: %v", err)
	}

	for _, owner := range owners {
		if owner.ObjectClass == AppACLObjectClassFunction && owner.SchemaName == "public" && owner.ObjectIdentity == functionName+"(text)" {
			t.Fatal("reserved extension-member overload leaked through normal owner reader")
		}
	}
	for _, privilege := range append(directPrivileges, effectivePrivileges...) {
		if privilege.ObjectClass == AppACLObjectClassFunction && privilege.ObjectIdentity == "public."+functionName+"(text)" {
			t.Fatal("reserved extension-member overload leaked through normal privilege reader")
		}
	}
	for _, function := range functions {
		if function.SchemaName == "public" && function.Name == functionName && function.IdentityArguments == "text" {
			t.Fatal("reserved extension-member overload leaked through normal function reader")
		}
	}
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsReachablePublicExtensionMember(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	functionIdentity := `public.unmanaged_public_extension_member_fixture()`
	fixture.exec(t, ctx, `
		create function `+functionIdentity+`
		returns text
		language sql
		immutable
		return 'reachable'::text
	`)
	if _, err := fixture.db.Exec(ctx, `alter extension pgcrypto add function `+functionIdentity); err != nil {
		t.Fatalf("add unmanaged public function to pgcrypto extension: %v", err)
	}

	runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.roles.centerRuntime)
	var result string
	if err := runtimeDB.QueryRow(ctx, `select `+functionIdentity).Scan(&result); err != nil {
		t.Fatalf("direct runtime call to default-PUBLIC public extension member: %v", err)
	}
	if result != "reachable" {
		t.Fatalf("direct runtime public extension member result = %q, want reachable", result)
	}

	fixture.requireRejects(t, ctx, "opaque extension member")
}

func (fixture appACLEffectiveCatalogPostgresFixture) createProjectorTextOverload(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	functionTarget := `public.` + quotePostgresIdentifier(name)
	functionIdentity := functionTarget + `(text)`
	fixture.exec(t, ctx, `
		create function `+functionTarget+`(text)
		returns bytea
		language sql
		immutable
		return convert_to($1, 'UTF8')
	`)
	for _, grantee := range []string{
		"public",
		quotePostgresIdentifier(fixture.roles.centerRuntime),
		quotePostgresIdentifier(fixture.roles.platformAdmin),
	} {
		fixture.exec(t, ctx, `revoke all on function `+functionIdentity+` from `+grantee)
	}
	return functionIdentity
}

func (fixture appACLEffectiveCatalogPostgresFixture) revokeFunctionExecuteFromPublicAndAppRoles(t *testing.T, ctx context.Context, functionIdentity string) {
	t.Helper()
	for _, grantee := range []string{
		"public",
		quotePostgresIdentifier(fixture.roles.centerRuntime),
		quotePostgresIdentifier(fixture.roles.platformAdmin),
	} {
		fixture.exec(t, ctx, `revoke execute on function `+functionIdentity+` from `+grantee)
	}
}

func assertProjectorPrivilegeObserved(
	t *testing.T,
	privileges []AppACLEffectiveCatalogPrivilegeObservationR1,
	role string,
	functionIdentity string,
	kind string,
) {
	t.Helper()
	for _, privilege := range privileges {
		if privilege.Grantee == role &&
			privilege.ObjectClass == AppACLObjectClassFunction &&
			privilege.ObjectIdentity == functionIdentity &&
			privilege.Privilege == AppACLPrivilegeExecute {
			return
		}
	}
	t.Fatalf("%s catalog snapshot is missing projector EXECUTE for %q on %s", kind, role, functionIdentity)
}

func (fixture appACLEffectiveCatalogPostgresFixture) createUnrelatedPublicTableAndRuntimeGrant(t *testing.T, ctx context.Context) {
	t.Helper()
	thirdPartyRole := "houfeng_catalog_unrelated_owner_" + fixture.roles.suffix
	if _, err := fixture.db.Exec(ctx, `create role `+quotePostgresIdentifier(thirdPartyRole)+` nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`); err != nil {
		t.Fatalf("create unrelated object owner %q: %v", thirdPartyRole, err)
	}
	fixture.dropRole(t, thirdPartyRole)
	if _, err := fixture.db.Exec(ctx, `create table public.unrelated_catalog_fixture (id integer primary key)`); err != nil {
		t.Fatalf("create unrelated public table: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `alter table public.unrelated_catalog_fixture owner to `+quotePostgresIdentifier(thirdPartyRole)); err != nil {
		t.Fatalf("assign unrelated public table owner: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant select on table public.unrelated_catalog_fixture to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant unrelated public table: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant select (id) on table public.unrelated_catalog_fixture to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant unrelated public table column: %v", err)
	}
	var columnACLPresent bool
	if err := fixture.db.QueryRow(ctx, `
		select attribute.attacl is not null
		from pg_catalog.pg_attribute attribute
		join pg_catalog.pg_class relation on relation.oid = attribute.attrelid
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = 'public'
		  and relation.relname = 'unrelated_catalog_fixture'
		  and attribute.attname = 'id'
	`).Scan(&columnACLPresent); err != nil {
		t.Fatalf("read unrelated public table column ACL: %v", err)
	}
	if !columnACLPresent {
		t.Fatal("unrelated public table column grant did not persist an attribute ACL")
	}
	if _, err := fixture.db.Exec(ctx, `
		create function public.unrelated_catalog_fixture_function()
		returns integer
		language sql
		immutable
		return 1
	`); err != nil {
		t.Fatalf("create unrelated public function: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `revoke all on function public.unrelated_catalog_fixture_function() from public`); err != nil {
		t.Fatalf("revoke unrelated public function PUBLIC access: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `alter function public.unrelated_catalog_fixture_function() owner to `+quotePostgresIdentifier(thirdPartyRole)); err != nil {
		t.Fatalf("assign unrelated public function owner: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant execute on function public.unrelated_catalog_fixture_function() to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant unrelated public function: %v", err)
	}
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1TreatsPgcryptoMembersOpaqueAndDeniesDirectRuntimeAdmin(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	var extensionSchema string
	if err := fixture.db.QueryRow(ctx, `
		select namespace.nspname
		from pg_catalog.pg_extension extension
		join pg_catalog.pg_namespace namespace on namespace.oid = extension.extnamespace
		where extension.extname = 'pgcrypto'
	`).Scan(&extensionSchema); err != nil {
		t.Fatalf("read pgcrypto extension schema: %v", err)
	}
	if extensionSchema != appACLManagedInternalSchemaR1 {
		t.Fatalf("pgcrypto extension schema = %q, want %q", extensionSchema, appACLManagedInternalSchemaR1)
	}
	snapshot, err := (postgresAppACLEffectiveCatalogReaderR1{db: fixture.db}).read(ctx, fixture.input)
	if err != nil {
		t.Fatalf("read managed catalog snapshot with pgcrypto: %v", err)
	}
	for _, function := range snapshot.Functions {
		if function.SchemaName == appACLManagedInternalSchemaR1 && function.Name == "digest" {
			t.Fatal("pgcrypto digest must remain opaque to the managed APP catalog reader")
		}
	}
	for _, owner := range snapshot.Owners {
		if owner.ObjectClass == AppACLObjectClassFunction && owner.SchemaName == appACLManagedInternalSchemaR1 && strings.HasPrefix(owner.ObjectIdentity, "digest(") {
			t.Fatal("pgcrypto digest owner must remain opaque to the managed APP catalog reader")
		}
	}
	for _, privilege := range append(snapshot.DirectPrivileges, snapshot.EffectivePrivileges...) {
		if privilege.ObjectClass == AppACLObjectClassFunction && strings.HasPrefix(privilege.ObjectIdentity, "record_platform_internal.digest(") {
			t.Fatal("pgcrypto digest privilege must remain opaque to the managed APP catalog reader")
		}
	}

	for _, role := range []string{fixture.roles.centerRuntime, fixture.roles.platformAdmin} {
		roleDB := fixture.openDirectRolePool(t, ctx, role)
		for _, functionIdentity := range []string{
			"public.record_platform_cas_contract_activation_projection($1)",
			"public.record_platform_cas_domain_rotation_projection($1)",
		} {
			_, err := roleDB.Exec(ctx, `select `+functionIdentity, []byte{})
			requirePostgresSQLState(t, err, "42501")
		}
		_, err := roleDB.Exec(ctx, `select record_platform_internal.digest($1, 'sha256')`, []byte("opaque-extension-member"))
		requirePostgresSQLState(t, err, "42501")
	}

	adminDB := fixture.openDirectRolePool(t, ctx, fixture.roles.platformAdmin)
	_, err = adminDB.Exec(ctx, `
		update public.app_acl_manifest_head
		set manifest_revision = manifest_revision
		where singleton
	`)
	requirePostgresSQLState(t, err, "42501")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsPgcryptoRelocatedToPublic(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	if _, err := fixture.db.Exec(ctx, `alter extension pgcrypto set schema public`); err != nil {
		t.Fatalf("relocate pgcrypto to public: %v", err)
	}
	runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.roles.centerRuntime)
	var digest []byte
	if err := runtimeDB.QueryRow(ctx, `select public.digest($1, 'sha256')`, []byte("relocated-pgcrypto")).Scan(&digest); err != nil {
		t.Fatalf("direct runtime call to relocated public.digest(): %v", err)
	}
	if len(digest) != 32 {
		t.Fatalf("direct runtime relocated public.digest() length = %d, want 32", len(digest))
	}

	fixture.requireRejects(t, ctx, "pgcrypto extension")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsColumnACL(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	if _, err := fixture.db.Exec(ctx, `grant update (name) on table public.schema_migrations to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant column ACL drift: %v", err)
	}
	fixture.requireRejects(t, ctx, "column ACL")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsMigratorDefaultACLs(t *testing.T) {
	ctx := context.Background()
	for _, testCase := range []struct {
		name string
		sql  func(appACLEffectiveCatalogPostgresFixture) string
	}{
		{
			name: "global",
			sql: func(fixture appACLEffectiveCatalogPostgresFixture) string {
				return `alter default privileges for role ` + quotePostgresIdentifier(fixture.roles.migrator) + ` grant select on tables to ` + quotePostgresIdentifier(fixture.roles.centerRuntime)
			},
		},
		{
			name: "public schema",
			sql: func(fixture appACLEffectiveCatalogPostgresFixture) string {
				return `alter default privileges for role ` + quotePostgresIdentifier(fixture.roles.migrator) + ` in schema public grant select on tables to ` + quotePostgresIdentifier(fixture.roles.centerRuntime)
			},
		},
		{
			name: "internal schema",
			sql: func(fixture appACLEffectiveCatalogPostgresFixture) string {
				return `alter default privileges for role ` + quotePostgresIdentifier(fixture.roles.migrator) + ` in schema record_platform_internal grant execute on functions to ` + quotePostgresIdentifier(fixture.roles.centerRuntime)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
			fixture.requireAccepts(t, ctx)
			if _, err := fixture.db.Exec(ctx, testCase.sql(fixture)); err != nil {
				t.Fatalf("grant %s default ACL drift: %v", testCase.name, err)
			}
			fixture.requireRejects(t, ctx, "default ACL")
		})
	}
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsSecurityDefinerDrift(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	for _, expected := range fixture.input.ExpectedFunctions {
		functionName := strings.TrimSuffix(strings.TrimPrefix(expected.Identity, "public."), "(bytea)")
		functionIdentity := `public.` + quotePostgresIdentifier(functionName) + `(bytea)`
		if _, err := fixture.db.Exec(ctx, `alter function `+functionIdentity+` security invoker`); err != nil {
			t.Fatalf("set expected function %q SECURITY INVOKER: %v", expected.Identity, err)
		}
	}
	fixture.requireRejects(t, ctx, "SECURITY DEFINER")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1ReadsCompleteDirectFunctionIdentity(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)
	functionIdentity := fixture.functionIdentity(t, "record_platform_cas_contract_activation_projection")
	if _, err := fixture.db.Exec(ctx, `grant execute on function `+functionIdentity+` to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant direct function identity fixture: %v", err)
	}

	snapshot, err := (postgresAppACLEffectiveCatalogReaderR1{db: fixture.db}).read(ctx, fixture.input)
	if err != nil {
		t.Fatalf("read app ACL effective catalog snapshot: %v", err)
	}
	want := fixture.expectedFunction(t, "record_platform_cas_contract_activation_projection").Identity
	for _, privilege := range snapshot.DirectPrivileges {
		if privilege.Grantee == fixture.roles.centerRuntime && privilege.ObjectClass == AppACLObjectClassFunction && strings.HasPrefix(privilege.ObjectIdentity, "public.record_platform_cas_contract_activation_projection(") {
			if privilege.ObjectIdentity != want {
				t.Fatalf("direct function identity = %q, want %q", privilege.ObjectIdentity, want)
			}
			return
		}
	}
	t.Fatalf("direct catalog snapshot is missing expected function %q", want)
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsInternalSchemaUsageAndCreateForPublicRuntimeAdmin(t *testing.T) {
	ctx := context.Background()
	for _, testCase := range []struct {
		name      string
		grantee   func(appACLEffectiveCatalogPostgresFixture) string
		privilege string
		reason    string
	}{
		{name: "public/usage", grantee: func(appACLEffectiveCatalogPostgresFixture) string { return "public" }, privilege: "usage", reason: "opaque extension member"},
		{name: "public/create", grantee: func(appACLEffectiveCatalogPostgresFixture) string { return "public" }, privilege: "create", reason: "app ACL"},
		{name: "runtime/usage", grantee: func(fixture appACLEffectiveCatalogPostgresFixture) string {
			return quotePostgresIdentifier(fixture.roles.centerRuntime)
		}, privilege: "usage", reason: "opaque extension member"},
		{name: "runtime/create", grantee: func(fixture appACLEffectiveCatalogPostgresFixture) string {
			return quotePostgresIdentifier(fixture.roles.centerRuntime)
		}, privilege: "create", reason: "app ACL"},
		{name: "admin/usage", grantee: func(fixture appACLEffectiveCatalogPostgresFixture) string {
			return quotePostgresIdentifier(fixture.roles.platformAdmin)
		}, privilege: "usage", reason: "opaque extension member"},
		{name: "admin/create", grantee: func(fixture appACLEffectiveCatalogPostgresFixture) string {
			return quotePostgresIdentifier(fixture.roles.platformAdmin)
		}, privilege: "create", reason: "app ACL"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
			fixture.requireAccepts(t, ctx)
			if _, err := fixture.db.Exec(ctx, `grant `+testCase.privilege+` on schema record_platform_internal to `+testCase.grantee(fixture)); err != nil {
				t.Fatalf("grant %s internal schema %s drift: %v", testCase.name, strings.ToUpper(testCase.privilege), err)
			}
			fixture.requireRejects(t, ctx, testCase.reason)
		})
	}
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsThirdPartyFunctionGrantOption(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.requireAccepts(t, ctx)

	thirdPartyRole := "houfeng_catalog_third_party_" + fixture.roles.suffix
	if _, err := fixture.db.Exec(ctx, `create role `+quotePostgresIdentifier(thirdPartyRole)+` nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`); err != nil {
		t.Fatalf("create third-party role %q: %v", thirdPartyRole, err)
	}
	fixture.dropRole(t, thirdPartyRole)

	functionIdentity := fixture.functionIdentity(t, "record_platform_cas_contract_activation_projection")
	if _, err := fixture.db.Exec(ctx, `grant execute on function `+functionIdentity+` to `+quotePostgresIdentifier(thirdPartyRole)+` with grant option`); err != nil {
		t.Fatalf("grant third-party function EXECUTE WITH GRANT OPTION drift: %v", err)
	}

	fixture.requireRejects(t, ctx, "direct app ACL privilege has unknown grantee")
}

type appACLEffectiveCatalogPostgresFixture struct {
	db             *pgxpool.Pool
	databaseName   string
	bootstrapOwner string
	roles          appACLEffectiveCatalogTestRoles
	rolePasswords  map[string]string
	input          AppACLEffectiveCatalogVerifierInputR1
}

func newAppACLEffectiveCatalogPostgresFixture(t *testing.T, ctx context.Context) appACLEffectiveCatalogPostgresFixture {
	t.Helper()
	db := openTemporaryPostgresDatabase(t, ctx)
	roles := appACLEffectiveCatalogTestRoleNames()
	fixture := appACLEffectiveCatalogPostgresFixture{
		db:            db,
		roles:         roles,
		rolePasswords: make(map[string]string, 3),
	}
	if err := db.QueryRow(ctx, `select pg_catalog.current_database(), current_user`).Scan(&fixture.databaseName, &fixture.bootstrapOwner); err != nil {
		t.Fatalf("read temporary database identity: %v", err)
	}
	for _, role := range []string{roles.centerRuntime, roles.platformAdmin, roles.migrator} {
		password := appACLEffectiveCatalogTemporaryPassword(t)
		fixture.rolePasswords[role] = password
		if _, err := db.Exec(ctx, `create role `+quotePostgresIdentifier(role)+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+password+`'`); err != nil {
			t.Fatalf("create temporary role %q: %v", role, err)
		}
		fixture.dropRole(t, role)
	}
	if _, err := db.Exec(ctx, `revoke all on database `+quotePostgresIdentifier(fixture.databaseName)+` from public`); err != nil {
		t.Fatalf("revoke default PUBLIC database privileges: %v", err)
	}
	if _, err := db.Exec(ctx, `revoke all on schema public from public`); err != nil {
		t.Fatalf("revoke default PUBLIC schema privileges: %v", err)
	}

	contract, err := CompileAppACLEffectiveCatalogContractR1(fixture.databaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: roles.centerRuntime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: roles.platformAdmin},
	})
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	fixture.input, err = NewAppACLEffectiveCatalogVerifierInputR1(contract, roles.migrator)
	if err != nil {
		t.Fatalf("NewAppACLEffectiveCatalogVerifierInputR1() error = %v", err)
	}
	fixture.materializeMigratedBaseline(t, ctx)
	return fixture
}

func appACLEffectiveCatalogTemporaryPassword(t *testing.T) string {
	t.Helper()
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		t.Fatalf("generate temporary direct-login password: %v", err)
	}
	return "test-" + hex.EncodeToString(entropy[:])
}

func (fixture appACLEffectiveCatalogPostgresFixture) requireRejects(t *testing.T, ctx context.Context, reason string) {
	t.Helper()
	err := VerifyPostgresAppACLEffectiveCatalogR1(ctx, fixture.db, fixture.input)
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("VerifyPostgresAppACLEffectiveCatalogR1() error = %v, want %s rejection", err, reason)
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) requireAccepts(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := VerifyPostgresAppACLEffectiveCatalogR1(ctx, fixture.db, fixture.input); err != nil {
		t.Fatalf("VerifyPostgresAppACLEffectiveCatalogR1() error = %v, want acceptance", err)
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) openDirectRolePool(t *testing.T, ctx context.Context, role string) *pgxpool.Pool {
	t.Helper()
	password, ok := fixture.rolePasswords[role]
	if !ok {
		t.Fatalf("no direct-login password for role %q", role)
	}
	config := fixture.db.Config().Copy()
	config.MaxConns = 1
	config.MinConns = 0
	config.ConnConfig.User = role
	config.ConnConfig.Password = password
	config.AfterConnect = nil
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open direct pool for role %q: %v", role, err)
	}
	t.Cleanup(pool.Close)

	var sessionUser, currentUser string
	if err := pool.QueryRow(ctx, `select session_user, current_user`).Scan(&sessionUser, &currentUser); err != nil {
		t.Fatalf("read direct pool identities for role %q: %v", role, err)
	}
	if sessionUser != role || currentUser != role {
		t.Fatalf("direct pool identities for role %q = (%q, %q), want (%q, %q)", role, sessionUser, currentUser, role, role)
	}
	return pool
}

func requirePostgresSQLState(t *testing.T, err error, want string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("PostgreSQL error = %v, want SQLSTATE %s", err, want)
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) materializeMigratedBaseline(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := Apply(ctx, fixture.db); err != nil {
		t.Fatalf("Apply() real migration baseline error = %v", err)
	}
	fixture.transferManagedSurfaceOwnershipToMigrator(t, ctx)
	fixture.revokeManagedSurfacePrivileges(t, ctx)

	rolesBySubject := make(map[AppACLSubject]string, len(fixture.input.Contract.RoleBindings))
	for _, binding := range fixture.input.Contract.RoleBindings {
		rolesBySubject[binding.Subject] = binding.CatalogRole
	}
	for _, privilege := range fixture.input.Contract.Privileges {
		role, ok := rolesBySubject[privilege.Subject]
		if !ok {
			t.Fatalf("compiler emitted privilege for unbound subject %q", privilege.Subject)
		}
		fixture.grant(t, ctx, privilege, role)
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) transferManagedSurfaceOwnershipToMigrator(t *testing.T, ctx context.Context) {
	t.Helper()
	surface, err := CompileAppACLManagedSurfaceR1(fixture.databaseName)
	if err != nil {
		t.Fatalf("CompileAppACLManagedSurfaceR1() error = %v", err)
	}
	migrator := quotePostgresIdentifier(fixture.roles.migrator)
	for _, objectClass := range []AppACLObjectClass{
		AppACLObjectClassDatabase,
		AppACLObjectClassSchema,
		AppACLObjectClassTable,
		AppACLObjectClassView,
		AppACLObjectClassSequence,
		AppACLObjectClassFunction,
	} {
		for _, object := range surface.Objects {
			if object.ObjectClass != objectClass {
				continue
			}
			switch object.ObjectClass {
			case AppACLObjectClassDatabase:
				fixture.exec(t, ctx, `alter database `+quotePostgresIdentifier(object.ObjectIdentity)+` owner to `+migrator)
			case AppACLObjectClassSchema:
				if object.SchemaName == appACLManagedPublicSchemaR1 {
					// PostgreSQL 16 owns bootstrap public through pg_database_owner.
					// Keep that normal state and bind it through the database owner.
					continue
				}
				fixture.exec(t, ctx, `alter schema `+quotePostgresIdentifier(object.ObjectIdentity)+` owner to `+migrator)
			case AppACLObjectClassTable:
				fixture.exec(t, ctx, `alter table `+fixture.managedRelationIdentity(object)+` owner to `+migrator)
			case AppACLObjectClassView:
				fixture.exec(t, ctx, `alter view `+fixture.managedRelationIdentity(object)+` owner to `+migrator)
			case AppACLObjectClassSequence:
				// The fixed r1 sequences are all OWNED BY table columns. PostgreSQL
				// moves them with their tables and rejects a separate ALTER SEQUENCE.
			case AppACLObjectClassFunction:
				fixture.exec(t, ctx, `alter function `+fixture.managedFunctionIdentity(t, object)+` owner to `+migrator)
			}
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fixture.db.Exec(cleanupCtx, `alter database `+quotePostgresIdentifier(fixture.databaseName)+` owner to `+quotePostgresIdentifier(fixture.bootstrapOwner)); err != nil {
			t.Errorf("restore temporary database owner %q: %v", fixture.bootstrapOwner, err)
		}
	})
}

func (fixture appACLEffectiveCatalogPostgresFixture) revokeManagedSurfacePrivileges(t *testing.T, ctx context.Context) {
	t.Helper()
	grantees := []string{"public", quotePostgresIdentifier(fixture.roles.centerRuntime), quotePostgresIdentifier(fixture.roles.platformAdmin)}
	for _, grantee := range grantees {
		fixture.exec(t, ctx, `revoke all privileges on database `+quotePostgresIdentifier(fixture.databaseName)+` from `+grantee)
	}
	for _, schemaName := range []string{appACLManagedPublicSchemaR1, appACLManagedInternalSchemaR1} {
		for _, grantee := range grantees {
			fixture.exec(t, ctx, `revoke all privileges on schema `+quotePostgresIdentifier(schemaName)+` from `+grantee)
		}
	}

	surface, err := CompileAppACLManagedSurfaceR1(fixture.databaseName)
	if err != nil {
		t.Fatalf("CompileAppACLManagedSurfaceR1() error = %v", err)
	}
	for _, object := range surface.Objects {
		var target string
		switch object.ObjectClass {
		case AppACLObjectClassTable, AppACLObjectClassView:
			target = `on table ` + fixture.managedRelationIdentity(object)
		case AppACLObjectClassSequence:
			target = `on sequence ` + fixture.managedRelationIdentity(object)
		case AppACLObjectClassFunction:
			target = `on function ` + fixture.managedFunctionIdentity(t, object)
		default:
			continue
		}
		for _, grantee := range grantees {
			fixture.exec(t, ctx, `revoke all privileges `+target+` from `+grantee)
		}
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) managedRelationIdentity(object AppACLManagedObjectR1) string {
	return quotePostgresIdentifier(object.SchemaName) + `.` + quotePostgresIdentifier(object.ObjectIdentity)
}

func (fixture appACLEffectiveCatalogPostgresFixture) managedFunctionIdentity(t *testing.T, object AppACLManagedObjectR1) string {
	t.Helper()
	functionName, arguments, found := strings.Cut(object.ObjectIdentity, "(")
	if !found || !strings.HasSuffix(arguments, ")") {
		t.Fatalf("invalid static managed function identity %q", object.ObjectIdentity)
	}
	return quotePostgresIdentifier(object.SchemaName) + `.` + quotePostgresIdentifier(functionName) + `(` + strings.TrimSuffix(arguments, ")") + `)`
}

func (fixture appACLEffectiveCatalogPostgresFixture) grant(t *testing.T, ctx context.Context, privilege AppACLPrivilege, role string) {
	t.Helper()
	grantee := quotePostgresIdentifier(role)
	privilegeName := string(privilege.Privilege)
	switch privilege.ObjectClass {
	case AppACLObjectClassDatabase:
		fixture.exec(t, ctx, `grant `+privilegeName+` on database `+quotePostgresIdentifier(privilege.ObjectIdentity)+` to `+grantee)
	case AppACLObjectClassSchema:
		fixture.exec(t, ctx, `grant `+privilegeName+` on schema `+quotePostgresIdentifier(privilege.ObjectIdentity)+` to `+grantee)
	case AppACLObjectClassTable, AppACLObjectClassView:
		fixture.exec(t, ctx, `grant `+privilegeName+` on table public.`+quotePostgresIdentifier(privilege.ObjectIdentity)+` to `+grantee)
	case AppACLObjectClassSequence:
		fixture.exec(t, ctx, `grant `+privilegeName+` on sequence public.`+quotePostgresIdentifier(privilege.ObjectIdentity)+` to `+grantee)
	case AppACLObjectClassFunction:
		functionName := strings.TrimSuffix(strings.TrimPrefix(privilege.ObjectIdentity, "public."), "(bytea)")
		fixture.exec(t, ctx, `grant `+privilegeName+` on function `+fixture.functionIdentity(t, functionName)+` to `+grantee)
	default:
		t.Fatalf("compiler emitted unsupported baseline grant object class %q", privilege.ObjectClass)
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) functionIdentity(t *testing.T, name string) string {
	t.Helper()
	fixture.expectedFunction(t, name)
	return `public.` + quotePostgresIdentifier(name) + `(bytea)`
}

func (fixture appACLEffectiveCatalogPostgresFixture) expectedFunction(t *testing.T, name string) AppACLEffectiveCatalogExpectedFunctionR1 {
	t.Helper()
	for _, expected := range fixture.input.ExpectedFunctions {
		if expected.Identity == "public."+name+"(bytea)" {
			return expected
		}
	}
	t.Fatalf("expected function %q is not in the static projector inventory", name)
	return AppACLEffectiveCatalogExpectedFunctionR1{}
}

func (fixture appACLEffectiveCatalogPostgresFixture) exec(t *testing.T, ctx context.Context, sql string) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, sql); err != nil {
		t.Fatalf("execute %q: %v", sql, err)
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) dropRole(t *testing.T, role string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if role == fixture.roles.migrator && fixture.bootstrapOwner != "" {
			if _, err := fixture.db.Exec(cleanupCtx, `reassign owned by `+quotePostgresIdentifier(role)+` to `+quotePostgresIdentifier(fixture.bootstrapOwner)); err != nil {
				t.Errorf("reassign temporary migrator role %q ownership: %v", role, err)
			}
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop owned by `+quotePostgresIdentifier(role)); err != nil {
			t.Errorf("drop owned by temporary role %q: %v", role, err)
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop role if exists `+quotePostgresIdentifier(role)); err != nil {
			t.Errorf("drop temporary role %q: %v", role, err)
		}
	})
}

type appACLEffectiveCatalogTestRoles struct {
	centerRuntime string
	platformAdmin string
	migrator      string
	suffix        string
}

func appACLEffectiveCatalogTestRoleNames() appACLEffectiveCatalogTestRoles {
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
	return appACLEffectiveCatalogTestRoles{
		centerRuntime: "houfeng_catalog_runtime_" + suffix,
		platformAdmin: "houfeng_catalog_admin_" + suffix,
		migrator:      "houfeng_catalog_migrator_" + suffix,
		suffix:        suffix,
	}
}
