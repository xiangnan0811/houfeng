// Package recorddeletion owns transport-neutral permanent-deletion readiness
// and orchestration contracts for Records.
package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sort"
)

var (
	ErrInvalidAdapterDescriptor     = errors.New("invalid record deletion adapter descriptor")
	ErrInvalidAdapterHealthSnapshot = errors.New("invalid record deletion adapter health snapshot")
	ErrInvalidAdapterRegistry       = errors.New("invalid record deletion adapter registry")
	ErrDeletionSafetyUnavailable    = errors.New("record deletion safety unavailable")
)

type AdapterName string

const (
	AdapterNameRecordCore               AdapterName = "record_core"
	AdapterNameRecordAttachments        AdapterName = "record_attachments"
	AdapterNameRecordEvidence           AdapterName = "record_evidence"
	AdapterNameRecordMarkdownClient     AdapterName = "record_markdown_client"
	AdapterNameRecordSearch             AdapterName = "record_search"
	AdapterNameRecordActivityProjection AdapterName = "record_activity_projection"
	AdapterNameRecordComparison         AdapterName = "record_comparison"
	AdapterNameRecordCollaboration      AdapterName = "record_collaboration"
	AdapterNameRecordPortability        AdapterName = "record_portability"
)

var requiredAdapterNames = []AdapterName{
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

type SurfaceName string

var recordCoreSurfaceNames = []SurfaceName{
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

var recordAttachmentsSurfaceNames = []SurfaceName{
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

var recordEvidenceSurfaceNames = []SurfaceName{
	"evidence_capture_intents",
	"evidence_copy_lineage",
	"evidence_payload_gc_receipts",
	"evidence_payloads",
	"evidence_purge_receipts",
	"evidence_snapshots",
	"record_revision_evidence",
}

var recordCollaborationSurfaceNames = []SurfaceName{
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

// record_search owns only the per-record projection rows. Generations and
// rebuild jobs are index-wide state shared by every record, so one record's
// purge must not be able to reach them.
var recordSearchSurfaceNames = []SurfaceName{
	"record_search_documents",
	"record_search_purge_receipts",
	"record_search_subjects",
}

// record_activity_projection owns only per-record derived rows. Projection
// heads and checkpoints are generation-wide: one record's purge must not be
// able to move every other record's watermark or steal a source lease.
var recordActivitySurfaceNames = []SurfaceName{
	"record_activity_projection",
	"record_activity_purge_receipts",
	"record_activity_revision_intervals",
	"record_activity_subjects",
}

// record_portability owns the 0058 job, artifact, origin, and receipt tables.
// Origin tombstones are listed because the adapter is responsible for them, but
// a record purge must leave those rows so a purged identity cannot be restored.
var recordPortabilitySurfaceNames = []SurfaceName{
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

func RequiredAdapterNames() []AdapterName {
	return append([]AdapterName(nil), requiredAdapterNames...)
}

func RecordCoreSurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordCoreSurfaceNames...)
}

func RecordAttachmentsSurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordAttachmentsSurfaceNames...)
}

func RecordEvidenceSurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordEvidenceSurfaceNames...)
}

func RecordCollaborationSurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordCollaborationSurfaceNames...)
}

func RecordSearchSurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordSearchSurfaceNames...)
}

func RecordActivitySurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordActivitySurfaceNames...)
}

func RecordPortabilitySurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordPortabilitySurfaceNames...)
}

type AdapterDescriptor struct {
	name     AdapterName
	surfaces []SurfaceName
}

func NewAdapterDescriptor(name AdapterName, surfaces []SurfaceName) (AdapterDescriptor, error) {
	normalizedSurfaces := append([]SurfaceName(nil), surfaces...)
	sort.Slice(normalizedSurfaces, func(left, right int) bool {
		return normalizedSurfaces[left] < normalizedSurfaces[right]
	})
	descriptor := AdapterDescriptor{name: name, surfaces: normalizedSurfaces}
	if err := descriptor.validate(); err != nil {
		return AdapterDescriptor{}, err
	}
	return descriptor, nil
}

func (descriptor AdapterDescriptor) Name() AdapterName {
	return descriptor.name
}

func (descriptor AdapterDescriptor) Surfaces() []SurfaceName {
	return append([]SurfaceName(nil), descriptor.surfaces...)
}

func (descriptor AdapterDescriptor) validate() error {
	if !knownAdapterName(descriptor.name) || len(descriptor.surfaces) == 0 {
		return ErrInvalidAdapterDescriptor
	}
	for index, surface := range descriptor.surfaces {
		if !validSurfaceName(surface) {
			return fmt.Errorf("%w: surface", ErrInvalidAdapterDescriptor)
		}
		if index > 0 && descriptor.surfaces[index-1] >= surface {
			return fmt.Errorf("%w: duplicate or unordered surface", ErrInvalidAdapterDescriptor)
		}
	}
	if descriptor.name == AdapterNameRecordCore && !slices.Equal(descriptor.surfaces, recordCoreSurfaceNames) {
		return fmt.Errorf("%w: record_core surfaces", ErrInvalidAdapterDescriptor)
	}
	if descriptor.name == AdapterNameRecordAttachments && !slices.Equal(descriptor.surfaces, recordAttachmentsSurfaceNames) {
		return fmt.Errorf("%w: record_attachments surfaces", ErrInvalidAdapterDescriptor)
	}
	if descriptor.name == AdapterNameRecordEvidence && !slices.Equal(descriptor.surfaces, recordEvidenceSurfaceNames) {
		return fmt.Errorf("%w: record_evidence surfaces", ErrInvalidAdapterDescriptor)
	}
	if descriptor.name == AdapterNameRecordCollaboration && !slices.Equal(descriptor.surfaces, recordCollaborationSurfaceNames) {
		return fmt.Errorf("%w: record_collaboration surfaces", ErrInvalidAdapterDescriptor)
	}
	if descriptor.name == AdapterNameRecordSearch && !slices.Equal(descriptor.surfaces, recordSearchSurfaceNames) {
		return fmt.Errorf("%w: record_search surfaces", ErrInvalidAdapterDescriptor)
	}
	if descriptor.name == AdapterNameRecordActivityProjection && !slices.Equal(descriptor.surfaces, recordActivitySurfaceNames) {
		return fmt.Errorf("%w: record_activity_projection surfaces", ErrInvalidAdapterDescriptor)
	}
	if descriptor.name == AdapterNameRecordPortability && !slices.Equal(descriptor.surfaces, recordPortabilitySurfaceNames) {
		return fmt.Errorf("%w: record_portability surfaces", ErrInvalidAdapterDescriptor)
	}
	return nil
}

type AdapterHealthSnapshot struct {
	healthy     bool
	revision    uint64
	proofDigest [sha256.Size]byte
}

func NewAdapterHealthSnapshot(
	healthy bool,
	revision uint64,
	proofDigest [sha256.Size]byte,
) (AdapterHealthSnapshot, error) {
	snapshot := AdapterHealthSnapshot{healthy: healthy, revision: revision, proofDigest: proofDigest}
	if err := snapshot.validate(); err != nil {
		return AdapterHealthSnapshot{}, err
	}
	return snapshot, nil
}

func (snapshot AdapterHealthSnapshot) Healthy() bool {
	return snapshot.healthy
}

func (snapshot AdapterHealthSnapshot) Revision() uint64 {
	return snapshot.revision
}

func (snapshot AdapterHealthSnapshot) ProofDigest() [sha256.Size]byte {
	return snapshot.proofDigest
}

func (snapshot AdapterHealthSnapshot) Validate() error {
	return snapshot.validate()
}

func (snapshot AdapterHealthSnapshot) validate() error {
	if snapshot.revision == 0 || snapshot.proofDigest == [sha256.Size]byte{} {
		return ErrInvalidAdapterHealthSnapshot
	}
	return nil
}

type Adapter interface {
	Descriptor() AdapterDescriptor
	HealthSnapshot(context.Context) (AdapterHealthSnapshot, error)
}

type AdapterReadinessSnapshot struct {
	name     AdapterName
	surfaces []SurfaceName
	health   AdapterHealthSnapshot
}

func (snapshot AdapterReadinessSnapshot) Name() AdapterName {
	return snapshot.name
}

func (snapshot AdapterReadinessSnapshot) Surfaces() []SurfaceName {
	return append([]SurfaceName(nil), snapshot.surfaces...)
}

func (snapshot AdapterReadinessSnapshot) Health() AdapterHealthSnapshot {
	return snapshot.health
}

func knownAdapterName(name AdapterName) bool {
	switch name {
	case AdapterNameRecordCore,
		AdapterNameRecordAttachments,
		AdapterNameRecordEvidence,
		AdapterNameRecordMarkdownClient,
		AdapterNameRecordSearch,
		AdapterNameRecordActivityProjection,
		AdapterNameRecordComparison,
		AdapterNameRecordCollaboration,
		AdapterNameRecordPortability:
		return true
	default:
		return false
	}
}

func validSurfaceName(surface SurfaceName) bool {
	if len(surface) == 0 || len(surface) > 128 || surface[0] < 'a' || surface[0] > 'z' {
		return false
	}
	for _, character := range surface[1:] {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '.' && character != ':' && character != '-' {
			return false
		}
	}
	last := surface[len(surface)-1]
	return (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
}
