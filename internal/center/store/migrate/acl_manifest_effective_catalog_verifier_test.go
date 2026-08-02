package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestAppACLManagedSurfaceR1UsesFixedMigrationInventory(t *testing.T) {
	payload, err := os.ReadFile("acl_manifest_managed_surface.go")
	if err != nil {
		t.Fatalf("read managed-surface source: %v", err)
	}
	if strings.Contains(string(payload), "appACLPrivilegesR1(") {
		t.Fatal("managed surface must come from the fixed 0001…0051 migration inventory, not runtime/admin privilege tuples")
	}
}

func TestAppACLProjectorInventoryR1IsSharedAndClosed(t *testing.T) {
	projectors := appACLProjectorFunctionsR1()
	if projectors[0].schemaName != appACLManagedPublicSchemaR1 || projectors[0].identity != "record_platform_cas_contract_activation_projection(bytea)" ||
		projectors[1].schemaName != appACLManagedPublicSchemaR1 || projectors[1].identity != "record_platform_cas_domain_rotation_projection(bytea)" {
		t.Fatalf("r1 projector inventory = %#v, want exactly the two public bytea projectors", projectors)
	}
	payload, err := os.ReadFile("acl_manifest_effective_catalog_verifier.go")
	if err != nil {
		t.Fatalf("read catalog verifier source: %v", err)
	}
	source := string(payload)
	if !strings.Contains(source, "appACLProjectorFunctionsR1()") {
		t.Fatal("catalog verifier must derive projector identities from the managed-surface inventory")
	}
	for _, identity := range []string{
		"public.record_platform_cas_contract_activation_projection(bytea)",
		"public.record_platform_cas_domain_rotation_projection(bytea)",
	} {
		if strings.Contains(source, identity) {
			t.Fatalf("catalog verifier must not duplicate managed projector identity %q", identity)
		}
	}
}

func TestCompileAppACLManagedSurfaceR1IncludesFullMigrationOwnedInventory(t *testing.T) {
	surface, err := CompileAppACLManagedSurfaceR1("houfeng")
	if err != nil {
		t.Fatalf("CompileAppACLManagedSurfaceR1() error = %v", err)
	}
	counts := make(map[AppACLObjectClass]int)
	objects := make(map[AppACLManagedObjectR1]struct{}, len(surface.Objects))
	for _, object := range surface.Objects {
		counts[object.ObjectClass]++
		objects[object] = struct{}{}
	}
	if want := map[AppACLObjectClass]int{
		AppACLObjectClassDatabase: 1,
		AppACLObjectClassSchema:   2,
		AppACLObjectClassTable:    65,
		AppACLObjectClassView:     3,
		AppACLObjectClassSequence: 4,
		AppACLObjectClassFunction: 10,
	}; len(surface.Objects) != 85 || !mapsEqual(counts, want) {
		t.Fatalf("managed surface counts = %#v (%d objects), want %#v (85 objects)", counts, len(surface.Objects), want)
	}
	for _, object := range []AppACLManagedObjectR1{
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "schema_migrations"},
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "app_acl_manifest_revisions"},
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "app_acl_manifest_head"},
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_purge_operations"},
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_platform_domain_attestations"},
		{ObjectClass: AppACLObjectClassView, SchemaName: "public", ObjectIdentity: "asset_decision_records_with_counts"},
		{ObjectClass: AppACLObjectClassSequence, SchemaName: "public", ObjectIdentity: "record_outbox_outbox_row_id_seq"},
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "record_platform_internal", ObjectIdentity: "reject_acl_manifest_revision_mutation()"},
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "public", ObjectIdentity: "record_platform_cas_domain_rotation_projection(bytea)"},
	} {
		if _, ok := objects[object]; !ok {
			t.Fatalf("managed surface is missing migration-owned object %#v", object)
		}
	}
	for _, object := range []AppACLManagedObjectR1{
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "record_platform_internal", ObjectIdentity: "record_platform_projection_read_bytes_v1(p_command bytea, p_offset integer, p_length integer)"},
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "record_platform_internal", ObjectIdentity: "record_platform_projection_read_uint64_v1(p_command bytea, p_offset integer)"},
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "record_platform_internal", ObjectIdentity: "record_platform_projection_read_token_v1(p_command bytea, p_offset integer, p_prefix text)"},
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "record_platform_internal", ObjectIdentity: "record_platform_projection_read_profile_v1(p_command bytea, p_offset integer)"},
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "record_platform_internal", ObjectIdentity: "record_platform_projection_validate_header_v1(p_command bytea, p_operation integer, p_field_count integer, p_exact_length integer)"},
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "record_platform_internal", ObjectIdentity: "record_platform_projection_cas_receipt_v1(p_command bytea)"},
	} {
		if _, ok := objects[object]; !ok {
			t.Fatalf("managed surface is missing exact PostgreSQL function identity %#v", object)
		}
	}
}

func TestPostgresAppACLEffectiveCatalogReaderR1UsesManagedScope(t *testing.T) {
	payload, err := os.ReadFile("acl_manifest_effective_catalog_postgres.go")
	if err != nil {
		t.Fatalf("read PostgreSQL catalog reader source: %v", err)
	}
	source := string(payload)
	if strings.Contains(source, "namespace.nspname !~ '^pg_'") {
		t.Fatal("managed APP catalog reader must not scan every persistent non-system schema")
	}
	for _, want := range []string{
		"namespace.nspname = any($",
		"scope.managedSchemaNames()",
		"default_acl.defaclrole = (select role.oid from pg_catalog.pg_roles role where role.rolname = $1)",
		"default_acl.defaclnamespace = 0",
		"pg_catalog.pg_depend dependency",
		"dependency.deptype = 'e'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("managed APP catalog reader is missing scoped catalog contract %q", want)
		}
	}
}

func mapsEqual[K comparable, V comparable](left, right map[K]V) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if rightValue, ok := right[key]; !ok || rightValue != leftValue {
			return false
		}
	}
	return true
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1AcceptsCompilerDerivedCompleteContract(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)

	if err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input); err != nil {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v", err)
	}
}

func TestAppACLCurrentCatalogVerifierAcceptsCompleteExtension(t *testing.T) {
	input, snapshot := validAppACLCurrentCatalogVerifierFixture(t)
	if err := verifyAppACLEffectiveCatalogSnapshot(snapshot, input); err != nil {
		t.Fatalf("verifyAppACLEffectiveCatalogSnapshot() error = %v", err)
	}
}

func TestAppACLCurrentCatalogVerifierRejectsMissingOrDriftingExtension(t *testing.T) {
	newTable, _, _, _ := appACLCurrentCatalogTestExtension()
	for _, tc := range []struct {
		name   string
		mutate func(*AppACLEffectiveCatalogSnapshotR1)
		want   string
	}{
		{
			name: "missing_table_owner",
			mutate: func(snapshot *AppACLEffectiveCatalogSnapshotR1) {
				for index, owner := range snapshot.Owners {
					if owner.ObjectClass == newTable.ObjectClass && owner.SchemaName == newTable.SchemaName && owner.ObjectIdentity == newTable.ObjectIdentity {
						snapshot.Owners = append(snapshot.Owners[:index], snapshot.Owners[index+1:]...)
						return
					}
				}
			},
			want: "managed object owner is missing",
		},
		{
			name: "missing_function",
			mutate: func(snapshot *AppACLEffectiveCatalogSnapshotR1) {
				for index, function := range snapshot.Functions {
					if function.Identity == "public.future_function()" {
						snapshot.Functions = append(snapshot.Functions[:index], snapshot.Functions[index+1:]...)
						return
					}
				}
			},
			want: "has 0 overloads",
		},
		{
			name: "function_hardening",
			mutate: func(snapshot *AppACLEffectiveCatalogSnapshotR1) {
				for index := range snapshot.Functions {
					if snapshot.Functions[index].Identity == "public.future_function()" {
						snapshot.Functions[index].SecurityDefiner = false
						return
					}
				}
			},
			want: "SECURITY DEFINER",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, snapshot := validAppACLCurrentCatalogVerifierFixture(t)
			tc.mutate(&snapshot)
			err := verifyAppACLEffectiveCatalogSnapshot(snapshot, input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("verifyAppACLEffectiveCatalogSnapshot() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1RejectsRuntimeProjectorExecuteGrant(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	runtimeRole := input.Contract.RoleBindings[0].CatalogRole
	projectorGrant := AppACLEffectiveCatalogPrivilegeObservationR1{
		Grantee:        runtimeRole,
		ObjectClass:    AppACLObjectClassFunction,
		ObjectIdentity: "public.record_platform_cas_contract_activation_projection(bytea)",
		Privilege:      AppACLPrivilegeExecute,
	}
	snapshot.DirectPrivileges = append(snapshot.DirectPrivileges, projectorGrant)
	snapshot.EffectivePrivileges = append(snapshot.EffectivePrivileges, projectorGrant)

	err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input)
	if err == nil || !strings.Contains(err.Error(), "unexpected direct") {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want projector EXECUTE rejection", err)
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1RejectsInvalidPGCryptoPlacement(t *testing.T) {
	for name, extension := range map[string]AppACLEffectiveCatalogExtensionR1{
		"missing": {},
		"public":  {ExtensionName: "pgcrypto", SchemaName: appACLManagedPublicSchemaR1},
	} {
		t.Run(name, func(t *testing.T) {
			input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
			snapshot.PGCryptoExtension = extension

			err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input)
			if err == nil || !strings.Contains(err.Error(), "pgcrypto extension") {
				t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want pgcrypto placement rejection", err)
			}
		})
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1RejectsProjectorTextOverload(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	want := input.ExpectedFunctions[0]
	name := strings.TrimSuffix(strings.TrimPrefix(want.Identity, "public."), "(bytea)")
	snapshot.Functions = append(snapshot.Functions, AppACLEffectiveCatalogFunctionR1{
		SchemaName:        appACLManagedPublicSchemaR1,
		Name:              name,
		IdentityArguments: "text",
		Identity:          appACLManagedPublicSchemaR1 + "." + name + "(text)",
		OwnerRole:         want.OwnerRole,
		Kind:              "f",
		SecurityDefiner:   true,
		Config:            []string{"search_path=pg_catalog"},
	})

	err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input)
	if err == nil || !strings.Contains(err.Error(), "has 2 overloads") {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want projector overload rejection", err)
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1AcceptsUnrelatedSchemaDefaultACL(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	snapshot.DefaultACLs = append(snapshot.DefaultACLs, AppACLEffectiveCatalogDefaultACLR1{
		OwnerRole:  "unrelated_owner",
		SchemaName: "unrelated_schema",
		ObjectType: "r",
		Grantee:    "unrelated_grantee",
		Privilege:  AppACLPrivilegeSelect,
	})

	if err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input); err != nil {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want unrelated-schema default ACL acceptance", err)
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1RejectsMissingManagedOwner(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	snapshot.Owners = nil

	err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input)
	if err == nil || !strings.Contains(err.Error(), "managed object owner") {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want managed-owner rejection", err)
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1RejectsManagedRelationOwnerAndSchemaDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, snapshot *AppACLEffectiveCatalogSnapshotR1)
		want   string
	}{
		{
			name: "wrong owner",
			mutate: func(t *testing.T, snapshot *AppACLEffectiveCatalogSnapshotR1) {
				findAppACLEffectiveCatalogSnapshotOwnerR1(t, snapshot, AppACLObjectClassTable, "public", "schema_migrations").OwnerRole = "unexpected_owner"
			},
			want: "does not match migrator role",
		},
		{
			name: "wrong schema",
			mutate: func(t *testing.T, snapshot *AppACLEffectiveCatalogSnapshotR1) {
				findAppACLEffectiveCatalogSnapshotOwnerR1(t, snapshot, AppACLObjectClassTable, "public", "schema_migrations").SchemaName = appACLManagedInternalSchemaR1
			},
			want: "unexpected managed object owner",
		},
		{
			name: "missing expected relation",
			mutate: func(t *testing.T, snapshot *AppACLEffectiveCatalogSnapshotR1) {
				for index, owner := range snapshot.Owners {
					if owner.ObjectClass == AppACLObjectClassTable && owner.SchemaName == "public" && owner.ObjectIdentity == "schema_migrations" {
						snapshot.Owners = append(snapshot.Owners[:index], snapshot.Owners[index+1:]...)
						return
					}
				}
				t.Fatal("managed relation public.schema_migrations is missing from verifier fixture")
			},
			want: "managed object owner is missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
			tt.mutate(t, &snapshot)

			err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1AcceptsPublicDatabaseOwnerWithMigratorDatabaseOwner(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	setAppACLEffectiveCatalogSnapshotOwnerR1(t, &snapshot, AppACLObjectClassSchema, "public", "public", "pg_database_owner")

	if err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input); err != nil {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want pg_database_owner public schema acceptance", err)
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1RejectsPublicDatabaseOwnerWithoutMigratorDatabaseOwner(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	setAppACLEffectiveCatalogSnapshotOwnerR1(t, &snapshot, AppACLObjectClassSchema, "public", "public", "pg_database_owner")
	setAppACLEffectiveCatalogSnapshotOwnerR1(t, &snapshot, AppACLObjectClassDatabase, "", input.Contract.DatabaseName, "another_database_owner")

	err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input)
	if err == nil || !strings.Contains(err.Error(), "database owner") {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want database-owner rejection", err)
	}
}

func TestVerifyAppACLEffectiveCatalogSnapshotR1RejectsOtherPublicSchemaOwner(t *testing.T) {
	input, snapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	setAppACLEffectiveCatalogSnapshotOwnerR1(t, &snapshot, AppACLObjectClassSchema, "public", "public", "another_public_owner")

	err := VerifyAppACLEffectiveCatalogSnapshotR1(snapshot, input)
	if err == nil || !strings.Contains(err.Error(), "public schema owner") {
		t.Fatalf("VerifyAppACLEffectiveCatalogSnapshotR1() error = %v, want public-schema-owner rejection", err)
	}
}

func setAppACLEffectiveCatalogSnapshotOwnerR1(
	t *testing.T,
	snapshot *AppACLEffectiveCatalogSnapshotR1,
	objectClass AppACLObjectClass,
	schemaName string,
	objectIdentity string,
	ownerRole string,
) {
	t.Helper()
	findAppACLEffectiveCatalogSnapshotOwnerR1(t, snapshot, objectClass, schemaName, objectIdentity).OwnerRole = ownerRole
}

func findAppACLEffectiveCatalogSnapshotOwnerR1(
	t *testing.T,
	snapshot *AppACLEffectiveCatalogSnapshotR1,
	objectClass AppACLObjectClass,
	schemaName string,
	objectIdentity string,
) *AppACLEffectiveCatalogObjectOwnerR1 {
	t.Helper()
	for index := range snapshot.Owners {
		owner := &snapshot.Owners[index]
		if owner.ObjectClass == objectClass && owner.SchemaName == schemaName && owner.ObjectIdentity == objectIdentity {
			return owner
		}
	}
	t.Fatalf("managed owner %s.%s is missing from fixture", schemaName, objectIdentity)
	return nil
}

func validAppACLEffectiveCatalogVerifierFixture(t *testing.T) (AppACLEffectiveCatalogVerifierInputR1, AppACLEffectiveCatalogSnapshotR1) {
	t.Helper()
	contract, err := CompileAppACLEffectiveCatalogContractR1("houfeng", []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	})
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, "houfeng_migrator")
	if err != nil {
		t.Fatalf("NewAppACLEffectiveCatalogVerifierInputR1() error = %v", err)
	}

	roleBySubject := make(map[AppACLSubject]string, len(contract.RoleBindings))
	for _, binding := range contract.RoleBindings {
		roleBySubject[binding.Subject] = binding.CatalogRole
	}
	snapshot := AppACLEffectiveCatalogSnapshotR1{
		DatabaseName: contract.DatabaseName,
		PGCryptoExtension: AppACLEffectiveCatalogExtensionR1{
			ExtensionName: "pgcrypto",
			SchemaName:    appACLManagedInternalSchemaR1,
		},
		Roles: []AppACLEffectiveCatalogRoleStateR1{
			{Name: roleBySubject[AppACLSubjectCenterRuntime], Login: true},
			{Name: roleBySubject[AppACLSubjectPlatformAdmin], Login: true},
			{Name: input.MigratorRole, Login: true},
		},
	}
	for _, privilege := range contract.Privileges {
		observation := AppACLEffectiveCatalogPrivilegeObservationR1{
			Grantee:        roleBySubject[privilege.Subject],
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
			ColumnName:     privilege.ColumnName,
			Privilege:      privilege.Privilege,
		}
		snapshot.DirectPrivileges = append(snapshot.DirectPrivileges, observation)
		snapshot.EffectivePrivileges = append(snapshot.EffectivePrivileges, observation)
	}
	for _, expected := range input.ExpectedFunctions {
		name := strings.TrimSuffix(strings.TrimPrefix(expected.Identity, "public."), "(bytea)")
		snapshot.Functions = append(snapshot.Functions, AppACLEffectiveCatalogFunctionR1{
			SchemaName:        "public",
			Name:              name,
			IdentityArguments: "bytea",
			Identity:          expected.Identity,
			OwnerRole:         expected.OwnerRole,
			Kind:              "f",
			SecurityDefiner:   true,
			Config:            []string{"search_path=pg_catalog"},
		})
	}
	surface, err := CompileAppACLManagedSurfaceR1(contract.DatabaseName)
	if err != nil {
		t.Fatalf("CompileAppACLManagedSurfaceR1() error = %v", err)
	}
	for _, object := range surface.Objects {
		snapshot.Owners = append(snapshot.Owners, AppACLEffectiveCatalogObjectOwnerR1{
			ObjectClass:    object.ObjectClass,
			SchemaName:     object.SchemaName,
			ObjectIdentity: object.ObjectIdentity,
			OwnerRole:      input.MigratorRole,
		})
	}
	return input, snapshot
}

func validAppACLCurrentCatalogVerifierFixture(t *testing.T) (appACLEffectiveCatalogVerifierInput, AppACLEffectiveCatalogSnapshotR1) {
	t.Helper()
	contract := appACLCurrentCatalogTestContract(t)
	input := appACLEffectiveCatalogVerifierInput{
		Contract:     contract,
		MigratorRole: "houfeng_migrator",
	}
	roleBySubject := make(map[AppACLSubject]string, len(contract.RoleBindings))
	for _, binding := range contract.RoleBindings {
		roleBySubject[binding.Subject] = binding.CatalogRole
	}
	snapshot := AppACLEffectiveCatalogSnapshotR1{
		DatabaseName: contract.DatabaseName,
		PGCryptoExtension: AppACLEffectiveCatalogExtensionR1{
			ExtensionName: "pgcrypto",
			SchemaName:    appACLManagedInternalSchemaR1,
		},
		Roles: []AppACLEffectiveCatalogRoleStateR1{
			{Name: roleBySubject[AppACLSubjectCenterRuntime], Login: true},
			{Name: roleBySubject[AppACLSubjectPlatformAdmin], Login: true},
			{Name: input.MigratorRole, Login: true},
		},
	}
	for _, privilege := range contract.Privileges {
		observation := AppACLEffectiveCatalogPrivilegeObservationR1{
			Grantee:        roleBySubject[privilege.Subject],
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
			ColumnName:     privilege.ColumnName,
			Privilege:      privilege.Privilege,
		}
		snapshot.DirectPrivileges = append(snapshot.DirectPrivileges, observation)
		snapshot.EffectivePrivileges = append(snapshot.EffectivePrivileges, observation)
	}
	for _, expected := range contract.ExpectedFunctions {
		name, arguments, found := strings.Cut(expected.Identity, "(")
		if !found {
			t.Fatalf("expected function identity %q has no arguments", expected.Identity)
		}
		arguments = strings.TrimSuffix(arguments, ")")
		snapshot.Functions = append(snapshot.Functions, AppACLEffectiveCatalogFunctionR1{
			SchemaName:        expected.SchemaName,
			Name:              name,
			IdentityArguments: arguments,
			Identity:          expected.SchemaName + "." + expected.Identity,
			OwnerRole:         expected.OwnerRole,
			Kind:              expected.Kind,
			SecurityDefiner:   expected.SecurityDefiner,
			Config:            append([]string(nil), expected.Config...),
		})
	}
	for _, object := range contract.ManagedObjects {
		snapshot.Owners = append(snapshot.Owners, AppACLEffectiveCatalogObjectOwnerR1{
			ObjectClass:    object.ObjectClass,
			SchemaName:     object.SchemaName,
			ObjectIdentity: object.ObjectIdentity,
			OwnerRole:      input.MigratorRole,
		})
	}
	return input, snapshot
}
