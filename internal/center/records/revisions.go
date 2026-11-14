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

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
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
	DomainActivityRecordCreated       DomainActivityKind = "record_created"
	DomainActivityRecordRevised       DomainActivityKind = "record_revised"
	DomainActivityRecordRestored      DomainActivityKind = "record_restored"
	DomainActivityRecordArchived      DomainActivityKind = "record_archived"
	DomainActivityRecordUnarchived    DomainActivityKind = "record_unarchived"
	DomainActivityRecordTrashed       DomainActivityKind = "record_trashed"
	DomainActivityRecordTrashRestored DomainActivityKind = "record_trash_restored"
)

type RevisionCommitCommand struct {
	Actor               recordauth.ActorScope
	RecordID            string
	BaseRevisionID      string
	LockVersion         uint64
	AuthorizationEpoch  uint64
	DraftID             string
	DraftETag           DraftETag
	Input               CompleteRevisionInput
	EvidencePreparation evidence.RevisionPreparation
	ImportedAttachments []attachments.ImportedAvailableAttachment
	ActivityKind        DomainActivityKind
	OutboxTTL           time.Duration
	Idempotency         recordplatform.IdempotencyClaimInputV1
	BirthRequest        recordbirth.Request
	BirthPermit         recordbirth.Permit
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
	// CertificationPending means a committed certifiable birth still needs
	// confirmation; it is not evidence that the birth is verified.
	CertificationPending bool
}

type RecordLifecycleCommand struct {
	RecordID           string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	ExpectedLifecycle  Lifecycle
	TargetLifecycle    Lifecycle
	Actor              recordauth.ActorScope
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
	DraftID             string
	BaseRevisionID      string
	Result              RevisionCommitResult
	Input               CompleteRevisionInput
	EvidencePreparation evidence.RevisionPreparation
	ImportedAttachments []attachments.ImportedAvailableAttachment
	ActivityKind        DomainActivityKind
	OutboxTTL           time.Duration
	Outbox              RevisionOutbox
}

// RevisionOutbox is the named transaction-bound recordplatform primitive
// exposed to revision participants. It has no generic SQL, renderer, sender,
// or network callback surface.
type RevisionOutbox interface {
	EnqueueOutbox(context.Context, recordplatform.OutboxEnqueueInputV1) (recordplatform.OutboxEventRecordV1, error)
}

func (command RevisionCommitCommand) Validate() error {
	actor, err := recordauth.NormalizeActorScope(command.Actor)
	if err != nil || actor.UserID != command.Input.AuthorID() {
		return fmt.Errorf("%w: actor", ErrInvalidRevisionCommand)
	}
	if !recordidentity.IsValid(command.RecordID) {
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
	if err := command.EvidencePreparation.ValidateForRecord(command.RecordID); err != nil ||
		!reflect.DeepEqual(command.EvidencePreparation.SnapshotIDs(), command.Input.EvidenceSnapshotIDs()) {
		return fmt.Errorf("%w: evidence preparation", ErrInvalidRevisionCommand)
	}
	if err := validateImportedAttachments(command.ImportedAttachments, command.Input.AttachmentIDs()); err != nil {
		return fmt.Errorf("%w: imported attachments", ErrInvalidRevisionCommand)
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
	hasBirth := command.BirthRequest != (recordbirth.Request{}) || command.BirthPermit.AllocationID != "" ||
		command.BirthPermit.ReservationID != "" || command.BirthPermit.RecordID != "" ||
		command.BirthPermit.Creation != (recordbirth.InitialCreation{}) || len(command.BirthPermit.PrepareReceipt) != 0
	if hasBirth {
		fingerprint, fingerprintErr := command.Idempotency.RequestFingerprint.PersistedBytes()
		revisionID, revisionErr := command.RevisionID()
		creation := command.BirthPermit.Creation
		if command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordCreate ||
			command.BirthRequest.Validate() != nil || command.BirthRequest.ProjectID != string(actor.ProjectID) ||
			command.BirthRequest.ActorID != actor.UserID || command.BirthRequest.ActorScopeDigest != actor.CanonicalHash() ||
			fingerprintErr != nil || revisionErr != nil || creation.Validate() != nil ||
			command.BirthPermit.AllocationID == "" || command.BirthPermit.ReservationID != creation.ReservationID ||
			command.BirthPermit.RecordID != command.RecordID || creation.RecordID != command.RecordID ||
			creation.RevisionID != revisionID || creation.AuthorID != actor.UserID ||
			creation.CanonicalHash != command.Input.CanonicalHash() || creation.RequestFingerprint != fingerprint {
			return fmt.Errorf("%w: birth binding", ErrInvalidRevisionCommand)
		}
	}
	if command.OutboxTTL.Microseconds() <= 0 {
		return fmt.Errorf("%w: outbox ttl", ErrInvalidRevisionCommand)
	}
	return nil
}

func validateImportedAttachments(items []attachments.ImportedAvailableAttachment, attachmentIDs []string) error {
	if len(items) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(attachmentIDs))
	for _, attachmentID := range attachmentIDs {
		allowed[attachmentID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.Validate() != nil {
			return ErrInvalidRevisionCommand
		}
		if _, ok := allowed[item.AttachmentID]; !ok {
			return ErrInvalidRevisionCommand
		}
		if _, exists := seen[item.AttachmentID]; exists {
			return ErrInvalidRevisionCommand
		}
		seen[item.AttachmentID] = struct{}{}
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
	if !recordidentity.IsValid(command.RecordID) || !validRevisionID(command.CurrentRevisionID) ||
		command.LockVersion == 0 || command.AuthorizationEpoch == 0 {
		return fmt.Errorf("%w: identity or versions", ErrInvalidRecordLifecycleCommand)
	}
	if !((command.ExpectedLifecycle == LifecycleActive &&
		(command.TargetLifecycle == LifecycleArchived || command.TargetLifecycle == LifecycleTrashed)) ||
		((command.ExpectedLifecycle == LifecycleArchived || command.ExpectedLifecycle == LifecycleTrashed) &&
			command.TargetLifecycle == LifecycleActive)) {
		return fmt.Errorf("%w: lifecycle transition", ErrInvalidRecordLifecycleCommand)
	}
	if _, err := recordauth.NormalizeActorScope(command.Actor); err != nil {
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
	if command.TargetLifecycle == LifecycleTrashed {
		return DomainActivityRecordTrashed, nil
	}
	if command.ExpectedLifecycle == LifecycleTrashed {
		return DomainActivityRecordTrashRestored, nil
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

// ValidRevisionID exposes the revision identity grammar to derived read models
// that pin a record to the revision they projected.
func ValidRevisionID(value string) bool {
	return validRevisionID(value)
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
	// MutationResources performs only unlocked SELECTs/pure discovery on tx:
	// no writes, explicit locks, external calls, or acquired-authority claims.
	MutationResources(context.Context, pgx.Tx, RevisionCommitCommand) (RevisionMutationResources, error)
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

// MutationResources collects the closed participant declarations on the exact
// caller transaction. It neither acquires nor authorizes the declared resources.
func (registry RevisionParticipantRegistry) MutationResources(ctx context.Context, tx pgx.Tx, command RevisionCommitCommand) (RevisionMutationResources, error) {
	if ctx == nil || tx == nil || reflect.ValueOf(tx).Kind() == reflect.Pointer && reflect.ValueOf(tx).IsNil() {
		return RevisionMutationResources{}, ErrInvalidRevisionCommand
	}
	if err := ctx.Err(); err != nil {
		return RevisionMutationResources{}, err
	}
	if err := command.Validate(); err != nil {
		return RevisionMutationResources{}, err
	}
	sets := make([]RevisionMutationResources, 0, len(registry.participants))
	for _, participant := range registry.participants {
		resources, err := participant.MutationResources(ctx, tx, command)
		if err != nil {
			return RevisionMutationResources{}, fmt.Errorf("discover revision participant %q: %w", participant.Name(), err)
		}
		sets = append(sets, resources)
	}
	combined, err := CombineRevisionMutationResources(sets...)
	if err != nil {
		return RevisionMutationResources{}, err
	}
	if len(combined.NewRevisionEffects) > 0 {
		revisionID, err := command.RevisionID()
		if err != nil {
			return RevisionMutationResources{}, err
		}
		for _, effect := range combined.NewRevisionEffects {
			if effect.RecordID != command.RecordID || effect.RevisionID != revisionID ||
				(effect.Kind == RevisionMutationNewRevisionRows && effect.EventKind != RevisionMutationEventKind(command.ActivityKind)) {
				return RevisionMutationResources{}, fmt.Errorf("%w: new revision effect does not bind command", ErrInvalidRevisionMutationResources)
			}
		}
	}
	return combined, nil
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
