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

type recordWatchHandlerApplication interface {
	SetWatch(context.Context, recordcollaboration.WatchSetApplicationRequest) (recordcollaboration.WatchStatus, error)
	GetWatch(context.Context, recordcollaboration.WatchReadApplicationRequest) (recordcollaboration.WatchStatus, error)
}

type recordWatchInput struct {
	Preference recordcollaboration.FollowerPreference `json:"preference"`
}

type recordWatchSourcesResponse struct {
	Author      bool `json:"author"`
	Owner       bool `json:"owner"`
	Participant bool `json:"participant"`
	Comment     bool `json:"comment"`
	Mention     bool `json:"mention"`
	Action      bool `json:"action"`
}

type recordWatchResponse struct {
	RecordID   string                                 `json:"record_id"`
	UserID     string                                 `json:"user_id"`
	Version    uint64                                 `json:"version"`
	Preference recordcollaboration.FollowerPreference `json:"preference"`
	Sources    recordWatchSourcesResponse             `json:"sources"`
	UpdatedAt  *time.Time                             `json:"updated_at"`
}

func RecordWatches(application recordWatchHandlerApplication) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		recordID, ok := recordWatchRecordID(request.URL.Path)
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
		switch request.Method {
		case http.MethodGet:
			result, err := application.GetWatch(request.Context(), recordcollaboration.WatchReadApplicationRequest{Actor: actor, RecordID: recordID})
			if err != nil {
				writeRecordWatchApplicationError(w, err)
				return
			}
			writeRecordWatchStatus(w, result)
		case http.MethodPatch:
			key, keyOK := recordActionIdempotencyKey(request, recordplatform.OperationKindRecordWatchPreference)
			version, versionOK := recordWatchIfMatch(request)
			if !keyOK || !versionOK {
				writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid watch command headers", nil)
				return
			}
			var input recordWatchInput
			if !decodeRecordsRequestJSON(w, request, &input) {
				return
			}
			if recordcollaboration.ValidateFollowerPreference(input.Preference) != nil {
				writeRecordError(w, http.StatusUnprocessableEntity, "watch_invalid", "watch preference is invalid", nil)
				return
			}
			result, err := application.SetWatch(request.Context(), recordcollaboration.WatchSetApplicationRequest{
				Actor: actor, RecordID: recordID, ExpectedVersion: version, Preference: input.Preference, IdempotencyKey: key,
			})
			if err != nil {
				writeRecordWatchApplicationError(w, err)
				return
			}
			writeRecordWatchStatus(w, result)
		default:
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		}
	})
}

func recordWatchRecordID(path string) (string, bool) {
	const prefix = "/api/records/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	segments := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(segments) != 2 || !validRecordTransportID(segments[0]) || segments[1] != "watch" {
		return "", false
	}
	return segments[0], true
}

func recordWatchIfMatch(request *http.Request) (uint64, bool) {
	values := request.Header.Values("If-Match")
	if len(values) != 1 || len(values[0]) < 3 || values[0][0] != '"' || values[0][len(values[0])-1] != '"' {
		return 0, false
	}
	raw := values[0][1 : len(values[0])-1]
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	version, err := strconv.ParseUint(raw, 10, 64)
	return version, err == nil && version <= uint64(^uint64(0)>>1)
}

func writeRecordWatchStatus(w http.ResponseWriter, status recordcollaboration.WatchStatus) {
	if status.Validate() != nil {
		writeRecordInternalError(w)
		return
	}
	var updatedAt *time.Time
	if !status.UpdatedAt.IsZero() {
		value := status.UpdatedAt.UTC()
		updatedAt = &value
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, status.Version))
	writeJSON(w, http.StatusOK, recordWatchResponse{
		RecordID: status.RecordID, UserID: status.UserID, Version: status.Version, Preference: status.Preference,
		Sources: recordWatchSourcesResponse{Author: status.Sources.Author, Owner: status.Sources.Owner,
			Participant: status.Sources.Participant, Comment: status.Sources.Comment,
			Mention: status.Sources.Mention, Action: status.Sources.Action}, UpdatedAt: updatedAt,
	})
}

func writeRecordWatchApplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordauth.ErrDenied), errors.Is(err, records.ErrRecordNotFound),
		errors.Is(err, records.ErrRecordDeletionReserved), errors.Is(err, store.ErrRecordSubjectNotFound):
		writeRecordNotFound(w)
	case errors.Is(err, recordcollaboration.ErrWatchConflict), errors.Is(err, recordplatform.ErrIdempotencyKeyReused),
		errors.Is(err, recordplatform.ErrIdempotencyConflictState), errors.Is(err, recordplatform.ErrIdempotencyInProgress):
		writeRecordError(w, http.StatusConflict, "watch_conflict", "watch preference changed", nil)
	case errors.Is(err, recordcollaboration.ErrInvalidWatchRequest), errors.Is(err, recordcollaboration.ErrInvalidWatchCommand),
		errors.Is(err, recordcollaboration.ErrInvalidFollowerPreference):
		writeRecordError(w, http.StatusUnprocessableEntity, "watch_invalid", "watch preference is invalid", nil)
	case errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable), errors.Is(err, store.ErrRecordSubjectUnavailable),
		errors.Is(err, recordcollaboration.ErrMembershipUnavailable), errors.Is(err, recordplatform.ErrDeletionReservationUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
	default:
		writeRecordInternalError(w)
	}
}
