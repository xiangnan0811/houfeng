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
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordMarkdownClient, input)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor() error = %v", err)
	}
	if descriptor.Name() != AdapterNameRecordMarkdownClient {
		t.Fatalf("descriptor.Name() = %q, want %q", descriptor.Name(), AdapterNameRecordMarkdownClient)
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

func TestRecordSearchDescriptorRequiresExactOwnedTables(t *testing.T) {
	t.Parallel()

	// Generations and rebuild jobs are index-wide, not record-owned, so they are
	// deliberately absent: a single record's purge must not be able to remove the
	// generation every other record is being served from.
	want := []SurfaceName{
		"record_search_documents",
		"record_search_purge_receipts",
		"record_search_subjects",
	}
	if got := RecordSearchSurfaceNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordSearchSurfaceNames() = %#v, want %#v", got, want)
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordSearch, want)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(record_search) error = %v", err)
	}
	if got := RecordSearchSurfaceDigest(); got == ([sha256.Size]byte{}) || got != digestAdapterSurfaces(descriptor) {
		t.Fatalf("RecordSearchSurfaceDigest() = %x", got)
	}
	if _, err := NewAdapterDescriptor(AdapterNameRecordSearch, want[1:]); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("missing search surface error = %v", err)
	}
	for _, extra := range []SurfaceName{"record_search_generations", "record_search_rebuild_jobs"} {
		widened := append(RecordSearchSurfaceNames(), extra)
		if _, err := NewAdapterDescriptor(AdapterNameRecordSearch, widened); !errors.Is(err, ErrInvalidAdapterDescriptor) {
			t.Fatalf("extra search surface %q error = %v", extra, err)
		}
	}
	returned := RecordSearchSurfaceNames()
	returned[0] = "tampered"
	if fresh := RecordSearchSurfaceNames(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RecordSearchSurfaceNames() after mutation = %#v", fresh)
	}
}

func TestRecordActivityDescriptorRequiresExactOwnedTables(t *testing.T) {
	t.Parallel()

	// Heads and checkpoints are generation-wide, so they are deliberately
	// absent: a single record's purge must not be able to move every other
	// record's watermark or steal a source lease.
	want := []SurfaceName{
		"record_activity_projection",
		"record_activity_purge_receipts",
		"record_activity_revision_intervals",
		"record_activity_subjects",
	}
	if got := RecordActivitySurfaceNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordActivitySurfaceNames() = %#v, want %#v", got, want)
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordActivityProjection, want)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(record_activity_projection) error = %v", err)
	}
	if got := RecordActivitySurfaceDigest(); got == ([sha256.Size]byte{}) || got != digestAdapterSurfaces(descriptor) {
		t.Fatalf("RecordActivitySurfaceDigest() = %x", got)
	}
	if _, err := NewAdapterDescriptor(AdapterNameRecordActivityProjection, want[1:]); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("missing activity surface error = %v", err)
	}
	for _, extra := range []SurfaceName{
		"record_activity_projection_heads",
		"record_activity_projection_checkpoints",
	} {
		widened := append(RecordActivitySurfaceNames(), extra)
		if _, err := NewAdapterDescriptor(AdapterNameRecordActivityProjection, widened); !errors.Is(err, ErrInvalidAdapterDescriptor) {
			t.Fatalf("extra activity surface %q error = %v", extra, err)
		}
	}
	returned := RecordActivitySurfaceNames()
	returned[0] = "tampered"
	if fresh := RecordActivitySurfaceNames(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RecordActivitySurfaceNames() after mutation = %#v", fresh)
	}
}

func TestRecordPortabilityDescriptorRequiresExactOwnedTables(t *testing.T) {
	t.Parallel()

	// Origin tombstones stay after a record purge, but the adapter still owns
	// the table so Child 11 can compose one closed surface list.
	want := []SurfaceName{
		"record_export_artifacts",
		"record_export_jobs",
		"record_import_artifacts",
		"record_import_entity_mappings",
		"record_import_jobs",
		"record_import_plans",
		"record_origin_tombstones",
		"record_origins",
		"record_portability_purge_receipts",
	}
	if got := RecordPortabilitySurfaceNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordPortabilitySurfaceNames() = %#v, want %#v", got, want)
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordPortability, want)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(record_portability) error = %v", err)
	}
	if got := RecordPortabilitySurfaceDigest(); got == ([sha256.Size]byte{}) || got != digestAdapterSurfaces(descriptor) {
		t.Fatalf("RecordPortabilitySurfaceDigest() = %x", got)
	}
	if _, err := NewAdapterDescriptor(AdapterNameRecordPortability, want[1:]); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("missing portability surface error = %v", err)
	}
	widened := append(RecordPortabilitySurfaceNames(), "experience_logs")
	if _, err := NewAdapterDescriptor(AdapterNameRecordPortability, widened); !errors.Is(err, ErrInvalidAdapterDescriptor) {
		t.Fatalf("extra portability surface error = %v", err)
	}
	returned := RecordPortabilitySurfaceNames()
	returned[0] = "tampered"
	if fresh := RecordPortabilitySurfaceNames(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RecordPortabilitySurfaceNames() after mutation = %#v", fresh)
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
