package records

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

var (
	ErrInvalidRevisionParticipant    = errors.New("invalid revision participant")
	ErrInvalidRevisionCommand        = errors.New("invalid revision command")
	ErrInvalidRecordLifecycleCommand = errors.New("invalid record lifecycle command")
	ErrRecordNotFound                = errors.New("record not found")
	ErrRecordAlreadyExists           = errors.New("record already exists")
	ErrRecordRevisionConflict        = errors.New("record revision conflict")
	ErrRecordDeletionReserved        = errors.New("record deletion reserved")
)

type DomainActivityKind string

const (
	DomainActivityRecordCreated    DomainActivityKind = "record_created"
	DomainActivityRecordRevised    DomainActivityKind = "record_revised"
	DomainActivityRecordRestored   DomainActivityKind = "record_restored"
	DomainActivityRecordArchived   DomainActivityKind = "record_archived"
	DomainActivityRecordUnarchived DomainActivityKind = "record_unarchived"
)

type RevisionCommitCommand struct {
	RecordID           string
	BaseRevisionID     string
	LockVersion        uint64
	AuthorizationEpoch uint64
	DraftID            string
	DraftETag          DraftETag
	Input              CompleteRevisionInput
	ActivityKind       DomainActivityKind
	OutboxTTL          time.Duration
	Idempotency        recordplatform.IdempotencyClaimInputV1
}

type RevisionCommitResult struct {
	RecordID           string
	RevisionID         string
	RevisionNo         uint64
	LockVersion        uint64
	AuthorizationEpoch uint64
	Lifecycle          Lifecycle
	Created            bool
	Replayed           bool
	CommittedAt        time.Time
}

type RecordLifecycleCommand struct {
	RecordID           string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	TargetLifecycle    Lifecycle
	ActorID            string
	OutboxTTL          time.Duration
	Idempotency        recordplatform.IdempotencyClaimInputV1
}

type RecordLifecycleResult struct {
	RecordID           string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	Lifecycle          Lifecycle
	Replayed           bool
	ChangedAt          time.Time
}

// RevisionCommitted is the transaction-local fact passed to registered
// revision participants after revision authority and the current projection
// have been written.
type RevisionCommitted struct {
	DraftID string
	Result  RevisionCommitResult
	Input   CompleteRevisionInput
}

func (command RevisionCommitCommand) Validate() error {
	if !validRecordRootID(command.RecordID) {
		return fmt.Errorf("%w: record id", ErrInvalidRevisionCommand)
	}
	switch command.Idempotency.Key.OperationKind {
	case recordplatform.OperationKindRecordCreate:
		if command.BaseRevisionID != "" || command.LockVersion != 0 || command.AuthorizationEpoch != 0 || command.ActivityKind != DomainActivityRecordCreated {
			return fmt.Errorf("%w: create shape", ErrInvalidRevisionCommand)
		}
	case recordplatform.OperationKindRecordUpdate:
		if !validRevisionID(command.BaseRevisionID) || command.LockVersion == 0 || command.AuthorizationEpoch == 0 ||
			(command.ActivityKind != DomainActivityRecordRevised && command.ActivityKind != DomainActivityRecordRestored) {
			return fmt.Errorf("%w: update shape", ErrInvalidRevisionCommand)
		}
	default:
		return fmt.Errorf("%w: operation", ErrInvalidRevisionCommand)
	}
	if command.Input.title == "" || command.Input.canonicalHash == [32]byte{} {
		return fmt.Errorf("%w: input", ErrInvalidRevisionCommand)
	}
	_, draftETagErr := command.DraftETag.Digest()
	hasDraftID := command.DraftID != ""
	hasDraftETag := draftETagErr == nil
	if hasDraftID != hasDraftETag || (hasDraftID && !validDraftID(command.DraftID)) {
		return fmt.Errorf("%w: published draft", ErrInvalidRevisionCommand)
	}
	if string(command.Input.visibilityScope.ProjectID) != string(recordplatform.ProjectIDDefault) {
		return fmt.Errorf("%w: project", ErrInvalidRevisionCommand)
	}
	if command.Idempotency.Key.ProjectID != recordplatform.ProjectIDDefault {
		return fmt.Errorf("%w: idempotency project", ErrInvalidRevisionCommand)
	}
	if err := command.Idempotency.Validate(); err != nil {
		return fmt.Errorf("%w: idempotency: %w", ErrInvalidRevisionCommand, err)
	}
	if command.OutboxTTL.Microseconds() <= 0 {
		return fmt.Errorf("%w: outbox ttl", ErrInvalidRevisionCommand)
	}
	return nil
}

func (command RevisionCommitCommand) RevisionID() (string, error) {
	return deterministicRevisionIdentity("rrv_", command.Idempotency.RequestFingerprint)
}

func (command RevisionCommitCommand) ActivityID() (string, error) {
	return deterministicRevisionIdentity("rac_", command.Idempotency.RequestFingerprint)
}

func (command RecordLifecycleCommand) Validate() error {
	if !validRecordRootID(command.RecordID) || !validRevisionID(command.CurrentRevisionID) ||
		command.LockVersion == 0 || command.AuthorizationEpoch == 0 {
		return fmt.Errorf("%w: identity or versions", ErrInvalidRecordLifecycleCommand)
	}
	if err := ValidateLifecycle(command.TargetLifecycle); err != nil {
		return fmt.Errorf("%w: lifecycle", ErrInvalidRecordLifecycleCommand)
	}
	if err := recordauth.ValidateActorUserID(command.ActorID); err != nil {
		return fmt.Errorf("%w: actor", ErrInvalidRecordLifecycleCommand)
	}
	if command.Idempotency.Key.ProjectID != recordplatform.ProjectIDDefault ||
		command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordUpdate {
		return fmt.Errorf("%w: idempotency key", ErrInvalidRecordLifecycleCommand)
	}
	if err := command.Idempotency.Validate(); err != nil {
		return fmt.Errorf("%w: idempotency: %w", ErrInvalidRecordLifecycleCommand, err)
	}
	if command.OutboxTTL.Microseconds() <= 0 {
		return fmt.Errorf("%w: outbox ttl", ErrInvalidRecordLifecycleCommand)
	}
	return nil
}

func (command RecordLifecycleCommand) ActivityKind() (DomainActivityKind, error) {
	if err := command.Validate(); err != nil {
		return "", err
	}
	if command.TargetLifecycle == LifecycleArchived {
		return DomainActivityRecordArchived, nil
	}
	return DomainActivityRecordUnarchived, nil
}

func (command RecordLifecycleCommand) ActivityID() (string, error) {
	if err := command.Validate(); err != nil {
		return "", err
	}
	digest, err := command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		return "", fmt.Errorf("%w: request fingerprint", ErrInvalidRecordLifecycleCommand)
	}
	return "rac_" + hex.EncodeToString(digest[:]), nil
}

func deterministicRevisionIdentity(prefix string, fingerprint recordplatform.RequestFingerprintV1) (string, error) {
	digest, err := fingerprint.PersistedBytes()
	if err != nil {
		return "", fmt.Errorf("%w: request fingerprint", ErrInvalidRevisionCommand)
	}
	return prefix + hex.EncodeToString(digest[:]), nil
}

func validRecordRootID(value string) bool {
	if len(value) < len("rec_")+1 || len(value) > len("rec_")+64 || value[:len("rec_")] != "rec_" {
		return false
	}
	for _, character := range value[len("rec_"):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validRevisionID(value string) bool {
	if len(value) < len("rrv_")+1 || len(value) > len("rrv_")+64 || value[:len("rrv_")] != "rrv_" {
		return false
	}
	for _, character := range value[len("rrv_"):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// RevisionParticipant extends a revision transaction with another durable
// projection. Implementations must only use the supplied transaction and must
// not perform network or other external calls.
type RevisionParticipant interface {
	Name() string
	ApplyRevision(context.Context, pgx.Tx, RevisionCommitted) error
}

// RevisionParticipantRegistry owns a deterministic, bootstrap-time-validated
// participant order.
type RevisionParticipantRegistry struct {
	participants []RevisionParticipant
	names        []string
}

func NewRevisionParticipantRegistry(participants []RevisionParticipant) (RevisionParticipantRegistry, error) {
	ordered := append([]RevisionParticipant(nil), participants...)
	for _, participant := range ordered {
		if nilRevisionParticipant(participant) || !validRegistryToken(participant.Name(), 64) {
			return RevisionParticipantRegistry{}, ErrInvalidRevisionParticipant
		}
	}
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].Name() < ordered[right].Name()
	})

	names := make([]string, len(ordered))
	for index, participant := range ordered {
		name := participant.Name()
		if index > 0 && names[index-1] == name {
			return RevisionParticipantRegistry{}, fmt.Errorf("%w: duplicate name", ErrInvalidRevisionParticipant)
		}
		names[index] = name
	}
	return RevisionParticipantRegistry{participants: ordered, names: names}, nil
}

func (registry RevisionParticipantRegistry) Names() []string {
	return append([]string(nil), registry.names...)
}

func (registry RevisionParticipantRegistry) ApplyRevision(ctx context.Context, tx pgx.Tx, committed RevisionCommitted) error {
	for _, participant := range registry.participants {
		if err := participant.ApplyRevision(ctx, tx, committed); err != nil {
			return fmt.Errorf("apply revision participant %q: %w", participant.Name(), err)
		}
	}
	return nil
}

func nilRevisionParticipant(participant RevisionParticipant) bool {
	if participant == nil {
		return true
	}
	value := reflect.ValueOf(participant)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
