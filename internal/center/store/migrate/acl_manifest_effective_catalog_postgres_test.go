package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsPublicDatabaseConnect(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	if _, err := fixture.db.Exec(ctx, `grant connect on database `+quotePostgresIdentifier(fixture.databaseName)+` to public`); err != nil {
		t.Fatalf("grant PUBLIC database CONNECT drift: %v", err)
	}
	fixture.requireRejects(t, ctx, "PUBLIC")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsRecursiveMembership(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
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

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsRuntimeSchemaOwner(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
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

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsColumnACL(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	if _, err := fixture.db.Exec(ctx, `create table public.catalog_column_acl_drift (id integer not null)`); err != nil {
		t.Fatalf("create column ACL fixture table: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant select (id) on table public.catalog_column_acl_drift to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant column ACL drift: %v", err)
	}
	fixture.requireRejects(t, ctx, "column ACL")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsDefaultACL(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	if _, err := fixture.db.Exec(ctx, `alter default privileges in schema public grant select on tables to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant default ACL drift: %v", err)
	}
	fixture.requireRejects(t, ctx, "default ACL")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsSecurityDefinerDrift(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	for _, expected := range fixture.input.ExpectedFunctions {
		functionName := strings.TrimSuffix(strings.TrimPrefix(expected.Identity, "public."), "(bytea)")
		functionIdentity := `public.` + quotePostgresIdentifier(functionName) + `(bytea)`
		if _, err := fixture.db.Exec(ctx, `create function `+functionIdentity+` returns void language plpgsql as $$ begin end $$`); err != nil {
			t.Fatalf("create temporary expected function %q: %v", expected.Identity, err)
		}
		if _, err := fixture.db.Exec(ctx, `alter function `+functionIdentity+` owner to `+quotePostgresIdentifier(fixture.roles.migrator)); err != nil {
			t.Fatalf("assign temporary expected function owner %q: %v", expected.Identity, err)
		}
		if _, err := fixture.db.Exec(ctx, `revoke all on function `+functionIdentity+` from public`); err != nil {
			t.Fatalf("revoke temporary expected function PUBLIC EXECUTE %q: %v", expected.Identity, err)
		}
	}
	fixture.requireRejects(t, ctx, "SECURITY DEFINER")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1ReadsCompleteDirectFunctionIdentity(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.materializeCompilerDerivedBaseline(t, ctx)

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

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsRuntimeUsageInInternalSchema(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.materializeCompilerDerivedBaseline(t, ctx)
	fixture.requireAccepts(t, ctx)

	if _, err := fixture.db.Exec(ctx, `create schema record_platform_internal`); err != nil {
		t.Fatalf("create internal schema drift: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant usage on schema record_platform_internal to `+quotePostgresIdentifier(fixture.roles.centerRuntime)); err != nil {
		t.Fatalf("grant runtime internal schema USAGE drift: %v", err)
	}

	fixture.requireRejects(t, ctx, "unexpected direct app ACL privilege")
}

func TestPostgresIntegrationVerifyAppACLEffectiveCatalogR1RejectsThirdPartyFunctionGrantOption(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLEffectiveCatalogPostgresFixture(t, ctx)
	fixture.materializeCompilerDerivedBaseline(t, ctx)
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
	db           *pgxpool.Pool
	databaseName string
	roles        appACLEffectiveCatalogTestRoles
	input        AppACLEffectiveCatalogVerifierInputR1
}

func newAppACLEffectiveCatalogPostgresFixture(t *testing.T, ctx context.Context) appACLEffectiveCatalogPostgresFixture {
	t.Helper()
	db := openTemporaryPostgresDatabase(t, ctx)
	roles := appACLEffectiveCatalogTestRoleNames()
	fixture := appACLEffectiveCatalogPostgresFixture{db: db, roles: roles}
	for _, role := range []string{roles.centerRuntime, roles.platformAdmin, roles.migrator} {
		if _, err := db.Exec(ctx, `create role `+quotePostgresIdentifier(role)+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`); err != nil {
			t.Fatalf("create temporary role %q: %v", role, err)
		}
		fixture.dropRole(t, role)
	}
	if err := db.QueryRow(ctx, `select pg_catalog.current_database()`).Scan(&fixture.databaseName); err != nil {
		t.Fatalf("read temporary database name: %v", err)
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
	return fixture
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

func (fixture appACLEffectiveCatalogPostgresFixture) materializeCompilerDerivedBaseline(t *testing.T, ctx context.Context) {
	t.Helper()
	rolesBySubject := make(map[AppACLSubject]string, len(fixture.input.Contract.RoleBindings))
	for _, binding := range fixture.input.Contract.RoleBindings {
		rolesBySubject[binding.Subject] = binding.CatalogRole
	}

	created := make(map[AppACLObjectClass]map[string]struct{})
	for _, privilege := range fixture.input.Contract.Privileges {
		switch privilege.ObjectClass {
		case AppACLObjectClassDatabase, AppACLObjectClassSchema:
			continue
		case AppACLObjectClassTable:
			if fixture.markCreated(created, privilege.ObjectClass, privilege.ObjectIdentity) {
				fixture.exec(t, ctx, `create table public.`+quotePostgresIdentifier(privilege.ObjectIdentity)+` (placeholder integer)`)
			}
		case AppACLObjectClassView:
			if fixture.markCreated(created, privilege.ObjectClass, privilege.ObjectIdentity) {
				fixture.exec(t, ctx, `create view public.`+quotePostgresIdentifier(privilege.ObjectIdentity)+` as select 1 as placeholder`)
			}
		case AppACLObjectClassSequence:
			if fixture.markCreated(created, privilege.ObjectClass, privilege.ObjectIdentity) {
				fixture.exec(t, ctx, `create sequence public.`+quotePostgresIdentifier(privilege.ObjectIdentity))
			}
		case AppACLObjectClassFunction:
			if fixture.markCreated(created, privilege.ObjectClass, privilege.ObjectIdentity) {
				functionIdentity := fixture.functionIdentity(t, strings.TrimSuffix(strings.TrimPrefix(privilege.ObjectIdentity, "public."), "(bytea)"))
				fixture.exec(t, ctx, `create function `+functionIdentity+` returns void language plpgsql security definer set search_path = pg_catalog as $$ begin end $$`)
				fixture.exec(t, ctx, `alter function `+functionIdentity+` owner to `+quotePostgresIdentifier(fixture.roles.migrator))
				fixture.exec(t, ctx, `revoke all on function `+functionIdentity+` from public`)
			}
		default:
			t.Fatalf("compiler emitted unsupported baseline object class %q", privilege.ObjectClass)
		}
	}

	for _, privilege := range fixture.input.Contract.Privileges {
		role, ok := rolesBySubject[privilege.Subject]
		if !ok {
			t.Fatalf("compiler emitted privilege for unbound subject %q", privilege.Subject)
		}
		fixture.grant(t, ctx, privilege, role)
	}
}

func (fixture appACLEffectiveCatalogPostgresFixture) markCreated(created map[AppACLObjectClass]map[string]struct{}, class AppACLObjectClass, identity string) bool {
	identities := created[class]
	if identities == nil {
		identities = make(map[string]struct{})
		created[class] = identities
	}
	if _, exists := identities[identity]; exists {
		return false
	}
	identities[identity] = struct{}{}
	return true
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
	t.Fatalf("expected function %q is not compiler-derived", name)
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
