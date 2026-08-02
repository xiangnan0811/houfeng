package migrate

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"houfeng/db/migrations"
)

func TestCompileAppACLCurrentSourceContractRejectsMissingFutureFragment(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}

	_, err := compileAppACLCurrentSourceContract(fsys, nil)
	if err == nil || !strings.Contains(err.Error(), `migration "0052_future.sql" has no current APP ACL fragment`) {
		t.Fatalf("compileAppACLCurrentSourceContract() error = %v, want missing-fragment rejection", err)
	}
}

func TestCompileAppACLCurrentSourceContractAcceptsRegisteredFutureMigration(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}

	contract, err := compileAppACLCurrentSourceContract(fsys, []AppACLCurrentMigrationFragment{{
		Migration: "0052_future.sql",
		Privileges: func(string) []AppACLPrivilege {
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("compileAppACLCurrentSourceContract() error = %v", err)
	}
	if got := contract.sources.names[len(contract.sources.names)-1]; got != "0052_future.sql" {
		t.Fatalf("last current migration = %q, want 0052_future.sql", got)
	}
}

func TestCompileAppACLCurrentSourceContractRejectsInvalidFragments(t *testing.T) {
	const futureMigration = "0052_future.sql"
	newTable := AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     "public",
		ObjectIdentity: "future_records",
	}
	newFunction := AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     "public",
		ObjectIdentity: "future_function()",
	}
	newTablePrivilege := AppACLPrivilege{
		Subject:        AppACLSubjectCenterRuntime,
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     "public",
		ObjectIdentity: "future_records",
		Privilege:      AppACLPrivilegeSelect,
	}
	emptyPrivileges := func(string) []AppACLPrivilege { return nil }

	for _, tc := range []struct {
		name      string
		fragments []AppACLCurrentMigrationFragment
		want      string
	}{
		{
			name: "duplicate_fragments",
			fragments: []AppACLCurrentMigrationFragment{
				{Migration: futureMigration, Privileges: emptyPrivileges},
				{Migration: futureMigration, Privileges: emptyPrivileges},
			},
			want: "duplicate current APP ACL fragment",
		},
		{
			name: "fragment_for_absent_migration",
			fragments: []AppACLCurrentMigrationFragment{
				{Migration: futureMigration, Privileges: emptyPrivileges},
				{Migration: "0053_absent.sql", Privileges: emptyPrivileges},
			},
			want: "is not present in the current migration sources",
		},
		{
			name: "duplicate_objects",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration:  futureMigration,
				Objects:    []AppACLManagedObjectR1{newTable, newTable},
				Privileges: emptyPrivileges,
			}},
			want: "duplicate managed object",
		},
		{
			name: "duplicate_privileges",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration: futureMigration,
				Objects:   []AppACLManagedObjectR1{newTable},
				Privileges: func(string) []AppACLPrivilege {
					return []AppACLPrivilege{newTablePrivilege, newTablePrivilege}
				},
			}},
			want: "duplicate canonical privilege tuple",
		},
		{
			name: "unknown_subject",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration: futureMigration,
				Objects:   []AppACLManagedObjectR1{newTable},
				Privileges: func(string) []AppACLPrivilege {
					privilege := newTablePrivilege
					privilege.Subject = AppACLSubject("unknown")
					return []AppACLPrivilege{privilege}
				},
			}},
			want: "unknown ACL subject",
		},
		{
			name: "privilege_for_unmanaged_object",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration: futureMigration,
				Privileges: func(string) []AppACLPrivilege {
					return []AppACLPrivilege{newTablePrivilege}
				},
			}},
			want: "privilege references unmanaged object",
		},
		{
			name: "hardening_for_unmanaged_function",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration:  futureMigration,
				Privileges: emptyPrivileges,
				Functions: []AppACLCurrentFunctionContract{{
					SchemaName: "public",
					Identity:   "future_function()",
					Kind:       "f",
				}},
			}},
			want: "function hardening references unmanaged function",
		},
		{
			name: "managed_function_without_hardening",
			fragments: []AppACLCurrentMigrationFragment{{
				Migration:  futureMigration,
				Objects:    []AppACLManagedObjectR1{newFunction},
				Privileges: emptyPrivileges,
			}},
			want: "managed function has no hardening contract",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := appACLCurrentTestMigrationFS(t)
			fsys[futureMigration] = &fstest.MapFile{Data: []byte("select 'future';")}

			_, err := compileAppACLCurrentSourceContract(fsys, tc.fragments)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("compileAppACLCurrentSourceContract() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

func appACLCurrentTestMigrationFS(t *testing.T) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	fsys := make(fstest.MapFS, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			t.Fatalf("read embedded migration %q: %v", entry.Name(), err)
		}
		fsys[entry.Name()] = &fstest.MapFile{Data: append([]byte(nil), data...)}
	}
	return fsys
}

func TestCompileAppACLCurrentCatalogContractMergesFragment(t *testing.T) {
	newTable, newFunction, privilege, function := appACLCurrentCatalogTestExtension()
	source := appACLCurrentTestSourceContract(t, []AppACLCurrentMigrationFragment{{
		Migration: "0052_future.sql",
		Objects:   []AppACLManagedObjectR1{newTable, newFunction},
		Privileges: func(string) []AppACLPrivilege {
			return []AppACLPrivilege{privilege}
		},
		Functions: []AppACLCurrentFunctionContract{function},
	}})

	contract, err := compileAppACLCurrentCatalogContract(
		source,
		"houfeng",
		appACLCurrentCatalogTestBindings(),
		"houfeng_migrator",
	)
	if err != nil {
		t.Fatalf("compileAppACLCurrentCatalogContract() error = %v", err)
	}
	if len(contract.ManagedObjects) != 87 || !containsAppACLCurrentManagedObject(contract.ManagedObjects, newTable) || !containsAppACLCurrentManagedObject(contract.ManagedObjects, newFunction) {
		t.Fatalf("current managed objects = %d, want frozen 85 plus exact extension objects", len(contract.ManagedObjects))
	}
	if len(contract.Privileges) != appACLEffectiveCatalogR1PrivilegeCount+1 || !containsAppACLCurrentPrivilege(contract.Privileges, privilege) {
		t.Fatalf("current privileges = %d, want frozen %d plus exact extension privilege", len(contract.Privileges), appACLEffectiveCatalogR1PrivilegeCount)
	}
	wantFunction := appACLEffectiveCatalogFunctionContract{
		SchemaName:      function.SchemaName,
		Identity:        function.Identity,
		OwnerRole:       "houfeng_migrator",
		Kind:            function.Kind,
		SecurityDefiner: function.SecurityDefiner,
		Config:          function.Config,
	}
	if len(contract.ExpectedFunctions) != 3 || !containsAppACLCurrentFunction(contract.ExpectedFunctions, wantFunction) {
		t.Fatalf("current expected functions = %#v, want frozen projectors plus %#v", contract.ExpectedFunctions, wantFunction)
	}
}

func TestCompileAppACLCurrentCatalogContractDefensivelyCopiesInputs(t *testing.T) {
	newTable, newFunction, privilege, function := appACLCurrentCatalogTestExtension()
	objects := []AppACLManagedObjectR1{newTable, newFunction}
	privileges := []AppACLPrivilege{privilege}
	functions := []AppACLCurrentFunctionContract{function}
	source := appACLCurrentTestSourceContract(t, []AppACLCurrentMigrationFragment{{
		Migration: "0052_future.sql",
		Objects:   objects,
		Privileges: func(string) []AppACLPrivilege {
			return privileges
		},
		Functions: functions,
	}})
	bindings := appACLCurrentCatalogTestBindings()

	contract, err := compileAppACLCurrentCatalogContract(source, "houfeng", bindings, "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	objects[0].ObjectIdentity = "mutated_input"
	privileges[0].ObjectIdentity = "mutated_input"
	functions[0].Identity = "mutated_input()"
	functions[0].Config[0] = "search_path=mutated_input"
	bindings[0].CatalogRole = "mutated_input"
	source.fragments[0].Objects[0].ObjectIdentity = "mutated_source"
	source.fragments[0].Functions[0].Config[0] = "search_path=mutated_source"

	if !containsAppACLCurrentManagedObject(contract.ManagedObjects, newTable) || !containsAppACLCurrentPrivilege(contract.Privileges, privilege) {
		t.Fatalf("compiled current contract changed after caller mutation")
	}
	wantFunction := appACLEffectiveCatalogFunctionContract{
		SchemaName:      function.SchemaName,
		Identity:        function.Identity,
		OwnerRole:       "houfeng_migrator",
		Kind:            function.Kind,
		SecurityDefiner: function.SecurityDefiner,
		Config:          []string{"search_path=pg_catalog"},
	}
	if !containsAppACLCurrentFunction(contract.ExpectedFunctions, wantFunction) {
		t.Fatalf("compiled current function hardening changed after caller mutation: %#v", contract.ExpectedFunctions)
	}
	if got := contract.RoleBindings[0].CatalogRole; got != "houfeng_center_runtime" {
		t.Fatalf("compiled current role binding changed to %q after caller mutation", got)
	}
}

func TestCompileAppACLCurrentCatalogContractRejectsDuplicateBaseValues(t *testing.T) {
	base, err := compileAppACLCurrentSourceContract(appACLCurrentTestMigrationFS(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	basePrivilege := appACLPrivilegesR1("houfeng")[0]
	for _, tc := range []struct {
		name     string
		fragment AppACLCurrentMigrationFragment
		want     string
	}{
		{
			name: "managed_object",
			fragment: AppACLCurrentMigrationFragment{
				Migration: "0052_synthetic.sql",
				Objects: []AppACLManagedObjectR1{{
					ObjectClass:    AppACLObjectClassTable,
					SchemaName:     "public",
					ObjectIdentity: "schema_migrations",
				}},
				Privileges: func(string) []AppACLPrivilege { return nil },
			},
			want: "duplicate managed object",
		},
		{
			name: "privilege",
			fragment: AppACLCurrentMigrationFragment{
				Migration: "0052_synthetic.sql",
				Privileges: func(string) []AppACLPrivilege {
					return []AppACLPrivilege{basePrivilege}
				},
			},
			want: "duplicate canonical privilege tuple",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := base
			source.fragments = []AppACLCurrentMigrationFragment{tc.fragment}
			_, err := compileAppACLCurrentCatalogContract(
				source,
				"houfeng",
				appACLCurrentCatalogTestBindings(),
				"houfeng_migrator",
			)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("compileAppACLCurrentCatalogContract() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

func TestCompileAppACLCurrentCatalogContractExplicitEmptyFragmentChangesOnlySources(t *testing.T) {
	baseSource, err := compileAppACLCurrentSourceContract(appACLCurrentTestMigrationFS(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	futureSource := appACLCurrentTestSourceContract(t, []AppACLCurrentMigrationFragment{{
		Migration:  "0052_future.sql",
		Privileges: func(string) []AppACLPrivilege { return nil },
	}})
	if reflect.DeepEqual(baseSource.sources.canonicalSet, futureSource.sources.canonicalSet) {
		t.Fatal("explicit empty future fragment did not change the canonical migration source set")
	}

	baseCatalog, err := compileAppACLCurrentCatalogContract(baseSource, "houfeng", appACLCurrentCatalogTestBindings(), "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	futureCatalog, err := compileAppACLCurrentCatalogContract(futureSource, "houfeng", appACLCurrentCatalogTestBindings(), "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseCatalog, futureCatalog) {
		t.Fatalf("explicit empty fragment changed catalog contract\nbase: %#v\nfuture: %#v", baseCatalog, futureCatalog)
	}
}

func appACLCurrentCatalogTestExtension() (
	AppACLManagedObjectR1,
	AppACLManagedObjectR1,
	AppACLPrivilege,
	AppACLCurrentFunctionContract,
) {
	return AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: "future_records",
		}, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     "public",
			ObjectIdentity: "future_function()",
		}, AppACLPrivilege{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: "future_records",
			Privilege:      AppACLPrivilegeSelect,
		}, AppACLCurrentFunctionContract{
			SchemaName:      "public",
			Identity:        "future_function()",
			Kind:            "f",
			SecurityDefiner: true,
			Config:          []string{"search_path=pg_catalog"},
		}
}

func appACLCurrentTestSourceContract(t *testing.T, fragments []AppACLCurrentMigrationFragment) appACLCurrentSourceContract {
	t.Helper()
	fsys := appACLCurrentTestMigrationFS(t)
	fsys["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}
	contract, err := compileAppACLCurrentSourceContract(fsys, fragments)
	if err != nil {
		t.Fatalf("compile current test source contract: %v", err)
	}
	return contract
}

func appACLCurrentCatalogTestBindings() []AppACLRoleBinding {
	return []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
}

func appACLCurrentCatalogTestContract(t *testing.T) appACLEffectiveCatalogContract {
	t.Helper()
	newTable, newFunction, privilege, function := appACLCurrentCatalogTestExtension()
	source := appACLCurrentTestSourceContract(t, []AppACLCurrentMigrationFragment{{
		Migration: "0052_future.sql",
		Objects:   []AppACLManagedObjectR1{newTable, newFunction},
		Privileges: func(string) []AppACLPrivilege {
			return []AppACLPrivilege{privilege}
		},
		Functions: []AppACLCurrentFunctionContract{function},
	}})
	contract, err := compileAppACLCurrentCatalogContract(
		source,
		"houfeng",
		appACLCurrentCatalogTestBindings(),
		"houfeng_migrator",
	)
	if err != nil {
		t.Fatalf("compile current catalog test contract: %v", err)
	}
	return contract
}

func containsAppACLCurrentManagedObject(objects []AppACLManagedObjectR1, want AppACLManagedObjectR1) bool {
	for _, object := range objects {
		if object == want {
			return true
		}
	}
	return false
}

func containsAppACLCurrentPrivilege(privileges []AppACLPrivilege, want AppACLPrivilege) bool {
	for _, privilege := range privileges {
		if privilege == want {
			return true
		}
	}
	return false
}

func containsAppACLCurrentFunction(functions []appACLEffectiveCatalogFunctionContract, want appACLEffectiveCatalogFunctionContract) bool {
	for _, function := range functions {
		if function.SchemaName == want.SchemaName &&
			function.Identity == want.Identity &&
			function.OwnerRole == want.OwnerRole &&
			function.Kind == want.Kind &&
			function.SecurityDefiner == want.SecurityDefiner &&
			reflect.DeepEqual(function.Config, want.Config) {
			return true
		}
	}
	return false
}
