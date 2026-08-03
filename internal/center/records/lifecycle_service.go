package records

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const (
	lifecycleRequestScopeDomainV1 = "houfeng.records.lifecycle-request-scope.v1"
	lifecyclePayloadDomainV1      = "houfeng.records.lifecycle-payload.v1"
)

var ErrInvalidRecordLifecycleRequest = errors.New("invalid record lifecycle request")

type RecordLifecycleRequest struct {
	Actor              recordauth.ActorScope
	RecordID           string
	TargetLifecycle    Lifecycle
	IdempotencyKey     string
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

type RecordLifecycleStore interface {
	CommitRecordLifecycle(context.Context, RecordLifecycleCommand) (RecordLifecycleResult, error)
}

type RecordLifecycleService struct {
	current CurrentRecordAuthorizationSource
	store   RecordLifecycleStore
}

func NewRecordLifecycleService(
	current CurrentRecordAuthorizationSource,
	store RecordLifecycleStore,
) (*RecordLifecycleService, error) {
	if nilRecordLifecycleDependency(current) || nilRecordLifecycleDependency(store) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidRecordLifecycleRequest)
	}
	return &RecordLifecycleService{current: current, store: store}, nil
}

func (service *RecordLifecycleService) ChangeLifecycle(
	ctx context.Context,
	request RecordLifecycleRequest,
) (RecordLifecycleResult, error) {
	if ctx == nil || service == nil || nilRecordLifecycleDependency(service.current) ||
		nilRecordLifecycleDependency(service.store) || !validRecordRootID(request.RecordID) ||
		ValidateLifecycle(request.TargetLifecycle) != nil {
		return RecordLifecycleResult{}, ErrInvalidRecordLifecycleRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return RecordLifecycleResult{}, fmt.Errorf("%w: actor", ErrInvalidRecordLifecycleRequest)
	}
	key := recordplatform.IdempotencyKey{
		ProjectID:     recordplatform.ProjectID(actor.ProjectID),
		OperationKind: recordplatform.OperationKindRecordUpdate,
		Key:           request.IdempotencyKey,
	}
	if key.Validate() != nil || request.OwnerLeaseDuration.Microseconds() <= 0 ||
		request.IdempotencyTTL.Microseconds() <= request.OwnerLeaseDuration.Microseconds() ||
		request.OutboxTTL.Microseconds() <= 0 {
		return RecordLifecycleResult{}, fmt.Errorf("%w: idempotency or expiry", ErrInvalidRecordLifecycleRequest)
	}

	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), request.RecordID)
	if err != nil {
		return RecordLifecycleResult{}, err
	}
	if err := validateCurrentRecordAuthorization(request.RecordID, actor.ProjectID, current); err != nil {
		return RecordLifecycleResult{}, err
	}
	if current.Lifecycle == request.TargetLifecycle {
		return RecordLifecycleResult{}, ErrRecordRevisionConflict
	}
	if err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordUpdate, current.Evidence); err != nil {
		return RecordLifecycleResult{}, err
	}

	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordUpdate,
		ProjectID:          recordplatform.ProjectID(actor.ProjectID),
		ActorScopeDigest:   actor.CanonicalHash(),
		RequestScopeDigest: lifecycleRequestScopeDigest(current, request.TargetLifecycle),
		PayloadDigest:      lifecyclePayloadDigest(request.TargetLifecycle),
	})
	if err != nil {
		return RecordLifecycleResult{}, fmt.Errorf("%w: fingerprint", ErrInvalidRecordLifecycleRequest)
	}
	command := RecordLifecycleCommand{
		RecordID:           current.RecordID,
		CurrentRevisionID:  current.CurrentRevisionID,
		LockVersion:        current.LockVersion,
		AuthorizationEpoch: current.AuthorizationEpoch,
		TargetLifecycle:    request.TargetLifecycle,
		ActorID:            actor.UserID,
		OutboxTTL:          request.OutboxTTL,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key:                key,
			RequestFingerprint: fingerprint,
			OwnerID:            request.IdempotencyOwnerID,
			OwnerLeaseDuration: request.OwnerLeaseDuration,
			RecordTTL:          request.IdempotencyTTL,
		},
	}
	if err := command.Validate(); err != nil {
		return RecordLifecycleResult{}, err
	}
	return service.store.CommitRecordLifecycle(ctx, command)
}

func lifecycleRequestScopeDigest(current CurrentRecordAuthorization, target Lifecycle) [sha256.Size]byte {
	encoder := revisionCanonicalEncoder{}
	encoder.string(lifecycleRequestScopeDomainV1)
	encoder.string(current.RecordID)
	encoder.string(current.CurrentRevisionID)
	encoder.uint64(current.LockVersion)
	encoder.uint64(current.AuthorizationEpoch)
	encoder.string(string(target))
	return sha256.Sum256(encoder.bytes)
}

func lifecyclePayloadDigest(target Lifecycle) [sha256.Size]byte {
	encoder := revisionCanonicalEncoder{}
	encoder.string(lifecyclePayloadDomainV1)
	encoder.string(string(target))
	return sha256.Sum256(encoder.bytes)
}

func nilRecordLifecycleDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
