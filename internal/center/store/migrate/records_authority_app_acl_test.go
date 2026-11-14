package migrate

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

func TestPostgresIntegrationRecordsAuthorityHeartbeatRejectsUnprivilegedRoles(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatalf("converge current APP ACL: %v", err)
	}

	var securityDefiner, fixedSearchPath, publicExecute bool
	err := fixture.db.QueryRow(ctx, `
		select p.prosecdef,
		       coalesce(p.proconfig @> array['search_path=pg_catalog']::text[], false),
		       exists (
		           select 1
		           from pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) acl
		           where acl.grantee = 0 and acl.privilege_type = 'EXECUTE'
		       )
		from pg_catalog.pg_proc p
		where p.oid = 'public.record_platform_compose_membership_heartbeat(bytea)'::regprocedure
	`).Scan(&securityDefiner, &fixedSearchPath, &publicExecute)
	if err != nil {
		t.Fatalf("read heartbeat function security: %v", err)
	}
	if !securityDefiner || !fixedSearchPath || publicExecute {
		t.Fatalf("heartbeat security definer=%t fixed search path=%t PUBLIC execute=%t", securityDefiner, fixedSearchPath, publicExecute)
	}

	for _, role := range []string{fixture.runtime, fixture.admin, fixture.deletionWorker} {
		t.Run(role, func(t *testing.T) {
			db := fixture.openDirectRolePool(t, ctx, role)
			_, err := db.Exec(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, []byte{})
			requirePostgresSQLState(t, err, "42501")
		})
	}
}

func TestRecordsAuthorityCurrentCatalogAllowsAbsenceButClosesPresentRole(t *testing.T) {
	_, contract, snapshot := appACLCurrentRuntimeAdmissionFixture(t, migrations.FS, appACLCurrentMigrationFragments)
	input, err := newAppACLEffectiveCatalogVerifierInput(contract, "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(snapshot, input); err != nil {
		t.Fatalf("optional authority role absence rejected: %v", err)
	}

	snapshot.Roles = append(snapshot.Roles, AppACLEffectiveCatalogRoleStateR1{Name: recordsAuthorityCatalogRole, Login: true})
	for _, privilege := range contract.AuxiliaryPrivileges {
		observation := AppACLEffectiveCatalogPrivilegeObservationR1{
			Grantee:        privilege.CatalogRole,
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
			Privilege:      privilege.Privilege,
			GrantOption:    privilege.GrantOption,
		}
		snapshot.DirectPrivileges = append(snapshot.DirectPrivileges, observation)
		snapshot.EffectivePrivileges = append(snapshot.EffectivePrivileges, observation)
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(snapshot, input); err != nil {
		t.Fatalf("exact authority role rejected: %v", err)
	}

	drifted := snapshot
	drifted.Roles = append([]AppACLEffectiveCatalogRoleStateR1(nil), snapshot.Roles...)
	drifted.Roles[len(drifted.Roles)-1].TemporaryObjects = true
	if err := verifyAppACLEffectiveCatalogSnapshot(drifted, input); err == nil || !strings.Contains(err.Error(), "TEMP") {
		t.Fatalf("authority TEMP drift error = %v", err)
	}
	drifted = snapshot
	drifted.EffectivePrivileges = append([]AppACLEffectiveCatalogPrivilegeObservationR1(nil), snapshot.EffectivePrivileges[:len(snapshot.EffectivePrivileges)-1]...)
	if err := verifyAppACLEffectiveCatalogSnapshot(drifted, input); err == nil || !strings.Contains(err.Error(), "missing effective") {
		t.Fatalf("missing authority privilege error = %v", err)
	}
	drifted = snapshot
	drifted.Memberships = []AppACLEffectiveCatalogMembershipR1{{MemberRole: recordsAuthorityCatalogRole, ParentRole: "unexpected_parent"}}
	if err := verifyAppACLEffectiveCatalogSnapshot(drifted, input); err == nil || !strings.Contains(err.Error(), "membership is forbidden") {
		t.Fatalf("authority membership drift error = %v", err)
	}
}

func TestRecordsAuthorityAppACLFragmentRegistersOnlyHeartbeatFunctionAndAuxiliaryPrivileges(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}

	var fragment appACLCurrentCompiledMigrationFragment
	for _, candidate := range source.fragments {
		if candidate.Migration == "0060_create_records_authority_heartbeat.sql" {
			fragment = candidate
			break
		}
	}
	if fragment.Migration == "" {
		t.Fatal("records authority APP ACL fragment is not registered")
	}

	wantObject := AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     "public",
		ObjectIdentity: "record_platform_compose_membership_heartbeat(bytea)",
	}
	if !reflect.DeepEqual(fragment.Objects, []AppACLManagedObjectR1{wantObject}) {
		t.Fatalf("records authority managed objects = %#v, want only %#v", fragment.Objects, wantObject)
	}
	if len(fragment.Privileges) != 0 {
		t.Fatalf("records authority runtime/admin privileges = %#v, want none", fragment.Privileges)
	}

	wantAuxiliary := []AppACLCurrentAuxiliaryPrivilege{
		{
			CatalogRole:    recordsAuthorityCatalogRole,
			ObjectClass:    AppACLObjectClassDatabase,
			ObjectIdentity: appACLCurrentValidationDatabase,
			Privilege:      AppACLPrivilegeConnect,
		},
		{
			CatalogRole:    recordsAuthorityCatalogRole,
			ObjectClass:    AppACLObjectClassSchema,
			ObjectIdentity: "public",
			Privilege:      AppACLPrivilegeUsage,
		},
		{
			CatalogRole:    recordsAuthorityCatalogRole,
			ObjectClass:    AppACLObjectClassFunction,
			ObjectIdentity: "public.record_platform_compose_membership_heartbeat(bytea)",
			Privilege:      AppACLPrivilegeExecute,
		},
	}
	if !reflect.DeepEqual(fragment.AuxiliaryPrivileges, wantAuxiliary) {
		t.Fatalf("records authority auxiliary privileges = %#v, want %#v", fragment.AuxiliaryPrivileges, wantAuxiliary)
	}

	wantFunction := AppACLCurrentFunctionContract{
		SchemaName:      "public",
		Identity:        "record_platform_compose_membership_heartbeat(bytea)",
		Kind:            "f",
		SecurityDefiner: true,
		Config:          []string{"search_path=pg_catalog"},
	}
	if !reflect.DeepEqual(fragment.Functions, []AppACLCurrentFunctionContract{wantFunction}) {
		t.Fatalf("records authority function hardening = %#v, want %#v", fragment.Functions, wantFunction)
	}
}

func TestRecordsAuthorityAppACLFragmentDoesNotChangeFrozenApplicationPrivilegeBody(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	source = appACLCurrentSourceAtProfile(t, source, appACLCurrentProfileP80)
	contract, err := compileAppACLCurrentCatalogContract(
		source,
		"houfeng",
		appACLCurrentCatalogTestBindings(),
		"houfeng_migrator",
	)
	if err != nil {
		t.Fatalf("compile production current APP ACL catalog contract: %v", err)
	}
	for _, privilege := range contract.Privileges {
		if privilege.Subject != AppACLSubjectCenterRuntime && privilege.Subject != AppACLSubjectPlatformAdmin {
			t.Fatalf("frozen application privilege body contains auxiliary subject: %#v", privilege)
		}
	}
}
