package migrate

import (
	"reflect"
	"testing"

	"houfeng/db/migrations"
)

func TestRecordAttachmentsAppACLFragmentRegistersExactObjectsAndPrivileges(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	if len(source.fragments) != 9 {
		t.Fatalf("production current APP ACL fragments = %d, want records-core through authority heartbeat", len(source.fragments))
	}
	fragment := source.fragments[1]
	if fragment.Migration != "0053_create_record_attachments.sql" {
		t.Fatalf("second production current APP ACL fragment migration = %q, want record attachments", fragment.Migration)
	}

	wantObjects, err := canonicalAppACLManagedObjects(recordAttachmentsExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("record-attachments managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	if len(fragment.Functions) != 0 {
		t.Fatalf("record-attachments function hardening contracts = %#v, want none", fragment.Functions)
	}

	wantPrivileges, err := canonicalPrivileges(recordAttachmentsExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("record-attachments APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
}

func TestRecordAttachmentsAppACLFragmentExtendsCatalogWithoutSequencesOrFunctions(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	contract, err := compileAppACLCurrentCatalogContract(source, "houfeng", appACLCurrentCatalogTestBindings(), "houfeng_migrator")
	if err != nil {
		t.Fatalf("compile production current APP ACL catalog contract: %v", err)
	}
	base, err := CompileAppACLManagedSurfaceR1("houfeng")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(contract.ManagedObjects), len(base.Objects)+len(recordsCoreExpectedAppACLObjects())+len(recordAttachmentsExpectedAppACLObjects())+len(recordEvidenceExpectedAppACLObjects())+len(recordCollaborationExpectedAppACLObjects())+len(recordSearchExpectedAppACLObjects())+len(recordActivityExpectedAppACLObjects())+len(recordPortabilityExpectedAppACLObjects())+len(recordsAuthorityExpectedAppACLObjects()); got != want {
		t.Fatalf("production current managed objects = %d, want %d", got, want)
	}
	if got, want := len(contract.Privileges), len(appACLPrivilegesR1("houfeng"))+len(recordsCoreExpectedAppACLPrivileges())+len(recordAttachmentsExpectedAppACLPrivileges())+len(recordEvidenceExpectedAppACLPrivileges())+len(recordCollaborationExpectedAppACLPrivileges())+len(recordSearchExpectedAppACLPrivileges())+len(recordActivityExpectedAppACLPrivileges())+len(recordPortabilityExpectedAppACLPrivileges()); got != want {
		t.Fatalf("production current privileges = %d, want %d", got, want)
	}
	if got, want := len(contract.ExpectedFunctions), len(appACLProjectorFunctionsR1())+1+len(recordCollaborationExpectedFunctionContracts())+len(recordSearchExpectedFunctionContracts())+len(recordActivityExpectedFunctionContracts())+len(recordPortabilityExpectedFunctionContracts())+len(recordsAuthorityExpectedFunctionContracts()); got != want {
		t.Fatalf("production current expected functions = %d, want %d including the authority heartbeat", got, want)
	}
	for _, object := range contract.ManagedObjects {
		if object.SchemaName != "public" {
			continue
		}
		if object.ObjectClass == AppACLObjectClassSequence && object.ObjectIdentity == "record_attachments_id_seq" {
			t.Fatalf("record-attachments unexpectedly registered synthetic sequence %#v", object)
		}
		if object.ObjectClass == AppACLObjectClassFunction && object.ObjectIdentity == "transition_attachment_upload()" {
			t.Fatalf("record-attachments unexpectedly registered state transition function %#v", object)
		}
	}
}

func TestRecordAttachmentsAppACLImmutableTablesNeverReceiveUpdate(t *testing.T) {
	immutable := map[string]struct{}{
		"blob_objects":                     {},
		"attachment_upload_parts":          {},
		"record_revision_attachments":      {},
		"blob_gc_pins":                     {},
		"attachment_purge_receipts":        {},
		"content_workspace_purge_receipts": {},
	}
	for _, privilege := range recordAttachmentsExpectedAppACLPrivileges() {
		if _, found := immutable[privilege.ObjectIdentity]; found && privilege.Privilege == AppACLPrivilegeUpdate {
			t.Fatalf("immutable record-attachments table %q receives UPDATE", privilege.ObjectIdentity)
		}
	}
}

func recordAttachmentsExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 13)
	for _, table := range []string{
		"blob_objects",
		"attachment_quota_accounts",
		"record_attachments",
		"attachment_uploads",
		"attachment_upload_parts",
		"record_revision_attachments",
		"attachment_processor_jobs",
		"content_processor_workspaces",
		"blob_gc_pins",
		"blob_gc_deletions",
		"blob_publication_intents",
		"attachment_purge_receipts",
		"content_workspace_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: table,
		})
	}
	return objects
}

func recordAttachmentsExpectedAppACLPrivileges() []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 46)
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
	for _, table := range []string{"blob_objects", "attachment_upload_parts", "record_revision_attachments", "blob_gc_pins"} {
		appendTable(runtime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	for _, table := range []string{"attachment_quota_accounts", "record_attachments", "attachment_uploads", "attachment_processor_jobs", "content_processor_workspaces"} {
		appendTable(runtime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	}
	appendTable(runtime, "blob_gc_deletions", AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable(runtime, "blob_publication_intents", AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable(runtime, "attachment_purge_receipts", AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(runtime, "content_workspace_purge_receipts", AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "attachment_purge_receipts", AppACLPrivilegeSelect)
	appendTable(AppACLSubjectPlatformAdmin, "content_workspace_purge_receipts", AppACLPrivilegeSelect)
	appendTable(AppACLSubjectPlatformAdmin, "blob_gc_deletions", AppACLPrivilegeSelect)
	appendTable(AppACLSubjectPlatformAdmin, "blob_publication_intents", AppACLPrivilegeSelect)
	return privileges
}
