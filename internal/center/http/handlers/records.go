package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

const (
	recordCursorVersion       = uint64(1)
	defaultRecordListLimit    = uint64(50)
	defaultRevisionListLimit  = uint64(50)
	initialVisibilityRevision = uint64(1)
	recordPrivateCacheControl = "private, no-store"
)

type recordHandlerApplication interface {
	GetRecord(context.Context, records.RecordGetRequest) (records.Record, error)
	ListRecords(context.Context, records.RecordListRequest) (records.RecordListResult, error)
	GetRevision(context.Context, records.RecordRevisionGetRequest) (records.RecordRevision, error)
	ListRevisions(context.Context, records.RecordRevisionListRequest) ([]records.RecordRevision, error)
	CreateRecord(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error)
	CreateRevision(context.Context, records.RecordRevisionCreateRequest) (records.RevisionCommitResult, error)
	RestoreRevision(context.Context, records.RecordRestoreRequest) (records.RevisionCommitResult, error)
	ChangeLifecycle(context.Context, records.RecordLifecycleChangeRequest) (records.RecordLifecycleResult, error)
	PreparePublish(context.Context, records.DraftPublishRequest) (records.Draft, error)
}

type RecordHandlerOptions struct {
	NewRecordID func() (string, error)
}

func Records(application recordHandlerApplication) http.Handler {
	return RecordsWithOptions(application, RecordHandlerOptions{})
}

func RecordsWithOptions(application recordHandlerApplication, options RecordHandlerOptions) http.Handler {
	if options.NewRecordID == nil {
		options.NewRecordID = func() (string, error) { return ids.New("rec") }
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		if application == nil {
			writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
			return
		}

		if request.URL.Path == "/api/records" {
			handleRecordsCollection(w, request, actor, application, options)
			return
		}
		handleRecordSubtree(w, request, actor, application)
	})
}

func handleRecordsCollection(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordHandlerApplication,
	options RecordHandlerOptions,
) {
	switch request.Method {
	case http.MethodGet:
		listRequest, err := recordListRequestFromHTTP(request, actor)
		if err != nil {
			writeRecordError(w, http.StatusBadRequest, "cursor_invalid", "invalid record cursor or query", nil)
			return
		}
		result, err := application.ListRecords(request.Context(), listRequest)
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		response := recordListResponse{Items: make([]recordResponse, 0, len(result.Records))}
		for _, record := range result.Records {
			response.Items = append(response.Items, newRecordResponse(record))
		}
		if result.NextCursor != nil {
			encoded, err := encodeRecordCursor(*result.NextCursor)
			if err != nil {
				writeRecordError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
				return
			}
			response.NextCursor = encoded
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPost:
		idempotencyKey, ok := recordIdempotencyKey(request)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required", nil)
			return
		}
		var input recordPublishRequest
		if !decodeRecordsRequestJSON(w, request, &input) {
			return
		}
		draft, etag, values, references, ok := prepareRecordDraftForPublish(w, request, actor, application, input)
		if !ok {
			return
		}
		if draft.RecordID != "" || draft.BaseRevisionID != "" {
			writeRecordsApplicationError(w, records.ErrDraftRevisionConflict)
			return
		}
		recordID, err := options.NewRecordID()
		if err != nil || !validRecordTransportID(recordID) {
			writeRecordError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		result, err := application.CreateRecord(request.Context(), records.RecordCreateRequest{
			Actor:             actor,
			RecordID:          recordID,
			DraftID:           draft.DraftID,
			DraftETag:         etag,
			Values:            values,
			SubjectReferences: references,
			IdempotencyKey:    idempotencyKey,
		})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		status := http.StatusOK
		if result.Created {
			status = http.StatusCreated
		}
		writeJSON(w, status, newRecordMutationResponse(result))
	default:
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

func handleRecordSubtree(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordHandlerApplication,
) {
	segments, ok := recordPathSegments(request.URL.Path)
	if !ok {
		writeRecordNotFound(w)
		return
	}
	recordID := segments[0]
	switch {
	case len(segments) == 1:
		if request.Method != http.MethodGet {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		record, err := application.GetRecord(request.Context(), records.RecordGetRequest{Actor: actor, RecordID: recordID})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, newRecordResponse(record))
	case len(segments) == 2 && segments[1] == "revisions":
		handleRecordRevisions(w, request, actor, application, recordID)
	case len(segments) == 3 && segments[1] == "revisions" && validRevisionTransportID(segments[2]):
		if request.Method != http.MethodGet {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		revision, err := application.GetRevision(request.Context(), records.RecordRevisionGetRequest{
			Actor: actor, RecordID: recordID, RevisionID: segments[2],
		})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, newRecordRevisionResponse(revision))
	case len(segments) == 4 && segments[1] == "revisions" && validRevisionTransportID(segments[2]) && segments[3] == "restore":
		handleRecordRevisionRestore(w, request, actor, application, recordID, segments[2])
	case len(segments) == 2 && (segments[1] == "archive" || segments[1] == "restore"):
		handleRecordLifecycle(w, request, actor, application, recordID, segments[1])
	default:
		writeRecordNotFound(w)
	}
}

func handleRecordRevisions(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordHandlerApplication,
	recordID string,
) {
	switch request.Method {
	case http.MethodGet:
		limit, ok := boundedUintQuery(request, "limit", defaultRevisionListLimit, 200)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid revision limit", nil)
			return
		}
		revisions, err := application.ListRevisions(request.Context(), records.RecordRevisionListRequest{
			Actor: actor, RecordID: recordID, Limit: limit,
		})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		response := recordRevisionListResponse{Items: make([]recordRevisionResponse, 0, len(revisions))}
		for _, revision := range revisions {
			response.Items = append(response.Items, newRecordRevisionResponse(revision))
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPost:
		idempotencyKey, ok := recordIdempotencyKey(request)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required", nil)
			return
		}
		var input recordRevisionPublishRequest
		if !decodeRecordsRequestJSON(w, request, &input) {
			return
		}
		draft, etag, values, references, ok := prepareRecordDraftForPublish(w, request, actor, application, input.recordPublishRequest)
		if !ok {
			return
		}
		if draft.RecordID != recordID || draft.BaseRevisionID != input.BaseRevisionID || input.LockVersion == 0 ||
			input.AuthorizationEpoch == 0 {
			writeRecordsApplicationError(w, records.ErrDraftRevisionConflict)
			return
		}
		result, err := application.CreateRevision(request.Context(), records.RecordRevisionCreateRequest{
			Actor:              actor,
			RecordID:           recordID,
			BaseRevisionID:     input.BaseRevisionID,
			LockVersion:        input.LockVersion,
			AuthorizationEpoch: input.AuthorizationEpoch,
			DraftID:            draft.DraftID,
			DraftETag:          etag,
			Values:             values,
			SubjectReferences:  references,
			IdempotencyKey:     idempotencyKey,
		})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		status := http.StatusOK
		if result.Created {
			status = http.StatusCreated
		}
		writeJSON(w, status, newRecordMutationResponse(result))
	default:
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

func handleRecordRevisionRestore(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordHandlerApplication,
	recordID string,
	revisionID string,
) {
	if request.Method != http.MethodPost {
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	idempotencyKey, ok := recordIdempotencyKey(request)
	if !ok {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required", nil)
		return
	}
	var input recordRestoreRequest
	if !decodeRecordsRequestJSON(w, request, &input) {
		return
	}
	result, err := application.RestoreRevision(request.Context(), records.RecordRestoreRequest{
		Actor: actor, RecordID: recordID, RevisionID: revisionID,
		SaveReason: input.SaveReason, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeRecordsApplicationError(w, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, newRecordMutationResponse(result))
}

func handleRecordLifecycle(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordHandlerApplication,
	recordID string,
	action string,
) {
	if request.Method != http.MethodPost {
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	idempotencyKey, ok := recordIdempotencyKey(request)
	if !ok {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required", nil)
		return
	}
	target := records.LifecycleArchived
	if action == "restore" {
		target = records.LifecycleActive
	}
	result, err := application.ChangeLifecycle(request.Context(), records.RecordLifecycleChangeRequest{
		Actor: actor, RecordID: recordID, TargetLifecycle: target, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeRecordsApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, recordLifecycleResponse{
		RecordID: result.RecordID, CurrentRevisionID: result.CurrentRevisionID,
		LockVersion: result.LockVersion, AuthorizationEpoch: result.AuthorizationEpoch,
		Lifecycle: result.Lifecycle, Replayed: result.Replayed, ChangedAt: result.ChangedAt.UTC(),
	})
}

type recordPublishRequest struct {
	DraftID   string `json:"draft_id"`
	DraftETag string `json:"draft_etag"`
}

type recordRevisionPublishRequest struct {
	recordPublishRequest
	BaseRevisionID     string `json:"base_revision_id"`
	LockVersion        uint64 `json:"lock_version"`
	AuthorizationEpoch uint64 `json:"authorization_epoch"`
}

func (input *recordRevisionPublishRequest) UnmarshalJSON(data []byte) error {
	type revisionPublishAlias struct {
		DraftID            string `json:"draft_id"`
		DraftETag          string `json:"draft_etag"`
		BaseRevisionID     string `json:"base_revision_id"`
		LockVersion        uint64 `json:"lock_version"`
		AuthorizationEpoch uint64 `json:"authorization_epoch"`
	}
	var decoded revisionPublishAlias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	input.recordPublishRequest = recordPublishRequest{DraftID: decoded.DraftID, DraftETag: decoded.DraftETag}
	input.BaseRevisionID = decoded.BaseRevisionID
	input.LockVersion = decoded.LockVersion
	input.AuthorizationEpoch = decoded.AuthorizationEpoch
	return nil
}

type recordRestoreRequest struct {
	SaveReason string `json:"save_reason"`
}

type recordDraftPayloadInput struct {
	Title                  string                         `json:"title"`
	BodyMarkdown           string                         `json:"body_markdown"`
	MarkdownDialectVersion records.MarkdownDialectVersion `json:"markdown_dialect_version"`
	RecordType             records.RecordType             `json:"record_type"`
	BusinessStatus         records.BusinessStatus         `json:"business_status"`
	ImpactLevel            records.ImpactLevel            `json:"impact_level"`
	OccurredAt             *time.Time                     `json:"occurred_at,omitempty"`
	CompletedAt            *time.Time                     `json:"completed_at,omitempty"`
	Visibility             recordVisibilityInput          `json:"visibility"`
	Subjects               []recordSubjectReferenceInput  `json:"subjects"`
	Tags                   []string                       `json:"tags"`
	OwnerID                string                         `json:"owner_id"`
	ParticipantIDs         []string                       `json:"participant_ids"`
	AttachmentIDs          []string                       `json:"attachment_ids"`
	FollowUpAt             *time.Time                     `json:"follow_up_at,omitempty"`
	Template               *recordTemplateInput           `json:"template,omitempty"`
	SaveReason             string                         `json:"save_reason"`
}

type recordVisibilityInput struct {
	Kind            recordauth.VisibilityKind `json:"kind"`
	AllowedRoles    []recordauth.Role         `json:"allowed_roles"`
	AllowedGroupIDs []string                  `json:"allowed_group_ids"`
}

type recordSubjectReferenceInput struct {
	RegistryVersion uint64               `json:"registry_version"`
	Kind            records.SubjectKind  `json:"kind"`
	Role            records.RelationRole `json:"role"`
	SourceID        string               `json:"source_id"`
	Primary         bool                 `json:"primary"`
}

type recordTemplateInput struct {
	ID      string `json:"id"`
	Version uint64 `json:"version"`
}

func prepareRecordDraftForPublish(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordHandlerApplication,
	input recordPublishRequest,
) (records.Draft, records.DraftETag, records.CompleteRevisionValues, []records.SubjectReference, bool) {
	etag, err := records.ParseDraftETag(input.DraftETag)
	if err != nil || records.ValidateDraftID(input.DraftID) != nil {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid draft reference", nil)
		return records.Draft{}, records.DraftETag{}, records.CompleteRevisionValues{}, nil, false
	}
	draft, err := application.PreparePublish(request.Context(), records.DraftPublishRequest{Actor: actor, DraftID: input.DraftID})
	if err != nil {
		writeRecordsApplicationError(w, err)
		return records.Draft{}, records.DraftETag{}, records.CompleteRevisionValues{}, nil, false
	}
	if draft.DraftID != input.DraftID || draft.ETag != etag {
		writeRecordsApplicationError(w, records.ErrDraftConflict)
		return records.Draft{}, records.DraftETag{}, records.CompleteRevisionValues{}, nil, false
	}
	payload, err := decodeRecordDraftPayload(draft.Payload.JSON())
	if err != nil {
		writeRecordInternalError(w)
		return records.Draft{}, records.DraftETag{}, records.CompleteRevisionValues{}, nil, false
	}
	values, references, err := payload.toDomain(actor)
	if err != nil {
		writeRecordsApplicationError(w, err)
		return records.Draft{}, records.DraftETag{}, records.CompleteRevisionValues{}, nil, false
	}
	return draft, etag, values, references, true
}

func decodeRecordDraftPayload(raw []byte) (recordDraftPayloadInput, error) {
	draftPayload, err := records.NewDraftPayload(raw)
	if err != nil {
		return recordDraftPayloadInput{}, err
	}
	var input recordDraftPayloadInput
	if err := decodeJSONValue(bytes.NewReader(draftPayload.JSON()), &input); err != nil {
		return recordDraftPayloadInput{}, fmt.Errorf("decode record draft payload: %w", err)
	}
	if input.Visibility.AllowedRoles == nil || input.Visibility.AllowedGroupIDs == nil ||
		input.Subjects == nil || input.Tags == nil || input.ParticipantIDs == nil || input.AttachmentIDs == nil {
		return recordDraftPayloadInput{}, errors.New("record draft payload arrays must not be null")
	}
	return input, nil
}

func (input recordDraftPayloadInput) toDomain(
	actor recordauth.ActorScope,
) (records.CompleteRevisionValues, []records.SubjectReference, error) {
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:         recordauth.VisibilityScopeVersionV1,
		Kind:            input.Visibility.Kind,
		ProjectID:       actor.ProjectID,
		AllowedRoles:    append([]recordauth.Role(nil), input.Visibility.AllowedRoles...),
		AllowedGroupIDs: append([]string(nil), input.Visibility.AllowedGroupIDs...),
		PolicyVersion:   recordauth.PolicyVersionV1,
		PolicyRevision:  initialVisibilityRevision,
	})
	if err != nil {
		return records.CompleteRevisionValues{}, nil, records.ErrInvalidRevisionInput
	}
	references := make([]records.SubjectReference, 0, len(input.Subjects))
	for _, subject := range input.Subjects {
		references = append(references, records.SubjectReference{
			RegistryVersion: subject.RegistryVersion,
			Kind:            subject.Kind,
			Role:            subject.Role,
			SourceID:        subject.SourceID,
			Primary:         subject.Primary,
		})
	}
	participants := make([]records.RevisionParticipantSnapshot, 0, len(input.ParticipantIDs))
	for _, participantID := range input.ParticipantIDs {
		participants = append(participants, records.RevisionParticipantSnapshot{
			ParticipantID: participantID,
			IdentitySnapshot: map[string]string{
				"display_name": participantID,
			},
		})
	}
	var template *records.TemplateProvenance
	if input.Template != nil {
		template = &records.TemplateProvenance{ID: input.Template.ID, Version: input.Template.Version}
	}
	return records.CompleteRevisionValues{
		Title:                  input.Title,
		BodyMarkdown:           input.BodyMarkdown,
		MarkdownDialectVersion: input.MarkdownDialectVersion,
		RecordType:             input.RecordType,
		BusinessStatus:         input.BusinessStatus,
		ImpactLevel:            input.ImpactLevel,
		OccurredAt:             input.OccurredAt,
		CompletedAt:            input.CompletedAt,
		VisibilityScope:        visibility,
		Tags:                   append([]string{}, input.Tags...),
		OwnerID:                input.OwnerID,
		Participants:           participants,
		AttachmentIDs:          append([]string{}, input.AttachmentIDs...),
		FollowUpAt:             input.FollowUpAt,
		Template:               template,
		SaveReason:             input.SaveReason,
	}, references, nil
}

type recordListResponse struct {
	Items      []recordResponse `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type recordRevisionListResponse struct {
	Items []recordRevisionResponse `json:"items"`
}

type recordResponse struct {
	RecordID           string                 `json:"record_id"`
	Lifecycle          records.Lifecycle      `json:"lifecycle"`
	CurrentRevisionID  string                 `json:"current_revision_id"`
	LockVersion        uint64                 `json:"lock_version"`
	AuthorizationEpoch uint64                 `json:"authorization_epoch"`
	Current            recordRevisionResponse `json:"current"`
	Capabilities       recordCapabilities     `json:"capabilities"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	ArchivedAt         *time.Time             `json:"archived_at,omitempty"`
}

type recordCapabilities struct {
	Read            bool `json:"read"`
	Update          bool `json:"update"`
	Archive         bool `json:"archive"`
	Restore         bool `json:"restore"`
	Draft           bool `json:"draft"`
	PermanentDelete bool `json:"permanent_delete"`
}

type recordRevisionResponse struct {
	RecordID        string                         `json:"record_id"`
	RevisionID      string                         `json:"revision_id"`
	BaseRevisionID  string                         `json:"base_revision_id,omitempty"`
	RevisionNo      uint64                         `json:"revision_no"`
	Title           string                         `json:"title"`
	BodyMarkdown    string                         `json:"body_markdown"`
	MarkdownDialect records.MarkdownDialectVersion `json:"markdown_dialect_version"`
	RecordType      records.RecordType             `json:"record_type"`
	BusinessStatus  records.BusinessStatus         `json:"business_status,omitempty"`
	StatusGroup     records.StatusGroup            `json:"status_group,omitempty"`
	ImpactLevel     records.ImpactLevel            `json:"impact_level"`
	OccurredAt      *time.Time                     `json:"occurred_at,omitempty"`
	CompletedAt     *time.Time                     `json:"completed_at,omitempty"`
	Visibility      recordVisibilityResponse       `json:"visibility"`
	Subjects        []recordSubjectResponse        `json:"subjects"`
	Tags            []string                       `json:"tags"`
	OwnerID         string                         `json:"owner_id,omitempty"`
	Participants    []recordParticipantResponse    `json:"participants"`
	AttachmentIDs   []string                       `json:"attachment_ids"`
	FollowUpAt      *time.Time                     `json:"follow_up_at,omitempty"`
	Template        *recordTemplateResponse        `json:"template,omitempty"`
	AuthorID        string                         `json:"author_id"`
	SaveReason      string                         `json:"save_reason"`
	CreatedAt       time.Time                      `json:"created_at"`
}

type recordVisibilityResponse struct {
	Kind            recordauth.VisibilityKind `json:"kind"`
	AllowedRoles    []recordauth.Role         `json:"allowed_roles"`
	AllowedGroupIDs []string                  `json:"allowed_group_ids"`
}

type recordSubjectResponse struct {
	RegistryVersion uint64                        `json:"registry_version"`
	Kind            records.SubjectKind           `json:"kind"`
	Role            records.RelationRole          `json:"role"`
	SourceID        string                        `json:"source_id"`
	Primary         bool                          `json:"primary"`
	Identity        recordSubjectIdentityResponse `json:"identity"`
}

type recordSubjectIdentityResponse struct {
	DisplayName string `json:"display_name"`
	Provider    string `json:"provider,omitempty"`
	Region      string `json:"region,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	Version     string `json:"version,omitempty"`
	TargetType  string `json:"target_type,omitempty"`
}

type recordParticipantResponse struct {
	ParticipantID string `json:"participant_id"`
	DisplayName   string `json:"display_name"`
}

type recordTemplateResponse struct {
	ID      string `json:"id"`
	Version uint64 `json:"version"`
}

type recordMutationResponse struct {
	RecordID           string            `json:"record_id"`
	RevisionID         string            `json:"revision_id"`
	RevisionNo         uint64            `json:"revision_no"`
	LockVersion        uint64            `json:"lock_version"`
	AuthorizationEpoch uint64            `json:"authorization_epoch"`
	Lifecycle          records.Lifecycle `json:"lifecycle"`
	Created            bool              `json:"created"`
	Replayed           bool              `json:"replayed"`
	CommittedAt        time.Time         `json:"committed_at"`
}

type recordLifecycleResponse struct {
	RecordID           string            `json:"record_id"`
	CurrentRevisionID  string            `json:"current_revision_id"`
	LockVersion        uint64            `json:"lock_version"`
	AuthorizationEpoch uint64            `json:"authorization_epoch"`
	Lifecycle          records.Lifecycle `json:"lifecycle"`
	Replayed           bool              `json:"replayed"`
	ChangedAt          time.Time         `json:"changed_at"`
}

func newRecordResponse(record records.Record) recordResponse {
	archivedAt := utcTimePointer(record.ArchivedAt)
	return recordResponse{
		RecordID:           record.RecordID,
		Lifecycle:          record.Lifecycle,
		CurrentRevisionID:  record.CurrentRevisionID,
		LockVersion:        record.LockVersion,
		AuthorizationEpoch: record.AuthorizationEpoch,
		Current:            newRecordRevisionResponse(record.Current),
		Capabilities: recordCapabilities{
			Read: record.Capabilities.Read, Update: record.Capabilities.Update,
			Archive: record.Capabilities.Archive, Restore: record.Capabilities.Restore,
			Draft: record.Capabilities.Draft, PermanentDelete: record.Capabilities.PermanentDelete,
		},
		CreatedAt: record.CreatedAt.UTC(), UpdatedAt: record.UpdatedAt.UTC(), ArchivedAt: archivedAt,
	}
}

func newRecordRevisionResponse(revision records.RecordRevision) recordRevisionResponse {
	input := revision.Input
	visibility := input.VisibilityScope()
	response := recordRevisionResponse{
		RecordID: revision.RecordID, RevisionID: revision.RevisionID,
		BaseRevisionID: revision.BaseRevisionID, RevisionNo: revision.RevisionNo,
		Title: input.Title(), BodyMarkdown: input.BodyMarkdown(), MarkdownDialect: input.MarkdownDialectVersion(),
		RecordType: input.RecordType(), BusinessStatus: input.BusinessStatus(), StatusGroup: input.StatusGroup(),
		ImpactLevel: input.ImpactLevel(), OccurredAt: utcTimePointer(input.OccurredAt()),
		CompletedAt: utcTimePointer(input.CompletedAt()),
		Visibility: recordVisibilityResponse{
			Kind: visibility.Kind, AllowedRoles: append([]recordauth.Role{}, visibility.AllowedRoles...),
			AllowedGroupIDs: append([]string{}, visibility.AllowedGroupIDs...),
		},
		Subjects: make([]recordSubjectResponse, 0), Tags: append([]string{}, input.Tags()...),
		OwnerID: input.OwnerID(), Participants: make([]recordParticipantResponse, 0),
		AttachmentIDs: append([]string{}, input.AttachmentIDs()...),
		FollowUpAt:    utcTimePointer(input.FollowUpAt()), AuthorID: input.AuthorID(),
		SaveReason: input.SaveReason(), CreatedAt: revision.CreatedAt.UTC(),
	}
	for _, subject := range input.Subjects() {
		identity := subject.IdentitySnapshot
		response.Subjects = append(response.Subjects, recordSubjectResponse{
			RegistryVersion: subject.RegistryVersion, Kind: subject.Kind, Role: subject.Role,
			SourceID: subject.SourceID, Primary: subject.Primary,
			Identity: recordSubjectIdentityResponse{
				DisplayName: identity["display_name"], Provider: identity["provider"],
				Region: identity["region"], Purpose: identity["purpose"],
				Version: identity["version"], TargetType: identity["target_type"],
			},
		})
	}
	for _, participant := range input.Participants() {
		response.Participants = append(response.Participants, recordParticipantResponse{
			ParticipantID: participant.ParticipantID,
			DisplayName:   participant.IdentitySnapshot["display_name"],
		})
	}
	if template := input.Template(); template != nil {
		response.Template = &recordTemplateResponse{ID: template.ID, Version: template.Version}
	}
	return response
}

func newRecordMutationResponse(result records.RevisionCommitResult) recordMutationResponse {
	return recordMutationResponse{
		RecordID: result.RecordID, RevisionID: result.RevisionID, RevisionNo: result.RevisionNo,
		LockVersion: result.LockVersion, AuthorizationEpoch: result.AuthorizationEpoch,
		Lifecycle: result.Lifecycle, Created: result.Created, Replayed: result.Replayed,
		CommittedAt: result.CommittedAt.UTC(),
	}
}

type recordErrorResponse struct {
	Code        string             `json:"code"`
	Message     string             `json:"message"`
	FieldErrors []recordFieldError `json:"field_errors"`
	Recovery    any                `json:"recovery,omitempty"`
}

type recordFieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func writeRecordError(w http.ResponseWriter, status int, code, message string, recovery any) {
	writeJSON(w, status, recordErrorResponse{
		Code: code, Message: message, FieldErrors: make([]recordFieldError, 0), Recovery: recovery,
	})
}

func writeRecordInternalError(w http.ResponseWriter) {
	writeRecordError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
}

func writeRecordNotFound(w http.ResponseWriter) {
	writeRecordError(w, http.StatusNotFound, "resource_not_found", "resource not found", nil)
}

func writeRecordsApplicationError(w http.ResponseWriter, err error) {
	var draftConflict *records.DraftConflictError
	var draftRevisionConflict *records.DraftRevisionConflictError
	switch {
	case errors.As(err, &draftConflict):
		serverDraft, serverErr := newRecordDraftResponse(draftConflict.Server)
		if serverErr != nil {
			writeRecordInternalError(w)
			return
		}
		if _, localErr := decodeRecordDraftPayload(draftConflict.LocalPayload.JSON()); localErr != nil {
			writeRecordInternalError(w)
			return
		}
		writeRecordError(w, http.StatusConflict, "draft_conflict", "draft changed", recordDraftConflictRecovery{
			ServerDraft:  serverDraft,
			LocalPayload: json.RawMessage(draftConflict.LocalPayload.JSON()),
		})
	case errors.As(err, &draftRevisionConflict):
		draft, draftErr := newRecordDraftResponse(draftRevisionConflict.Draft)
		if draftErr != nil {
			writeRecordInternalError(w)
			return
		}
		writeRecordError(w, http.StatusConflict, "record_revision_conflict", "record revision changed", recordRevisionConflictRecovery{
			ServerRevisionID:         draftRevisionConflict.ServerRevisionID,
			ServerLockVersion:        draftRevisionConflict.ServerLockVersion,
			ServerAuthorizationEpoch: draftRevisionConflict.ServerAuthorizationEpoch,
			Draft:                    draft,
		})
	case errors.Is(err, recordauth.ErrDenied), errors.Is(err, records.ErrRecordNotFound),
		errors.Is(err, records.ErrDraftNotFound), errors.Is(err, records.ErrRecordDeletionReserved),
		errors.Is(err, store.ErrRecordSubjectNotFound):
		writeRecordNotFound(w)
	case errors.Is(err, records.ErrRecordRevisionConflict), errors.Is(err, records.ErrDraftRevisionConflict):
		writeRecordError(w, http.StatusConflict, "record_revision_conflict", "record revision changed", nil)
	case errors.Is(err, records.ErrDraftConflict):
		writeRecordError(w, http.StatusConflict, "draft_conflict", "draft changed", nil)
	case errors.Is(err, records.ErrRecordAlreadyExists):
		writeRecordError(w, http.StatusConflict, "record_conflict", "record already exists", nil)
	case errors.Is(err, recordplatform.ErrIdempotencyKeyReused), errors.Is(err, recordplatform.ErrIdempotencyConflictState):
		writeRecordError(w, http.StatusConflict, "idempotency_key_reused", "idempotency key was reused", nil)
	case errors.Is(err, recordplatform.ErrIdempotencyInProgress):
		writeRecordError(w, http.StatusConflict, "record_operation_in_progress", "record operation is in progress", nil)
	case errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable), errors.Is(err, store.ErrRecordSubjectUnavailable),
		errors.Is(err, recordplatform.ErrDeletionReservationUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
	case recordSemanticValidationError(err):
		writeRecordError(w, http.StatusUnprocessableEntity, "record_invalid", "record content is invalid", nil)
	default:
		writeRecordError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}

func recordSemanticValidationError(err error) bool {
	return errors.Is(err, records.ErrInvalidRevisionInput) ||
		errors.Is(err, records.ErrInvalidSubjectReference) ||
		errors.Is(err, records.ErrInvalidLifecycle) ||
		errors.Is(err, records.ErrInvalidRecordType) ||
		errors.Is(err, records.ErrInvalidBusinessStatus) ||
		errors.Is(err, records.ErrStatusTransitionReasonRequired) ||
		errors.Is(err, records.ErrInvalidTemplate) ||
		errors.Is(err, records.ErrTemplateNotFound) ||
		errors.Is(err, records.ErrInvalidDraftPayload) ||
		errors.Is(err, records.ErrInvalidDraftCommand) ||
		errors.Is(err, records.ErrInvalidRecordReadRequest) ||
		errors.Is(err, records.ErrInvalidRevisionServiceRequest) ||
		errors.Is(err, records.ErrInvalidRevisionCommand) ||
		errors.Is(err, records.ErrInvalidRecordLifecycleRequest) ||
		errors.Is(err, records.ErrInvalidRecordLifecycleCommand) ||
		errors.Is(err, records.ErrInvalidApplicationRequest) ||
		errors.Is(err, recordplatform.ErrInvalidIdempotencyKey) ||
		errors.Is(err, recordplatform.ErrInvalidIdempotencyClaim)
}

type recordDraftConflictRecovery struct {
	ServerDraft  recordDraftResponse `json:"server_draft"`
	LocalPayload json.RawMessage     `json:"local_payload"`
}

type recordRevisionConflictRecovery struct {
	ServerRevisionID         string              `json:"server_revision_id"`
	ServerLockVersion        uint64              `json:"server_lock_version"`
	ServerAuthorizationEpoch uint64              `json:"server_authorization_epoch"`
	Draft                    recordDraftResponse `json:"draft"`
}

func decodeRecordsRequestJSON(w http.ResponseWriter, request *http.Request, destination any) bool {
	err := decodeJSONLimited(w, request, destination, DefaultJSONBodyLimit)
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeRecordError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large", nil)
		return false
	}
	if err != nil {
		writeRecordError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request", nil)
		return false
	}
	return true
}

func recordListRequestFromHTTP(request *http.Request, actor recordauth.ActorScope) (records.RecordListRequest, error) {
	limit, ok := boundedUintQuery(request, "limit", defaultRecordListLimit, 100)
	if !ok {
		return records.RecordListRequest{}, errors.New("invalid limit")
	}
	result := records.RecordListRequest{
		Actor: actor, Query: request.URL.Query().Get("q"),
		Lifecycle:  records.Lifecycle(request.URL.Query().Get("lifecycle")),
		RecordType: records.RecordType(request.URL.Query().Get("record_type")),
		Sort:       records.RecordSort(request.URL.Query().Get("sort")), Limit: limit,
	}
	if result.Sort == "" {
		result.Sort = records.RecordSortUpdatedDesc
	}
	if encoded := request.URL.Query().Get("cursor"); encoded != "" {
		cursor, err := decodeRecordCursor(encoded)
		if err != nil {
			return records.RecordListRequest{}, err
		}
		result.After = &cursor
	}
	return result, nil
}

type recordCursorEnvelope struct {
	Version   uint64    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	RecordID  string    `json:"record_id"`
}

func encodeRecordCursor(cursor records.RecordCursor) (string, error) {
	if cursor.UpdatedAt.IsZero() || !validRecordTransportID(cursor.RecordID) {
		return "", errors.New("invalid record cursor")
	}
	encoded, err := json.Marshal(recordCursorEnvelope{
		Version: recordCursorVersion, UpdatedAt: cursor.UpdatedAt.UTC(), RecordID: cursor.RecordID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeRecordCursor(value string) (records.RecordCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return records.RecordCursor{}, errors.New("invalid record cursor")
	}
	var envelope recordCursorEnvelope
	if err := decodeJSONValue(bytes.NewReader(raw), &envelope); err != nil ||
		envelope.Version != recordCursorVersion || envelope.UpdatedAt.IsZero() || !validRecordTransportID(envelope.RecordID) {
		return records.RecordCursor{}, errors.New("invalid record cursor")
	}
	return records.RecordCursor{UpdatedAt: envelope.UpdatedAt.UTC(), RecordID: envelope.RecordID}, nil
}

func boundedUintQuery(request *http.Request, name string, defaultValue, maximum uint64) (uint64, bool) {
	raw := request.URL.Query().Get(name)
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	return value, err == nil && value > 0 && value <= maximum
}

func recordIdempotencyKey(request *http.Request) (string, bool) {
	value := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	return value, value != "" && len(value) <= 200
}

func recordPathSegments(path string) ([]string, bool) {
	if !strings.HasPrefix(path, "/api/records/") {
		return nil, false
	}
	trimmed := strings.TrimPrefix(path, "/api/records/")
	if trimmed == "" {
		return nil, false
	}
	segments := strings.Split(trimmed, "/")
	if !validRecordTransportID(segments[0]) {
		return nil, false
	}
	return segments, true
}

func validRecordTransportID(value string) bool {
	return validPrefixedTransportID(value, "rec_")
}

func validRevisionTransportID(value string) bool {
	return validPrefixedTransportID(value, "rrv_")
}

func validPrefixedTransportID(value, prefix string) bool {
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

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
