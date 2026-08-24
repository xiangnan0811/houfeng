package migrate

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

func TestRecordsAuthorityHeartbeatMigrationIsClosedAndHardened(t *testing.T) {
	source, err := fs.ReadFile(migrations.FS, "0060_create_records_authority_heartbeat.sql")
	if err != nil {
		t.Fatalf("read records authority heartbeat migration: %v", err)
	}
	sql := string(source)
	for _, required := range []string{
		"create or replace function public.record_platform_compose_membership_heartbeat(bytea)",
		"returns timestamptz",
		"security definer",
		"set search_path = pg_catalog",
		"record_platform_projection_validate_header_v1(",
		"from public.deployment_contract_state",
		"from public.deployment_membership",
		"for update",
		"heartbeat_expires_at = v_heartbeat_expires_at",
		"revoke all on function public.record_platform_compose_membership_heartbeat(bytea) from public",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("records authority heartbeat migration lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"create role",
		"grant execute",
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		"delete from public.deployment_membership",
	} {
		if strings.Contains(strings.ToLower(sql), forbidden) {
			t.Fatalf("records authority heartbeat migration contains forbidden %q", forbidden)
		}
	}
}

func TestRecordsAuthorityCurrentDCLConditionallyConvergesOnlyItsThreeGrants(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := compileAppACLCurrentCatalogContract(source, "houfeng", appACLCurrentCatalogTestBindings(), "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	statements, err := appACLConvergenceDCLStatementsForContract(contract)
	if err != nil {
		t.Fatalf("appACLConvergenceDCLStatementsForContract() error = %v", err)
	}
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		"pg_catalog.to_regrole('houfeng_records_authority') is not null",
		`revoke all privileges on function "public"."record_platform_compose_membership_heartbeat"(bytea) from "houfeng_records_authority"`,
		`grant CONNECT on database "houfeng" to "houfeng_records_authority"`,
		`grant USAGE on schema "public" to "houfeng_records_authority"`,
		`grant EXECUTE on function "public"."record_platform_compose_membership_heartbeat"(bytea) to "houfeng_records_authority"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("current DCL lacks conditional authority statement %q", want)
		}
	}
	if strings.Contains(strings.ToLower(joined), "create role") {
		t.Fatal("current APP ACL DCL creates the optional authority role")
	}
	if got := strings.Count(joined, ` to "houfeng_records_authority"`); got != 3 {
		t.Fatalf("authority grant count = %d, want exactly 3", got)
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
	contract, err := compileAppACLCurrentCatalogContract(
		source,
		"houfeng",
		appACLCurrentCatalogTestBindings(),
		"houfeng_migrator",
	)
	if err != nil {
		t.Fatalf("compile production current APP ACL catalog contract: %v", err)
	}
	if got := len(contract.AuxiliaryPrivileges); got != 3 {
		t.Fatalf("current auxiliary privileges = %d, want 3", got)
	}
	for _, privilege := range contract.Privileges {
		if privilege.Subject != AppACLSubjectCenterRuntime && privilege.Subject != AppACLSubjectPlatformAdmin {
			t.Fatalf("frozen application privilege body contains auxiliary subject: %#v", privilege)
		}
	}
}

func recordsAuthorityExpectedAppACLObjects() []AppACLManagedObjectR1 {
	return []AppACLManagedObjectR1{{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     "public",
		ObjectIdentity: "record_platform_compose_membership_heartbeat(bytea)",
	}}
}

func recordsAuthorityExpectedFunctionContracts() []AppACLCurrentFunctionContract {
	return []AppACLCurrentFunctionContract{{
		SchemaName:      "public",
		Identity:        "record_platform_compose_membership_heartbeat(bytea)",
		Kind:            "f",
		SecurityDefiner: true,
		Config:          []string{"search_path=pg_catalog"},
	}}
}
