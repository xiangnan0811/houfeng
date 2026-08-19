package records

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"houfeng/internal/center/recordauth"
)

const (
	draftPayloadCanonicalDomainV1 = "houfeng.records.draft-payload.v1"
	draftETagCanonicalDomainV1    = "houfeng.records.draft-etag.v1"
	draftETagPrefix               = `"draft-v1-`
)

var (
	ErrInvalidDraftPayload   = errors.New("invalid record draft payload")
	ErrInvalidDraftETag      = errors.New("invalid record draft etag")
	ErrInvalidDraftCommand   = errors.New("invalid record draft command")
	ErrDraftNotFound         = errors.New("record draft not found")
	ErrDraftConflict         = errors.New("record draft conflict")
	ErrDraftRevisionConflict = errors.New("record draft base revision conflict")
)

type Draft struct {
	DraftID        string
	ProjectID      recordauth.ProjectID
	RecordID       string
	BaseRevisionID string
	AuthorID       string
	Payload        DraftPayload
	Version        uint64
	ETag           DraftETag
	WarningAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      time.Time
}

func (draft Draft) Validate() error {
	if !validDraftID(draft.DraftID) || recordauth.ValidateProjectID(draft.ProjectID) != nil ||
		recordauth.ValidateActorUserID(draft.AuthorID) != nil || draft.Version == 0 ||
		len(draft.Payload.json) == 0 || draft.Payload.hash == [sha256.Size]byte{} ||
		draft.CreatedAt.IsZero() || draft.UpdatedAt.IsZero() || draft.WarningAt.IsZero() || draft.ExpiresAt.IsZero() ||
		draft.UpdatedAt.Before(draft.CreatedAt) || draft.WarningAt.Before(draft.UpdatedAt) ||
		draft.WarningAt.After(draft.ExpiresAt) || !draft.ExpiresAt.After(draft.UpdatedAt) {
		return ErrInvalidDraftCommand
	}
	newRecord := draft.RecordID == "" && draft.BaseRevisionID == ""
	existingRecord := validRecordRootID(draft.RecordID) && validRevisionID(draft.BaseRevisionID)
	if !newRecord && !existingRecord {
		return fmt.Errorf("%w: record and base revision", ErrInvalidDraftCommand)
	}
	wantETag, err := NewDraftETag(draft.DraftID, draft.AuthorID, draft.Version, draft.Payload)
	if err != nil || draft.ETag != wantETag {
		return fmt.Errorf("%w: etag", ErrInvalidDraftCommand)
	}
	return nil
}

type DraftRevisionConflictError struct {
	ServerRevisionID         string
	ServerLockVersion        uint64
	ServerAuthorizationEpoch uint64
	Draft                    Draft
}

func (*DraftRevisionConflictError) Error() string {
	return ErrDraftRevisionConflict.Error()
}

func (*DraftRevisionConflictError) Unwrap() error {
	return ErrDraftRevisionConflict
}

type DraftConflictError struct {
	Server       Draft
	LocalPayload DraftPayload
}

func (*DraftConflictError) Error() string {
	return ErrDraftConflict.Error()
}

func (*DraftConflictError) Unwrap() error {
	return ErrDraftConflict
}

type DraftPublishRequest struct {
	Actor   recordauth.ActorScope
	DraftID string
}

type DraftCreateRequest struct {
	Actor          recordauth.ActorScope
	DraftID        string
	RecordID       string
	BaseRevisionID string
	Payload        DraftPayload
}

type DraftPatchRequest struct {
	Actor   recordauth.ActorScope
	DraftID string
	IfMatch DraftETag
	Payload DraftPayload
}

type DraftDiscardRequest struct {
	Actor   recordauth.ActorScope
	DraftID string
}

type DraftReadRequest struct {
	Actor   recordauth.ActorScope
	DraftID string
}

// DraftCursor is a position in the author's activity order, not a filter, so a
// draft edited between pages moves rather than disappearing from the sequence.
type DraftCursor struct {
	UpdatedAt time.Time
	DraftID   string
}

func (cursor DraftCursor) Validate() error {
	if cursor.UpdatedAt.IsZero() || !validDraftID(cursor.DraftID) {
		return ErrInvalidDraftCommand
	}
	return nil
}

type DraftListRequest struct {
	Actor recordauth.ActorScope
	After *DraftCursor
	Limit uint64
}

type DraftListResult struct {
	Drafts     []Draft
	NextCursor *DraftCursor
}

// DraftCandidatePage is the store-facing read. Its limit is a candidate budget
// rather than a page size, because authorization and hydration drop candidates
// after the store has already returned them.
type DraftCandidatePage struct {
	After *DraftCursor
	Limit uint64
}

type DraftRouting struct {
	DraftID        string
	ProjectID      recordauth.ProjectID
	RecordID       string
	BaseRevisionID string
	AuthorID       string
	UpdatedAt      time.Time
}

func DraftRoutingFromDraft(draft Draft) DraftRouting {
	return DraftRouting{
		DraftID:        draft.DraftID,
		ProjectID:      draft.ProjectID,
		RecordID:       draft.RecordID,
		BaseRevisionID: draft.BaseRevisionID,
		AuthorID:       draft.AuthorID,
		UpdatedAt:      draft.UpdatedAt.UTC(),
	}
}

func (routing DraftRouting) Validate() error {
	if !validDraftID(routing.DraftID) || recordauth.ValidateProjectID(routing.ProjectID) != nil ||
		recordauth.ValidateActorUserID(routing.AuthorID) != nil || routing.UpdatedAt.IsZero() {
		return ErrInvalidDraftCommand
	}
	newRecord := routing.RecordID == "" && routing.BaseRevisionID == ""
	existingRecord := validRecordRootID(routing.RecordID) && validRevisionID(routing.BaseRevisionID)
	if !newRecord && !existingRecord {
		return ErrInvalidDraftCommand
	}
	return nil
}

type DraftReader interface {
	GetDraft(context.Context, string, string) (Draft, error)
}

type DraftRepository interface {
	DraftReader
	GetDraftRouting(context.Context, string, string) (DraftRouting, error)
	ListDraftRoutings(context.Context, string, DraftCandidatePage) ([]DraftRouting, error)
	CreateDraft(context.Context, DraftCreateCommand) (Draft, error)
	PatchDraft(context.Context, DraftPatchCommand) (Draft, error)
	DeleteDraft(context.Context, DraftDeleteCommand) error
}

func (service *DraftService) ReadDraft(ctx context.Context, request DraftReadRequest) (Draft, error) {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.drafts) ||
		nilRevisionServiceDependency(service.current) || !validDraftID(request.DraftID) {
		return Draft{}, ErrInvalidDraftCommand
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return Draft{}, fmt.Errorf("%w: actor", ErrInvalidDraftCommand)
	}
	routing, err := service.drafts.GetDraftRouting(ctx, request.DraftID, actor.UserID)
	if err != nil {
		return Draft{}, err
	}
	if _, err := service.authorizeDraftRouting(ctx, actor, routing, recordauth.CapabilityDraftRead); err != nil {
		return Draft{}, err
	}
	draft, err := service.drafts.GetDraft(ctx, routing.DraftID, actor.UserID)
	if err != nil {
		return Draft{}, err
	}
	if draft.Validate() != nil || !sameDraftRouting(routing, DraftRoutingFromDraft(draft)) {
		return Draft{}, ErrDraftNotFound
	}
	return draft, nil
}

// draftCandidateBatch reads ahead of the requested page because authorization and
// hydration drop candidates. Without the read-ahead a page would come back short
// whenever a draft was skipped, and the caller could not tell a short page from
// the last one.
const draftCandidateBatch = uint64(200)

// ListDrafts returns one page of the author's drafts plus, when more remain, the
// position to resume from.
func (service *DraftService) ListDrafts(ctx context.Context, request DraftListRequest) (DraftListResult, error) {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.drafts) ||
		nilRevisionServiceDependency(service.current) || request.Limit == 0 || request.Limit > 100 {
		return DraftListResult{}, ErrInvalidDraftCommand
	}
	if request.After != nil {
		if err := request.After.Validate(); err != nil {
			return DraftListResult{}, err
		}
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return DraftListResult{}, fmt.Errorf("%w: actor", ErrInvalidDraftCommand)
	}

	// One extra visible draft is what distinguishes "the page is full" from "there
	// is another page", so it is collected and then withheld.
	visible := make([]Draft, 0, request.Limit+1)
	after := cloneDraftCursor(request.After)
	batch := maxUint64(draftCandidateBatch, request.Limit+1)
	for uint64(len(visible)) <= request.Limit {
		routings, err := service.drafts.ListDraftRoutings(ctx, actor.UserID, DraftCandidatePage{
			After: cloneDraftCursor(after), Limit: batch,
		})
		if err != nil {
			return DraftListResult{}, err
		}
		if len(routings) == 0 {
			break
		}
		for _, routing := range routings {
			draft, ok, err := service.listedDraft(ctx, actor, routing)
			if err != nil {
				return DraftListResult{}, err
			}
			if !ok {
				continue
			}
			visible = append(visible, draft)
			if uint64(len(visible)) > request.Limit {
				break
			}
		}
		last := routings[len(routings)-1]
		after = &DraftCursor{UpdatedAt: last.UpdatedAt.UTC(), DraftID: last.DraftID}
		if uint64(len(routings)) < batch {
			break
		}
	}

	result := DraftListResult{}
	if uint64(len(visible)) > request.Limit {
		visible = visible[:request.Limit]
		last := visible[len(visible)-1]
		result.NextCursor = &DraftCursor{UpdatedAt: last.UpdatedAt.UTC(), DraftID: last.DraftID}
	}
	result.Drafts = visible
	return result, nil
}

// listedDraft reports whether one candidate survives authorization and
// hydration. A draft the actor cannot reach, or one that changed under the
// listing, is absent rather than an error: the page describes what is currently
// visible.
func (service *DraftService) listedDraft(
	ctx context.Context,
	actor recordauth.ActorScope,
	routing DraftRouting,
) (Draft, bool, error) {
	if _, err := service.authorizeDraftRouting(ctx, actor, routing, recordauth.CapabilityDraftRead); err != nil {
		if errors.Is(err, recordauth.ErrDenied) || errors.Is(err, ErrRecordNotFound) ||
			errors.Is(err, ErrDraftNotFound) || errors.Is(err, ErrRecordDeletionReserved) {
			return Draft{}, false, nil
		}
		return Draft{}, false, err
	}
	draft, err := service.drafts.GetDraft(ctx, routing.DraftID, actor.UserID)
	if err != nil {
		if errors.Is(err, ErrDraftNotFound) || errors.Is(err, ErrRecordDeletionReserved) {
			return Draft{}, false, nil
		}
		return Draft{}, false, err
	}
	if draft.Validate() != nil || !sameDraftRouting(routing, DraftRoutingFromDraft(draft)) {
		return Draft{}, false, nil
	}
	return draft, true, nil
}

func cloneDraftCursor(cursor *DraftCursor) *DraftCursor {
	if cursor == nil {
		return nil
	}
	clone := *cursor
	clone.UpdatedAt = clone.UpdatedAt.UTC()
	return &clone
}

func maxUint64(left, right uint64) uint64 {
	if left > right {
		return left
	}
	return right
}

type DraftService struct {
	drafts  DraftRepository
	current CurrentRecordAuthorizationSource
}

func NewDraftService(drafts DraftRepository, current CurrentRecordAuthorizationSource) (*DraftService, error) {
	if nilRevisionServiceDependency(drafts) || nilRevisionServiceDependency(current) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidDraftCommand)
	}
	return &DraftService{drafts: drafts, current: current}, nil
}

func (service *DraftService) CreateDraft(ctx context.Context, request DraftCreateRequest) (Draft, error) {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.drafts) ||
		nilRevisionServiceDependency(service.current) || !validDraftID(request.DraftID) ||
		len(request.Payload.json) == 0 || request.Payload.hash == [sha256.Size]byte{} {
		return Draft{}, ErrInvalidDraftCommand
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return Draft{}, fmt.Errorf("%w: actor", ErrInvalidDraftCommand)
	}
	if request.RecordID == "" && request.BaseRevisionID == "" {
		if actor.Role != recordauth.RoleProjectAdmin {
			return Draft{}, recordauth.ErrDenied
		}
	} else {
		if !validRecordRootID(request.RecordID) || !validRevisionID(request.BaseRevisionID) {
			return Draft{}, ErrInvalidDraftCommand
		}
		current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), request.RecordID)
		if err != nil {
			return Draft{}, fmt.Errorf("resolve current record authorization: %w", err)
		}
		if current.RecordID != request.RecordID {
			return Draft{}, ErrRecordNotFound
		}
		if err := AuthorizeRecordResource(actor, recordauth.CapabilityDraftCreate, current.Evidence); err != nil {
			return Draft{}, err
		}
		if current.CurrentRevisionID != request.BaseRevisionID || current.Lifecycle != LifecycleActive {
			return Draft{}, &DraftRevisionConflictError{
				ServerRevisionID:         current.CurrentRevisionID,
				ServerLockVersion:        current.LockVersion,
				ServerAuthorizationEpoch: current.AuthorizationEpoch,
			}
		}
	}

	command := DraftCreateCommand{
		DraftID:        request.DraftID,
		ProjectID:      actor.ProjectID,
		RecordID:       request.RecordID,
		BaseRevisionID: request.BaseRevisionID,
		AuthorID:       actor.UserID,
		Payload:        request.Payload,
		Policy:         DefaultDraftRetentionPolicy(),
	}
	if err := command.Validate(); err != nil {
		return Draft{}, err
	}
	draft, err := service.drafts.CreateDraft(ctx, command)
	if err != nil {
		return Draft{}, err
	}
	if draft.Validate() != nil || draft.DraftID != command.DraftID || draft.ProjectID != command.ProjectID ||
		draft.RecordID != command.RecordID || draft.BaseRevisionID != command.BaseRevisionID ||
		draft.AuthorID != command.AuthorID || draft.Payload.Hash() != command.Payload.Hash() {
		return Draft{}, ErrInvalidDraftCommand
	}
	return draft, nil
}

func (service *DraftService) PatchDraft(ctx context.Context, request DraftPatchRequest) (Draft, error) {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.drafts) ||
		nilRevisionServiceDependency(service.current) || !validDraftID(request.DraftID) ||
		len(request.Payload.json) == 0 || request.Payload.hash == [sha256.Size]byte{} {
		return Draft{}, ErrInvalidDraftCommand
	}
	if _, err := request.IfMatch.Digest(); err != nil {
		return Draft{}, fmt.Errorf("%w: if match", ErrInvalidDraftCommand)
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return Draft{}, fmt.Errorf("%w: actor", ErrInvalidDraftCommand)
	}
	routing, err := service.drafts.GetDraftRouting(ctx, request.DraftID, actor.UserID)
	if err != nil {
		return Draft{}, err
	}
	if _, err := service.authorizeDraftRouting(ctx, actor, routing, recordauth.CapabilityDraftUpdate); err != nil {
		return Draft{}, err
	}
	draft, err := service.drafts.GetDraft(ctx, routing.DraftID, actor.UserID)
	if err != nil {
		return Draft{}, err
	}
	if draft.Validate() != nil || !sameDraftRouting(routing, DraftRoutingFromDraft(draft)) {
		return Draft{}, ErrDraftNotFound
	}

	updated, err := service.drafts.PatchDraft(ctx, DraftPatchCommand{
		DraftID:  draft.DraftID,
		AuthorID: actor.UserID,
		IfMatch:  request.IfMatch,
		Payload:  request.Payload,
		Policy:   DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		return Draft{}, err
	}
	if updated.Validate() != nil || updated.DraftID != draft.DraftID || updated.AuthorID != draft.AuthorID ||
		updated.ProjectID != draft.ProjectID || updated.RecordID != draft.RecordID || updated.BaseRevisionID != draft.BaseRevisionID {
		return Draft{}, ErrInvalidDraftCommand
	}
	return updated, nil
}

func (service *DraftService) DiscardDraft(ctx context.Context, request DraftDiscardRequest) error {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.drafts) ||
		nilRevisionServiceDependency(service.current) || !validDraftID(request.DraftID) {
		return ErrInvalidDraftCommand
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return fmt.Errorf("%w: actor", ErrInvalidDraftCommand)
	}
	routing, err := service.drafts.GetDraftRouting(ctx, request.DraftID, actor.UserID)
	if err != nil {
		return err
	}
	if _, err := service.authorizeDraftRouting(ctx, actor, routing, recordauth.CapabilityDraftDelete); err != nil {
		return err
	}
	return service.drafts.DeleteDraft(ctx, DraftDeleteCommand{
		DraftID:  routing.DraftID,
		AuthorID: actor.UserID,
		Reason:   DraftDeleteDiscarded,
	})
}

func (service *DraftService) PreparePublish(ctx context.Context, request DraftPublishRequest) (Draft, error) {
	if ctx == nil || service == nil || nilRevisionServiceDependency(service.drafts) ||
		nilRevisionServiceDependency(service.current) || !validDraftID(request.DraftID) {
		return Draft{}, ErrInvalidDraftCommand
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return Draft{}, fmt.Errorf("%w: actor", ErrInvalidDraftCommand)
	}
	routing, err := service.drafts.GetDraftRouting(ctx, request.DraftID, actor.UserID)
	if err != nil {
		return Draft{}, err
	}
	current, err := service.authorizeDraftRouting(ctx, actor, routing, recordauth.CapabilityDraftPublish)
	if err != nil {
		return Draft{}, err
	}
	draft, err := service.drafts.GetDraft(ctx, routing.DraftID, actor.UserID)
	if err != nil {
		return Draft{}, err
	}
	if draft.Validate() != nil || !sameDraftRouting(routing, DraftRoutingFromDraft(draft)) {
		return Draft{}, ErrDraftNotFound
	}
	if draft.RecordID == "" {
		return draft, nil
	}
	if current.CurrentRevisionID != draft.BaseRevisionID || current.Lifecycle != LifecycleActive {
		return Draft{}, &DraftRevisionConflictError{
			ServerRevisionID:         current.CurrentRevisionID,
			ServerLockVersion:        current.LockVersion,
			ServerAuthorizationEpoch: current.AuthorizationEpoch,
			Draft:                    draft,
		}
	}
	return draft, nil
}

func (service *DraftService) authorizeDraftRouting(
	ctx context.Context,
	actor recordauth.ActorScope,
	routing DraftRouting,
	capability recordauth.Capability,
) (CurrentRecordAuthorization, error) {
	if routing.Validate() != nil || routing.AuthorID != actor.UserID || routing.ProjectID != actor.ProjectID {
		return CurrentRecordAuthorization{}, ErrDraftNotFound
	}
	if routing.RecordID == "" {
		if actor.Role != recordauth.RoleProjectAdmin {
			return CurrentRecordAuthorization{}, recordauth.ErrDenied
		}
		return CurrentRecordAuthorization{}, nil
	}
	current, err := service.current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), routing.RecordID)
	if err != nil {
		return CurrentRecordAuthorization{}, fmt.Errorf("resolve current record authorization: %w", err)
	}
	if current.RecordID != routing.RecordID {
		return CurrentRecordAuthorization{}, ErrDraftNotFound
	}
	if err := AuthorizeRecordResource(actor, capability, current.Evidence); err != nil {
		return CurrentRecordAuthorization{}, err
	}
	return current, nil
}

func sameDraftRouting(left, right DraftRouting) bool {
	return left.DraftID == right.DraftID && left.ProjectID == right.ProjectID &&
		left.RecordID == right.RecordID && left.BaseRevisionID == right.BaseRevisionID &&
		left.AuthorID == right.AuthorID && left.UpdatedAt.Equal(right.UpdatedAt)
}

type DraftRetentionPolicy struct {
	DraftTTL         time.Duration
	WarningLead      time.Duration
	CheckpointBucket time.Duration
	CheckpointTTL    time.Duration
	CheckpointLimit  uint64
}

func DefaultDraftRetentionPolicy() DraftRetentionPolicy {
	return DraftRetentionPolicy{
		DraftTTL:         90 * 24 * time.Hour,
		WarningLead:      7 * 24 * time.Hour,
		CheckpointBucket: 5 * time.Minute,
		CheckpointTTL:    7 * 24 * time.Hour,
		CheckpointLimit:  20,
	}
}

func (policy DraftRetentionPolicy) Validate() error {
	if policy.DraftTTL.Microseconds() <= 0 || policy.WarningLead.Microseconds() <= 0 ||
		policy.WarningLead >= policy.DraftTTL || policy.CheckpointBucket.Microseconds() <= 0 ||
		policy.CheckpointTTL.Microseconds() <= 0 || policy.CheckpointLimit == 0 {
		return fmt.Errorf("%w: retention policy", ErrInvalidDraftCommand)
	}
	return nil
}

type DraftCreateCommand struct {
	DraftID        string
	ProjectID      recordauth.ProjectID
	RecordID       string
	BaseRevisionID string
	AuthorID       string
	Payload        DraftPayload
	Policy         DraftRetentionPolicy
}

func (command DraftCreateCommand) Validate() error {
	if !validDraftID(command.DraftID) || recordauth.ValidateProjectID(command.ProjectID) != nil ||
		recordauth.ValidateActorUserID(command.AuthorID) != nil || len(command.Payload.json) == 0 ||
		command.Payload.hash == [sha256.Size]byte{} || command.Policy.Validate() != nil {
		return ErrInvalidDraftCommand
	}
	newRecord := command.RecordID == "" && command.BaseRevisionID == ""
	existingRecord := validRecordRootID(command.RecordID) && validRevisionID(command.BaseRevisionID)
	if !newRecord && !existingRecord {
		return fmt.Errorf("%w: record and base revision", ErrInvalidDraftCommand)
	}
	return nil
}

type DraftPatchCommand struct {
	DraftID  string
	AuthorID string
	IfMatch  DraftETag
	Payload  DraftPayload
	Policy   DraftRetentionPolicy
}

func (command DraftPatchCommand) Validate() error {
	if !validDraftID(command.DraftID) || recordauth.ValidateActorUserID(command.AuthorID) != nil ||
		len(command.Payload.json) == 0 || command.Payload.hash == [sha256.Size]byte{} ||
		command.Policy.Validate() != nil {
		return ErrInvalidDraftCommand
	}
	if _, err := command.IfMatch.Digest(); err != nil {
		return fmt.Errorf("%w: if match", ErrInvalidDraftCommand)
	}
	return nil
}

type DraftDeleteReason string

const (
	DraftDeletePublished DraftDeleteReason = "published"
	DraftDeleteDiscarded DraftDeleteReason = "discarded"
	DraftDeleteRevoked   DraftDeleteReason = "revoked"
)

type DraftDeleteCommand struct {
	DraftID  string
	AuthorID string
	Reason   DraftDeleteReason
	IfMatch  DraftETag
}

func (command DraftDeleteCommand) Validate() error {
	if !validDraftID(command.DraftID) || recordauth.ValidateActorUserID(command.AuthorID) != nil {
		return ErrInvalidDraftCommand
	}
	switch command.Reason {
	case DraftDeletePublished:
		if _, err := command.IfMatch.Digest(); err != nil {
			return fmt.Errorf("%w: publish etag", ErrInvalidDraftCommand)
		}
	case DraftDeleteDiscarded, DraftDeleteRevoked:
		if command.IfMatch != (DraftETag{}) {
			return fmt.Errorf("%w: unexpected etag", ErrInvalidDraftCommand)
		}
	default:
		return fmt.Errorf("%w: delete reason", ErrInvalidDraftCommand)
	}
	return nil
}

// DraftPayload is an immutable canonical JSON object. Transport layers map
// allowlisted DTOs into this value before draft persistence.
type DraftPayload struct {
	json []byte
	hash [sha256.Size]byte
}

func NewDraftPayload(input []byte) (DraftPayload, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return DraftPayload{}, fmt.Errorf("%w: json", ErrInvalidDraftPayload)
	}
	if _, ok := decoded.(map[string]any); !ok {
		return DraftPayload{}, fmt.Errorf("%w: object", ErrInvalidDraftPayload)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return DraftPayload{}, fmt.Errorf("%w: trailing value", ErrInvalidDraftPayload)
	}

	canonical, err := json.Marshal(decoded)
	if err != nil {
		return DraftPayload{}, fmt.Errorf("%w: canonical json", ErrInvalidDraftPayload)
	}
	digestInput := make([]byte, 0, len(draftPayloadCanonicalDomainV1)+1+len(canonical))
	digestInput = append(digestInput, draftPayloadCanonicalDomainV1...)
	digestInput = append(digestInput, 0)
	digestInput = append(digestInput, canonical...)
	return DraftPayload{
		json: append([]byte(nil), canonical...),
		hash: sha256.Sum256(digestInput),
	}, nil
}

func (payload DraftPayload) JSON() []byte {
	return append([]byte(nil), payload.json...)
}

func (payload DraftPayload) Hash() [sha256.Size]byte {
	return payload.hash
}

type DraftETag struct {
	digest [sha256.Size]byte
	valid  bool
}

func NewDraftETag(draftID, authorID string, version uint64, payload DraftPayload) (DraftETag, error) {
	if !validDraftID(draftID) || recordauth.ValidateActorUserID(authorID) != nil || version == 0 || len(payload.json) == 0 || payload.hash == [sha256.Size]byte{} {
		return DraftETag{}, ErrInvalidDraftETag
	}
	encoder := revisionCanonicalEncoder{}
	encoder.string(draftETagCanonicalDomainV1)
	encoder.string(draftID)
	encoder.string(authorID)
	encoder.uint64(version)
	encoder.raw(payload.hash[:])
	return DraftETag{digest: sha256.Sum256(encoder.bytes), valid: true}, nil
}

func ParseDraftETag(input string) (DraftETag, error) {
	if len(input) != len(draftETagPrefix)+sha256.Size*2+1 || !strings.HasPrefix(input, draftETagPrefix) || input[len(input)-1] != '"' {
		return DraftETag{}, ErrInvalidDraftETag
	}
	encoded := input[len(draftETagPrefix) : len(input)-1]
	if strings.ToLower(encoded) != encoded {
		return DraftETag{}, ErrInvalidDraftETag
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) != sha256.Size {
		return DraftETag{}, ErrInvalidDraftETag
	}
	var digest [sha256.Size]byte
	copy(digest[:], decoded)
	return DraftETag{digest: digest, valid: true}, nil
}

func (etag DraftETag) String() string {
	if !etag.valid {
		return ""
	}
	return draftETagPrefix + hex.EncodeToString(etag.digest[:]) + `"`
}

func (etag DraftETag) Digest() ([sha256.Size]byte, error) {
	if !etag.valid || etag.digest == [sha256.Size]byte{} {
		return [sha256.Size]byte{}, ErrInvalidDraftETag
	}
	return etag.digest, nil
}

func validDraftID(value string) bool {
	return validPrefixedDraftID(value, "rdf_")
}

func ValidateDraftID(value string) error {
	if !validDraftID(value) {
		return fmt.Errorf("%w: draft id", ErrInvalidDraftCommand)
	}
	return nil
}

func validDraftCheckpointID(value string) bool {
	return validPrefixedDraftID(value, "rdc_")
}

func validPrefixedDraftID(value, prefix string) bool {
	if len(value) < len(prefix)+1 || len(value) > len(prefix)+64 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}
