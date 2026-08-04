package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

const recordDeletionRetryAfter = "1"

type recordDeletionHandlerApplication interface {
	Preview(context.Context, recorddeletion.PreviewRequest) (recorddeletion.PreviewResult, error)
	Execute(context.Context, recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, error)
	Status(context.Context, recorddeletion.StatusRequest) (recorddeletion.DeletionOperation, error)
}

type recordDeletionRoute uint8

const (
	recordDeletionRouteUnknown recordDeletionRoute = iota
	recordDeletionRoutePreview
	recordDeletionRouteExecute
	recordDeletionRouteStatus
)

func RecordDeletions(application recordDeletionHandlerApplication) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)

		route, identifier, ok := recordDeletionRouteFromPath(request.URL.Path)
		if !ok {
			writeRecordNotFound(w)
			return
		}
		if !recordDeletionMethodAllowed(route, request.Method) {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		if application == nil {
			writeRecordDeletionUnavailable(w, route)
			return
		}

		switch route {
		case recordDeletionRoutePreview:
			handleRecordDeletionPreview(w, request, actor, identifier, application)
		case recordDeletionRouteExecute:
			handleRecordDeletionExecute(w, request, actor, identifier, application)
		case recordDeletionRouteStatus:
			handleRecordDeletionStatus(w, request, actor, identifier, application)
		default:
			writeRecordNotFound(w)
		}
	})
}

func handleRecordDeletionPreview(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	recordID string,
	application recordDeletionHandlerApplication,
) {
	result, err := application.Preview(request.Context(), recorddeletion.PreviewRequest{
		Actor:    actor,
		RecordID: recordID,
	})
	if err != nil {
		writeRecordDeletionApplicationError(w, err, false)
		return
	}
	transport := result.Token.Transport()
	if !validPrefixedTransportID(result.ReservationID, "drs_") || result.ExpiresAt.IsZero() {
		writeRecordInternalError(w)
		return
	}
	if _, err := recordplatform.ParseDeletionRequestTokenTransportV1(transport); err != nil {
		writeRecordInternalError(w)
		return
	}
	if err := result.Summary.Validate(); err != nil {
		writeRecordInternalError(w)
		return
	}
	onlinePurgeScopes := make([]string, len(result.Summary.OnlinePurgeScopes))
	for index, scope := range result.Summary.OnlinePurgeScopes {
		onlinePurgeScopes[index] = string(scope)
	}
	survivingCopies := make([]recordDeletionSurvivingCopyResponse, len(result.Summary.SurvivingCopies))
	for index, surviving := range result.Summary.SurvivingCopies {
		survivingCopies[index] = recordDeletionSurvivingCopyResponse{
			Scope:     string(surviving.Scope),
			Kind:      string(surviving.Kind),
			CopyCount: surviving.CopyCount,
		}
	}
	var latestBackupExpiry *time.Time
	if !result.Summary.ManagedBackup.LatestExpiresAt.IsZero() {
		utc := result.Summary.ManagedBackup.LatestExpiresAt.UTC()
		latestBackupExpiry = &utc
	}
	writeJSON(w, http.StatusOK, recordDeletionPreviewResponse{
		ReservationID:        result.ReservationID,
		DeletionRequestToken: transport,
		ExpiresAt:            result.ExpiresAt.UTC(),
		OnlinePurgeScopes:    onlinePurgeScopes,
		SurvivingCopies:      survivingCopies,
		ManagedBackup: recordDeletionManagedBackupResponse{
			RetainedCopyCount:    result.Summary.ManagedBackup.RetainedCopyCount,
			MaximumRetentionDays: result.Summary.ManagedBackup.MaximumRetentionDays,
			LatestExpiresAt:      latestBackupExpiry,
		},
		LedgerHealth: string(result.Summary.LedgerHealth),
	})
}

type recordDeletionExecuteRequest struct {
	ReservationID string `json:"reservation_id"`
}

func handleRecordDeletionExecute(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	recordID string,
	application recordDeletionHandlerApplication,
) {
	var input recordDeletionExecuteRequest
	if !decodeRecordsRequestJSON(w, request, &input) {
		return
	}
	if !validPrefixedTransportID(input.ReservationID, "drs_") {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid deletion request", nil)
		return
	}
	token, ok := recordDeletionIdempotencyToken(request)
	if !ok {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid deletion request", nil)
		return
	}
	operation, err := application.Execute(request.Context(), recorddeletion.ExecuteRequest{
		Actor:         actor,
		RecordID:      recordID,
		ReservationID: input.ReservationID,
		Token:         token,
		ReasonCode:    recorddeletion.DeletionReasonUserConfirmed,
	})
	if err != nil {
		writeRecordDeletionApplicationError(w, err, false)
		return
	}
	writeRecordDeletionOperation(w, operation, true)
}

func handleRecordDeletionStatus(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	operationID string,
	application recordDeletionHandlerApplication,
) {
	operation, err := application.Status(request.Context(), recorddeletion.StatusRequest{
		Actor:       actor,
		OperationID: operationID,
	})
	if err != nil {
		writeRecordDeletionApplicationError(w, err, true)
		return
	}
	writeRecordDeletionOperation(w, operation, false)
}

type recordDeletionPreviewResponse struct {
	ReservationID        string                                `json:"reservation_id"`
	DeletionRequestToken string                                `json:"deletion_request_token"`
	ExpiresAt            time.Time                             `json:"expires_at"`
	OnlinePurgeScopes    []string                              `json:"online_purge_scopes"`
	SurvivingCopies      []recordDeletionSurvivingCopyResponse `json:"surviving_copies"`
	ManagedBackup        recordDeletionManagedBackupResponse   `json:"managed_backup"`
	LedgerHealth         string                                `json:"ledger_health"`
}

type recordDeletionSurvivingCopyResponse struct {
	Scope     string `json:"scope"`
	Kind      string `json:"kind"`
	CopyCount uint64 `json:"copy_count"`
}

type recordDeletionManagedBackupResponse struct {
	RetainedCopyCount    uint64     `json:"retained_copy_count"`
	MaximumRetentionDays uint32     `json:"maximum_retention_days"`
	LatestExpiresAt      *time.Time `json:"latest_expires_at"`
}

type recordDeletionOperationResponse struct {
	OperationID string                       `json:"operation_id"`
	State       recorddeletion.DeletionState `json:"state"`
}

func writeRecordDeletionOperation(w http.ResponseWriter, operation recorddeletion.DeletionOperation, execute bool) {
	if operation.Validate() != nil {
		writeRecordInternalError(w)
		return
	}
	status := http.StatusAccepted
	if operation.State == recorddeletion.DeletionStateNotCommitted ||
		(!execute && operation.State == recorddeletion.DeletionStateOnlinePurged) {
		status = http.StatusOK
	} else {
		w.Header().Set("Retry-After", recordDeletionRetryAfter)
	}
	writeJSON(w, status, recordDeletionOperationResponse{
		OperationID: operation.OperationID,
		State:       operation.State,
	})
}

func writeRecordDeletionUnavailable(w http.ResponseWriter, route recordDeletionRoute) {
	if route == recordDeletionRouteStatus {
		writeRecordError(w, http.StatusServiceUnavailable, "deletion_status_unavailable", "deletion status unavailable", nil)
		return
	}
	writeRecordError(w, http.StatusServiceUnavailable, "deletion_safety_unavailable", "deletion safety unavailable", nil)
}

func writeRecordDeletionApplicationError(w http.ResponseWriter, err error, statusRoute bool) {
	switch {
	case errors.Is(err, recordauth.ErrDenied),
		errors.Is(err, records.ErrRecordNotFound),
		errors.Is(err, recorddeletion.ErrDeletionPreviewNotFound),
		errors.Is(err, recorddeletion.ErrDeletionOperationNotFound):
		writeRecordNotFound(w)
	case errors.Is(err, recorddeletion.ErrDeletionPreviewStale):
		writeRecordError(w, http.StatusConflict, "deletion_preview_stale", "deletion preview is stale", nil)
	case errors.Is(err, recorddeletion.ErrDeletionRequestTokenReused):
		writeRecordError(w, http.StatusConflict, "deletion_request_token_reused", "deletion request token was reused", nil)
	case errors.Is(err, recorddeletion.ErrDeletionStatusUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "deletion_status_unavailable", "deletion status unavailable", nil)
	case errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable),
		errors.Is(err, recordplatform.ErrDeletionReservationUnavailable),
		errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable),
		errors.Is(err, store.ErrRecordSubjectUnavailable),
		errors.Is(err, store.ErrRecordDeletionStoreUnavailable):
		code := "deletion_safety_unavailable"
		message := "deletion safety unavailable"
		if statusRoute {
			code = "deletion_status_unavailable"
			message = "deletion status unavailable"
		}
		writeRecordError(w, http.StatusServiceUnavailable, code, message, nil)
	case errors.Is(err, recorddeletion.ErrInvalidDeletionPreview),
		errors.Is(err, recordplatform.ErrInvalidDeletionRequestToken):
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid deletion request", nil)
	default:
		writeRecordInternalError(w)
	}
}

func recordDeletionIdempotencyToken(request *http.Request) (recordplatform.DeletionRequestTokenTransportV1, bool) {
	values := request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return recordplatform.DeletionRequestTokenTransportV1{}, false
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(values[0])
	if err != nil || token.Transport() != values[0] {
		return recordplatform.DeletionRequestTokenTransportV1{}, false
	}
	return token, true
}

func recordDeletionRouteFromPath(path string) (recordDeletionRoute, string, bool) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 4 && segments[0] == "api" && segments[1] == "records" && validRecordTransportID(segments[2]) {
		switch segments[3] {
		case "permanent-delete-preview":
			return recordDeletionRoutePreview, segments[2], true
		case "permanent-delete":
			return recordDeletionRouteExecute, segments[2], true
		}
	}
	if len(segments) == 3 && segments[0] == "api" && segments[1] == "record-deletions" &&
		validPrefixedTransportID(segments[2], "rpo_") {
		return recordDeletionRouteStatus, segments[2], true
	}
	return recordDeletionRouteUnknown, "", false
}

func recordDeletionMethodAllowed(route recordDeletionRoute, method string) bool {
	switch route {
	case recordDeletionRoutePreview, recordDeletionRouteExecute:
		return method == http.MethodPost
	case recordDeletionRouteStatus:
		return method == http.MethodGet
	default:
		return false
	}
}
