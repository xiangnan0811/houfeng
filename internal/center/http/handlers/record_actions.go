package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type recordActionHandlerApplication interface {
	CreateAction(context.Context, recordcollaboration.ActionCreateApplicationRequest) (recordcollaboration.ActionMutationResult, error)
	UpdateAction(context.Context, recordcollaboration.ActionUpdateApplicationRequest) (recordcollaboration.ActionMutationResult, error)
	CompleteAction(context.Context, recordcollaboration.ActionTransitionApplicationRequest) (recordcollaboration.ActionMutationResult, error)
	CancelAction(context.Context, recordcollaboration.ActionTransitionApplicationRequest) (recordcollaboration.ActionMutationResult, error)
	ReopenAction(context.Context, recordcollaboration.ActionTransitionApplicationRequest) (recordcollaboration.ActionMutationResult, error)
	ListActions(context.Context, recordcollaboration.ActionListApplicationRequest) ([]recordcollaboration.ActionRecord, error)
}

type recordActionInput struct {
	Title             string     `json:"title"`
	Details           string     `json:"details"`
	AssigneeID        string     `json:"assignee_id"`
	DueAt             *time.Time `json:"due_at"`
	SubjectRevisionID string     `json:"subject_revision_id"`
}

type recordActionMutationResponse struct {
	ActionID  string                                 `json:"action_id"`
	RecordID  string                                 `json:"record_id"`
	Version   uint64                                 `json:"version"`
	Status    recordcollaboration.ActionStatus       `json:"status"`
	EventKind recordcollaboration.ActionMutationKind `json:"event_kind"`
	Replayed  bool                                   `json:"replayed"`
	ChangedAt time.Time                              `json:"changed_at"`
}

type recordActionReadResponse struct {
	ActionID          string                           `json:"action_id"`
	RecordID          string                           `json:"record_id"`
	Version           uint64                           `json:"version"`
	Status            recordcollaboration.ActionStatus `json:"status"`
	Title             string                           `json:"title"`
	AssigneeID        string                           `json:"assignee_id"`
	DueAt             *time.Time                       `json:"due_at"`
	CompletedAt       *time.Time                       `json:"completed_at"`
	SubjectRevisionID string                           `json:"subject_revision_id"`
	CreatedAt         time.Time                        `json:"created_at"`
	UpdatedAt         time.Time                        `json:"updated_at"`
}

func RecordActions(application recordActionHandlerApplication) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		route, ok := recordActionRouteFromPath(request.URL.Path)
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
		handleRecordActionRoute(w, request, actor, application, route)
	})
}

type recordActionRoute struct {
	recordID   string
	actionID   string
	transition recordcollaboration.ActionMutationKind
}

func recordActionRouteFromPath(path string) (recordActionRoute, bool) {
	const prefix = "/api/records/"
	if !strings.HasPrefix(path, prefix) {
		return recordActionRoute{}, false
	}
	segments := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(segments) < 2 || !validRecordTransportID(segments[0]) || segments[1] != "actions" {
		return recordActionRoute{}, false
	}
	route := recordActionRoute{recordID: segments[0]}
	if len(segments) == 2 {
		return route, true
	}
	if len(segments) < 3 || recordcollaboration.ValidateActionID(segments[2]) != nil {
		return recordActionRoute{}, false
	}
	route.actionID = segments[2]
	if len(segments) == 3 {
		return route, true
	}
	if len(segments) != 4 {
		return recordActionRoute{}, false
	}
	switch segments[3] {
	case "complete":
		route.transition = recordcollaboration.ActionMutationComplete
	case "cancel":
		route.transition = recordcollaboration.ActionMutationCancel
	case "reopen":
		route.transition = recordcollaboration.ActionMutationReopen
	default:
		return recordActionRoute{}, false
	}
	return route, true
}

func handleRecordActionRoute(w http.ResponseWriter, request *http.Request, actor recordauth.ActorScope, application recordActionHandlerApplication, route recordActionRoute) {
	switch {
	case route.actionID == "" && route.transition == "":
		if request.Method == http.MethodGet {
			limit, ok := recordActionLimit(request)
			if !ok {
				writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid action list request", nil)
				return
			}
			actions, err := application.ListActions(request.Context(), recordcollaboration.ActionListApplicationRequest{
				Actor: actor, RecordID: route.recordID, Limit: limit,
			})
			if err != nil {
				writeRecordActionApplicationError(w, err)
				return
			}
			writeRecordActionList(w, route.recordID, actions)
			return
		}
		if request.Method != http.MethodPost {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		key, ok := recordActionIdempotencyKey(request, recordplatform.OperationKindRecordActionCreate)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid Idempotency-Key", nil)
			return
		}
		var input recordActionInput
		if !decodeRecordsRequestJSON(w, request, &input) {
			return
		}
		result, err := application.CreateAction(request.Context(), recordcollaboration.ActionCreateApplicationRequest{
			Actor: actor, RecordID: route.recordID, Fields: recordActionFields(input), IdempotencyKey: key,
		})
		if err != nil {
			writeRecordActionApplicationError(w, err)
			return
		}
		status := http.StatusCreated
		if result.Replayed {
			status = http.StatusOK
		}
		writeRecordActionResult(w, status, result)
	case route.actionID != "" && route.transition == "":
		if request.Method != http.MethodPatch {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		key, ok := recordActionIdempotencyKey(request, recordplatform.OperationKindRecordActionUpdate)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid Idempotency-Key", nil)
			return
		}
		version, ok := recordActionIfMatch(request)
		if !ok {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid If-Match", nil)
			return
		}
		var input recordActionInput
		if !decodeRecordsRequestJSON(w, request, &input) {
			return
		}
		result, err := application.UpdateAction(request.Context(), recordcollaboration.ActionUpdateApplicationRequest{
			Actor: actor, RecordID: route.recordID, ActionID: route.actionID, ExpectedVersion: version,
			Fields: recordActionFields(input), IdempotencyKey: key,
		})
		if err != nil {
			writeRecordActionApplicationError(w, err)
			return
		}
		writeRecordActionResult(w, http.StatusOK, result)
	default:
		if request.Method != http.MethodPost {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		operation := map[recordcollaboration.ActionMutationKind]recordplatform.OperationKind{
			recordcollaboration.ActionMutationComplete: recordplatform.OperationKindRecordActionComplete,
			recordcollaboration.ActionMutationCancel:   recordplatform.OperationKindRecordActionCancel,
			recordcollaboration.ActionMutationReopen:   recordplatform.OperationKindRecordActionReopen,
		}[route.transition]
		key, keyOK := recordActionIdempotencyKey(request, operation)
		version, versionOK := recordActionIfMatch(request)
		if !keyOK || !versionOK {
			writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid action command headers", nil)
			return
		}
		var input struct{}
		if !decodeRecordsRequestJSON(w, request, &input) {
			return
		}
		command := recordcollaboration.ActionTransitionApplicationRequest{Actor: actor, RecordID: route.recordID, ActionID: route.actionID, ExpectedVersion: version, IdempotencyKey: key}
		var result recordcollaboration.ActionMutationResult
		var err error
		switch route.transition {
		case recordcollaboration.ActionMutationComplete:
			result, err = application.CompleteAction(request.Context(), command)
		case recordcollaboration.ActionMutationCancel:
			result, err = application.CancelAction(request.Context(), command)
		case recordcollaboration.ActionMutationReopen:
			result, err = application.ReopenAction(request.Context(), command)
		}
		if err != nil {
			writeRecordActionApplicationError(w, err)
			return
		}
		writeRecordActionResult(w, http.StatusOK, result)
	}
}

func recordActionLimit(request *http.Request) (uint64, bool) {
	query := request.URL.Query()
	for key := range query {
		if key != "limit" {
			return 0, false
		}
	}
	values, ok := query["limit"]
	if !ok {
		return 50, true
	}
	if len(values) != 1 || values[0] == "" || (len(values[0]) > 1 && values[0][0] == '0') {
		return 0, false
	}
	limit, err := strconv.ParseUint(values[0], 10, 64)
	return limit, err == nil && limit > 0 && limit <= 100
}

func writeRecordActionList(w http.ResponseWriter, recordID string, actions []recordcollaboration.ActionRecord) {
	response := make([]recordActionReadResponse, len(actions))
	for index := range actions {
		action := actions[index].Clone()
		if action.Validate() != nil || action.RecordID != recordID {
			writeRecordInternalError(w)
			return
		}
		response[index] = recordActionReadResponse{
			ActionID: action.ActionID, RecordID: action.RecordID, Version: action.Version,
			Status: action.Status, Title: action.Title, AssigneeID: action.AssigneeID,
			DueAt: action.DueAt, CompletedAt: action.CompletedAt, SubjectRevisionID: action.SubjectRevisionID,
			CreatedAt: action.CreatedAt.UTC(), UpdatedAt: action.UpdatedAt.UTC(),
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Items []recordActionReadResponse `json:"items"`
	}{Items: response})
}

func recordActionFields(input recordActionInput) recordcollaboration.ActionFieldValues {
	return recordcollaboration.ActionFieldValues{Title: input.Title, Details: input.Details, AssigneeID: input.AssigneeID, DueAt: input.DueAt, SubjectRevisionID: input.SubjectRevisionID}
}

func recordActionIdempotencyKey(request *http.Request, operation recordplatform.OperationKind) (string, bool) {
	values := request.Header.Values("Idempotency-Key")
	if len(values) != 1 || values[0] == "" || values[0] != strings.TrimSpace(values[0]) {
		return "", false
	}
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: operation, Key: values[0]}
	return values[0], key.Validate() == nil
}

func recordActionIfMatch(request *http.Request) (uint64, bool) {
	values := request.Header.Values("If-Match")
	if len(values) != 1 || len(values[0]) < 3 || values[0][0] != '"' || values[0][len(values[0])-1] != '"' {
		return 0, false
	}
	raw := values[0][1 : len(values[0])-1]
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	version, err := strconv.ParseUint(raw, 10, 64)
	return version, err == nil && recordcollaboration.IsIncrementableActionVersion(version)
}

func writeRecordActionResult(w http.ResponseWriter, status int, result recordcollaboration.ActionMutationResult) {
	if result.Validate() != nil {
		writeRecordInternalError(w)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", result.Version))
	writeJSON(w, status, recordActionMutationResponse{ActionID: result.ActionID, RecordID: result.RecordID, Version: result.Version, Status: result.Status, EventKind: result.EventKind, Replayed: result.Replayed, ChangedAt: result.ChangedAt.UTC()})
}

func writeRecordActionApplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordauth.ErrDenied), errors.Is(err, records.ErrRecordNotFound),
		errors.Is(err, records.ErrRecordDeletionReserved), errors.Is(err, recordcollaboration.ErrActionNotFound),
		errors.Is(err, store.ErrRecordSubjectNotFound):
		writeRecordNotFound(w)
	case errors.Is(err, recordcollaboration.ErrActionConflict), errors.Is(err, recordcollaboration.ErrInvalidActionStateTransition):
		writeRecordError(w, http.StatusConflict, "action_conflict", "action changed", nil)
	case errors.Is(err, recordplatform.ErrIdempotencyKeyReused), errors.Is(err, recordplatform.ErrIdempotencyConflictState):
		writeRecordError(w, http.StatusConflict, "idempotency_key_reused", "idempotency key was reused", nil)
	case errors.Is(err, recordplatform.ErrIdempotencyInProgress):
		writeRecordError(w, http.StatusConflict, "action_operation_in_progress", "action operation is in progress", nil)
	case errors.Is(err, recordcollaboration.ErrInvalidActionFields), errors.Is(err, recordcollaboration.ErrMembershipDenied):
		writeRecordError(w, http.StatusUnprocessableEntity, "action_invalid", "action content is invalid", nil)
	case errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable), errors.Is(err, store.ErrRecordSubjectUnavailable),
		errors.Is(err, recordcollaboration.ErrMembershipUnavailable), errors.Is(err, recordplatform.ErrDeletionReservationUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
	default:
		writeRecordInternalError(w)
	}
}
