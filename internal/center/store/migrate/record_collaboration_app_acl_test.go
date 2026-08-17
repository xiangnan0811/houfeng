package migrate

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"

	"houfeng/db/migrations"
)

func TestRecordCollaborationAppACLFragmentRegistersExactObjectsPrivilegesAndFunctions(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	var fragment appACLCurrentCompiledMigrationFragment
	found := false
	for _, candidate := range source.fragments {
		if candidate.Migration == "0055_create_record_collaboration.sql" {
			fragment = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("production current APP ACL fragments are missing record-collaboration migration")
	}

	wantObjects, err := canonicalAppACLManagedObjects(recordCollaborationExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("record-collaboration managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	if got, want := fragment.Functions, recordCollaborationExpectedFunctionContracts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("record-collaboration function hardening contracts = %#v, want %#v", got, want)
	}
	wantPrivileges, err := canonicalPrivileges(recordCollaborationExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("record-collaboration APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
}

func TestRecordCollaborationAppACLRestrictsAdminAndOneWayHistoryUpdates(t *testing.T) {
	mutable := map[string]struct{}{
		"record_actions":                 {},
		"record_comments":                {},
		"record_comment_revisions":       {},
		"record_followers":               {},
		"record_notification_recipients": {},
		"record_notification_deliveries": {},
	}
	for _, privilege := range recordCollaborationExpectedAppACLPrivileges() {
		if privilege.Subject == AppACLSubjectPlatformAdmin &&
			(privilege.ObjectIdentity != "record_collaboration_purge_receipts" || privilege.Privilege != AppACLPrivilegeSelect) {
			t.Fatalf("platform admin receives collaboration content privilege %#v", privilege)
		}
		if privilege.Privilege == AppACLPrivilegeUpdate {
			if _, ok := mutable[privilege.ObjectIdentity]; !ok {
				t.Fatalf("immutable collaboration table %q receives UPDATE", privilege.ObjectIdentity)
			}
		}
	}
}

func TestRecordCollaborationAppACLDoesNotPregrantDirectPurgeOrHistoryReplacement(t *testing.T) {
	for _, privilege := range recordCollaborationAppACLCurrentPrivileges("") {
		if privilege.Privilege == AppACLPrivilegeDelete && privilege.ObjectIdentity != "record_followers" {
			t.Errorf("runtime receives premature direct DELETE on collaboration table %q", privilege.ObjectIdentity)
		}
	}
}

func TestRecordCollaborationMissingFragmentFailsBeforeBeginTx(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	for _, name := range []string{
		"0052_create_records_core.sql",
		"0053_create_record_attachments.sql",
		"0054_create_record_evidence.sql",
		"0055_create_record_collaboration.sql",
	} {
		fsys[name] = &fstest.MapFile{Data: []byte("select '" + name + "';")}
	}
	fragments := []AppACLCurrentMigrationFragment{
		{Migration: "0052_create_records_core.sql", Privileges: func(string) []AppACLPrivilege { return nil }},
		{Migration: "0053_create_record_attachments.sql", Privileges: func(string) []AppACLPrivilege { return nil }},
		{Migration: "0054_create_record_evidence.sql", Privileges: func(string) []AppACLPrivilege { return nil }},
	}

	t.Run("convergence", func(t *testing.T) {
		beginCalls := 0
		_, err := convergeAppACLCurrentWithDependencies(
			context.Background(),
			func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				beginCalls++
				return nil, errors.New("begin must not run")
			},
			"houfeng_center_runtime",
			"houfeng_platform_admin",
			fsys,
			fragments,
			appACLCurrentConvergenceTestDependencies(),
		)
		if err == nil || !strings.Contains(err.Error(), `migration "0055_create_record_collaboration.sql" has no current APP ACL fragment`) {
			t.Fatalf("record-collaboration convergence error = %v, want missing-fragment rejection", err)
		}
		if beginCalls != 0 {
			t.Fatalf("record-collaboration convergence BeginTx calls = %d, want 0", beginCalls)
		}
	})

	t.Run("runtime admission", func(t *testing.T) {
		beginCalls := 0
		err := admitAppACLCurrentRuntimeWithDependencies(
			context.Background(),
			fsys,
			fragments,
			appACLCurrentRuntimeAdmissionDependencies{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					beginCalls++
					return nil, errors.New("begin must not run")
				},
			},
		)
		if err == nil || !strings.Contains(err.Error(), `migration "0055_create_record_collaboration.sql" has no current APP ACL fragment`) {
			t.Fatalf("record-collaboration runtime admission error = %v, want missing-fragment rejection", err)
		}
		if beginCalls != 0 {
			t.Fatalf("record-collaboration runtime admission BeginTx calls = %d, want 0", beginCalls)
		}
	})
}

func recordCollaborationExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 15)
	for _, table := range []string{
		"record_actions",
		"record_action_events",
		"record_comments",
		"record_comment_revisions",
		"record_comment_tombstones",
		"record_comment_replies",
		"record_comment_mentions",
		"record_followers",
		"record_notifications",
		"record_notification_recipients",
		"record_notification_deliveries",
		"record_notification_delivery_attempts",
		"record_collaboration_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: table,
		})
	}
	for _, identity := range []string{
		"enforce_record_comment_mutation()",
		"enforce_record_comment_revision_mutation()",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     appACLManagedInternalSchemaR1,
			ObjectIdentity: identity,
		})
	}
	return objects
}

func recordCollaborationExpectedFunctionContracts() []AppACLCurrentFunctionContract {
	return []AppACLCurrentFunctionContract{
		{
			SchemaName:      appACLManagedInternalSchemaR1,
			Identity:        "enforce_record_comment_mutation()",
			Kind:            "f",
			SecurityDefiner: false,
			Config:          []string{"search_path=pg_catalog"},
		},
		{
			SchemaName:      appACLManagedInternalSchemaR1,
			Identity:        "enforce_record_comment_revision_mutation()",
			Kind:            "f",
			SecurityDefiner: false,
			Config:          []string{"search_path=pg_catalog"},
		},
	}
}

func recordCollaborationExpectedAppACLPrivileges() []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 34)
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
		"record_actions",
		"record_comments",
		"record_comment_revisions",
		"record_notification_recipients",
		"record_notification_deliveries",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	}
	appendTable(runtime, "record_followers",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert,
		AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	for _, table := range []string{
		"record_action_events",
		"record_comment_tombstones",
		"record_comment_replies",
		"record_comment_mentions",
		"record_notifications",
		"record_notification_delivery_attempts",
	} {
		appendTable(runtime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	}
	appendTable(runtime, "record_collaboration_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "record_collaboration_purge_receipts", AppACLPrivilegeSelect)
	return privileges
}
