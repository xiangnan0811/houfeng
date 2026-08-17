package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const (
	commentRequestScopeDomainV1 = "houfeng.record-collaboration.comment-request-scope.v1"
	commentPayloadDomainV1      = "houfeng.record-collaboration.comment-payload.v1"
	commentIdentityDomainV1     = "houfeng.record-collaboration.comment-identity.v1"
	commentResultDomainV1       = "houfeng.record-collaboration.comment-result.v1"
)

type CommentCreateRequest struct {
	Actor              recordauth.ActorScope
	RecordID           string
	BodyMarkdown       string
	ReplyToCommentID   string
	MentionUserIDs     []string
	IdempotencyKey     string
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

type CommentCommandRequest struct {
	Actor              recordauth.ActorScope
	RecordID           string
	CommentID          string
	ExpectedVersion    uint64
	IdempotencyKey     string
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

type CommentEditRequest struct {
	CommentCommandRequest
	BodyMarkdown   string
	MentionUserIDs []string
}

type CommentListRequest struct {
	Actor    recordauth.ActorScope
	RecordID string
	Limit    uint64
}

type CommentCommand struct {
	Kind                  CommentMutationKind
	Actor                 recordauth.ActorScope
	RecordID              string
	CommentID             string
	ExpectedVersion       uint64
	CurrentRevisionID     string
	RecordLockVersion     uint64
	AuthorizationEpoch    uint64
	AuthorizationEvidence records.RecordAuthorizationEvidence
	Content               CommentContent
	ReplyToCommentID      string
	MentionUserIDs        []string
	Idempotency           recordplatform.IdempotencyClaimInputV1
	ResultFingerprint     recordplatform.RequestFingerprintV1
	OutboxTTL             time.Duration
}

func (command CommentCommand) Validate() error {
	if !validCommentMutationKind(command.Kind) || !validRecordID(command.RecordID) ||
		ValidateCommentID(command.CommentID) != nil || !validCollaborationRevisionIdentity(command.CurrentRevisionID) ||
		command.RecordLockVersion == 0 || command.AuthorizationEpoch == 0 || command.Actor.ProjectID != recordauth.ProjectIDDefault ||
		command.Idempotency.Key.ProjectID != recordplatform.ProjectIDDefault ||
		command.Idempotency.Key.OperationKind != commentOperationKind(command.Kind) || command.Idempotency.Validate() != nil ||
		command.ResultFingerprint.Validate() != nil || command.OutboxTTL.Microseconds() <= 0 {
		return ErrInvalidCommentCommand
	}
	mentions, err := NormalizeCommentMentionUserIDs(command.MentionUserIDs)
	if err != nil || !equalCommentStrings(mentions, command.MentionUserIDs) {
		return ErrInvalidCommentCommand
	}
	switch command.Kind {
	case CommentMutationCreate:
		if command.ExpectedVersion != 0 || command.Content.Validate() != nil ||
			(command.ReplyToCommentID != "" && ValidateCommentID(command.ReplyToCommentID) != nil) {
			return ErrInvalidCommentCommand
		}
	case CommentMutationEdit:
		if !IsIncrementableCommentVersion(command.ExpectedVersion) || command.Content.Validate() != nil || command.ReplyToCommentID != "" {
			return ErrInvalidCommentCommand
		}
	case CommentMutationRedact:
		if !IsIncrementableCommentVersion(command.ExpectedVersion) || !command.Content.Empty() ||
			command.ReplyToCommentID != "" || len(command.MentionUserIDs) != 0 {
			return ErrInvalidCommentCommand
		}
	}
	if err := records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityRecordUpdate, command.AuthorizationEvidence); err != nil {
		return err
	}
	return nil
}

type CommentReadCommand struct {
	Actor                 recordauth.ActorScope
	RecordID              string
	CurrentRevisionID     string
	RecordLockVersion     uint64
	AuthorizationEpoch    uint64
	AuthorizationEvidence records.RecordAuthorizationEvidence
	Limit                 uint64
}

func (command CommentReadCommand) Validate() error {
	if !validRecordID(command.RecordID) || !validCollaborationRevisionIdentity(command.CurrentRevisionID) ||
		command.RecordLockVersion == 0 || command.AuthorizationEpoch == 0 || command.Limit == 0 || command.Limit > 200 {
		return ErrInvalidCommentRequest
	}
	return records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityRecordRead, command.AuthorizationEvidence)
}

type CommentStore interface {
	CommitComment(context.Context, CommentCommand) (CommentMutationResult, error)
	ListComments(context.Context, CommentReadCommand) ([]CommentRecord, error)
}

type CommentService struct {
	current records.CurrentRecordAuthorizationSource
	store   CommentStore
}

func NewCommentService(current records.CurrentRecordAuthorizationSource, store CommentStore) (*CommentService, error) {
	if nilCommentDependency(current) || nilCommentDependency(store) {
		return nil, ErrInvalidCommentRequest
	}
	return &CommentService{current: current, store: store}, nil
}

func (service *CommentService) CreateComment(ctx context.Context, request CommentCreateRequest) (CommentMutationResult, error) {
	return service.commit(ctx, CommentMutationCreate, request.Actor, request.RecordID, "", 0,
		request.BodyMarkdown, request.ReplyToCommentID, request.MentionUserIDs, request.IdempotencyKey,
		request.IdempotencyOwnerID, request.OwnerLeaseDuration, request.IdempotencyTTL, request.OutboxTTL)
}

func (service *CommentService) EditComment(ctx context.Context, request CommentEditRequest) (CommentMutationResult, error) {
	command := request.CommentCommandRequest
	return service.commit(ctx, CommentMutationEdit, command.Actor, command.RecordID, command.CommentID, command.ExpectedVersion,
		request.BodyMarkdown, "", request.MentionUserIDs, command.IdempotencyKey, command.IdempotencyOwnerID,
		command.OwnerLeaseDuration, command.IdempotencyTTL, command.OutboxTTL)
}

func (service *CommentService) RedactComment(ctx context.Context, request CommentCommandRequest) (CommentMutationResult, error) {
	return service.commit(ctx, CommentMutationRedact, request.Actor, request.RecordID, request.CommentID, request.ExpectedVersion,
		"", "", nil, request.IdempotencyKey, request.IdempotencyOwnerID, request.OwnerLeaseDuration, request.IdempotencyTTL, request.OutboxTTL)
}

func (service *CommentService) ListComments(ctx context.Context, request CommentListRequest) ([]CommentRecord, error) {
	actor, current, err := service.resolveCurrent(ctx, request.Actor, request.RecordID, recordauth.CapabilityRecordRead)
	if err != nil {
		return nil, err
	}
	command := CommentReadCommand{
		Actor: actor, RecordID: request.RecordID, CurrentRevisionID: current.CurrentRevisionID,
		RecordLockVersion: current.LockVersion, AuthorizationEpoch: current.AuthorizationEpoch,
		AuthorizationEvidence: cloneCommentAuthorizationEvidence(current.Evidence), Limit: request.Limit,
	}
	if command.Validate() != nil {
		return nil, ErrInvalidCommentRequest
	}
	comments, err := service.store.ListComments(ctx, command)
	if err != nil {
		return nil, err
	}
	result := make([]CommentRecord, len(comments))
	for index := range comments {
		if comments[index].Validate() != nil || comments[index].RecordID != request.RecordID {
			return nil, ErrCommentConflict
		}
		result[index] = comments[index].Clone()
	}
	return result, nil
}

func (service *CommentService) commit(
	ctx context.Context,
	kind CommentMutationKind,
	actorInput recordauth.ActorScope,
	recordID, commentID string,
	expectedVersion uint64,
	bodyMarkdown, replyTo string,
	mentionUserIDs []string,
	idempotencyKey, ownerID string,
	ownerLeaseDuration, idempotencyTTL, outboxTTL time.Duration,
) (CommentMutationResult, error) {
	if !validCommentMutationKind(kind) || (kind != CommentMutationCreate && (ValidateCommentID(commentID) != nil || !IsIncrementableCommentVersion(expectedVersion))) {
		return CommentMutationResult{}, ErrInvalidCommentRequest
	}
	actor, current, err := service.resolveCurrent(ctx, actorInput, recordID, recordauth.CapabilityRecordUpdate)
	if err != nil {
		return CommentMutationResult{}, err
	}
	if kind == CommentMutationCreate && replyTo != "" && ValidateCommentID(replyTo) != nil {
		return CommentMutationResult{}, ErrInvalidCommentContent
	}
	var content CommentContent
	if kind != CommentMutationRedact {
		content, err = NewCommentContent(bodyMarkdown)
		if err != nil {
			return CommentMutationResult{}, err
		}
	}
	mentions, err := NormalizeCommentMentionUserIDs(mentionUserIDs)
	if err != nil {
		return CommentMutationResult{}, err
	}
	operation := commentOperationKind(kind)
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectID(actor.ProjectID), OperationKind: operation, Key: idempotencyKey}
	if key.Validate() != nil || ownerLeaseDuration.Microseconds() <= 0 || idempotencyTTL.Microseconds() <= ownerLeaseDuration.Microseconds() || outboxTTL.Microseconds() <= 0 {
		return CommentMutationResult{}, ErrInvalidCommentRequest
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: operation,
		ProjectID: recordplatform.ProjectID(actor.ProjectID), ActorScopeDigest: actor.CanonicalHash(),
		RequestScopeDigest: commentRequestScopeDigest(kind, recordID, commentID, expectedVersion, replyTo),
		PayloadDigest:      commentPayloadDigest(kind, content, mentions),
	})
	if err != nil {
		return CommentMutationResult{}, ErrInvalidCommentRequest
	}
	if kind == CommentMutationCreate {
		commentID, err = deterministicCommentID(idempotencyKey, fingerprint)
		if err != nil {
			return CommentMutationResult{}, err
		}
	}
	version := expectedVersion + 1
	if kind == CommentMutationCreate {
		version = 1
	}
	resultFingerprint, err := commentResultFingerprint(operation, actor.UserID, recordID, commentID, version, kind)
	if err != nil {
		return CommentMutationResult{}, ErrInvalidCommentRequest
	}
	command := CommentCommand{
		Kind: kind, Actor: actor, RecordID: recordID, CommentID: commentID, ExpectedVersion: expectedVersion,
		CurrentRevisionID: current.CurrentRevisionID, RecordLockVersion: current.LockVersion,
		AuthorizationEpoch: current.AuthorizationEpoch, AuthorizationEvidence: cloneCommentAuthorizationEvidence(current.Evidence),
		Content: content, ReplyToCommentID: replyTo, MentionUserIDs: mentions,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: key, RequestFingerprint: fingerprint, OwnerID: ownerID,
			OwnerLeaseDuration: ownerLeaseDuration, RecordTTL: idempotencyTTL,
		},
		ResultFingerprint: resultFingerprint, OutboxTTL: outboxTTL,
	}
	if err := command.Validate(); err != nil {
		return CommentMutationResult{}, err
	}
	return service.store.CommitComment(ctx, command)
}

func (service *CommentService) resolveCurrent(ctx context.Context, actorInput recordauth.ActorScope, recordID string, capability recordauth.Capability) (recordauth.ActorScope, records.CurrentRecordAuthorization, error) {
	if ctx == nil || service == nil || nilCommentDependency(service.current) || nilCommentDependency(service.store) || !validRecordID(recordID) {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, ErrInvalidCommentRequest
	}
	actor, err := recordauth.NormalizeActorScope(actorInput)
	if err != nil {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, ErrInvalidCommentRequest
	}
	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), recordID)
	if err != nil {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, err
	}
	if current.RecordID != recordID || !validCollaborationRevisionIdentity(current.CurrentRevisionID) ||
		current.LockVersion == 0 || current.AuthorizationEpoch == 0 || current.Lifecycle != records.LifecycleActive {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, ErrCommentConflict
	}
	if err := records.AuthorizeRecordResource(actor, capability, current.Evidence); err != nil {
		return recordauth.ActorScope{}, records.CurrentRecordAuthorization{}, err
	}
	return actor, current, nil
}

func commentOperationKind(kind CommentMutationKind) recordplatform.OperationKind {
	switch kind {
	case CommentMutationCreate:
		return recordplatform.OperationKindRecordCommentCreate
	case CommentMutationEdit:
		return recordplatform.OperationKindRecordCommentEdit
	case CommentMutationRedact:
		return recordplatform.OperationKindRecordCommentRedact
	default:
		return ""
	}
}

func commentRequestScopeDigest(kind CommentMutationKind, recordID, commentID string, expectedVersion uint64, replyTo string) [sha256.Size]byte {
	encoder := actionCanonicalEncoder{}
	encoder.string(commentRequestScopeDomainV1)
	encoder.string(string(kind))
	encoder.string(recordID)
	encoder.string(commentID)
	encoder.uint64(expectedVersion)
	encoder.string(replyTo)
	return sha256.Sum256(encoder.bytes)
}

func commentPayloadDigest(kind CommentMutationKind, content CommentContent, mentions []string) [sha256.Size]byte {
	encoder := actionCanonicalEncoder{}
	encoder.string(commentPayloadDomainV1)
	encoder.string(string(kind))
	if !content.Empty() {
		digest := content.Digest()
		encoder.string(string(digest[:]))
	}
	for _, userID := range mentions {
		encoder.string(userID)
	}
	return sha256.Sum256(encoder.bytes)
}

func deterministicCommentID(key string, fingerprint recordplatform.RequestFingerprintV1) (string, error) {
	digest, err := fingerprint.PersistedBytes()
	if err != nil || key == "" {
		return "", ErrInvalidCommentRequest
	}
	encoder := actionCanonicalEncoder{}
	encoder.string(commentIdentityDomainV1)
	encoder.string(key)
	encoder.string(string(digest[:]))
	identity := sha256.Sum256(encoder.bytes)
	return "rcm_" + hex.EncodeToString(identity[:]), nil
}

func commentResultFingerprint(operation recordplatform.OperationKind, actorID, recordID, commentID string, version uint64, kind CommentMutationKind) (recordplatform.RequestFingerprintV1, error) {
	encoder := actionCanonicalEncoder{}
	encoder.string(commentResultDomainV1)
	encoder.string(actorID)
	encoder.string(recordID)
	encoder.string(commentID)
	encoder.uint64(version)
	encoder.string(string(kind))
	digest := sha256.Sum256(encoder.bytes)
	return recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: operation,
		ProjectID: recordplatform.ProjectIDDefault, ActorScopeDigest: digest,
		RequestScopeDigest: digest, PayloadDigest: digest,
	})
}

func cloneCommentAuthorizationEvidence(evidence records.RecordAuthorizationEvidence) records.RecordAuthorizationEvidence {
	evidence.Sources = append([]recordauth.SourceAuthorization(nil), evidence.Sources...)
	return evidence
}

func nilCommentDependency(value any) bool {
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
