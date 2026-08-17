package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const (
	actionRequestScopeDomainV1 = "houfeng.record-collaboration.action-request-scope.v1"
	actionPayloadDomainV1      = "houfeng.record-collaboration.action-payload.v1"
	actionResultDomainV1       = "houfeng.record-collaboration.action-result.v1"
	actionIdentityDomainV1     = "houfeng.record-collaboration.action-identity.v1"
)

var (
	ErrInvalidActionRequest = errors.New("invalid record action request")
	ErrInvalidActionCommand = errors.New("invalid record action command")
	ErrActionNotFound       = errors.New("record action not found")
	ErrActionConflict       = errors.New("record action conflict")
)

type ActionCreateRequest struct {
	Actor              recordauth.ActorScope
	RecordID           string
	Fields             ActionFieldValues
	IdempotencyKey     string
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

type ActionCommandRequest struct {
	Actor              recordauth.ActorScope
	RecordID           string
	ActionID           string
	ExpectedVersion    uint64
	IdempotencyKey     string
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

type ActionUpdateRequest struct {
	ActionCommandRequest
	Fields ActionFieldValues
}

// ActionCommand is the transport-neutral, fully authorized storage command.
// Evidence is canonical policy input, while result identity is a sealed,
// content-free fingerprint.
type ActionCommand struct {
	Kind                  ActionMutationKind
	Actor                 recordauth.ActorScope
	RecordID              string
	ActionID              string
	ExpectedVersion       uint64
	CurrentRevisionID     string
	RecordLockVersion     uint64
	AuthorizationEpoch    uint64
	AuthorizationEvidence records.RecordAuthorizationEvidence
	Fields                ActionFields
	Idempotency           recordplatform.IdempotencyClaimInputV1
	ResultFingerprint     recordplatform.RequestFingerprintV1
	OutboxTTL             time.Duration
}

type ActionMutationResult struct {
	ActionID  string
	RecordID  string
	Version   uint64
	Status    ActionStatus
	EventKind ActionMutationKind
	Replayed  bool
	ChangedAt time.Time
}

func (result ActionMutationResult) Validate() error {
	if ValidateActionID(result.ActionID) != nil || !validRecordID(result.RecordID) ||
		result.Version == 0 || !validActionStatus(result.Status) || !validActionMutationKind(result.EventKind) ||
		result.ChangedAt.IsZero() {
		return ErrInvalidActionCommand
	}
	return nil
}

func (command ActionCommand) Validate() error {
	if !validActionMutationKind(command.Kind) || !validRecordID(command.RecordID) ||
		ValidateActionID(command.ActionID) != nil || !validCollaborationRevisionIdentity(command.CurrentRevisionID) ||
		command.RecordLockVersion == 0 || command.AuthorizationEpoch == 0 ||
		command.Actor.ProjectID != recordauth.ProjectIDDefault || command.Idempotency.Key.ProjectID != recordplatform.ProjectIDDefault ||
		command.Idempotency.Key.OperationKind != actionOperationKind(command.Kind) ||
		command.Idempotency.Validate() != nil || command.ResultFingerprint.Validate() != nil ||
		command.OutboxTTL.Microseconds() <= 0 {
		return ErrInvalidActionCommand
	}
	if command.Kind == ActionMutationCreate {
		if command.ExpectedVersion != 0 || command.Fields.Title() == "" {
			return ErrInvalidActionCommand
		}
	} else if command.ExpectedVersion == 0 {
		return ErrInvalidActionCommand
	}
	if command.Kind == ActionMutationUpdate && command.Fields.Title() == "" {
		return ErrInvalidActionCommand
	}
	if command.Kind != ActionMutationCreate && command.Kind != ActionMutationUpdate && command.Fields.Title() != "" {
		return ErrInvalidActionCommand
	}
	if err := records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityRecordUpdate, command.AuthorizationEvidence); err != nil {
		return err
	}
	return nil
}

type ActionCommandStore interface {
	CommitAction(context.Context, ActionCommand) (ActionMutationResult, error)
}

type ActionService struct {
	current records.CurrentRecordAuthorizationSource
	store   ActionCommandStore
}

func NewActionService(current records.CurrentRecordAuthorizationSource, store ActionCommandStore) (*ActionService, error) {
	if nilActionDependency(current) || nilActionDependency(store) {
		return nil, ErrInvalidActionRequest
	}
	return &ActionService{current: current, store: store}, nil
}

func (service *ActionService) CreateAction(ctx context.Context, request ActionCreateRequest) (ActionMutationResult, error) {
	fields, err := NormalizeActionFields(request.Fields)
	if err != nil {
		return ActionMutationResult{}, err
	}
	return service.commit(ctx, ActionMutationCreate, request.Actor, request.RecordID, "", 0, fields,
		request.IdempotencyKey, request.IdempotencyOwnerID, request.OwnerLeaseDuration, request.IdempotencyTTL, request.OutboxTTL)
}

func (service *ActionService) UpdateAction(ctx context.Context, request ActionUpdateRequest) (ActionMutationResult, error) {
	fields, err := NormalizeActionFields(request.Fields)
	if err != nil {
		return ActionMutationResult{}, err
	}
	return service.commitCommand(ctx, ActionMutationUpdate, request.ActionCommandRequest, fields)
}

func (service *ActionService) CompleteAction(ctx context.Context, request ActionCommandRequest) (ActionMutationResult, error) {
	return service.commitCommand(ctx, ActionMutationComplete, request, ActionFields{})
}

func (service *ActionService) CancelAction(ctx context.Context, request ActionCommandRequest) (ActionMutationResult, error) {
	return service.commitCommand(ctx, ActionMutationCancel, request, ActionFields{})
}

func (service *ActionService) ReopenAction(ctx context.Context, request ActionCommandRequest) (ActionMutationResult, error) {
	return service.commitCommand(ctx, ActionMutationReopen, request, ActionFields{})
}

func (service *ActionService) commitCommand(ctx context.Context, kind ActionMutationKind, request ActionCommandRequest, fields ActionFields) (ActionMutationResult, error) {
	return service.commit(ctx, kind, request.Actor, request.RecordID, request.ActionID, request.ExpectedVersion, fields,
		request.IdempotencyKey, request.IdempotencyOwnerID, request.OwnerLeaseDuration, request.IdempotencyTTL, request.OutboxTTL)
}

func (service *ActionService) commit(
	ctx context.Context,
	kind ActionMutationKind,
	actorInput recordauth.ActorScope,
	recordID string,
	actionID string,
	expectedVersion uint64,
	fields ActionFields,
	idempotencyKey string,
	ownerID string,
	ownerLeaseDuration time.Duration,
	idempotencyTTL time.Duration,
	outboxTTL time.Duration,
) (ActionMutationResult, error) {
	if ctx == nil || service == nil || nilActionDependency(service.current) || nilActionDependency(service.store) ||
		!validActionMutationKind(kind) || !validRecordID(recordID) ||
		(kind != ActionMutationCreate && (ValidateActionID(actionID) != nil || expectedVersion == 0)) {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}
	actor, err := recordauth.NormalizeActorScope(actorInput)
	if err != nil {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}
	operation := actionOperationKind(kind)
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectID(actor.ProjectID), OperationKind: operation, Key: idempotencyKey}
	if key.Validate() != nil || ownerLeaseDuration.Microseconds() <= 0 ||
		idempotencyTTL.Microseconds() <= ownerLeaseDuration.Microseconds() || outboxTTL.Microseconds() <= 0 {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}

	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), recordID)
	if err != nil {
		return ActionMutationResult{}, err
	}
	if current.RecordID != recordID || !validCollaborationRevisionIdentity(current.CurrentRevisionID) ||
		current.LockVersion == 0 || current.AuthorizationEpoch == 0 || current.Lifecycle != records.LifecycleActive {
		return ActionMutationResult{}, ErrActionConflict
	}
	if err := records.AuthorizeRecordResource(actor, recordauth.CapabilityRecordUpdate, current.Evidence); err != nil {
		return ActionMutationResult{}, err
	}

	requestScopeDigest := actionRequestScopeDigest(kind, recordID, actionID, expectedVersion)
	payloadDigest := actionPayloadDigest(kind, fields)
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: operation,
		ProjectID: recordplatform.ProjectID(actor.ProjectID), ActorScopeDigest: actor.CanonicalHash(),
		RequestScopeDigest: requestScopeDigest, PayloadDigest: payloadDigest,
	})
	if err != nil {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}
	if kind == ActionMutationCreate {
		actionID, err = deterministicActionID(idempotencyKey, fingerprint)
		if err != nil {
			return ActionMutationResult{}, err
		}
	}
	resultVersion := expectedVersion + 1
	if kind == ActionMutationCreate {
		resultVersion = 1
	}
	resultFingerprint, err := actionResultFingerprint(operation, actor.UserID, recordID, actionID, resultVersion, kind)
	if err != nil {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}
	command := ActionCommand{
		Kind: kind, Actor: actor, RecordID: recordID, ActionID: actionID, ExpectedVersion: expectedVersion,
		CurrentRevisionID: current.CurrentRevisionID, RecordLockVersion: current.LockVersion,
		AuthorizationEpoch: current.AuthorizationEpoch, AuthorizationEvidence: cloneActionAuthorizationEvidence(current.Evidence),
		Fields: fields, ResultFingerprint: resultFingerprint, OutboxTTL: outboxTTL,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: key, RequestFingerprint: fingerprint, OwnerID: ownerID,
			OwnerLeaseDuration: ownerLeaseDuration, RecordTTL: idempotencyTTL,
		},
	}
	if err := command.Validate(); err != nil {
		return ActionMutationResult{}, err
	}
	return service.store.CommitAction(ctx, command)
}

func deterministicActionID(idempotencyKey string, fingerprint recordplatform.RequestFingerprintV1) (string, error) {
	digest, err := fingerprint.PersistedBytes()
	if err != nil || idempotencyKey == "" {
		return "", ErrInvalidActionRequest
	}
	encoder := actionCanonicalEncoder{}
	encoder.string(actionIdentityDomainV1)
	encoder.string(idempotencyKey)
	encoder.string(string(digest[:]))
	identity := sha256.Sum256(encoder.bytes)
	return "ract_" + hex.EncodeToString(identity[:]), nil
}

func actionOperationKind(kind ActionMutationKind) recordplatform.OperationKind {
	switch kind {
	case ActionMutationCreate:
		return recordplatform.OperationKindRecordActionCreate
	case ActionMutationUpdate:
		return recordplatform.OperationKindRecordActionUpdate
	case ActionMutationComplete:
		return recordplatform.OperationKindRecordActionComplete
	case ActionMutationCancel:
		return recordplatform.OperationKindRecordActionCancel
	case ActionMutationReopen:
		return recordplatform.OperationKindRecordActionReopen
	default:
		return ""
	}
}

func validActionMutationKind(kind ActionMutationKind) bool { return actionOperationKind(kind) != "" }

func actionRequestScopeDigest(kind ActionMutationKind, recordID, actionID string, expectedVersion uint64) [sha256.Size]byte {
	encoder := actionCanonicalEncoder{}
	encoder.string(actionRequestScopeDomainV1)
	encoder.string(string(kind))
	encoder.string(recordID)
	encoder.string(actionID)
	encoder.uint64(expectedVersion)
	return sha256.Sum256(encoder.bytes)
}

func actionPayloadDigest(kind ActionMutationKind, fields ActionFields) [sha256.Size]byte {
	encoder := actionCanonicalEncoder{}
	encoder.string(actionPayloadDomainV1)
	encoder.string(string(kind))
	if kind == ActionMutationCreate || kind == ActionMutationUpdate {
		encoder.string(fields.Title())
		encoder.string(fields.Details())
		encoder.string(fields.AssigneeID())
		encoder.string(fields.SubjectRevisionID())
		if due := fields.DueAt(); due != nil {
			encoder.string(due.Format(time.RFC3339Nano))
		} else {
			encoder.string("")
		}
	}
	return sha256.Sum256(encoder.bytes)
}

func actionResultFingerprint(operation recordplatform.OperationKind, actorID, recordID, actionID string, version uint64, kind ActionMutationKind) (recordplatform.RequestFingerprintV1, error) {
	encoder := actionCanonicalEncoder{}
	encoder.string(actionResultDomainV1)
	encoder.string(recordID)
	encoder.string(actionID)
	encoder.uint64(version)
	encoder.string(string(kind))
	identity := sha256.Sum256(encoder.bytes)
	actorDigest := sha256.Sum256([]byte(actorID))
	contentFree := sha256.Sum256([]byte(actionResultDomainV1 + ":content-free"))
	return recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: operation,
		ProjectID: recordplatform.ProjectIDDefault, ActorScopeDigest: actorDigest,
		RequestScopeDigest: identity, PayloadDigest: contentFree,
	})
}

type actionCanonicalEncoder struct{ bytes []byte }

func (encoder *actionCanonicalEncoder) string(value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	encoder.bytes = append(encoder.bytes, length[:]...)
	encoder.bytes = append(encoder.bytes, value...)
}

func (encoder *actionCanonicalEncoder) uint64(value uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	encoder.bytes = append(encoder.bytes, raw[:]...)
}

func cloneActionAuthorizationEvidence(evidence records.RecordAuthorizationEvidence) records.RecordAuthorizationEvidence {
	evidence.Sources = append([]recordauth.SourceAuthorization(nil), evidence.Sources...)
	return evidence
}

func nilActionDependency(dependency any) bool {
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
