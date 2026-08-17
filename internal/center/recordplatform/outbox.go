package recordplatform

import (
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrInvalidOutboxEvent   = errors.New("invalid outbox event")
	ErrInvalidOutboxEnqueue = errors.New("invalid outbox enqueue")
	ErrInvalidOutboxClaim   = errors.New("invalid outbox claim")
)

const (
	OutboxEventKindRecordCreated            = "record_created"
	OutboxEventKindRecordUpdated            = "record_updated"
	OutboxEventKindRecordDeleted            = "record_deleted"
	OutboxEventKindRecordOwnerChanged       = "record_owner_changed"
	OutboxEventKindRecordParticipantChanged = "record_participant_changed"
	OutboxEventKindRecordActionCreated      = "record_action_created"
	OutboxEventKindRecordActionUpdated      = "record_action_updated"
	OutboxEventKindRecordActionAssigned     = "record_action_assigned"
	OutboxEventKindRecordActionCompleted    = "record_action_completed"
	OutboxEventKindRecordActionCancelled    = "record_action_cancelled"
	OutboxEventKindRecordActionReopened     = "record_action_reopened"
	OutboxEventKindRecordCommentCreated     = "record_comment_created"
	OutboxEventKindRecordCommentEdited      = "record_comment_edited"
	OutboxEventKindRecordCommentRedacted    = "record_comment_redacted"
	OutboxEventKindRecordCommentReplied     = "record_comment_replied"
	OutboxEventKindRecordCommentMentioned   = "record_comment_mentioned"
)

const (
	OutboxSubjectKindRecord  = "record"
	OutboxSubjectKindAction  = "action"
	OutboxSubjectKindComment = "comment"
)

// OutboxEvent contains only durable identity and authorization epoch data. It
// intentionally cannot retain a payload, recipient, rendered body, or sender
// callback because those are re-derived after fresh authorization by a worker.
type OutboxEvent struct {
	RowID              int64
	ProjectID          string
	EventKind          string
	SubjectKind        string
	SubjectID          string
	SourceVersion      uint64
	AuthorizationEpoch uint64
}

// OutboxEnqueueInputV1 requests an identity-only outbox row. PostgreSQL
// derives created and expiry timestamps from transaction time.
type OutboxEnqueueInputV1 struct {
	Event        OutboxEvent
	ExpiresAfter time.Duration
}

// OutboxEventRecordV1 is the content-free durable row identity returned after
// an enqueue succeeds inside the caller's transaction.
type OutboxEventRecordV1 struct {
	Event     OutboxEvent
	ExpiresAt time.Time
}

// OutboxClaimInputV1 identifies the worker and duration for one database-time
// ownership claim. No local timestamp can grant durable owner authority.
type OutboxClaimInputV1 struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
}

// ClaimedOutboxEventV1 is an event returned only after a processing claim has
// committed. The owner fence must accompany every send, retry, or cancellation.
type ClaimedOutboxEventV1 struct {
	Event     OutboxEvent
	Owner     OwnerLease
	ExpiresAt time.Time
}

// Validate checks the closed identity-only event schema before any database
// write. It does not normalize caller input.
func (event OutboxEvent) Validate() error {
	if event.RowID < 0 {
		return fmt.Errorf("%w: row id", ErrInvalidOutboxEvent)
	}
	if err := ValidateProjectID(ProjectID(event.ProjectID)); err != nil {
		return fmt.Errorf("%w: project", ErrInvalidOutboxEvent)
	}
	expectedSubjectKind, ok := outboxSubjectKindForEventKind(event.EventKind)
	if !ok {
		return fmt.Errorf("%w: event kind", ErrInvalidOutboxEvent)
	}
	if event.SubjectKind != expectedSubjectKind {
		return fmt.Errorf("%w: subject kind", ErrInvalidOutboxEvent)
	}
	if !validOutboxSubjectID(event.SubjectID) {
		return fmt.Errorf("%w: subject id", ErrInvalidOutboxEvent)
	}
	if event.SourceVersion > math.MaxInt64 || notificationProducingOutboxEventKind(event.EventKind) != (event.SourceVersion > 0) {
		return fmt.Errorf("%w: source version", ErrInvalidOutboxEvent)
	}
	return nil
}

func notificationProducingOutboxEventKind(kind string) bool {
	switch kind {
	case OutboxEventKindRecordOwnerChanged,
		OutboxEventKindRecordParticipantChanged,
		OutboxEventKindRecordActionAssigned,
		OutboxEventKindRecordActionCompleted,
		OutboxEventKindRecordActionCancelled,
		OutboxEventKindRecordCommentReplied,
		OutboxEventKindRecordCommentMentioned:
		return true
	default:
		return false
	}
}

// Validate rejects durations PostgreSQL cannot represent in the transaction
// microsecond arithmetic used for the immutable enqueue request.
func (input OutboxEnqueueInputV1) Validate() error {
	if err := input.Event.Validate(); err != nil {
		return fmt.Errorf("%w: event: %w", ErrInvalidOutboxEnqueue, err)
	}
	if input.Event.RowID != 0 {
		return fmt.Errorf("%w: row identity", ErrInvalidOutboxEnqueue)
	}
	if input.ExpiresAfter.Microseconds() <= 0 {
		return fmt.Errorf("%w: expiry", ErrInvalidOutboxEnqueue)
	}
	return nil
}

// Validate checks a database-time claim request before it reaches a claim
// statement. Sub-microsecond leases cannot be represented by the SQL contract.
func (input OutboxClaimInputV1) Validate() error {
	if !validRecordPlatformOwnerID(input.OwnerID) || input.OwnerLeaseDuration.Microseconds() <= 0 {
		return fmt.Errorf("%w: owner or lease", ErrInvalidOutboxClaim)
	}
	return nil
}

// Validate ensures a claimed row cannot outlive its owner fence. This allows a
// worker to stop safely without treating its local clock as durable authority.
func (claim ClaimedOutboxEventV1) Validate() error {
	if claim.Event.RowID <= 0 {
		return fmt.Errorf("%w: row id", ErrInvalidOutboxClaim)
	}
	if err := claim.Event.Validate(); err != nil {
		return fmt.Errorf("%w: event: %w", ErrInvalidOutboxClaim, err)
	}
	if err := claim.Owner.Validate(); err != nil {
		return fmt.Errorf("%w: owner: %w", ErrInvalidOutboxClaim, err)
	}
	if claim.ExpiresAt.IsZero() || !claim.ExpiresAt.After(claim.Owner.ExpiresAt) {
		return fmt.Errorf("%w: expiry", ErrInvalidOutboxClaim)
	}
	return nil
}

func outboxSubjectKindForEventKind(kind string) (string, bool) {
	switch kind {
	case OutboxEventKindRecordCreated,
		OutboxEventKindRecordUpdated,
		OutboxEventKindRecordDeleted,
		OutboxEventKindRecordOwnerChanged,
		OutboxEventKindRecordParticipantChanged:
		return OutboxSubjectKindRecord, true
	case OutboxEventKindRecordActionCreated,
		OutboxEventKindRecordActionUpdated,
		OutboxEventKindRecordActionAssigned,
		OutboxEventKindRecordActionCompleted,
		OutboxEventKindRecordActionCancelled,
		OutboxEventKindRecordActionReopened:
		return OutboxSubjectKindAction, true
	case OutboxEventKindRecordCommentCreated,
		OutboxEventKindRecordCommentEdited,
		OutboxEventKindRecordCommentRedacted,
		OutboxEventKindRecordCommentReplied,
		OutboxEventKindRecordCommentMentioned:
		return OutboxSubjectKindComment, true
	default:
		return "", false
	}
}

func validOutboxSubjectID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	if isDeletionRequestTokenTransportEncoding(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' {
			return false
		}
	}
	return true
}
