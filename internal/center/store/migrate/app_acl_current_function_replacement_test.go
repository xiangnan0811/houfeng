package migrate

import (
	"reflect"
	"testing"
	"testing/fstest"

	"houfeng/db/migrations"
)

func TestAppACLCurrentFunctionReplacementContract(t *testing.T) {
	before := AppACLCurrentFunctionContract{SchemaName: "public", Identity: "replacement_fixture()", Kind: "f", Config: []string{"search_path=pg_catalog"}}
	after := before
	after.Config = append([]string(nil), before.Config...)
	after.SecurityDefiner = true
	object := AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: before.SchemaName, ObjectIdentity: before.Identity}
	newFragments := func() []AppACLCurrentMigrationFragment {
		fragments := []AppACLCurrentMigrationFragment{
			{Migration: "0052_replacement_before.sql", Objects: []AppACLManagedObjectR1{object}, Functions: []AppACLCurrentFunctionContract{before}, Privileges: func(string) []AppACLPrivilege { return nil }},
			{Migration: "0053_replacement_after.sql", FunctionReplacements: []AppACLCurrentFunctionReplacement{{Before: before, After: after}}, Privileges: func(string) []AppACLPrivilege { return nil }},
		}
		for i := range fragments {
			fragments[i] = cloneAppACLCurrentMigrationFragment(fragments[i])
		}
		return fragments
	}
	for _, tc := range []struct {
		name   string
		change func([]AppACLCurrentMigrationFragment)
	}{
		{"valid", func([]AppACLCurrentMigrationFragment) {}},
		{"absent predecessor", func(f []AppACLCurrentMigrationFragment) { f[0].Objects, f[0].Functions = nil, nil }},
		{"unhardened predecessor", func(f []AppACLCurrentMigrationFragment) { f[0].Functions = nil }},
		{"wrong predecessor security", func(f []AppACLCurrentMigrationFragment) { f[1].FunctionReplacements[0].Before.SecurityDefiner = true }},
		{"wrong predecessor kind", func(f []AppACLCurrentMigrationFragment) { f[1].FunctionReplacements[0].Before.Kind = "p" }},
		{"wrong predecessor config", func(f []AppACLCurrentMigrationFragment) {
			f[1].FunctionReplacements[0].Before.Config = []string{"search_path=public"}
		}},
		{"different identity", func(f []AppACLCurrentMigrationFragment) {
			f[1].FunctionReplacements[0].After.Identity = "other_fixture()"
		}},
		{"different schema", func(f []AppACLCurrentMigrationFragment) {
			f[1].FunctionReplacements[0].After.SchemaName = "other_schema"
		}},
		{"duplicate replacement", func(f []AppACLCurrentMigrationFragment) {
			f[1].FunctionReplacements = append(f[1].FunctionReplacements, f[1].FunctionReplacements[0])
		}},
		{"add and replace", func(f []AppACLCurrentMigrationFragment) { f[1].Objects = []AppACLManagedObjectR1{object} }},
		{"retire and replace", func(f []AppACLCurrentMigrationFragment) { f[1].RetiredObjects = []AppACLManagedObjectR1{object} }},
		{"redeclare and replace", func(f []AppACLCurrentMigrationFragment) { f[1].Functions = []AppACLCurrentFunctionContract{after} }},
		{"invalid before kind", func(f []AppACLCurrentMigrationFragment) { f[1].FunctionReplacements[0].Before.Kind = "invalid" }},
		{"invalid after kind", func(f []AppACLCurrentMigrationFragment) { f[1].FunctionReplacements[0].After.Kind = "invalid" }},
		{"invalid before config", func(f []AppACLCurrentMigrationFragment) {
			f[1].FunctionReplacements[0].Before.Config = []string{"broken"}
		}},
		{"invalid after config", func(f []AppACLCurrentMigrationFragment) {
			f[1].FunctionReplacements[0].After.Config = []string{"search_path="}
		}},
		{"duplicate config", func(f []AppACLCurrentMigrationFragment) {
			f[1].FunctionReplacements[0].After.Config = []string{"search_path=pg_catalog", "search_path=public"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fragments := newFragments()
			tc.change(fragments)
			fsys := appACLCurrentTestMigrationFS(t)
			for _, fragment := range fragments {
				fsys[fragment.Migration] = &fstest.MapFile{Data: []byte("select 1;")}
			}
			source, err := compileAppACLCurrentSourceContract(fsys, fragments)
			if tc.name == "valid" {
				if err != nil {
					t.Fatalf("valid replacement rejected: %v", err)
				}
				fragments[1].FunctionReplacements[0].Before.Config[0] = "mutated"
				fragments[1].FunctionReplacements[0].After.Config[0] = "mutated"
				got := source.fragments[1].FunctionReplacements[0]
				if got.Before.Config[0] != "search_path=pg_catalog" || got.After.Config[0] != "search_path=pg_catalog" {
					t.Fatal("source aliases input replacement")
				}
				catalog, err := compileAppACLCurrentCatalogContract(source, "houfeng", appACLCurrentCatalogTestBindings(), "houfeng_migrator")
				if err != nil {
					t.Fatal(err)
				}
				source.fragments[1].FunctionReplacements[0].After.Config[0] = "mutated again"
				found := false
				for _, function := range catalog.ExpectedFunctions {
					if function.SchemaName == "public" && function.Identity == "replacement_fixture()" {
						found = true
						if !function.SecurityDefiner || !reflect.DeepEqual(function.Config, []string{"search_path=pg_catalog"}) {
							t.Fatalf("catalog aliases source: %#v", function)
						}
					}
				}
				if !found {
					t.Fatal("replacement absent from catalog")
				}
			} else if err == nil {
				t.Fatal("invalid replacement accepted")
			}
		})
	}
}

func TestAppACLCurrentFunctionReplacementCloneIsolation(t *testing.T) {
	fragment := AppACLCurrentMigrationFragment{Migration: "0052_clone.sql", Privileges: func(string) []AppACLPrivilege { return nil }, FunctionReplacements: []AppACLCurrentFunctionReplacement{{
		Before: AppACLCurrentFunctionContract{Config: []string{"search_path=before"}},
		After:  AppACLCurrentFunctionContract{Config: []string{"search_path=after"}},
	}}}
	cloned := cloneAppACLCurrentMigrationFragment(fragment)
	compiled, err := compileAppACLCurrentMigrationFragment(fragment)
	if err != nil {
		t.Fatal(err)
	}
	compiledClone := cloneAppACLCurrentCompiledMigrationFragment(compiled)
	fragment.FunctionReplacements[0].Before.Config[0] = "changed"
	fragment.FunctionReplacements[0].After.Config[0] = "changed"
	if compiled.FunctionReplacements[0].Before.Config[0] != "search_path=before" || compiled.FunctionReplacements[0].After.Config[0] != "search_path=after" {
		t.Fatal("compiled fragment aliases input config")
	}
	compiled.FunctionReplacements[0].Before.Config[0] = "compiled changed"
	compiled.FunctionReplacements[0].After.Config[0] = "compiled changed"
	for name, got := range map[string]AppACLCurrentFunctionReplacement{"input clone": cloned.FunctionReplacements[0], "compiled clone": compiledClone.FunctionReplacements[0]} {
		if !reflect.DeepEqual(got.Before.Config, []string{"search_path=before"}) || !reflect.DeepEqual(got.After.Config, []string{"search_path=after"}) {
			t.Fatalf("%s aliases replacement config: %#v", name, got)
		}
	}
}

func TestAppACLCurrentFunctionReplacementRejectsWrongOwner(t *testing.T) {
	before := AppACLCurrentFunctionContract{SchemaName: "public", Identity: "replacement_owner_fixture()", Kind: "f", Config: []string{"search_path=pg_catalog"}}
	after := before
	after.SecurityDefiner = true
	object := AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: before.SchemaName, ObjectIdentity: before.Identity}
	contract := appACLEffectiveCatalogContract{
		ManagedObjects:    []AppACLManagedObjectR1{object},
		ExpectedFunctions: []appACLEffectiveCatalogFunctionContract{{SchemaName: before.SchemaName, Identity: before.Identity, OwnerRole: "other_owner", Kind: before.Kind, Config: before.Config}},
	}
	fragment := appACLCurrentCompiledMigrationFragment{Migration: "0053_replacement.sql", FunctionReplacements: []AppACLCurrentFunctionReplacement{{Before: before, After: after}}}
	if _, err := materializeAppACLCurrentFragments(&contract, []appACLCurrentCompiledMigrationFragment{fragment}, "houfeng", "houfeng_migrator"); err == nil {
		t.Fatal("replacement accepted predecessor owned by another role")
	}
}

func TestAppACLCurrentPrimarySubjectReplacementPrefixes(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	replacementCount := 0
	for _, fragment := range current.fragments {
		for _, replacement := range fragment.FunctionReplacements {
			replacementCount++
			primary := replacement.Before.SchemaName == "record_platform_internal" && replacement.Before.Identity == "validate_record_revision_primary_subject()" && !replacement.Before.SecurityDefiner && replacement.After.SecurityDefiner
			heartbeat := replacement.Before.SchemaName == "public" && replacement.Before.Identity == "record_platform_compose_membership_heartbeat(bytea)" && replacement.Before.SecurityDefiner && replacement.After.SecurityDefiner
			if fragment.Migration != recordDeletionSuccessorMigration || (!primary && !heartbeat) {
				t.Fatalf("unexpected replacement in %s: %#v", fragment.Migration, replacement)
			}
		}
	}
	if replacementCount != 2 {
		t.Fatalf("replacement count = %d, want 2", replacementCount)
	}
	c80 := appACLCurrentSourceAtProfile(t, current, appACLCurrentProfileP80)
	var first appACLCurrentSourceContract
	for _, fragment := range current.fragments {
		first.fragments = append(first.fragments, fragment)
		if fragment.Migration == "0052_create_records_core.sql" {
			break
		}
	}
	for _, tc := range []struct {
		name    string
		source  appACLCurrentSourceContract
		definer bool
	}{
		{"0052", first, false}, {"C80", c80, false}, {"C81", current, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract, err := compileAppACLCurrentCatalogContract(tc.source, appACLCurrentTransitionDatabase, appACLCurrentTransitionBindingsV2, appACLCurrentTransitionMigrator)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, function := range contract.ExpectedFunctions {
				if function.SchemaName != "record_platform_internal" || function.Identity != "validate_record_revision_primary_subject()" {
					continue
				}
				count++
				if function.Kind != "f" || function.OwnerRole != appACLCurrentTransitionMigrator || function.SecurityDefiner != tc.definer || !reflect.DeepEqual(function.Config, []string{"search_path=pg_catalog"}) {
					t.Fatalf("unexpected validator contract: %#v", function)
				}
			}
			if count != 1 {
				t.Fatalf("validator count = %d, want 1", count)
			}
		})
	}
}
