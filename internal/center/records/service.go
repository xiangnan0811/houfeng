package records

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const (
	revisionRequestScopeDomainV1  = "houfeng.records.revision-request-scope.v1"
	revisionCommitPayloadDomainV1 = "houfeng.records.revision-commit-payload.v1"
)

var ErrInvalidRevisionServiceRequest = errors.New("invalid revision service request")

type RevisionSaveRequest struct {
	Actor               recordauth.ActorScope
	RecordID            string
	BaseRevisionID      string
	LockVersion         uint64
	AuthorizationEpoch  uint64
	DraftID             string
	DraftETag           DraftETag
	Values              CompleteRevisionValues
	SubjectReferences   []SubjectReference
	EvidencePreparation evidence.RevisionPreparation
	ActivityKind        DomainActivityKind
	IdempotencyKey      string
	IdempotencyOwnerID  string
	OwnerLeaseDuration  time.Duration
	IdempotencyTTL      time.Duration
	OutboxTTL           time.Duration
}

type CurrentRecordAuthorization struct {
	RecordID           string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	Lifecycle          Lifecycle
	Evidence           RecordAuthorizationEvidence
}

type CurrentRecordAuthorizationSource interface {
	ResolveCurrentRecordAuthorization(
		context.Context,
		recordauth.ActorScope,
		string,
	) (CurrentRecordAuthorization, error)
}

type RevisionCommitStore interface {
	CommitRevision(context.Context, RevisionCommitCommand) (RevisionCommitResult, error)
	CommitRevisions(context.Context, []RevisionCommitCommand) ([]RevisionCommitResult, error)
	CommitRevisionsFinishing(context.Context, []RevisionCommitCommand, RevisionCommitFinish) ([]RevisionCommitResult, error)
}

type RevisionService struct {
	subjects SubjectAdapterRegistry
	current  CurrentRecordAuthorizationSource
	store    RevisionCommitStore
}

func NewRevisionService(
	subjects SubjectAdapterRegistry,
	current CurrentRecordAuthorizationSource,
	store RevisionCommitStore,
) (*RevisionService, error) {
	if nilRevisionServiceDependency(current) || nilRevisionServiceDependency(store) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidRevisionServiceRequest)
	}
	return &RevisionService{subjects: subjects, current: current, store: store}, nil
}

func (service *RevisionService) SaveRevision(
	ctx context.Context,
	request RevisionSaveRequest,
) (RevisionCommitResult, error) {
	results, err := service.SaveRevisions(ctx, []RevisionSaveRequest{request})
	if err != nil {
		return RevisionCommitResult{}, err
	}
	if len(results) != 1 {
		return RevisionCommitResult{}, fmt.Errorf("%w: service", ErrInvalidRevisionServiceRequest)
	}
	return results[0], nil
}

func (service *RevisionService) SaveRevisions(
	ctx context.Context,
	requests []RevisionSaveRequest,
) ([]RevisionCommitResult, error) {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.current) || nilRevisionServiceDependency(service.store) {
		return nil, fmt.Errorf("%w: service", ErrInvalidRevisionServiceRequest)
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("%w: empty", ErrInvalidRevisionServiceRequest)
	}
	commands := make([]RevisionCommitCommand, 0, len(requests))
	for _, request := range requests {
		command, err := service.prepareRevisionCommit(ctx, request)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if len(commands) == 1 {
		result, err := service.store.CommitRevision(ctx, commands[0])
		if err != nil {
			return nil, err
		}
		return []RevisionCommitResult{result}, nil
	}
	return service.store.CommitRevisions(ctx, commands)
}

func (service *RevisionService) SaveRevisionsFinishing(
	ctx context.Context,
	requests []RevisionSaveRequest,
	finish RevisionCommitFinish,
) ([]RevisionCommitResult, error) {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.current) || nilRevisionServiceDependency(service.store) {
		return nil, fmt.Errorf("%w: service", ErrInvalidRevisionServiceRequest)
	}
	if len(requests) == 0 || finish.Validate() != nil {
		return nil, fmt.Errorf("%w: finish", ErrInvalidRevisionServiceRequest)
	}
	commands := make([]RevisionCommitCommand, 0, len(requests))
	for _, request := range requests {
		command, err := service.prepareRevisionCommit(ctx, request)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return service.store.CommitRevisionsFinishing(ctx, commands, finish)
}

func (service *RevisionService) prepareRevisionCommit(
	ctx context.Context,
	request RevisionSaveRequest,
) (RevisionCommitCommand, error) {
	actor, references, operation, capability, err := validateRevisionSaveRequest(request)
	if err != nil {
		return RevisionCommitCommand{}, err
	}

	if operation == recordplatform.OperationKindRecordUpdate {
		current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), request.RecordID)
		if err != nil {
			return RevisionCommitCommand{}, fmt.Errorf("resolve current record authorization: %w", err)
		}
		if current.RecordID != request.RecordID || current.CurrentRevisionID != request.BaseRevisionID ||
			current.LockVersion != request.LockVersion || current.AuthorizationEpoch != request.AuthorizationEpoch ||
			current.Lifecycle != LifecycleActive {
			return RevisionCommitCommand{}, ErrRecordRevisionConflict
		}
		if err := AuthorizeRecordResource(actor, capability, current.Evidence); err != nil {
			return RevisionCommitCommand{}, err
		}
	}
	if err := request.EvidencePreparation.ValidateForRecord(request.RecordID); err != nil ||
		request.EvidencePreparation.ValidateReferencesForActor(actor) != nil {
		return RevisionCommitCommand{}, fmt.Errorf("%w: evidence preparation", ErrInvalidRevisionServiceRequest)
	}

	resolvedSubjects := make([]RevisionSubject, 0, len(references))
	sourceAuthorizations := make([]recordauth.SourceAuthorization, 0, len(references))
	for _, reference := range references {
		resolved, err := service.subjects.Resolve(ctx, actor.Clone(), reference)
		if err != nil {
			return RevisionCommitCommand{}, err
		}
		resolvedSubjects = append(resolvedSubjects, RevisionSubject{
			RegistryVersion:      reference.RegistryVersion,
			Kind:                 reference.Kind,
			Role:                 reference.Role,
			SourceID:             reference.SourceID,
			Primary:              reference.Primary,
			IdentitySnapshot:     resolved.IdentitySnapshot.Fields(),
			CaptureAuthorization: resolved.CaptureAuthorization,
		})
		sourceAuthorizations = append(sourceAuthorizations, resolved.CaptureAuthorization)
	}

	values := request.Values
	values.AuthorID = actor.UserID
	values.Subjects = resolvedSubjects
	values.EvidenceSnapshotIDs = request.EvidencePreparation.SnapshotIDs()
	input, err := NormalizeCompleteRevisionInput(values)
	if err != nil {
		return RevisionCommitCommand{}, err
	}
	if err := AuthorizeRecordResource(actor, capability, RecordAuthorizationEvidence{
		ProjectID:  actor.ProjectID,
		Visibility: input.VisibilityScope(),
		Sources:    sourceAuthorizations,
	}); err != nil {
		return RevisionCommitCommand{}, err
	}

	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      operation,
		ProjectID:          recordplatform.ProjectID(actor.ProjectID),
		ActorScopeDigest:   actor.CanonicalHash(),
		RequestScopeDigest: revisionRequestScopeDigest(request),
		PayloadDigest:      revisionCommitPayloadDigest(input),
	})
	if err != nil {
		return RevisionCommitCommand{}, fmt.Errorf("%w: fingerprint", ErrInvalidRevisionServiceRequest)
	}
	command := RevisionCommitCommand{
		RecordID:            request.RecordID,
		BaseRevisionID:      request.BaseRevisionID,
		LockVersion:         request.LockVersion,
		AuthorizationEpoch:  request.AuthorizationEpoch,
		DraftID:             request.DraftID,
		DraftETag:           request.DraftETag,
		Input:               input,
		EvidencePreparation: request.EvidencePreparation,
		ActivityKind:        request.ActivityKind,
		OutboxTTL:           request.OutboxTTL,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectID(actor.ProjectID),
				OperationKind: operation,
				Key:           request.IdempotencyKey,
			},
			RequestFingerprint: fingerprint,
			OwnerID:            request.IdempotencyOwnerID,
			OwnerLeaseDuration: request.OwnerLeaseDuration,
			RecordTTL:          request.IdempotencyTTL,
		},
	}
	if err := command.Validate(); err != nil {
		return RevisionCommitCommand{}, err
	}
	return command, nil
}

func validateRevisionSaveRequest(
	request RevisionSaveRequest,
) (recordauth.ActorScope, []SubjectReference, recordplatform.OperationKind, recordauth.Capability, error) {
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return recordauth.ActorScope{}, nil, "", "", fmt.Errorf("%w: actor", ErrInvalidRevisionServiceRequest)
	}
	if !validRecordRootID(request.RecordID) || len(request.Values.Subjects) != 0 || len(request.Values.EvidenceSnapshotIDs) != 0 {
		return recordauth.ActorScope{}, nil, "", "", fmt.Errorf("%w: record or client subject evidence", ErrInvalidRevisionServiceRequest)
	}
	_, draftETagErr := request.DraftETag.Digest()
	hasDraftID := request.DraftID != ""
	hasDraftETag := draftETagErr == nil
	if hasDraftID != hasDraftETag || (hasDraftID && !validDraftID(request.DraftID)) {
		return recordauth.ActorScope{}, nil, "", "", fmt.Errorf("%w: published draft", ErrInvalidRevisionServiceRequest)
	}
	references, err := NormalizeSubjectReferences(request.SubjectReferences)
	if err != nil {
		return recordauth.ActorScope{}, nil, "", "", err
	}

	var operation recordplatform.OperationKind
	var capability recordauth.Capability
	switch request.ActivityKind {
	case DomainActivityRecordCreated:
		if request.BaseRevisionID != "" || request.LockVersion != 0 || request.AuthorizationEpoch != 0 {
			return recordauth.ActorScope{}, nil, "", "", fmt.Errorf("%w: create CAS", ErrInvalidRevisionServiceRequest)
		}
		operation = recordplatform.OperationKindRecordCreate
		capability = recordauth.CapabilityRecordCreate
	case DomainActivityRecordRevised, DomainActivityRecordRestored:
		if !validRevisionID(request.BaseRevisionID) || request.LockVersion == 0 || request.AuthorizationEpoch == 0 {
			return recordauth.ActorScope{}, nil, "", "", fmt.Errorf("%w: update CAS", ErrInvalidRevisionServiceRequest)
		}
		operation = recordplatform.OperationKindRecordUpdate
		capability = recordauth.CapabilityRecordUpdate
	default:
		return recordauth.ActorScope{}, nil, "", "", fmt.Errorf("%w: activity", ErrInvalidRevisionServiceRequest)
	}

	key := recordplatform.IdempotencyKey{
		ProjectID:     recordplatform.ProjectID(actor.ProjectID),
		OperationKind: operation,
		Key:           request.IdempotencyKey,
	}
	if key.Validate() != nil || request.OwnerLeaseDuration.Microseconds() <= 0 ||
		request.IdempotencyTTL.Microseconds() <= request.OwnerLeaseDuration.Microseconds() ||
		request.OutboxTTL.Microseconds() <= 0 {
		return recordauth.ActorScope{}, nil, "", "", fmt.Errorf("%w: idempotency or expiry", ErrInvalidRevisionServiceRequest)
	}
	return actor, references, operation, capability, nil
}

func revisionRequestScopeDigest(request RevisionSaveRequest) [sha256.Size]byte {
	encoder := revisionCanonicalEncoder{}
	encoder.string(revisionRequestScopeDomainV1)
	encoder.string(request.RecordID)
	encoder.string(request.BaseRevisionID)
	encoder.uint64(request.LockVersion)
	encoder.uint64(request.AuthorizationEpoch)
	encoder.string(string(request.ActivityKind))
	encoder.string(request.DraftID)
	encoder.string(request.DraftETag.String())
	return sha256.Sum256(encoder.bytes)
}

func revisionCommitPayloadDigest(input CompleteRevisionInput) [sha256.Size]byte {
	encoder := revisionCanonicalEncoder{}
	encoder.string(revisionCommitPayloadDomainV1)
	canonicalHash := input.CanonicalHash()
	encoder.raw(canonicalHash[:])
	encoder.string(input.AuthorID())
	encoder.string(input.SaveReason())
	return sha256.Sum256(encoder.bytes)
}

func nilRevisionServiceDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
