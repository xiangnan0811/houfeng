package migrate

import (
	"reflect"
	"testing"

	"houfeng/db/migrations"
)

func TestRecordsCoreAppACLFragmentRegistersExactObjectsAndPrivileges(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	var fragment appACLCurrentCompiledMigrationFragment
	found := false
	for _, candidate := range source.fragments {
		if candidate.Migration == "0052_create_records_core.sql" {
			fragment = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("production current APP ACL fragments are missing records-core migration")
	}

	wantObjects, err := canonicalAppACLManagedObjects(recordsCoreExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("records-core managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	wantFunctions := []AppACLCurrentFunctionContract{recordsCoreExpectedPrimarySubjectFunctionContract()}
	if !reflect.DeepEqual(fragment.Functions, wantFunctions) {
		t.Fatalf("records-core function hardening contracts = %#v, want %#v", fragment.Functions, wantFunctions)
	}

	wantPrivileges, err := canonicalPrivileges(recordsCoreExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("records-core APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
}

func TestRecordsCoreAppACLFragmentExtendsCatalogWithPrimaryValidationFunctionAndNoSequences(t *testing.T) {
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
	base, err := CompileAppACLManagedSurfaceR1("houfeng")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(contract.ManagedObjects), len(base.Objects)+len(recordsCoreExpectedAppACLObjects())+len(recordAttachmentsExpectedAppACLObjects())+len(recordEvidenceExpectedAppACLObjects())+len(recordCollaborationExpectedAppACLObjects())+len(recordSearchExpectedAppACLObjects())+len(recordActivityExpectedAppACLObjects()); got != want {
		t.Fatalf("production current managed objects = %d, want %d", got, want)
	}
	if got, want := len(contract.Privileges), len(appACLPrivilegesR1("houfeng"))+len(recordsCoreExpectedAppACLPrivileges())+len(recordAttachmentsExpectedAppACLPrivileges())+len(recordEvidenceExpectedAppACLPrivileges())+len(recordCollaborationExpectedAppACLPrivileges())+len(recordSearchExpectedAppACLPrivileges())+len(recordActivityExpectedAppACLPrivileges()); got != want {
		t.Fatalf("production current privileges = %d, want %d", got, want)
	}
	if got, want := len(contract.ExpectedFunctions), len(appACLProjectorFunctionsR1())+1+len(recordCollaborationExpectedFunctionContracts())+len(recordSearchExpectedFunctionContracts())+len(recordActivityExpectedFunctionContracts()); got != want {
		t.Fatalf("production current expected functions = %d, want %d frozen projectors, records-core validator, and collaboration mutation guards", got, want)
	}
	for _, object := range contract.ManagedObjects {
		if object.ObjectClass == AppACLObjectClassSequence && object.SchemaName == "public" &&
			(object.ObjectIdentity == "records_id_seq" || object.ObjectIdentity == "record_revisions_id_seq") {
			t.Fatalf("records-core unexpectedly registered synthetic sequence %#v", object)
		}
	}
}

func TestRecordsCoreAppACLImmutableTablesNeverReceiveUpdate(t *testing.T) {
	immutable := map[string]struct{}{
		"record_revisions":             {},
		"record_revision_subjects":     {},
		"record_revision_tags":         {},
		"record_revision_participants": {},
		"record_draft_checkpoints":     {},
		"record_domain_activities":     {},
		"record_core_purge_receipts":   {},
	}
	for _, privilege := range recordsCoreExpectedAppACLPrivileges() {
		if _, found := immutable[privilege.ObjectIdentity]; found && privilege.Privilege == AppACLPrivilegeUpdate {
			t.Fatalf("immutable records-core table %q receives UPDATE", privilege.ObjectIdentity)
		}
	}
}

func recordsCoreExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 10)
	for _, table := range []string{
		"records",
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_drafts",
		"record_draft_checkpoints",
		"record_domain_activities",
		"record_core_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: table,
		})
	}
	objects = append(objects, AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "validate_record_revision_primary_subject()",
	})
	return objects
}

func recordsCoreExpectedPrimarySubjectFunctionContract() AppACLCurrentFunctionContract {
	return AppACLCurrentFunctionContract{
		SchemaName:      appACLManagedInternalSchemaR1,
		Identity:        "validate_record_revision_primary_subject()",
		Kind:            "f",
		SecurityDefiner: false,
		Config:          []string{"search_path=pg_catalog"},
	}
}

func recordsCoreExpectedAppACLPrivileges() []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 29)
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
	appendTable(runtime, "records",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	for _, table := range []string{
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_draft_checkpoints",
		"record_domain_activities",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	appendTable(runtime, "record_drafts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	appendTable(runtime, "record_core_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "record_core_purge_receipts", AppACLPrivilegeSelect)
	return privileges
}
