package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"houfeng/internal/center/attachments"
)

func TestPostgresIntegrationLocalRecoveryInventoryRetainsOwnerlessResponsibilities(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	blob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
	if err != nil {
		t.Fatal(err)
	}
	written := seedPostgresManagedLocalBlob(t, ctx, fixture, blob, []byte("inventory responsibility survives origin removal"))
	runtime := fixture.openDirectRuntimePool(t, ctx, "local-recovery-inventory", 2)
	adapter, err := attachments.NewRecoveryAdapter(NewPostgresAttachmentRepository(runtime))
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := adapter.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Blobs) != 0 || len(inventory.UploadIDs) != 0 || inventory.LocalDB == nil || inventory.LocalDB.PhysicalEvidence != "not_observed" {
		t.Fatal("ownerless database inventory lost its physical uncertainty")
	}
	var obligation map[string]json.RawMessage
	var incarnations, anchors bool
	for _, table := range inventory.LocalDB.Tables {
		for _, row := range table.Rows {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(row, &fields); err != nil {
				t.Fatal(err)
			}
			text := func(key string) string {
				var value string
				if err := json.Unmarshal(fields[key], &value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			switch table.Name {
			case "record_write_obligations":
				if text("obligation_id") == written.ObligationID {
					obligation = fields
				}
			case "local_blob_namespace_incarnations":
				incarnations = incarnations || text("incarnation_id") == blob.Namespace().IncarnationID
			case "local_blob_key_anchors":
				anchors = anchors || text("canonical_key") == written.Version.Key
			}
		}
	}
	if obligation == nil || !incarnations || !anchors {
		t.Fatal("database snapshot omitted namespace, anchor or ownerless obligation")
	}
	for key, want := range map[string]string{"temporary_key": "sha256/.blob-tmp-" + written.ObligationID, "object_version": written.Version.VersionID,
		"origin_kind": "import_archive", "quota_settlement_id": written.ObligationID, "namespace_id": blob.Namespace().NamespaceID} {
		var got string
		if err := json.Unmarshal(obligation[key], &got); err != nil || got != want {
			t.Fatalf("lost stored recovery field %s: %q %v", key, got, err)
		}
	}
	for _, key := range []string{"cleanup_receipt", "cleanup_completed_at", "quota_settled_at"} {
		if string(obligation[key]) != "null" {
			t.Fatalf("unknown %s was omitted or invented", key)
		}
	}
	for _, key := range []string{"registration_command", "return_command", "temporary_device", "temporary_inode", "handed_off_at"} {
		if len(obligation[key]) == 0 || string(obligation[key]) == "null" {
			t.Fatalf("stored %s was projected away", key)
		}
	}
	var origin string
	if err := json.Unmarshal(obligation["origin_id"], &origin); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_import_jobs where import_job_id=$1)`, origin).Scan(&exists); err != nil || exists {
		t.Fatalf("fixture retained original owner: %v %v", exists, err)
	}
	// A narrow raw read verifies that the inventory retained the exact integer
	// encoding rather than converting physical identity through float64.
	var inodeJSON string
	if err := fixture.db.QueryRow(ctx, `select to_jsonb(temporary_inode)::text from public.record_write_obligations where obligation_id=$1`, written.ObligationID).Scan(&inodeJSON); err != nil {
		t.Fatal(err)
	}
	if string(obligation["temporary_inode"]) != inodeJSON {
		t.Fatal("physical inode changed in recovery encoding")
	}
}
