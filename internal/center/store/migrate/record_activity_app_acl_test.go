package migrate

import (
	"reflect"
	"testing"

	"houfeng/db/migrations"
)

func TestRecordActivityAppACLFragmentRegistersExactObjectsAndPrivileges(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	var fragment appACLCurrentCompiledMigrationFragment
	found := false
	for _, candidate := range source.fragments {
		if candidate.Migration == "0057_create_record_activity.sql" {
			fragment = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("production current APP ACL fragments are missing the record-activity migration")
	}

	wantObjects, err := canonicalAppACLManagedObjects(recordActivityExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("record-activity managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	if len(fragment.Functions) != 0 {
		t.Fatalf("record-activity declares function hardening contracts it does not own: %#v", fragment.Functions)
	}
	wantPrivileges, err := canonicalPrivileges(recordActivityExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("record-activity APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
}

// The projection holds authorized record and evidence presentation. The platform
// admin role administers the database and has no business reading operator
// timeline content, so it gets nothing here.
func TestRecordActivityAppACLKeepsAdminOutOfProjectedContent(t *testing.T) {
	for _, privilege := range recordActivityAppACLCurrentPrivileges("") {
		if privilege.Subject == AppACLSubjectPlatformAdmin {
			t.Fatalf("platform admin receives a record-activity privilege: %#v", privilege)
		}
	}
}

// The projection is rebuildable, so the runtime owns DELETE on all of it. What
// it must not have is UPDATE on a published projection row: correcting a fact
// happens by projecting a new corrective event, never by rewriting history in
// place.
func TestRecordActivityAppACLAllowsRebuildButNotInPlaceRewriteOfProjectedFacts(t *testing.T) {
	updates := make(map[string]struct{})
	deletes := make(map[string]struct{})
	for _, privilege := range recordActivityAppACLCurrentPrivileges("") {
		if privilege.Subject != AppACLSubjectCenterRuntime {
			continue
		}
		switch privilege.Privilege {
		case AppACLPrivilegeUpdate:
			updates[privilege.ObjectIdentity] = struct{}{}
		case AppACLPrivilegeDelete:
			deletes[privilege.ObjectIdentity] = struct{}{}
		}
	}

	wantUpdates := map[string]struct{}{
		"record_activity_projection_heads":       {},
		"record_activity_projection_checkpoints": {},
		"record_activity_revision_intervals":     {},
	}
	if !reflect.DeepEqual(updates, wantUpdates) {
		t.Fatalf("runtime record-activity UPDATE privileges = %#v, want %#v", updates, wantUpdates)
	}
	if _, forbidden := updates["record_activity_projection"]; forbidden {
		t.Fatal("runtime must not be able to rewrite a projected fact in place")
	}

	wantDeletes := map[string]struct{}{
		"record_activity_projection":             {},
		"record_activity_subjects":               {},
		"record_activity_revision_intervals":     {},
		"record_activity_projection_checkpoints": {},
	}
	if !reflect.DeepEqual(deletes, wantDeletes) {
		t.Fatalf("runtime record-activity DELETE privileges = %#v, want %#v", deletes, wantDeletes)
	}
}

func recordActivityExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 5)
	for _, table := range []string{
		"record_activity_projection_heads",
		"record_activity_projection",
		"record_activity_subjects",
		"record_activity_projection_checkpoints",
		"record_activity_revision_intervals",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: table,
		})
	}
	return objects
}

func recordActivityExpectedAppACLPrivileges() []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 17)
	appendTable := func(table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     "public",
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}
	appendTable("record_activity_projection_heads",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable("record_activity_projection",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	appendTable("record_activity_subjects",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	appendTable("record_activity_projection_checkpoints",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	appendTable("record_activity_revision_intervals",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	return privileges
}
