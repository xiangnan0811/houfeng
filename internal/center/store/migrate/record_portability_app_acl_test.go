package migrate

import (
	"reflect"
	"testing"

	"houfeng/db/migrations"
)

func TestRecordPortabilityAppACLFragmentRegistersExactObjectsAndPrivileges(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	var fragment appACLCurrentCompiledMigrationFragment
	found := false
	for _, candidate := range source.fragments {
		if candidate.Migration == "0058_create_record_portability.sql" {
			fragment = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("production current APP ACL fragments are missing the record-portability migration")
	}

	wantObjects, err := canonicalAppACLManagedObjects(recordPortabilityExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("record-portability managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	if got, want := fragment.Functions, recordPortabilityExpectedFunctionContracts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("record-portability function hardening contracts = %#v, want %#v", got, want)
	}
	wantPrivileges, err := canonicalPrivileges(recordPortabilityExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("record-portability APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
}

func TestRecordPortabilityBlobKeyMuslFragmentAddsNoObjectsOrPrivileges(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	var fragment appACLCurrentCompiledMigrationFragment
	found := false
	for _, candidate := range source.fragments {
		if candidate.Migration == "0059_relax_portability_blob_key_regex.sql" {
			fragment = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("production current APP ACL fragments are missing the 0059 blob_key CHECK migration")
	}
	if len(fragment.Objects) != 0 || len(fragment.Functions) != 0 || len(fragment.Privileges) != 0 {
		t.Fatalf("0059 fragment must add no ACL surface: objects=%d functions=%d privileges=%d",
			len(fragment.Objects), len(fragment.Functions), len(fragment.Privileges))
	}
}

func TestRecordPortabilityAppACLRemovesRowsOnlyThroughControlledFunction(t *testing.T) {
	gotDelete := make(map[string]struct{})
	gotExecute := make(map[string]struct{})
	for _, privilege := range recordPortabilityAppACLCurrentPrivileges("") {
		if privilege.Subject != AppACLSubjectCenterRuntime {
			continue
		}
		switch privilege.Privilege {
		case AppACLPrivilegeDelete:
			gotDelete[privilege.ObjectIdentity] = struct{}{}
		case AppACLPrivilegeExecute:
			gotExecute[privilege.ObjectIdentity] = struct{}{}
		}
	}
	if len(gotDelete) != 0 {
		t.Fatalf("runtime record-portability DELETE privileges = %#v, want none", gotDelete)
	}
	wantExecute := map[string]struct{}{"public.record_portability_purge(bytea)": {}}
	if !reflect.DeepEqual(gotExecute, wantExecute) {
		t.Fatalf("runtime record-portability controlled removal EXECUTE privileges = %#v, want %#v", gotExecute, wantExecute)
	}
}

func TestRecordPortabilityAppACLKeepsAdminOutOfJobAndArtifactContent(t *testing.T) {
	for _, privilege := range recordPortabilityAppACLCurrentPrivileges("") {
		if privilege.Subject != AppACLSubjectPlatformAdmin {
			continue
		}
		switch privilege.ObjectIdentity {
		case "record_portability_purge_receipts", "record_origin_tombstones":
			if privilege.Privilege != AppACLPrivilegeSelect {
				t.Fatalf("platform admin receives a mutating portability privilege: %#v", privilege)
			}
		default:
			t.Fatalf("platform admin receives a content-bearing portability privilege: %#v", privilege)
		}
	}
}

func recordPortabilityExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 11)
	for _, table := range recordPortabilityTableNames() {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: table,
		})
	}
	for _, function := range []struct {
		schema   string
		identity string
	}{
		{appACLManagedInternalSchemaR1, "purge_record_portability(text, text, text, text, bigint, bigint, bytea)"},
		{appACLManagedPublicSchemaR1, "record_portability_purge(bytea)"},
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.schema,
			ObjectIdentity: function.identity,
		})
	}
	return objects
}

func recordPortabilityExpectedFunctionContracts() []AppACLCurrentFunctionContract {
	return []AppACLCurrentFunctionContract{
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "purge_record_portability(text, text, text, text, bigint, bigint, bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_portability_purge(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}
}

func recordPortabilityExpectedAppACLPrivileges() []AppACLPrivilege {
	return recordPortabilityAppACLCurrentPrivileges("")
}

func recordPortabilityTableNames() []string {
	return []string{
		"record_export_jobs",
		"record_export_artifacts",
		"record_import_jobs",
		"record_import_plans",
		"record_import_artifacts",
		"record_import_entity_mappings",
		"record_origins",
		"record_origin_tombstones",
		"record_portability_purge_receipts",
	}
}
