package recorddeletion

import (
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
)

func TestRequiredAdapterNamesAreClosedOrderedAndDefensivelyCopied(t *testing.T) {
	t.Parallel()

	want := []AdapterName{
		AdapterNameRecordCore,
		AdapterNameRecordAttachments,
		AdapterNameRecordEvidence,
		AdapterNameRecordMarkdownClient,
		AdapterNameRecordSearch,
		AdapterNameRecordActivityProjection,
		AdapterNameRecordComparison,
		AdapterNameRecordCollaboration,
		AdapterNameRecordPortability,
	}
	got := RequiredAdapterNames()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RequiredAdapterNames() = %#v, want %#v", got, want)
	}

	got[0] = "tampered"
	if fresh := RequiredAdapterNames(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RequiredAdapterNames() after caller mutation = %#v, want %#v", fresh, want)
	}
}

func TestRecordAttachmentsSurfaceNamesAreClosedOrderedAndDefensivelyCopied(t *testing.T) {
	t.Parallel()

	want := []SurfaceName{
		"attachment_processor_jobs",
		"attachment_purge_receipts",
		"attachment_quota_accounts",
		"attachment_upload_parts",
		"attachment_uploads",
		"blob_gc_deletions",
		"blob_gc_pins",
		"blob_objects",
		"blob_publication_intents",
		"content_processor_workspaces",
		"content_workspace_purge_receipts",
		"record_attachments",
		"record_revision_attachments",
	}
	got := RecordAttachmentsSurfaceNames()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordAttachmentsSurfaceNames() = %#v, want %#v", got, want)
	}
	got[0] = "tampered"
	if fresh := RecordAttachmentsSurfaceNames(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RecordAttachmentsSurfaceNames() after mutation = %#v", fresh)
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordAttachments, want)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor() error = %v", err)
	}
	if gotDigest := RecordAttachmentsSurfaceDigest(); gotDigest == ([sha256.Size]byte{}) || gotDigest != digestAdapterSurfaces(descriptor) {
		t.Fatalf("RecordAttachmentsSurfaceDigest() = %x", gotDigest)
	}
}

func TestNewAdapterDescriptorNormalizesAndProtectsOwnedSurfaces(t *testing.T) {
	t.Parallel()

	input := []SurfaceName{"fixture.surface_z", "fixture.surface_a"}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordSearch, input)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor() error = %v", err)
	}
	if descriptor.Name() != AdapterNameRecordSearch {
		t.Fatalf("descriptor.Name() = %q, want %q", descriptor.Name(), AdapterNameRecordSearch)
	}
	wantSurfaces := []SurfaceName{"fixture.surface_a", "fixture.surface_z"}
	if got := descriptor.Surfaces(); !reflect.DeepEqual(got, wantSurfaces) {
		t.Fatalf("descriptor.Surfaces() = %#v, want %#v", got, wantSurfaces)
	}

	input[0] = "tampered.input"
	returned := descriptor.Surfaces()
	returned[0] = "tampered.output"
	if got := descriptor.Surfaces(); !reflect.DeepEqual(got, wantSurfaces) {
		t.Fatalf("descriptor.Surfaces() after mutation = %#v, want %#v", got, wantSurfaces)
	}
}

func TestNewAdapterDescriptorRejectsUnknownEmptyInvalidAndDuplicateSurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		adapter  AdapterName
		surfaces []SurfaceName
	}{
		{name: "unknown adapter", adapter: "record_unknown", surfaces: []SurfaceName{"fixture.surface"}},
		{name: "empty surfaces", adapter: AdapterNameRecordAttachments},
		{name: "empty surface", adapter: AdapterNameRecordAttachments, surfaces: []SurfaceName{""}},
		{name: "uppercase surface", adapter: AdapterNameRecordAttachments, surfaces: []SurfaceName{"Fixture.Surface"}},
		{name: "path surface", adapter: AdapterNameRecordAttachments, surfaces: []SurfaceName{"fixture/surface"}},
		{name: "duplicate surface", adapter: AdapterNameRecordAttachments, surfaces: []SurfaceName{"fixture.surface", "fixture.surface"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewAdapterDescriptor(tt.adapter, tt.surfaces); !errors.Is(err, ErrInvalidAdapterDescriptor) {
				t.Fatalf("NewAdapterDescriptor() error = %v, want ErrInvalidAdapterDescriptor", err)
			}
		})
	}
}

func TestRecordCoreDescriptorRequiresExactOwnedTables(t *testing.T) {
	t.Parallel()

	want := []SurfaceName{
		"content_delivery_epochs",
		"record_core_purge_receipts",
		"record_domain_activities",
		"record_draft_checkpoints",
		"record_drafts",
		"record_revision_participants",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revisions",
		"records",
	}
	if got := RecordCoreSurfaceNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordCoreSurfaceNames() = %#v, want %#v", got, want)
	}

	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCore, RecordCoreSurfaceNames())
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(record_core) error = %v", err)
	}
	if got := descriptor.Surfaces(); !reflect.DeepEqual(got, want) {
		t.Fatalf("record_core surfaces = %#v, want %#v", got, want)
	}

	missing := RecordCoreSurfaceNames()[1:]
	if _, err := NewAdapterDescriptor(AdapterNameRecordCore, missing); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("missing core surface error = %v, want ErrInvalidAdapterDescriptor", err)
	}
	extra := append(RecordCoreSurfaceNames(), SurfaceName("record_search_documents"))
	if _, err := NewAdapterDescriptor(AdapterNameRecordCore, extra); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("extra core surface error = %v, want ErrInvalidAdapterDescriptor", err)
	}

	returned := RecordCoreSurfaceNames()
	returned[0] = "tampered"
	if fresh := RecordCoreSurfaceNames(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RecordCoreSurfaceNames() after mutation = %#v, want %#v", fresh, want)
	}
}

func TestRecordCollaborationDescriptorRequiresExactOwnedTables(t *testing.T) {
	t.Parallel()

	want := []SurfaceName{
		"record_action_events",
		"record_actions",
		"record_collaboration_purge_receipts",
		"record_comment_mentions",
		"record_comment_replies",
		"record_comment_revisions",
		"record_comment_tombstones",
		"record_comments",
		"record_followers",
		"record_notification_audit_summaries",
		"record_notification_deliveries",
		"record_notification_delivery_attempts",
		"record_notification_recipients",
		"record_notifications",
	}
	if got := RecordCollaborationSurfaceNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordCollaborationSurfaceNames() = %#v, want %#v", got, want)
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCollaboration, want)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(record_collaboration) error = %v", err)
	}
	if got := RecordCollaborationSurfaceDigest(); got == ([sha256.Size]byte{}) || got != digestAdapterSurfaces(descriptor) {
		t.Fatalf("RecordCollaborationSurfaceDigest() = %x", got)
	}
	if _, err := NewAdapterDescriptor(AdapterNameRecordCollaboration, want[1:]); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("missing collaboration surface error = %v", err)
	}
	extra := append(RecordCollaborationSurfaceNames(), "record_outbox")
	if _, err := NewAdapterDescriptor(AdapterNameRecordCollaboration, extra); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("extra collaboration surface error = %v", err)
	}
	returned := RecordCollaborationSurfaceNames()
	returned[0] = "tampered"
	if fresh := RecordCollaborationSurfaceNames(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RecordCollaborationSurfaceNames() after mutation = %#v", fresh)
	}
}

func TestAdapterHealthSnapshotRequiresVersionedProof(t *testing.T) {
	t.Parallel()

	proof := testHealthProof(7)
	snapshot, err := NewAdapterHealthSnapshot(true, 3, proof)
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	if !snapshot.Healthy() || snapshot.Revision() != 3 || snapshot.ProofDigest() != proof {
		t.Fatalf("health snapshot = healthy:%t revision:%d proof:%x", snapshot.Healthy(), snapshot.Revision(), snapshot.ProofDigest())
	}

	unhealthy, err := NewAdapterHealthSnapshot(false, 4, testHealthProof(8))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot(unhealthy) error = %v", err)
	}
	if unhealthy.Healthy() {
		t.Fatal("unhealthy snapshot reports healthy")
	}

	if _, err := NewAdapterHealthSnapshot(true, 0, proof); !errors.Is(err, ErrInvalidAdapterHealthSnapshot) {
		t.Fatalf("zero revision error = %v, want ErrInvalidAdapterHealthSnapshot", err)
	}
	if _, err := NewAdapterHealthSnapshot(true, 1, [sha256.Size]byte{}); !errors.Is(err, ErrInvalidAdapterHealthSnapshot) {
		t.Fatalf("zero proof error = %v, want ErrInvalidAdapterHealthSnapshot", err)
	}
}

func testHealthProof(seed byte) [sha256.Size]byte {
	var proof [sha256.Size]byte
	for index := range proof {
		proof[index] = seed + byte(index)
	}
	return proof
}
