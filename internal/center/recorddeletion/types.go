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

func RequiredAdapterNames() []AdapterName {
	return append([]AdapterName(nil), requiredAdapterNames...)
}

func RecordCoreSurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordCoreSurfaceNames...)
}

func RecordAttachmentsSurfaceNames() []SurfaceName {
	return append([]SurfaceName(nil), recordAttachmentsSurfaceNames...)
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
