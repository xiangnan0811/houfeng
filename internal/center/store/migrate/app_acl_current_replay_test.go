package migrate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"testing/fstest"

	"houfeng/db/migrations"
)

func TestAppACLCurrentC79IndependentGoldensPreserveC78(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		size int
		sha  string
	}{
		{"migrations", appACLCurrentC79MigrationGolden, 5832, "3879119c952bd0a59dee938a0468ce848070985bd0b6bd55a7da26d40263c7db"},
		{"privileges", appACLCurrentC79PrivilegeGolden, 35745, "3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c"},
	} {
		if len(tc.body) != tc.size || fmt.Sprintf("%x", sha256.Sum256(tc.body)) != tc.sha {
			t.Fatalf("independent pre-C80 C79 %s golden changed", tc.name)
		}
	}
	entries, err := ParseCanonicalMigrationSetBodyV1(appACLCurrentC79MigrationGolden)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 80 || entries[79].Filename != appACLCurrentP79Migration {
		t.Fatalf("C79 source oracle is not the released 80-source root: %#v", entries)
	}
	prefix, err := CanonicalMigrationSetBodyV1(entries[:79])
	if err != nil || !bytes.Equal(prefix, appACLCurrentC78MigrationGolden) || !bytes.Equal(appACLCurrentC79PrivilegeGolden, appACLCurrentC78PrivilegeGolden) {
		t.Fatalf("C79 oracle changed the released C78 prefix or privileges: %v", err)
	}
}

func appACLCurrentReplayExpectedPrivileges() []AppACLPrivilege {
	var result []AppACLPrivilege
	for table, privileges := range map[string][]AppACLPrivilegeKind{
		"agent_replay_states":  {AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate},
		"agent_batch_receipts": {AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete},
	} {
		for _, privilege := range privileges {
			result = append(result, AppACLPrivilege{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: table, Privilege: privilege})
		}
	}
	canonical, err := canonicalPrivileges(result)
	if err != nil {
		panic(err)
	}
	return canonical
}

func TestAppACLCurrentReplayFragmentExactRetirement(t *testing.T) {
	var fragment AppACLCurrentMigrationFragment
	for _, candidate := range appACLCurrentMigrationFragments {
		if candidate.Migration == appACLCurrentP80Migration {
			fragment = candidate
		}
	}
	wantRetired := []AppACLManagedObjectR1{
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "agent_sync_batches"},
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "agent_live_signals"},
	}
	if len(fragment.RetiredObjects) != 2 {
		t.Fatalf("0080 retirement = %#v, want exactly two old replay tables", fragment.RetiredObjects)
	}
	for _, want := range wantRetired {
		found := false
		for _, got := range fragment.RetiredObjects {
			found = found || got == want
		}
		if !found {
			t.Fatalf("0080 retirement lacks %#v", want)
		}
	}
	wantObjects := map[AppACLManagedObjectR1]bool{
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "agent_replay_states"}:              true,
		{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "agent_batch_receipts"}:             true,
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "public", ObjectIdentity: "enforce_agent_replay_state()"}:  true,
		{ObjectClass: AppACLObjectClassFunction, SchemaName: "public", ObjectIdentity: "enforce_agent_batch_receipt()"}: true,
	}
	if len(fragment.Objects) != len(wantObjects) || len(fragment.Functions) != 2 || len(fragment.AuxiliaryPrivileges) != 0 {
		t.Fatalf("0080 must add exactly two tables and two invoker functions: %#v", fragment)
	}
	for _, object := range fragment.Objects {
		if !wantObjects[object] {
			t.Fatalf("unexpected 0080 object %#v", object)
		}
		delete(wantObjects, object)
	}
	for _, function := range fragment.Functions {
		if function.SchemaName != "public" || function.Kind != "f" || function.SecurityDefiner || !reflect.DeepEqual(function.Config, []string{"search_path=pg_catalog"}) {
			t.Fatalf("0080 function is not a hardened invoker: %#v", function)
		}
	}
	for _, database := range []string{"houfeng", "other_database"} {
		got, err := canonicalPrivileges(fragment.Privileges(database))
		if err != nil || !reflect.DeepEqual(got, appACLCurrentReplayExpectedPrivileges()) {
			t.Fatalf("0080 privileges = %#v/%v, want exact six runtime grants", got, err)
		}
	}
}

func TestAppACLCurrentRetirementValidationAndClones(t *testing.T) {
	old := AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "agent_sync_batches"}
	newFragment := func() AppACLCurrentMigrationFragment {
		return AppACLCurrentMigrationFragment{Migration: "0052_retirement.sql", RetiredObjects: []AppACLManagedObjectR1{old}, Privileges: func(string) []AppACLPrivilege { return nil }}
	}
	for _, kind := range []string{"absent", "duplicate", "redeclare", "dangling_privilege", "repeat_retirement"} {
		t.Run(kind, func(t *testing.T) {
			fragment := newFragment()
			fragments := []AppACLCurrentMigrationFragment{fragment}
			fsys := appACLCurrentTestMigrationFS(t)
			fsys[fragment.Migration] = &fstest.MapFile{Data: []byte("select 1;")}
			switch kind {
			case "absent":
				fragments[0].RetiredObjects[0].ObjectIdentity = "nonexistent_replay_table"
			case "duplicate":
				fragments[0].RetiredObjects = append(fragments[0].RetiredObjects, old)
			case "redeclare":
				fragments[0].Objects = []AppACLManagedObjectR1{old}
			case "dangling_privilege":
				fragments[0].Privileges = func(string) []AppACLPrivilege {
					return []AppACLPrivilege{{Subject: AppACLSubjectCenterRuntime, ObjectClass: old.ObjectClass, SchemaName: old.SchemaName, ObjectIdentity: old.ObjectIdentity, Privilege: AppACLPrivilegeSelect}}
				}
			case "repeat_retirement":
				second := newFragment()
				second.Migration = "0053_retirement.sql"
				fsys[second.Migration] = &fstest.MapFile{Data: []byte("select 2;")}
				fragments = append(fragments, second)
			}
			if _, err := compileAppACLCurrentSourceContract(fsys, fragments); err == nil {
				t.Fatalf("accepted invalid retirement %s", kind)
			}
		})
	}
	fragment := newFragment()
	cloned := cloneAppACLCurrentMigrationFragment(fragment)
	compiled, err := compileAppACLCurrentMigrationFragment(fragment)
	if err != nil {
		t.Fatal(err)
	}
	fragment.RetiredObjects[0].ObjectIdentity = "mutated"
	if cloned.RetiredObjects[0] != old || compiled.RetiredObjects[0] != old {
		t.Fatal("fragment clone or compiled fragment aliases retirement input")
	}
	fsys := appACLCurrentTestMigrationFS(t)
	compiledClone := cloneAppACLCurrentCompiledMigrationFragment(compiled)
	compiled.RetiredObjects[0].ObjectIdentity = "compiled_mutation"
	if compiledClone.RetiredObjects[0] != old {
		t.Fatal("compiled-fragment clone aliases retirement slice")
	}
	fsys[cloned.Migration] = &fstest.MapFile{Data: []byte("select 1;")}
	source, err := compileAppACLCurrentSourceContract(fsys, []AppACLCurrentMigrationFragment{cloned})
	if err != nil {
		t.Fatal(err)
	}
	cloned.RetiredObjects[0].ObjectIdentity = "mutated_again"
	if source.fragments[0].RetiredObjects[0] != old {
		t.Fatal("source compiler aliases retirement slice")
	}
	contract, err := compileAppACLCurrentCatalogContract(source, "houfeng", appACLCurrentCatalogTestBindings(), "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range contract.ManagedObjects {
		if object == old {
			t.Fatal("retired object remained in effective catalog")
		}
	}
	for _, privilege := range contract.Privileges {
		if privilege.ObjectIdentity == old.ObjectIdentity && privilege.SchemaName == old.SchemaName {
			t.Fatal("retired object's privilege remained in effective catalog")
		}
	}
}

func TestAppACLCurrentP79RejectsUnrelatedPrivilegeDelta(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAppACLCurrentTransitionPrivilegeDelta(appACLCurrentC79PrivilegeGolden, source, appACLCurrentProfileP79); err != nil {
		t.Fatalf("exact 0080 privilege delta rejected: %v", err)
	}
	for _, kind := range []string{"unrelated_add", "unrelated_remove", "grant_option", "role_binding", "retired_grant_restored", "missing_new_grant", "false_retirement"} {
		t.Run(kind, func(t *testing.T) {
			modified := source
			modified.fragments = make([]appACLCurrentCompiledMigrationFragment, len(source.fragments))
			for i, fragment := range source.fragments {
				modified.fragments[i] = cloneAppACLCurrentCompiledMigrationFragment(fragment)
			}
			predecessor := appACLCurrentC79PrivilegeGolden
			for i := range modified.fragments {
				fragment := &modified.fragments[i]
				switch {
				case kind == "unrelated_add" && fragment.Migration == appACLCurrentP77Migration:
					fragment.Privileges = append(fragment.Privileges, AppACLPrivilege{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_intelligence", Privilege: AppACLPrivilegeDelete})
				case kind == "unrelated_remove" && fragment.Migration == appACLCurrentP77Migration:
					fragment.Privileges = fragment.Privileges[1:]
				case kind == "grant_option" && fragment.Migration == appACLCurrentP80Migration:
					fragment.Privileges[0].GrantOption = true
				case kind == "retired_grant_restored" && fragment.Migration == appACLCurrentP80Migration:
					fragment.Privileges = append(fragment.Privileges, AppACLPrivilege{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "agent_sync_batches", Privilege: AppACLPrivilegeSelect})
				case kind == "missing_new_grant" && fragment.Migration == appACLCurrentP80Migration:
					fragment.Privileges = fragment.Privileges[1:]
				case kind == "false_retirement" && fragment.Migration == appACLCurrentP80Migration:
					fragment.RetiredObjects = append(fragment.RetiredObjects, AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_intelligence"})
				}
			}
			if kind == "role_binding" {
				decoded, err := ParseCanonicalPrivilegeSetBodyV1(predecessor)
				if err != nil {
					t.Fatal(err)
				}
				decoded.RoleBindings[0].CatalogRole += "_other"
				predecessor, err = CanonicalPrivilegeSetBodyV1(decoded.RoleBindings, decoded.Privileges)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := validateAppACLCurrentTransitionPrivilegeDelta(predecessor, modified, appACLCurrentProfileP79); err == nil {
				t.Fatalf("accepted unapproved C80 privilege delta %s", kind)
			}
		})
	}
}

func TestAppACLCurrentP79RejectsPublishedSourceDriftBeforeBeginTx(t *testing.T) {
	fsys := appACLCurrentTransitionTestFS(t)
	file := fsys[appACLCurrentP79Migration]
	file.Data = append(append([]byte(nil), file.Data...), []byte("\n-- changed published C79 source\n")...)
	assertAppACLCurrentTransitionRejectedBeforeBeginTx(t, fsys, appACLCurrentMigrationFragments, appACLCurrentTransitionDefinitions,
		"predecessor migration source differs from released golden")
}

func TestAppACLCurrentReplaySuffixClassification(t *testing.T) {
	for _, definition := range appACLCurrentTransitionDefinitions {
		for _, kind := range []string{"exact", "missing_replay", "duplicate_replay", "unknown_tail", "reordered"} {
			t.Run(fmt.Sprintf("profile_%d_%s", definition.profile, kind), func(t *testing.T) {
				names := append([]string(nil), definition.successorMigrations...)
				switch kind {
				case "missing_replay":
					names = names[:len(names)-1]
				case "duplicate_replay":
					names = append(names, appACLCurrentP80Migration)
				case "unknown_tail":
					names = append(names, "0081_unregistered.sql")
				case "reordered":
					names = append([]string{appACLCurrentP80Migration}, names[:len(names)-1]...)
					if len(names) == 1 {
						names = append(names, appACLCurrentP79Migration)
					}
				}
				got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(appACLCurrentTransition{
					profile: definition.profile, successor: migrationSourceSnapshot{names: names},
				})
				if kind == "exact" {
					if err != nil || got != (definition.profile == appACLCurrentProfileP62) {
						t.Fatalf("registered C80 suffix classification = %t/%v", got, err)
					}
				} else if err == nil {
					t.Fatalf("accepted nonexact C80 suffix %v", names)
				}
			})
		}
	}
}
