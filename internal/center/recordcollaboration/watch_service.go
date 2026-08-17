package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const (
	watchRequestScopeDomainV1 = "houfeng.record-collaboration.watch-request-scope.v1"
	watchPayloadDomainV1      = "houfeng.record-collaboration.watch-payload.v1"
	watchResultDomainV1       = "houfeng.record-collaboration.watch-result.v1"
)

var (
	ErrInvalidWatchRequest = errors.New("invalid record watch request")
	ErrInvalidWatchCommand = errors.New("invalid record watch command")
	ErrWatchConflict       = errors.New("record watch conflict")
)

type WatchStatus struct {
	RecordID         string
	UserID           string
	Version          uint64
	Preference       FollowerPreference
	Sources          FollowerSources
	RecordFenceEpoch uint64
	UpdatedAt        time.Time
}

func (status WatchStatus) Validate() error {
	if !validRecordID(status.RecordID) || recordauth.ValidateActorUserID(status.UserID) != nil ||
		ValidateFollowerPreference(status.Preference) != nil || status.Version > math.MaxInt64 ||
		status.RecordFenceEpoch > math.MaxInt64 {
		return ErrInvalidWatchCommand
	}
	if status.Version == 0 {
		if status.Preference != FollowerPreferenceDefault || status.Sources.Any() || !status.UpdatedAt.IsZero() {
			return ErrInvalidWatchCommand
		}
		return nil
	}
	if status.UpdatedAt.IsZero() {
		return ErrInvalidWatchCommand
	}
	return nil
}

type WatchSetRequest struct {
	Actor              recordauth.ActorScope
	RecordID           string
	ExpectedVersion    uint64
	Preference         FollowerPreference
	IdempotencyKey     string
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
}

type WatchReadRequest struct {
	Actor    recordauth.ActorScope
	RecordID string
}

type WatchCommand struct {
	Actor                 recordauth.ActorScope
	RecordID              string
	CurrentRevisionID     string
	RecordLockVersion     uint64
	AuthorizationEpoch    uint64
	AuthorizationEvidence records.RecordAuthorizationEvidence
	ExpectedVersion       uint64
	Preference            FollowerPreference
	Idempotency           recordplatform.IdempotencyClaimInputV1
}

func (command WatchCommand) Validate() error {
	if !validRecordID(command.RecordID) || !validCollaborationRevisionIdentity(command.CurrentRevisionID) ||
		command.RecordLockVersion == 0 || command.AuthorizationEpoch == 0 || command.ExpectedVersion > math.MaxInt64 ||
		ValidateFollowerPreference(command.Preference) != nil || command.Actor.ProjectID != recordauth.ProjectIDDefault ||
		command.Idempotency.Key.ProjectID != recordplatform.ProjectIDDefault ||
		command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordWatchPreference ||
		command.Idempotency.Validate() != nil {
		return ErrInvalidWatchCommand
	}
	return records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityNotificationManage, command.AuthorizationEvidence)
}

type WatchReadCommand struct {
	Actor                 recordauth.ActorScope
	RecordID              string
	CurrentRevisionID     string
	RecordLockVersion     uint64
	AuthorizationEpoch    uint64
	AuthorizationEvidence records.RecordAuthorizationEvidence
}

func (command WatchReadCommand) Validate() error {
	if !validRecordID(command.RecordID) || !validCollaborationRevisionIdentity(command.CurrentRevisionID) ||
		command.RecordLockVersion == 0 || command.AuthorizationEpoch == 0 {
		return ErrInvalidWatchCommand
	}
	return records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityNotificationRead, command.AuthorizationEvidence)
}

type WatchStore interface {
	SetWatch(context.Context, WatchCommand) (WatchStatus, error)
	GetWatch(context.Context, WatchReadCommand) (WatchStatus, error)
}

type WatchService struct {
	current records.CurrentRecordAuthorizationSource
	store   WatchStore
}

func NewWatchService(current records.CurrentRecordAuthorizationSource, store WatchStore) (*WatchService, error) {
	if nilActionDependency(current) || nilActionDependency(store) {
		return nil, ErrInvalidWatchRequest
	}
	return &WatchService{current: current, store: store}, nil
}

func (service *WatchService) SetWatch(ctx context.Context, request WatchSetRequest) (WatchStatus, error) {
	actor, current, err := service.resolve(ctx, request.Actor, request.RecordID, recordauth.CapabilityNotificationManage)
	if err != nil {
		return WatchStatus{}, err
	}
	if ValidateFollowerPreference(request.Preference) != nil || request.ExpectedVersion > math.MaxInt64 ||
		request.OwnerLeaseDuration.Microseconds() <= 0 || request.IdempotencyTTL.Microseconds() <= request.OwnerLeaseDuration.Microseconds() {
		return WatchStatus{}, ErrInvalidWatchRequest
	}
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordWatchPreference, Key: request.IdempotencyKey}
	if key.Validate() != nil {
		return WatchStatus{}, ErrInvalidWatchRequest
	}
	requestScope := actionCanonicalEncoder{}
	requestScope.string(watchRequestScopeDomainV1)
	requestScope.string(request.RecordID)
	requestScope.uint64(request.ExpectedVersion)
	payload := actionCanonicalEncoder{}
	payload.string(watchPayloadDomainV1)
	payload.string(string(request.Preference))
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: recordplatform.OperationKindRecordWatchPreference,
		ProjectID: recordplatform.ProjectIDDefault, ActorScopeDigest: actor.CanonicalHash(),
		RequestScopeDigest: sha256.Sum256(requestScope.bytes), PayloadDigest: sha256.Sum256(payload.bytes),
	})
	if err != nil {
		return WatchStatus{}, ErrInvalidWatchRequest
	}
	command := WatchCommand{
		Actor: actor, RecordID: request.RecordID, CurrentRevisionID: current.CurrentRevisionID,
		RecordLockVersion: current.LockVersion, AuthorizationEpoch: current.AuthorizationEpoch,
		AuthorizationEvidence: cloneActionAuthorizationEvidence(current.Evidence), ExpectedVersion: request.ExpectedVersion,
		Preference: request.Preference,
		Idempotency: recordplatform.IdempotencyClaimInputV1{Key: key, RequestFingerprint: fingerprint,
			OwnerID: request.IdempotencyOwnerID, OwnerLeaseDuration: request.OwnerLeaseDuration, RecordTTL: request.IdempotencyTTL},
	}
	if err := command.Validate(); err != nil {
		return WatchStatus{}, err
	}
	result, err := service.store.SetWatch(ctx, command)
	if err != nil {
		return WatchStatus{}, err
	}
	if result.Validate() != nil || result.RecordID != request.RecordID || result.UserID != actor.UserID {
		return WatchStatus{}, ErrWatchConflict
	}
	return result, nil
}

// ResultFingerprint binds a completed watch command to the exact content-free
// status it returned and to its idempotency key. The foundation persists this
// value; an existing follower row retains the same opaque marker and monotonic
// version so later preference or automatic-source state cannot impersonate it.
func (status WatchStatus) ResultFingerprint(key recordplatform.IdempotencyKey) (recordplatform.RequestFingerprintV1, error) {
	if status.Validate() != nil || key.Validate() != nil || key.ProjectID != recordplatform.ProjectIDDefault ||
		key.OperationKind != recordplatform.OperationKindRecordWatchPreference {
		return recordplatform.RequestFingerprintV1{}, ErrInvalidWatchCommand
	}
	identity := actionCanonicalEncoder{}
	identity.string(watchResultDomainV1)
	identity.string(key.Key)
	identity.string(status.RecordID)
	identity.string(status.UserID)
	identity.uint64(status.Version)
	identity.string(string(status.Preference))
	for _, source := range []bool{
		status.Sources.Author, status.Sources.Owner, status.Sources.Participant,
		status.Sources.Comment, status.Sources.Mention, status.Sources.Action,
	} {
		if source {
			identity.uint64(1)
		} else {
			identity.uint64(0)
		}
	}
	identity.uint64(status.RecordFenceEpoch)
	if status.UpdatedAt.IsZero() {
		identity.string("")
	} else {
		identity.string(status.UpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	return recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: recordplatform.OperationKindRecordWatchPreference,
		ProjectID: recordplatform.ProjectIDDefault, ActorScopeDigest: sha256.Sum256([]byte(status.UserID)),
		RequestScopeDigest: sha256.Sum256(identity.bytes), PayloadDigest: sha256.Sum256([]byte(watchResultDomainV1 + ":content-free")),
	})
}

func (service *WatchService) GetWatch(ctx context.Context, request WatchReadRequest) (WatchStatus, error) {
	actor, current, err := service.resolve(ctx, request.Actor, request.RecordID, recordauth.CapabilityNotificationRead)
	if err != nil {
		return WatchStatus{}, err
	}
	command := WatchReadCommand{Actor: actor, RecordID: request.RecordID, CurrentRevisionID: current.CurrentRevisionID,
		RecordLockVersion: current.LockVersion, AuthorizationEpoch: current.AuthorizationEpoch,
		AuthorizationEvidence: cloneActionAuthorizationEvidence(current.Evidence)}
	if err := command.Validate(); err != nil {
		return WatchStatus{}, err
	}
	result, err := service.store.GetWatch(ctx, command)
	if err != nil {
		return WatchStatus{}, err
	}
	if result.Validate() != nil || result.RecordID != request.RecordID || result.UserID != actor.UserID {
		return WatchStatus{}, ErrWatchConflict
	}
	return result, nil
}

func (service *WatchService) resolve(ctx context.Context, actorInput recordauth.ActorScope, recordID string, capability recordauth.Capability) (recordauth.ActorScope, records.CurrentRecordAuthorization, error) {
	if ctx == nil || service == nil || nilActionDependency(service.current) || nilActionDependency(service.store) || !validRecordID(recordID) {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, ErrInvalidWatchRequest
	}
	actor, err := recordauth.NormalizeActorScope(actorInput)
	if err != nil {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, ErrInvalidWatchRequest
	}
	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), recordID)
	if err != nil {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, err
	}
	if current.RecordID != recordID || !validCollaborationRevisionIdentity(current.CurrentRevisionID) || current.LockVersion == 0 ||
		current.AuthorizationEpoch == 0 || current.Lifecycle != records.LifecycleActive {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, ErrWatchConflict
	}
	if err := records.AuthorizeRecordResource(actor, capability, current.Evidence); err != nil {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, err
	}
	return actor, current, nil
}
