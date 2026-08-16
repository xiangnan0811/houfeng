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

func TestRecordEvidenceAppACLFragmentRegistersExactObjectsAndPrivileges(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	if len(source.fragments) != 3 {
		t.Fatalf("production current APP ACL fragments = %d, want records-core, attachments, and evidence", len(source.fragments))
	}
	fragment := source.fragments[2]
	if fragment.Migration != "0054_create_record_evidence.sql" {
		t.Fatalf("third production current APP ACL fragment migration = %q, want record evidence", fragment.Migration)
	}

	wantObjects, err := canonicalAppACLManagedObjects(recordEvidenceExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("record-evidence managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	if len(fragment.Functions) != 0 {
		t.Fatalf("record-evidence function hardening contracts = %#v, want none", fragment.Functions)
	}

	wantPrivileges, err := canonicalPrivileges(recordEvidenceExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("record-evidence APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
}

func TestRecordEvidenceAppACLImmutableTablesNeverReceiveUpdate(t *testing.T) {
	for _, privilege := range recordEvidenceExpectedAppACLPrivileges() {
		if privilege.Privilege == AppACLPrivilegeUpdate {
			t.Fatalf("record-evidence table %q receives UPDATE", privilege.ObjectIdentity)
		}
	}
}

func TestRecordEvidenceMissingFragmentFailsBeforeBeginTx(t *testing.T) {
	fsys := appACLCurrentTestMigrationFS(t)
	for _, name := range []string{
		"0052_create_records_core.sql",
		"0053_create_record_attachments.sql",
		"0054_create_record_evidence.sql",
	} {
		fsys[name] = &fstest.MapFile{Data: []byte("select '" + name + "';")}
	}
	fragments := []AppACLCurrentMigrationFragment{
		{Migration: "0052_create_records_core.sql", Privileges: func(string) []AppACLPrivilege { return nil }},
		{Migration: "0053_create_record_attachments.sql", Privileges: func(string) []AppACLPrivilege { return nil }},
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
		if err == nil || !strings.Contains(err.Error(), `migration "0054_create_record_evidence.sql" has no current APP ACL fragment`) {
			t.Fatalf("record-evidence convergence error = %v, want missing-fragment rejection", err)
		}
		if beginCalls != 0 {
			t.Fatalf("record-evidence convergence BeginTx calls = %d, want 0", beginCalls)
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
		if err == nil || !strings.Contains(err.Error(), `migration "0054_create_record_evidence.sql" has no current APP ACL fragment`) {
			t.Fatalf("record-evidence runtime admission error = %v, want missing-fragment rejection", err)
		}
		if beginCalls != 0 {
			t.Fatalf("record-evidence runtime admission BeginTx calls = %d, want 0", beginCalls)
		}
	})
}

func recordEvidenceExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 7)
	for _, table := range []string{
		"evidence_payloads",
		"evidence_snapshots",
		"evidence_capture_intents",
		"record_revision_evidence",
		"evidence_copy_lineage",
		"evidence_purge_receipts",
		"evidence_payload_gc_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     "public",
			ObjectIdentity: table,
		})
	}
	return objects
}

func recordEvidenceExpectedAppACLPrivileges() []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 21)
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
		"evidence_payloads",
		"evidence_snapshots",
		"evidence_capture_intents",
		"record_revision_evidence",
		"evidence_copy_lineage",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	for _, table := range []string{"evidence_purge_receipts", "evidence_payload_gc_receipts"} {
		appendTable(runtime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert)
		appendTable(AppACLSubjectPlatformAdmin, table, AppACLPrivilegeSelect)
	}
	return privileges
}
