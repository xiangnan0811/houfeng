package migrate

import (
	"reflect"
	"testing"

	"houfeng/db/migrations"
)

func TestRecordSearchAppACLFragmentRegistersExactObjectsPrivilegesAndFunctions(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	var fragment appACLCurrentCompiledMigrationFragment
	found := false
	for _, candidate := range source.fragments {
		if candidate.Migration == "0056_create_record_search.sql" {
			fragment = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("production current APP ACL fragments are missing record-search migration")
	}

	wantObjects, err := canonicalAppACLManagedObjects(recordSearchExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("record-search managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	if got, want := fragment.Functions, recordSearchExpectedFunctionContracts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("record-search function hardening contracts = %#v, want %#v", got, want)
	}
	wantPrivileges, err := canonicalPrivileges(recordSearchExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("record-search APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
}

// A derived index is still a read surface for authorized record content, so the
// platform admin may see only the content-free purge receipt.
func TestRecordSearchAppACLKeepsAdminOutOfIndexedContent(t *testing.T) {
	for _, privilege := range recordSearchExpectedAppACLPrivileges() {
		if privilege.Subject != AppACLSubjectPlatformAdmin {
			continue
		}
		if privilege.ObjectIdentity != "record_search_purge_receipts" || privilege.Privilege != AppACLPrivilegeSelect {
			t.Fatalf("platform admin receives indexed-content privilege %#v", privilege)
		}
	}
}

// Rows may leave the index only where a transaction owns them outright. Subject
// rows are replaced with their parent document; everything else leaves through a
// controlled function that checks generation state or purge authority first.
func TestRecordSearchAppACLLimitsRawDeleteToOwnedChildRowsAndGrantsControlledRemoval(t *testing.T) {
	gotDelete := make(map[string]struct{})
	gotExecute := make(map[string]struct{})
	for _, privilege := range recordSearchAppACLCurrentPrivileges("") {
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
	wantDelete := map[string]struct{}{"record_search_subjects": {}}
	if !reflect.DeepEqual(gotDelete, wantDelete) {
		t.Fatalf("runtime record-search DELETE privileges = %#v, want %#v", gotDelete, wantDelete)
	}
	wantExecute := map[string]struct{}{
		"public.record_search_purge(bytea)":             {},
		"public.record_search_retire_generation(bytea)": {},
	}
	if !reflect.DeepEqual(gotExecute, wantExecute) {
		t.Fatalf("runtime record-search controlled removal EXECUTE privileges = %#v, want %#v", gotExecute, wantExecute)
	}
}

func recordSearchExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 9)
	for _, table := range []string{
		"record_search_generations",
		"record_search_documents",
		"record_search_subjects",
		"record_search_rebuild_jobs",
		"record_search_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: table,
		})
	}
	for _, identity := range []string{
		"purge_record_search(text, text, text, text, bigint, bigint, bytea)",
		"retire_record_search_generation(bigint)",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     appACLManagedInternalSchemaR1,
			ObjectIdentity: identity,
		})
	}
	for _, identity := range []string{
		"record_search_purge(bytea)",
		"record_search_retire_generation(bytea)",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     "public",
			ObjectIdentity: identity,
		})
	}
	return objects
}

func recordSearchExpectedFunctionContracts() []AppACLCurrentFunctionContract {
	return []AppACLCurrentFunctionContract{
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "purge_record_search(text, text, text, text, bigint, bigint, bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "retire_record_search_generation(bigint)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: "public", Identity: "record_search_purge(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: "public", Identity: "record_search_retire_generation(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}
}

func recordSearchExpectedAppACLPrivileges() []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 16)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     "public",
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	for _, table := range []string{
		"record_search_generations",
		"record_search_documents",
		"record_search_rebuild_jobs",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	}
	appendTable(runtime, "record_search_subjects",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	appendTable(runtime, "record_search_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "record_search_purge_receipts", AppACLPrivilegeSelect)
	for _, function := range []string{
		"public.record_search_purge(bytea)",
		"public.record_search_retire_generation(bytea)",
	} {
		privileges = append(privileges, AppACLPrivilege{
			Subject:        runtime,
			ObjectClass:    AppACLObjectClassFunction,
			ObjectIdentity: function,
			Privilege:      AppACLPrivilegeExecute,
		})
	}
	return privileges
}
