package records

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
)

var ErrInvalidRecordReadRequest = errors.New("invalid record read request")

type RecordSort string

const (
	RecordSortUpdatedDesc RecordSort = "updated_at_desc"
	RecordSortUpdatedAsc  RecordSort = "updated_at_asc"
)

type RecordCursor struct {
	UpdatedAt time.Time
	RecordID  string
}

type RecordCandidatePage struct {
	After *RecordCursor
	Sort  RecordSort
	Limit uint64
}

type RecordCandidate struct {
	RecordID  string
	UpdatedAt time.Time
}

type RecordRevisionCandidatePage struct {
	RecordID           string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	Limit              uint64
}

type RecordRevisionCandidate struct {
	RevisionID string
	RevisionNo uint64
}

type StoredRecordRevisionRequest struct {
	RecordID           string
	RevisionID         string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
}

type StoredRecordRevision struct {
	RecordID           string
	RevisionID         string
	BaseRevisionID     string
	RevisionNo         uint64
	LockVersion        uint64
	AuthorizationEpoch uint64
	Lifecycle          Lifecycle
	Input              CompleteRevisionInput
	CreatedAt          time.Time
	RecordCreatedAt    time.Time
	RecordUpdatedAt    time.Time
	ArchivedAt         *time.Time
}

type RecordReadStore interface {
	ListRecordCandidates(context.Context, RecordCandidatePage) ([]RecordCandidate, error)
	ListRevisionCandidates(context.Context, RecordRevisionCandidatePage) ([]RecordRevisionCandidate, error)
	ReadRecordRevision(context.Context, StoredRecordRevisionRequest) (StoredRecordRevision, error)
}

type RecordRevisionAuthorization struct {
	RecordID           string
	RevisionID         string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	Lifecycle          Lifecycle
	Evidence           RecordAuthorizationEvidence
}

type RecordRevisionAuthorizationSource interface {
	ResolveRecordRevisionAuthorization(
		context.Context,
		recordauth.ActorScope,
		string,
		string,
	) (RecordRevisionAuthorization, error)
}

type RecordCapabilities struct {
	Read            bool
	Update          bool
	Archive         bool
	Restore         bool
	Draft           bool
	PermanentDelete bool
}

type RecordRevision struct {
	RecordID       string
	RevisionID     string
	BaseRevisionID string
	RevisionNo     uint64
	Input          CompleteRevisionInput
	CreatedAt      time.Time
}

type Record struct {
	RecordID           string
	ProjectID          recordauth.ProjectID
	Lifecycle          Lifecycle
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	Current            RecordRevision
	Capabilities       RecordCapabilities
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ArchivedAt         *time.Time
}

type RecordGetRequest struct {
	Actor    recordauth.ActorScope
	RecordID string
}

// RecordListRequest walks records in sort order only. Filtering by text,
// lifecycle, or type belongs to the search index: doing it here meant hydrating
// and authorizing every record just to discard most of them, and it would now be
// a second search path with its own matching rules.
type RecordListRequest struct {
	Actor recordauth.ActorScope
	Sort  RecordSort
	After *RecordCursor
	Limit uint64
}

type RecordListResult struct {
	Records    []Record
	NextCursor *RecordCursor
}

type RecordRevisionGetRequest struct {
	Actor      recordauth.ActorScope
	RecordID   string
	RevisionID string
}

type RecordRevisionListRequest struct {
	Actor    recordauth.ActorScope
	RecordID string
	Limit    uint64
}

type RecordReadService struct {
	current   CurrentRecordAuthorizationSource
	revisions RecordRevisionAuthorizationSource
	store     RecordReadStore
}

func NewRecordReadService(
	current CurrentRecordAuthorizationSource,
	revisions RecordRevisionAuthorizationSource,
	store RecordReadStore,
) (*RecordReadService, error) {
	if nilRecordReadDependency(current) || nilRecordReadDependency(revisions) || nilRecordReadDependency(store) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidRecordReadRequest)
	}
	return &RecordReadService{current: current, revisions: revisions, store: store}, nil
}

func (service *RecordReadService) GetRecord(ctx context.Context, request RecordGetRequest) (Record, error) {
	if ctx == nil || service == nil || nilRecordReadDependency(service.current) ||
		nilRecordReadDependency(service.store) || !validRecordRootID(request.RecordID) {
		return Record{}, ErrInvalidRecordReadRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return Record{}, fmt.Errorf("%w: actor", ErrInvalidRecordReadRequest)
	}
	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), request.RecordID)
	if err != nil {
		return Record{}, err
	}
	if err := validateCurrentRecordAuthorization(request.RecordID, actor.ProjectID, current); err != nil {
		return Record{}, err
	}
	if err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, current.Evidence); err != nil {
		return Record{}, err
	}

	stored, err := service.store.ReadRecordRevision(ctx, StoredRecordRevisionRequest{
		RecordID:           current.RecordID,
		RevisionID:         current.CurrentRevisionID,
		CurrentRevisionID:  current.CurrentRevisionID,
		LockVersion:        current.LockVersion,
		AuthorizationEpoch: current.AuthorizationEpoch,
	})
	if err != nil {
		return Record{}, err
	}
	if err := validateStoredRecordRevision(stored, current.RecordID, current.CurrentRevisionID, current.LockVersion, current.AuthorizationEpoch, current.Lifecycle); err != nil {
		return Record{}, err
	}
	return recordFromStored(actor, current, stored), nil
}

func (service *RecordReadService) ListRecords(ctx context.Context, request RecordListRequest) (RecordListResult, error) {
	if ctx == nil || service == nil || nilRecordReadDependency(service.current) ||
		nilRecordReadDependency(service.store) || request.Limit == 0 || request.Limit > 100 {
		return RecordListResult{}, ErrInvalidRecordReadRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return RecordListResult{}, fmt.Errorf("%w: actor", ErrInvalidRecordReadRequest)
	}
	if request.Sort == "" {
		request.Sort = RecordSortUpdatedDesc
	}
	if request.Sort != RecordSortUpdatedDesc && request.Sort != RecordSortUpdatedAsc {
		return RecordListResult{}, ErrInvalidRecordReadRequest
	}

	type listedRecord struct {
		record    Record
		candidate RecordCandidate
	}
	matched := make([]listedRecord, 0, request.Limit+1)
	after := cloneRecordCursor(request.After)
	const batchLimit = uint64(200)
	for uint64(len(matched)) <= request.Limit {
		candidates, err := service.store.ListRecordCandidates(ctx, RecordCandidatePage{
			After: after,
			Sort:  request.Sort,
			Limit: batchLimit,
		})
		if err != nil {
			return RecordListResult{}, err
		}
		if len(candidates) == 0 {
			break
		}
		for _, candidate := range candidates {
			current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), candidate.RecordID)
			if err != nil {
				if errors.Is(err, ErrRecordNotFound) {
					continue
				}
				return RecordListResult{}, err
			}
			if err := validateCurrentRecordAuthorization(candidate.RecordID, actor.ProjectID, current); err != nil {
				continue
			}
			if err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, current.Evidence); err != nil {
				if errors.Is(err, recordauth.ErrDenied) {
					continue
				}
				return RecordListResult{}, err
			}
			stored, err := service.store.ReadRecordRevision(ctx, StoredRecordRevisionRequest{
				RecordID:           current.RecordID,
				RevisionID:         current.CurrentRevisionID,
				CurrentRevisionID:  current.CurrentRevisionID,
				LockVersion:        current.LockVersion,
				AuthorizationEpoch: current.AuthorizationEpoch,
			})
			if err != nil {
				if errors.Is(err, ErrRecordNotFound) || errors.Is(err, ErrRecordRevisionConflict) ||
					errors.Is(err, ErrRecordDeletionReserved) {
					continue
				}
				return RecordListResult{}, err
			}
			if validateStoredRecordRevision(stored, current.RecordID, current.CurrentRevisionID, current.LockVersion, current.AuthorizationEpoch, current.Lifecycle) != nil {
				continue
			}
			matched = append(matched, listedRecord{
				record: recordFromStored(actor, current, stored), candidate: candidate,
			})
			if uint64(len(matched)) > request.Limit {
				break
			}
		}
		last := candidates[len(candidates)-1]
		after = &RecordCursor{UpdatedAt: last.UpdatedAt.UTC(), RecordID: last.RecordID}
		if uint64(len(candidates)) < batchLimit {
			break
		}
	}

	result := RecordListResult{Records: make([]Record, 0, minUint64(request.Limit, uint64(len(matched))))}
	visible := matched
	if uint64(len(visible)) > request.Limit {
		visible = visible[:request.Limit]
		last := visible[len(visible)-1].candidate
		result.NextCursor = &RecordCursor{UpdatedAt: last.UpdatedAt.UTC(), RecordID: last.RecordID}
	}
	for _, item := range visible {
		result.Records = append(result.Records, item.record)
	}
	return result, nil
}

func (service *RecordReadService) GetRevision(ctx context.Context, request RecordRevisionGetRequest) (RecordRevision, error) {
	if ctx == nil || service == nil || nilRecordReadDependency(service.current) ||
		nilRecordReadDependency(service.revisions) ||
		nilRecordReadDependency(service.store) || !validRecordRootID(request.RecordID) || !validRevisionID(request.RevisionID) {
		return RecordRevision{}, ErrInvalidRecordReadRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return RecordRevision{}, fmt.Errorf("%w: actor", ErrInvalidRecordReadRequest)
	}
	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), request.RecordID)
	if err != nil {
		return RecordRevision{}, err
	}
	if err := validateCurrentRecordAuthorization(request.RecordID, actor.ProjectID, current); err != nil {
		return RecordRevision{}, err
	}
	if err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, current.Evidence); err != nil {
		return RecordRevision{}, err
	}
	return service.getRevisionWithCurrent(ctx, actor, current, request.RevisionID)
}

func (service *RecordReadService) getRevisionWithCurrent(
	ctx context.Context,
	actor recordauth.ActorScope,
	current CurrentRecordAuthorization,
	revisionID string,
) (RecordRevision, error) {
	authorization, err := service.revisions.ResolveRecordRevisionAuthorization(
		ctx,
		actor.Clone(),
		current.RecordID,
		revisionID,
	)
	if err != nil {
		return RecordRevision{}, err
	}
	if err := validateRecordRevisionAuthorization(current.RecordID, revisionID, actor.ProjectID, authorization); err != nil {
		return RecordRevision{}, err
	}
	if authorization.CurrentRevisionID != current.CurrentRevisionID ||
		authorization.LockVersion != current.LockVersion ||
		authorization.AuthorizationEpoch != current.AuthorizationEpoch ||
		authorization.Lifecycle != current.Lifecycle {
		return RecordRevision{}, ErrRecordRevisionConflict
	}
	if err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, authorization.Evidence); err != nil {
		return RecordRevision{}, err
	}

	stored, err := service.store.ReadRecordRevision(ctx, StoredRecordRevisionRequest{
		RecordID:           authorization.RecordID,
		RevisionID:         authorization.RevisionID,
		CurrentRevisionID:  authorization.CurrentRevisionID,
		LockVersion:        authorization.LockVersion,
		AuthorizationEpoch: authorization.AuthorizationEpoch,
	})
	if err != nil {
		return RecordRevision{}, err
	}
	if err := validateStoredRecordRevision(stored, authorization.RecordID, authorization.RevisionID, authorization.LockVersion, authorization.AuthorizationEpoch, authorization.Lifecycle); err != nil {
		return RecordRevision{}, err
	}
	return revisionFromStored(stored), nil
}

func (service *RecordReadService) ListRevisions(
	ctx context.Context,
	request RecordRevisionListRequest,
) ([]RecordRevision, error) {
	if ctx == nil || service == nil || nilRecordReadDependency(service.current) ||
		nilRecordReadDependency(service.revisions) || nilRecordReadDependency(service.store) ||
		!validRecordRootID(request.RecordID) || request.Limit == 0 || request.Limit > 200 {
		return nil, ErrInvalidRecordReadRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return nil, fmt.Errorf("%w: actor", ErrInvalidRecordReadRequest)
	}
	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), request.RecordID)
	if err != nil {
		return nil, err
	}
	if err := validateCurrentRecordAuthorization(request.RecordID, actor.ProjectID, current); err != nil {
		return nil, err
	}
	if err := AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, current.Evidence); err != nil {
		return nil, err
	}
	candidates, err := service.store.ListRevisionCandidates(ctx, RecordRevisionCandidatePage{
		RecordID:           current.RecordID,
		CurrentRevisionID:  current.CurrentRevisionID,
		LockVersion:        current.LockVersion,
		AuthorizationEpoch: current.AuthorizationEpoch,
		Limit:              request.Limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]RecordRevision, 0, len(candidates))
	for _, candidate := range candidates {
		revision, err := service.getRevisionWithCurrent(ctx, actor, current, candidate.RevisionID)
		if err != nil {
			if errors.Is(err, recordauth.ErrDenied) || errors.Is(err, ErrRecordNotFound) ||
				errors.Is(err, ErrRecordRevisionConflict) || errors.Is(err, ErrRecordDeletionReserved) {
				continue
			}
			return nil, err
		}
		if revision.RevisionNo != candidate.RevisionNo {
			return nil, ErrRecordRevisionConflict
		}
		result = append(result, revision)
	}
	return result, nil
}

func validateCurrentRecordAuthorization(
	recordID string,
	projectID recordauth.ProjectID,
	current CurrentRecordAuthorization,
) error {
	if current.RecordID != recordID || !validRevisionID(current.CurrentRevisionID) ||
		current.LockVersion == 0 || current.AuthorizationEpoch == 0 ||
		ValidateLifecycle(current.Lifecycle) != nil || current.Evidence.ProjectID != projectID {
		return ErrRecordNotFound
	}
	return nil
}

func validateRecordRevisionAuthorization(
	recordID string,
	revisionID string,
	projectID recordauth.ProjectID,
	authorization RecordRevisionAuthorization,
) error {
	if authorization.RecordID != recordID || authorization.RevisionID != revisionID ||
		!validRevisionID(authorization.CurrentRevisionID) || authorization.LockVersion == 0 ||
		authorization.AuthorizationEpoch == 0 || ValidateLifecycle(authorization.Lifecycle) != nil ||
		authorization.Evidence.ProjectID != projectID {
		return ErrRecordNotFound
	}
	return nil
}

func validateStoredRecordRevision(
	stored StoredRecordRevision,
	recordID string,
	revisionID string,
	lockVersion uint64,
	authorizationEpoch uint64,
	lifecycle Lifecycle,
) error {
	if stored.RecordID != recordID || stored.RevisionID != revisionID || stored.RevisionNo == 0 ||
		stored.LockVersion != lockVersion || stored.AuthorizationEpoch != authorizationEpoch ||
		stored.Lifecycle != lifecycle || stored.Input.Title() == "" || stored.CreatedAt.IsZero() ||
		stored.RecordCreatedAt.IsZero() || stored.RecordUpdatedAt.IsZero() ||
		stored.RecordUpdatedAt.Before(stored.RecordCreatedAt) {
		return ErrRecordRevisionConflict
	}
	return nil
}

func recordFromStored(actor recordauth.ActorScope, current CurrentRecordAuthorization, stored StoredRecordRevision) Record {
	canUpdate := AuthorizeRecordResource(actor, recordauth.CapabilityRecordUpdate, current.Evidence) == nil
	canDraft := AuthorizeRecordResource(actor, recordauth.CapabilityDraftCreate, current.Evidence) == nil
	capabilities := RecordCapabilities{
		Read:   true,
		Update: canUpdate && current.Lifecycle == LifecycleActive,
		Draft:  canDraft && current.Lifecycle == LifecycleActive,
	}
	if canUpdate {
		capabilities.Archive = current.Lifecycle == LifecycleActive
		capabilities.Restore = current.Lifecycle == LifecycleArchived
	}
	return Record{
		RecordID:           stored.RecordID,
		ProjectID:          current.Evidence.ProjectID,
		Lifecycle:          stored.Lifecycle,
		CurrentRevisionID:  current.CurrentRevisionID,
		LockVersion:        stored.LockVersion,
		AuthorizationEpoch: stored.AuthorizationEpoch,
		Current:            revisionFromStored(stored),
		Capabilities:       capabilities,
		CreatedAt:          stored.RecordCreatedAt.UTC(),
		UpdatedAt:          stored.RecordUpdatedAt.UTC(),
		ArchivedAt:         cloneTimePointer(stored.ArchivedAt),
	}
}

func revisionFromStored(stored StoredRecordRevision) RecordRevision {
	return RecordRevision{
		RecordID:       stored.RecordID,
		RevisionID:     stored.RevisionID,
		BaseRevisionID: stored.BaseRevisionID,
		RevisionNo:     stored.RevisionNo,
		Input:          stored.Input,
		CreatedAt:      stored.CreatedAt.UTC(),
	}
}

func nilRecordReadDependency(value any) bool {
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

func cloneRecordCursor(value *RecordCursor) *RecordCursor {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.UpdatedAt = cloned.UpdatedAt.UTC()
	return &cloned
}

func minUint64(left, right uint64) uint64 {
	if left < right {
		return left
	}
	return right
}
