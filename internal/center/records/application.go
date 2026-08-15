package records

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

var ErrInvalidApplicationRequest = errors.New("invalid records application request")

type applicationReadService interface {
	GetRecord(context.Context, RecordGetRequest) (Record, error)
	ListRecords(context.Context, RecordListRequest) (RecordListResult, error)
	GetRevision(context.Context, RecordRevisionGetRequest) (RecordRevision, error)
	ListRevisions(context.Context, RecordRevisionListRequest) ([]RecordRevision, error)
}

type applicationRevisionService interface {
	SaveRevision(context.Context, RevisionSaveRequest) (RevisionCommitResult, error)
}

type applicationLifecycleService interface {
	ChangeLifecycle(context.Context, RecordLifecycleRequest) (RecordLifecycleResult, error)
}

type applicationDraftService interface {
	ReadDraft(context.Context, DraftReadRequest) (Draft, error)
	ListDrafts(context.Context, DraftListRequest) ([]Draft, error)
	CreateDraft(context.Context, DraftCreateRequest) (Draft, error)
	PatchDraft(context.Context, DraftPatchRequest) (Draft, error)
	DiscardDraft(context.Context, DraftDiscardRequest) error
	PreparePublish(context.Context, DraftPublishRequest) (Draft, error)
}

type ApplicationOptions struct {
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

func (options ApplicationOptions) validate() error {
	owner := recordplatform.OwnerLease{
		OwnerID:    options.IdempotencyOwnerID,
		Generation: 1,
		ExpiresAt:  time.Unix(1, 0).UTC(),
	}
	if owner.Validate() != nil || options.OwnerLeaseDuration.Microseconds() <= 0 ||
		options.IdempotencyTTL.Microseconds() <= options.OwnerLeaseDuration.Microseconds() ||
		options.OutboxTTL.Microseconds() <= 0 {
		return ErrInvalidApplicationRequest
	}
	return nil
}

type Application struct {
	read      applicationReadService
	revisions applicationRevisionService
	lifecycle applicationLifecycleService
	drafts    applicationDraftService
	options   ApplicationOptions
}

func NewApplication(
	read applicationReadService,
	revisions applicationRevisionService,
	lifecycle applicationLifecycleService,
	drafts applicationDraftService,
	options ApplicationOptions,
) (*Application, error) {
	if nilRevisionServiceDependency(read) || nilRevisionServiceDependency(revisions) ||
		nilRevisionServiceDependency(lifecycle) || nilRevisionServiceDependency(drafts) ||
		options.validate() != nil {
		return nil, fmt.Errorf("%w: dependency or options", ErrInvalidApplicationRequest)
	}
	return &Application{
		read:      read,
		revisions: revisions,
		lifecycle: lifecycle,
		drafts:    drafts,
		options:   options,
	}, nil
}

func (application *Application) GetRecord(ctx context.Context, request RecordGetRequest) (Record, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.read) {
		return Record{}, ErrInvalidApplicationRequest
	}
	return application.read.GetRecord(ctx, request)
}

func (application *Application) ListRecords(
	ctx context.Context,
	request RecordListRequest,
) (RecordListResult, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.read) {
		return RecordListResult{}, ErrInvalidApplicationRequest
	}
	return application.read.ListRecords(ctx, request)
}

func (application *Application) GetRevision(
	ctx context.Context,
	request RecordRevisionGetRequest,
) (RecordRevision, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.read) {
		return RecordRevision{}, ErrInvalidApplicationRequest
	}
	return application.read.GetRevision(ctx, request)
}

func (application *Application) ListRevisions(
	ctx context.Context,
	request RecordRevisionListRequest,
) ([]RecordRevision, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.read) {
		return nil, ErrInvalidApplicationRequest
	}
	return application.read.ListRevisions(ctx, request)
}

func (application *Application) ReadDraft(ctx context.Context, request DraftReadRequest) (Draft, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.drafts) {
		return Draft{}, ErrInvalidApplicationRequest
	}
	return application.drafts.ReadDraft(ctx, request)
}

func (application *Application) ListDrafts(ctx context.Context, request DraftListRequest) ([]Draft, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.drafts) {
		return nil, ErrInvalidApplicationRequest
	}
	return application.drafts.ListDrafts(ctx, request)
}

func (application *Application) CreateDraft(ctx context.Context, request DraftCreateRequest) (Draft, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.drafts) {
		return Draft{}, ErrInvalidApplicationRequest
	}
	return application.drafts.CreateDraft(ctx, request)
}

func (application *Application) PatchDraft(ctx context.Context, request DraftPatchRequest) (Draft, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.drafts) {
		return Draft{}, ErrInvalidApplicationRequest
	}
	return application.drafts.PatchDraft(ctx, request)
}

func (application *Application) DiscardDraft(ctx context.Context, request DraftDiscardRequest) error {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.drafts) {
		return ErrInvalidApplicationRequest
	}
	return application.drafts.DiscardDraft(ctx, request)
}

func (application *Application) PreparePublish(
	ctx context.Context,
	request DraftPublishRequest,
) (Draft, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.drafts) {
		return Draft{}, ErrInvalidApplicationRequest
	}
	return application.drafts.PreparePublish(ctx, request)
}

type RecordRestoreRequest struct {
	Actor               recordauth.ActorScope
	RecordID            string
	RevisionID          string
	SaveReason          string
	EvidencePreparation evidence.RevisionPreparation
	IdempotencyKey      string
}

type RecordCreateRequest struct {
	Actor               recordauth.ActorScope
	RecordID            string
	DraftID             string
	DraftETag           DraftETag
	Values              CompleteRevisionValues
	SubjectReferences   []SubjectReference
	EvidencePreparation evidence.RevisionPreparation
	IdempotencyKey      string
}

type RecordRevisionCreateRequest struct {
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
	IdempotencyKey      string
}

type RecordLifecycleChangeRequest struct {
	Actor           recordauth.ActorScope
	RecordID        string
	TargetLifecycle Lifecycle
	IdempotencyKey  string
}

func (application *Application) CreateRecord(
	ctx context.Context,
	request RecordCreateRequest,
) (RevisionCommitResult, error) {
	return application.saveRevision(ctx, RevisionSaveRequest{
		Actor:               request.Actor,
		RecordID:            request.RecordID,
		DraftID:             request.DraftID,
		DraftETag:           request.DraftETag,
		Values:              request.Values,
		SubjectReferences:   request.SubjectReferences,
		EvidencePreparation: request.EvidencePreparation,
		ActivityKind:        DomainActivityRecordCreated,
		IdempotencyKey:      request.IdempotencyKey,
	})
}

func (application *Application) CreateRevision(
	ctx context.Context,
	request RecordRevisionCreateRequest,
) (RevisionCommitResult, error) {
	return application.saveRevision(ctx, RevisionSaveRequest{
		Actor:               request.Actor,
		RecordID:            request.RecordID,
		BaseRevisionID:      request.BaseRevisionID,
		LockVersion:         request.LockVersion,
		AuthorizationEpoch:  request.AuthorizationEpoch,
		DraftID:             request.DraftID,
		DraftETag:           request.DraftETag,
		Values:              request.Values,
		SubjectReferences:   request.SubjectReferences,
		EvidencePreparation: request.EvidencePreparation,
		ActivityKind:        DomainActivityRecordRevised,
		IdempotencyKey:      request.IdempotencyKey,
	})
}

func (application *Application) saveRevision(
	ctx context.Context,
	request RevisionSaveRequest,
) (RevisionCommitResult, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.revisions) ||
		application.options.validate() != nil {
		return RevisionCommitResult{}, ErrInvalidApplicationRequest
	}
	request.IdempotencyOwnerID = application.options.IdempotencyOwnerID
	request.OwnerLeaseDuration = application.options.OwnerLeaseDuration
	request.IdempotencyTTL = application.options.IdempotencyTTL
	request.OutboxTTL = application.options.OutboxTTL
	return application.revisions.SaveRevision(ctx, request)
}

func (application *Application) ChangeLifecycle(
	ctx context.Context,
	request RecordLifecycleChangeRequest,
) (RecordLifecycleResult, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.lifecycle) ||
		application.options.validate() != nil {
		return RecordLifecycleResult{}, ErrInvalidApplicationRequest
	}
	return application.lifecycle.ChangeLifecycle(ctx, RecordLifecycleRequest{
		Actor:              request.Actor,
		RecordID:           request.RecordID,
		TargetLifecycle:    request.TargetLifecycle,
		IdempotencyKey:     request.IdempotencyKey,
		IdempotencyOwnerID: application.options.IdempotencyOwnerID,
		OwnerLeaseDuration: application.options.OwnerLeaseDuration,
		IdempotencyTTL:     application.options.IdempotencyTTL,
		OutboxTTL:          application.options.OutboxTTL,
	})
}

func (application *Application) RestoreRevision(
	ctx context.Context,
	request RecordRestoreRequest,
) (RevisionCommitResult, error) {
	if ctx == nil || application == nil || nilRevisionServiceDependency(application.read) ||
		nilRevisionServiceDependency(application.revisions) || application.options.validate() != nil ||
		!validRecordRootID(request.RecordID) || !validRevisionID(request.RevisionID) {
		return RevisionCommitResult{}, ErrInvalidApplicationRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return RevisionCommitResult{}, fmt.Errorf("%w: actor", ErrInvalidApplicationRequest)
	}
	key := recordplatform.IdempotencyKey{
		ProjectID:     recordplatform.ProjectID(actor.ProjectID),
		OperationKind: recordplatform.OperationKindRecordUpdate,
		Key:           request.IdempotencyKey,
	}
	if key.Validate() != nil {
		return RevisionCommitResult{}, fmt.Errorf("%w: idempotency key", ErrInvalidApplicationRequest)
	}

	historical, err := application.read.GetRevision(ctx, RecordRevisionGetRequest{
		Actor:      actor.Clone(),
		RecordID:   request.RecordID,
		RevisionID: request.RevisionID,
	})
	if err != nil {
		return RevisionCommitResult{}, err
	}
	if historical.RecordID != request.RecordID || historical.RevisionID != request.RevisionID ||
		historical.RevisionNo == 0 || historical.Input.Title() == "" || historical.CreatedAt.IsZero() {
		return RevisionCommitResult{}, ErrRecordRevisionConflict
	}
	if !slices.Equal(request.EvidencePreparation.SnapshotIDs(), historical.Input.EvidenceSnapshotIDs()) {
		return RevisionCommitResult{}, fmt.Errorf("%w: evidence preparation", ErrInvalidApplicationRequest)
	}

	current, err := application.read.GetRecord(ctx, RecordGetRequest{
		Actor:    actor.Clone(),
		RecordID: request.RecordID,
	})
	if err != nil {
		return RevisionCommitResult{}, err
	}
	if current.RecordID != request.RecordID || !validRevisionID(current.CurrentRevisionID) ||
		current.LockVersion == 0 || current.AuthorizationEpoch == 0 || current.Lifecycle != LifecycleActive {
		return RevisionCommitResult{}, ErrRecordRevisionConflict
	}

	values := completeRevisionValuesForRestore(historical.Input, request.SaveReason)
	references := subjectReferencesForRestore(historical.Input.Subjects())
	return application.revisions.SaveRevision(ctx, RevisionSaveRequest{
		Actor:               actor,
		RecordID:            current.RecordID,
		BaseRevisionID:      current.CurrentRevisionID,
		LockVersion:         current.LockVersion,
		AuthorizationEpoch:  current.AuthorizationEpoch,
		Values:              values,
		SubjectReferences:   references,
		EvidencePreparation: request.EvidencePreparation,
		ActivityKind:        DomainActivityRecordRestored,
		IdempotencyKey:      request.IdempotencyKey,
		IdempotencyOwnerID:  application.options.IdempotencyOwnerID,
		OwnerLeaseDuration:  application.options.OwnerLeaseDuration,
		IdempotencyTTL:      application.options.IdempotencyTTL,
		OutboxTTL:           application.options.OutboxTTL,
	})
}

func completeRevisionValuesForRestore(input CompleteRevisionInput, saveReason string) CompleteRevisionValues {
	return CompleteRevisionValues{
		Title:                  input.Title(),
		BodyMarkdown:           input.BodyMarkdown(),
		MarkdownDialectVersion: input.MarkdownDialectVersion(),
		RecordType:             input.RecordType(),
		BusinessStatus:         input.BusinessStatus(),
		ImpactLevel:            input.ImpactLevel(),
		OccurredAt:             input.OccurredAt(),
		CompletedAt:            input.CompletedAt(),
		VisibilityScope:        input.VisibilityScope(),
		Tags:                   input.Tags(),
		OwnerID:                input.OwnerID(),
		Participants:           input.Participants(),
		AttachmentIDs:          input.AttachmentIDs(),
		FollowUpAt:             input.FollowUpAt(),
		Template:               input.Template(),
		SaveReason:             saveReason,
	}
}

func subjectReferencesForRestore(subjects []RevisionSubject) []SubjectReference {
	references := make([]SubjectReference, 0, len(subjects))
	for _, subject := range subjects {
		references = append(references, SubjectReference{
			RegistryVersion: subject.RegistryVersion,
			Kind:            subject.Kind,
			Role:            subject.Role,
			SourceID:        subject.SourceID,
			Primary:         subject.Primary,
		})
	}
	return references
}
