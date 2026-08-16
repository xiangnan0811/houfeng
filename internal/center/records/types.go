// Package records owns the transport-neutral record, revision, and draft
// domain contracts.
package records

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidLifecycle               = errors.New("invalid record lifecycle")
	ErrInvalidRecordType              = errors.New("invalid record type")
	ErrInvalidBusinessStatus          = errors.New("invalid record business status")
	ErrStatusTransitionReasonRequired = errors.New("record status transition reason required")
	ErrInvalidTemplate                = errors.New("invalid record template")
	ErrTemplateNotFound               = errors.New("record template not found")
	ErrInvalidRevisionInput           = errors.New("invalid complete record revision input")
)

type Lifecycle string

const (
	LifecycleActive   Lifecycle = "active"
	LifecycleArchived Lifecycle = "archived"
)

type RecordType string

type BusinessStatus string

type StatusGroup string

type TemplateProvenance struct {
	ID      string
	Version uint64
}

type MarkdownDialectVersion uint64

const MarkdownDialectVersionV1 MarkdownDialectVersion = 1

type ImpactLevel string

type SubjectKind string

type RelationRole string

type RevisionSubject struct {
	RegistryVersion      uint64
	Kind                 SubjectKind
	Role                 RelationRole
	SourceID             string
	Primary              bool
	IdentitySnapshot     map[string]string
	CaptureAuthorization recordauth.SourceAuthorization
}

type RevisionParticipantSnapshot struct {
	ParticipantID    string
	IdentitySnapshot map[string]string
}

type CompleteRevisionValues struct {
	Title                  string
	BodyMarkdown           string
	MarkdownDialectVersion MarkdownDialectVersion
	RecordType             RecordType
	BusinessStatus         BusinessStatus
	ImpactLevel            ImpactLevel
	OccurredAt             *time.Time
	CompletedAt            *time.Time
	VisibilityScope        recordauth.VisibilityScope
	Subjects               []RevisionSubject
	Tags                   []string
	OwnerID                string
	Participants           []RevisionParticipantSnapshot
	AttachmentIDs          []string
	EvidenceSnapshotIDs    []string
	FollowUpAt             *time.Time
	Template               *TemplateProvenance
	AuthorID               string
	SaveReason             string
}

// CompleteRevisionInput is the normalized immutable input for one complete
// revision. Construct it through NormalizeCompleteRevisionInput.
type CompleteRevisionInput struct {
	title                  string
	bodyMarkdown           string
	markdownDialectVersion MarkdownDialectVersion
	recordType             RecordType
	businessStatus         BusinessStatus
	statusGroup            StatusGroup
	impactLevel            ImpactLevel
	occurredAt             *time.Time
	completedAt            *time.Time
	visibilityScope        recordauth.VisibilityScope
	subjects               []RevisionSubject
	tags                   []string
	ownerID                string
	participants           []RevisionParticipantSnapshot
	attachmentIDs          []string
	evidenceSnapshotIDs    []string
	followUpAt             *time.Time
	template               *TemplateProvenance
	authorID               string
	saveReason             string
	canonicalHash          [sha256.Size]byte
}

func (input CompleteRevisionInput) Title() string {
	return input.title
}

func (input CompleteRevisionInput) BodyMarkdown() string {
	return input.bodyMarkdown
}

func (input CompleteRevisionInput) MarkdownDialectVersion() MarkdownDialectVersion {
	return input.markdownDialectVersion
}

func (input CompleteRevisionInput) RecordType() RecordType {
	return input.recordType
}

func (input CompleteRevisionInput) BusinessStatus() BusinessStatus {
	return input.businessStatus
}

func (input CompleteRevisionInput) StatusGroup() StatusGroup {
	return input.statusGroup
}

func (input CompleteRevisionInput) ImpactLevel() ImpactLevel {
	return input.impactLevel
}

func (input CompleteRevisionInput) OccurredAt() *time.Time {
	return cloneTimePointer(input.occurredAt)
}

func (input CompleteRevisionInput) CompletedAt() *time.Time {
	return cloneTimePointer(input.completedAt)
}

func (input CompleteRevisionInput) VisibilityScope() recordauth.VisibilityScope {
	return cloneVisibilityScope(input.visibilityScope)
}

func (input CompleteRevisionInput) Subjects() []RevisionSubject {
	return cloneRevisionSubjects(input.subjects)
}

func (input CompleteRevisionInput) Tags() []string {
	return append([]string(nil), input.tags...)
}

func (input CompleteRevisionInput) OwnerID() string {
	return input.ownerID
}

func (input CompleteRevisionInput) Participants() []RevisionParticipantSnapshot {
	return cloneRevisionParticipants(input.participants)
}

func (input CompleteRevisionInput) AttachmentIDs() []string {
	return append([]string{}, input.attachmentIDs...)
}

func (input CompleteRevisionInput) EvidenceSnapshotIDs() []string {
	return append([]string{}, input.evidenceSnapshotIDs...)
}

func (input CompleteRevisionInput) FollowUpAt() *time.Time {
	return cloneTimePointer(input.followUpAt)
}

func (input CompleteRevisionInput) Template() *TemplateProvenance {
	if input.template == nil {
		return nil
	}
	cloned := *input.template
	return &cloned
}

func (input CompleteRevisionInput) AuthorID() string {
	return input.authorID
}

func (input CompleteRevisionInput) SaveReason() string {
	return input.saveReason
}

// CanonicalHash identifies revision content for deterministic no-change
// detection. AuthorID and SaveReason are immutable commit metadata and are
// deliberately outside this digest.
func (input CompleteRevisionInput) CanonicalHash() [sha256.Size]byte {
	return input.canonicalHash
}

const (
	RecordTypeTroubleshooting       RecordType = "troubleshooting"
	RecordTypeMaintenance           RecordType = "maintenance"
	RecordTypeMigration             RecordType = "migration"
	RecordTypeProviderCommunication RecordType = "provider_communication"
	RecordTypeBilling               RecordType = "billing"
	RecordTypeImportantFinding      RecordType = "important_finding"
	RecordTypeNote                  RecordType = "note"
)

const (
	StatusPendingInvestigation BusinessStatus = "pending_investigation"
	StatusInvestigating        BusinessStatus = "investigating"
	StatusVerifying            BusinessStatus = "verifying"
	StatusResolved             BusinessStatus = "resolved"
	StatusClosed               BusinessStatus = "closed"
	StatusCancelled            BusinessStatus = "cancelled"
	StatusPlanned              BusinessStatus = "planned"
	StatusExecuting            BusinessStatus = "executing"
	StatusCompleted            BusinessStatus = "completed"
	StatusPendingContact       BusinessStatus = "pending_contact"
	StatusWaitingProvider      BusinessStatus = "waiting_provider"
	StatusWaitingInternal      BusinessStatus = "waiting_internal"
	StatusPendingReview        BusinessStatus = "pending_review"
	StatusProcessing           BusinessStatus = "processing"
)

const (
	StatusGroupPending      StatusGroup = "pending"
	StatusGroupInProgress   StatusGroup = "in_progress"
	StatusGroupWaiting      StatusGroup = "waiting"
	StatusGroupVerification StatusGroup = "verification"
	StatusGroupCompleted    StatusGroup = "completed"
	StatusGroupCancelled    StatusGroup = "cancelled"
)

func ValidateLifecycle(lifecycle Lifecycle) error {
	if lifecycle != LifecycleActive && lifecycle != LifecycleArchived {
		return fmt.Errorf("%w: %q", ErrInvalidLifecycle, lifecycle)
	}
	return nil
}
