package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type recordCommentHandlerApplication interface {
	CreateComment(context.Context, recordcollaboration.CommentCreateApplicationRequest) (recordcollaboration.CommentMutationResult, error)
	EditComment(context.Context, recordcollaboration.CommentEditApplicationRequest) (recordcollaboration.CommentMutationResult, error)
	RedactComment(context.Context, recordcollaboration.CommentRedactApplicationRequest) (recordcollaboration.CommentMutationResult, error)
	ListComments(context.Context, recordcollaboration.CommentListApplicationRequest) ([]recordcollaboration.CommentRecord, error)
}

type recordCommentInput struct {
	BodyMarkdown     string   `json:"body_markdown"`
	ReplyToCommentID string   `json:"reply_to_comment_id"`
	MentionUserIDs   []string `json:"mention_user_ids"`
}

type recordCommentMutationResponse struct {
	CommentID string                                  `json:"comment_id"`
	RecordID  string                                  `json:"record_id"`
	Version   uint64                                  `json:"version"`
	State     recordcollaboration.CommentState        `json:"state"`
	EventKind recordcollaboration.CommentMutationKind `json:"event_kind"`
	Replayed  bool                                    `json:"replayed"`
	ChangedAt time.Time                               `json:"changed_at"`
}

type recordCommentReadResponse struct {
	CommentID        string                                  `json:"comment_id"`
	RecordID         string                                  `json:"record_id"`
	AuthorID         string                                  `json:"author_id"`
	Version          uint64                                  `json:"version"`
	State            recordcollaboration.CommentState        `json:"state"`
	BodyMarkdown     *string                                 `json:"body_markdown"`
	RenderModel      *recordcollaboration.CommentRenderModel `json:"render_model"`
	ReplyToCommentID string                                  `json:"reply_to_comment_id"`
	MentionUserIDs   []string                                `json:"mention_user_ids"`
	CreatedAt        time.Time                               `json:"created_at"`
	UpdatedAt        time.Time                               `json:"updated_at"`
	RedactedAt       *time.Time                              `json:"redacted_at"`
}

func RecordComments(application recordCommentHandlerApplication) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		route, ok := recordCommentRouteFromPath(request.URL.Path)
		if !ok {
			writeRecordNotFound(w)
			return
		}
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		if application == nil {
			writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
			return
		}
		handleRecordCommentRoute(w, request, actor, application, route)
	})
}

type recordCommentRoute struct {
	recordID  string
	commentID string
	redact    bool
}

func recordCommentRouteFromPath(path string) (recordCommentRoute, bool) {
	const prefix = "/api/records/"
	if !strings.HasPrefix(path, prefix) {
		return recordCommentRoute{}, false
	}
	segments := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(segments) < 2 || !validRecordTransportID(segments[0]) || segments[1] != "comments" {
		return recordCommentRoute{}, false
	}
	route := recordCommentRoute{recordID: segments[0]}
	if len(segments) == 2 {
		return route, true
	}
	if len(segments) < 3 || recordcollaboration.ValidateCommentID(segments[2]) != nil {
		return recordCommentRoute{}, false
	}
	route.commentID = segments[2]
	if len(segments) == 3 {
		return route, true
	}
	if len(segments) == 4 && segments[3] == "redact" {
		route.redact = true
		return route, true
	}
	return recordCommentRoute{}, false
}

func handleRecordCommentRoute(w http.ResponseWriter, request *http.Request, actor recordauth.ActorScope, application recordCommentHandlerApplication, route recordCommentRoute) {
	switch {
	case route.commentID == "" && request.Method == http.MethodGet:
		limit, ok := recordCommentLimit(request)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid comment list request", nil)
			return
		}
		comments, err := application.ListComments(request.Context(), recordcollaboration.CommentListApplicationRequest{
			Actor: actor, RecordID: route.recordID, Limit: limit,
		})
		if err != nil {
			writeRecordCommentApplicationError(w, err)
			return
		}
		writeRecordCommentList(w, route.recordID, comments)
	case route.commentID == "" && request.Method == http.MethodPost:
		key, ok := recordActionIdempotencyKey(request, recordplatform.OperationKindRecordCommentCreate)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid Idempotency-Key", nil)
			return
		}
		var input recordCommentInput
		if !decodeRecordCommentRequestJSON(w, request, &input) {
			return
		}
		result, err := application.CreateComment(request.Context(), recordcollaboration.CommentCreateApplicationRequest{
			Actor: actor, RecordID: route.recordID, BodyMarkdown: input.BodyMarkdown,
			ReplyToCommentID: input.ReplyToCommentID, MentionUserIDs: append([]string(nil), input.MentionUserIDs...),
			IdempotencyKey: key,
		})
		if err != nil {
			writeRecordCommentApplicationError(w, err)
			return
		}
		status := http.StatusCreated
		if result.Replayed {
			status = http.StatusOK
		}
		writeRecordCommentResult(w, status, result)
	case route.commentID != "" && !route.redact && request.Method == http.MethodPatch:
		key, keyOK := recordActionIdempotencyKey(request, recordplatform.OperationKindRecordCommentEdit)
		version, versionOK := recordCommentIfMatch(request)
		if !keyOK || !versionOK {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid comment command headers", nil)
			return
		}
		var input recordCommentInput
		if !decodeRecordCommentRequestJSON(w, request, &input) {
			return
		}
		if input.ReplyToCommentID != "" {
			writeRecordError(w, http.StatusUnprocessableEntity, "comment_invalid", "comment content is invalid", nil)
			return
		}
		result, err := application.EditComment(request.Context(), recordcollaboration.CommentEditApplicationRequest{
			Actor: actor, RecordID: route.recordID, CommentID: route.commentID, ExpectedVersion: version,
			BodyMarkdown: input.BodyMarkdown, MentionUserIDs: append([]string(nil), input.MentionUserIDs...), IdempotencyKey: key,
		})
		if err != nil {
			writeRecordCommentApplicationError(w, err)
			return
		}
		writeRecordCommentResult(w, http.StatusOK, result)
	case route.commentID != "" && route.redact && request.Method == http.MethodPost:
		key, keyOK := recordActionIdempotencyKey(request, recordplatform.OperationKindRecordCommentRedact)
		version, versionOK := recordCommentIfMatch(request)
		if !keyOK || !versionOK {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid comment command headers", nil)
			return
		}
		var input struct{}
		if !decodeRecordCommentRequestJSON(w, request, &input) {
			return
		}
		result, err := application.RedactComment(request.Context(), recordcollaboration.CommentRedactApplicationRequest{
			Actor: actor, RecordID: route.recordID, CommentID: route.commentID, ExpectedVersion: version, IdempotencyKey: key,
		})
		if err != nil {
			writeRecordCommentApplicationError(w, err)
			return
		}
		writeRecordCommentResult(w, http.StatusOK, result)
	default:
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	}
}

func recordCommentLimit(request *http.Request) (uint64, bool) {
	query := request.URL.Query()
	for key := range query {
		if key != "limit" {
			return 0, false
		}
	}
	values, ok := query["limit"]
	if !ok {
		return 100, true
	}
	if len(values) != 1 || values[0] == "" || (len(values[0]) > 1 && values[0][0] == '0') {
		return 0, false
	}
	limit, err := strconv.ParseUint(values[0], 10, 64)
	return limit, err == nil && limit > 0 && limit <= 200
}

func recordCommentIfMatch(request *http.Request) (uint64, bool) {
	version, ok := recordActionIfMatch(request)
	return version, ok && recordcollaboration.IsIncrementableCommentVersion(version)
}

func decodeRecordCommentRequestJSON(w http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(w, request.Body, DefaultJSONBodyLimit)
	raw, err := io.ReadAll(request.Body)
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeRecordError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large", nil)
		return false
	}
	if err != nil {
		writeRecordError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request", nil)
		return false
	}
	if !utf8.Valid(raw) {
		writeRecordCommentApplicationError(w, recordcollaboration.ErrInvalidCommentMarkdown)
		return false
	}
	if !json.Valid(raw) {
		writeRecordError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request", nil)
		return false
	}
	if hasInvalidCommentJSONUnicode(raw) {
		writeRecordCommentApplicationError(w, recordcollaboration.ErrInvalidCommentMarkdown)
		return false
	}
	if err := decodeJSONValue(bytes.NewReader(raw), destination); err != nil {
		writeRecordError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request", nil)
		return false
	}
	return true
}

func hasInvalidCommentJSONUnicode(raw []byte) bool {
	for index := 0; index < len(raw); index++ {
		if raw[index] != '"' {
			continue
		}
		for index++; index < len(raw) && raw[index] != '"'; index++ {
			if raw[index] != '\\' {
				continue
			}
			if index+1 >= len(raw) || raw[index+1] != 'u' {
				index++
				continue
			}
			value, ok := commentJSONHex16(raw, index+2)
			if !ok {
				return true
			}
			index += 5
			switch {
			case value >= 0xd800 && value <= 0xdbff:
				if index+6 >= len(raw) || raw[index+1] != '\\' || raw[index+2] != 'u' {
					return true
				}
				low, lowOK := commentJSONHex16(raw, index+3)
				if !lowOK || low < 0xdc00 || low > 0xdfff {
					return true
				}
				index += 6
			case value >= 0xdc00 && value <= 0xdfff:
				return true
			}
		}
	}
	return false
}

func commentJSONHex16(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, digit := range raw[start : start+4] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func writeRecordCommentResult(w http.ResponseWriter, status int, result recordcollaboration.CommentMutationResult) {
	if result.Validate() != nil {
		writeRecordInternalError(w)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, result.Version))
	writeJSON(w, status, recordCommentMutationResponse{
		CommentID: result.CommentID, RecordID: result.RecordID, Version: result.Version, State: result.State,
		EventKind: result.EventKind, Replayed: result.Replayed, ChangedAt: result.ChangedAt.UTC(),
	})
}

func writeRecordCommentList(w http.ResponseWriter, recordID string, comments []recordcollaboration.CommentRecord) {
	response := make([]recordCommentReadResponse, len(comments))
	for index := range comments {
		comment := comments[index].Clone()
		if comment.Validate() != nil || comment.RecordID != recordID {
			writeRecordInternalError(w)
			return
		}
		response[index] = recordCommentReadResponse{
			CommentID: comment.CommentID, RecordID: comment.RecordID, AuthorID: comment.AuthorID, Version: comment.Version,
			State: comment.State, ReplyToCommentID: comment.ReplyToCommentID,
			MentionUserIDs: append(make([]string, 0, len(comment.MentionUserIDs)), comment.MentionUserIDs...), CreatedAt: comment.CreatedAt.UTC(),
			UpdatedAt: comment.UpdatedAt.UTC(), RedactedAt: comment.RedactedAt,
		}
		if comment.State == recordcollaboration.CommentStateActive {
			body := comment.BodyMarkdown
			model := comment.RenderModel.Clone()
			response[index].BodyMarkdown = &body
			response[index].RenderModel = &model
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Comments []recordCommentReadResponse `json:"comments"`
	}{Comments: response})
}

func writeRecordCommentApplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordauth.ErrDenied), errors.Is(err, records.ErrRecordNotFound),
		errors.Is(err, records.ErrRecordDeletionReserved), errors.Is(err, recordcollaboration.ErrCommentNotFound),
		errors.Is(err, recordcollaboration.ErrCommentPolicyDenied), errors.Is(err, store.ErrRecordSubjectNotFound):
		writeRecordNotFound(w)
	case errors.Is(err, recordcollaboration.ErrCommentConflict):
		writeRecordError(w, http.StatusConflict, "comment_conflict", "comment changed", nil)
	case errors.Is(err, recordplatform.ErrIdempotencyKeyReused), errors.Is(err, recordplatform.ErrIdempotencyConflictState):
		writeRecordError(w, http.StatusConflict, "idempotency_key_reused", "idempotency key was reused", nil)
	case errors.Is(err, recordplatform.ErrIdempotencyInProgress):
		writeRecordError(w, http.StatusConflict, "comment_operation_in_progress", "comment operation is in progress", nil)
	case errors.Is(err, recordcollaboration.ErrInvalidCommentMarkdown):
		writeRecordError(w, http.StatusUnprocessableEntity, "invalid_comment_markdown", "comment markdown is invalid", nil)
	case errors.Is(err, recordcollaboration.ErrInvalidCommentContent), errors.Is(err, recordcollaboration.ErrMembershipDenied):
		writeRecordError(w, http.StatusUnprocessableEntity, "comment_invalid", "comment content is invalid", nil)
	case errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable), errors.Is(err, store.ErrRecordSubjectUnavailable),
		errors.Is(err, recordcollaboration.ErrMembershipUnavailable), errors.Is(err, recordplatform.ErrDeletionReservationUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
	default:
		writeRecordInternalError(w)
	}
}
