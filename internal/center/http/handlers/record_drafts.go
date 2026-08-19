package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

const defaultRecordDraftListLimit = uint64(50)

type recordDraftHandlerApplication interface {
	ReadDraft(context.Context, records.DraftReadRequest) (records.Draft, error)
	ListDrafts(context.Context, records.DraftListRequest) (records.DraftListResult, error)
	CreateDraft(context.Context, records.DraftCreateRequest) (records.Draft, error)
	PatchDraft(context.Context, records.DraftPatchRequest) (records.Draft, error)
	DiscardDraft(context.Context, records.DraftDiscardRequest) error
}

type RecordDraftHandlerOptions struct {
	NewDraftID func() (string, error)
}

func RecordDrafts(application recordDraftHandlerApplication) http.Handler {
	return RecordDraftsWithOptions(application, RecordDraftHandlerOptions{})
}

func RecordDraftsWithOptions(
	application recordDraftHandlerApplication,
	options RecordDraftHandlerOptions,
) http.Handler {
	if options.NewDraftID == nil {
		options.NewDraftID = func() (string, error) { return ids.New("rdf") }
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

		if request.URL.Path == "/api/record-drafts" {
			handleRecordDraftCollection(w, request, actor, application, options)
			return
		}
		draftID, ok := recordDraftPathID(request.URL.Path)
		if !ok {
			writeRecordNotFound(w)
			return
		}
		handleRecordDraftItem(w, request, actor, application, draftID)
	})
}

func handleRecordDraftCollection(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordDraftHandlerApplication,
	options RecordDraftHandlerOptions,
) {
	switch request.Method {
	case http.MethodGet:
		limit, ok := boundedUintQuery(request, "limit", defaultRecordDraftListLimit, 100)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid draft list query", nil)
			return
		}
		listRequest := records.DraftListRequest{Actor: actor, Limit: limit}
		if encoded := request.URL.Query().Get("cursor"); encoded != "" {
			cursor, err := decodeRecordDraftCursor(encoded)
			if err != nil {
				writeRecordError(w, http.StatusBadRequest, "cursor_invalid", "invalid draft cursor", nil)
				return
			}
			listRequest.After = &cursor
		}
		result, err := application.ListDrafts(request.Context(), listRequest)
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		response := recordDraftListResponse{Items: make([]recordDraftResponse, 0, len(result.Drafts))}
		for _, draft := range result.Drafts {
			item, err := newRecordDraftResponse(draft)
			if err != nil {
				writeRecordInternalError(w)
				return
			}
			response.Items = append(response.Items, item)
		}
		if result.NextCursor != nil {
			encoded, err := encodeRecordDraftCursor(*result.NextCursor)
			if err != nil {
				writeRecordInternalError(w)
				return
			}
			response.NextCursor = encoded
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPost:
		var input recordDraftWriteRequest
		if !decodeRecordsRequestJSON(w, request, &input) {
			return
		}
		payload, ok := recordDraftPayloadFromTransport(w, input.Payload)
		if !ok {
			return
		}
		draftID, err := options.NewDraftID()
		if err != nil || records.ValidateDraftID(draftID) != nil {
			writeRecordError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			return
		}
		draft, err := application.CreateDraft(request.Context(), records.DraftCreateRequest{
			Actor:          actor,
			DraftID:        draftID,
			RecordID:       input.RecordID,
			BaseRevisionID: input.BaseRevisionID,
			Payload:        payload,
		})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		writeRecordDraft(w, http.StatusCreated, draft)
	default:
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

func handleRecordDraftItem(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordDraftHandlerApplication,
	draftID string,
) {
	switch request.Method {
	case http.MethodGet:
		draft, err := application.ReadDraft(request.Context(), records.DraftReadRequest{
			Actor: actor, DraftID: draftID,
		})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		writeRecordDraft(w, http.StatusOK, draft)
	case http.MethodPatch:
		etag, ok := recordDraftIfMatch(request)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "If-Match is required", nil)
			return
		}
		var input recordDraftWriteRequest
		if !decodeRecordsRequestJSON(w, request, &input) {
			return
		}
		if input.RecordID != "" || input.BaseRevisionID != "" {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "draft routing is immutable", nil)
			return
		}
		payload, ok := recordDraftPayloadFromTransport(w, input.Payload)
		if !ok {
			return
		}
		draft, err := application.PatchDraft(request.Context(), records.DraftPatchRequest{
			Actor: actor, DraftID: draftID, IfMatch: etag, Payload: payload,
		})
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		writeRecordDraft(w, http.StatusOK, draft)
	case http.MethodDelete:
		if err := application.DiscardDraft(request.Context(), records.DraftDiscardRequest{
			Actor: actor, DraftID: draftID,
		}); err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

type recordDraftWriteRequest struct {
	RecordID       string          `json:"record_id"`
	BaseRevisionID string          `json:"base_revision_id"`
	Payload        json.RawMessage `json:"payload"`
}

type recordDraftListResponse struct {
	Items      []recordDraftResponse `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

// recordDraftCursorEnvelope carries its own version and domain so a cursor minted
// for another list cannot be replayed here as a valid position.
type recordDraftCursorEnvelope struct {
	Version   uint64    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	DraftID   string    `json:"draft_id"`
}

const recordDraftCursorVersion = uint64(1)

func encodeRecordDraftCursor(cursor records.DraftCursor) (string, error) {
	if cursor.Validate() != nil {
		return "", errors.New("invalid draft cursor")
	}
	encoded, err := json.Marshal(recordDraftCursorEnvelope{
		Version: recordDraftCursorVersion, UpdatedAt: cursor.UpdatedAt.UTC(), DraftID: cursor.DraftID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeRecordDraftCursor(value string) (records.DraftCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return records.DraftCursor{}, errors.New("invalid draft cursor")
	}
	var envelope recordDraftCursorEnvelope
	if err := decodeJSONValue(bytes.NewReader(raw), &envelope); err != nil ||
		envelope.Version != recordDraftCursorVersion {
		return records.DraftCursor{}, errors.New("invalid draft cursor")
	}
	cursor := records.DraftCursor{UpdatedAt: envelope.UpdatedAt.UTC(), DraftID: envelope.DraftID}
	if cursor.Validate() != nil {
		return records.DraftCursor{}, errors.New("invalid draft cursor")
	}
	return cursor, nil
}

type recordDraftResponse struct {
	DraftID        string          `json:"draft_id"`
	RecordID       string          `json:"record_id,omitempty"`
	BaseRevisionID string          `json:"base_revision_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	Version        uint64          `json:"version"`
	ETag           string          `json:"etag"`
	WarningAt      time.Time       `json:"warning_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
}

func newRecordDraftResponse(draft records.Draft) (recordDraftResponse, error) {
	if _, err := decodeRecordDraftPayload(draft.Payload.JSON()); err != nil {
		return recordDraftResponse{}, fmt.Errorf("validate record draft response payload: %w", err)
	}
	return recordDraftResponse{
		DraftID:        draft.DraftID,
		RecordID:       draft.RecordID,
		BaseRevisionID: draft.BaseRevisionID,
		Payload:        json.RawMessage(draft.Payload.JSON()),
		Version:        draft.Version,
		ETag:           draft.ETag.String(),
		WarningAt:      draft.WarningAt.UTC(),
		CreatedAt:      draft.CreatedAt.UTC(),
		UpdatedAt:      draft.UpdatedAt.UTC(),
		ExpiresAt:      draft.ExpiresAt.UTC(),
	}, nil
}

func writeRecordDraft(w http.ResponseWriter, status int, draft records.Draft) {
	response, err := newRecordDraftResponse(draft)
	if err != nil {
		writeRecordInternalError(w)
		return
	}
	w.Header().Set("ETag", draft.ETag.String())
	writeJSON(w, status, response)
}

func recordDraftPayloadFromTransport(w http.ResponseWriter, raw json.RawMessage) (records.DraftPayload, bool) {
	payload, err := records.NewDraftPayload(raw)
	if err != nil {
		writeRecordError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request", nil)
		return records.DraftPayload{}, false
	}
	if _, err := decodeRecordDraftPayload(payload.JSON()); err != nil {
		writeRecordError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request", nil)
		return records.DraftPayload{}, false
	}
	return payload, true
}

func recordDraftIfMatch(request *http.Request) (records.DraftETag, bool) {
	values := request.Header.Values("If-Match")
	if len(values) != 1 || strings.TrimSpace(values[0]) != values[0] {
		return records.DraftETag{}, false
	}
	etag, err := records.ParseDraftETag(values[0])
	return etag, err == nil
}

func recordDraftPathID(path string) (string, bool) {
	const prefix = "/api/record-drafts/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	draftID := strings.TrimPrefix(path, prefix)
	if draftID == "" || strings.Contains(draftID, "/") || records.ValidateDraftID(draftID) != nil {
		return "", false
	}
	return draftID, true
}
