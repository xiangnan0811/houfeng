package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type recordInboxApplication interface {
	ListInbox(context.Context, recordcollaboration.InboxListRequest) ([]recordcollaboration.InboxItem, error)
	GetInboxItem(context.Context, recordcollaboration.InboxItemRequest) (recordcollaboration.InboxItem, error)
	GetInboxDeepLink(context.Context, recordcollaboration.InboxItemRequest) (recordcollaboration.InboxDeepLinkTarget, error)
	TransitionInbox(context.Context, recordcollaboration.InboxTransitionRequest) (recordcollaboration.InboxItem, error)
	CountUnreadInbox(context.Context, recordcollaboration.InboxListRequest) (int, error)
}

type recordInboxItemResponse struct {
	NotificationID string                                      `json:"notification_id"`
	RecordID       string                                      `json:"record_id"`
	EventKind      recordcollaboration.NotificationEventKind   `json:"event_kind"`
	SubjectKind    recordcollaboration.NotificationSubjectKind `json:"subject_kind"`
	SubjectID      string                                      `json:"subject_id"`
	SourceVersion  uint64                                      `json:"source_version"`
	Reason         recordcollaboration.NotificationReason      `json:"reason"`
	Mandatory      bool                                        `json:"mandatory"`
	EventAt        time.Time                                   `json:"event_at"`
	ReadAt         *time.Time                                  `json:"read_at"`
	DismissedAt    *time.Time                                  `json:"dismissed_at"`
}

type recordInboxListResponse struct {
	Items []recordInboxItemResponse `json:"items"`
}

type recordInboxUnreadResponse struct {
	UnreadCount int `json:"unread_count"`
}

type recordInboxTargetResponse struct {
	RecordID    string                                      `json:"record_id"`
	SubjectKind recordcollaboration.NotificationSubjectKind `json:"subject_kind"`
	SubjectID   string                                      `json:"subject_id"`
}

func RecordInbox(application recordInboxApplication) http.Handler {
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
		notificationID, action, routeOK := recordInboxRoute(request.URL.Path)
		if !routeOK {
			writeRecordNotFound(w)
			return
		}
		if notificationID == "" {
			if request.Method != http.MethodGet {
				writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
				return
			}
			if action == "unread-count" {
				if request.URL.RawQuery != "" {
					writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid unread count request", nil)
					return
				}
				count, err := application.CountUnreadInbox(request.Context(), recordcollaboration.InboxListRequest{Actor: actor, Limit: 100})
				if err != nil {
					writeRecordInboxError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, recordInboxUnreadResponse{UnreadCount: count})
				return
			}
			limit, ok := recordListLimit(request, 50, 100)
			if !ok {
				writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid inbox limit", nil)
				return
			}
			items, err := application.ListInbox(request.Context(), recordcollaboration.InboxListRequest{Actor: actor, Limit: int(limit)})
			if err != nil {
				writeRecordInboxError(w, err)
				return
			}
			response := make([]recordInboxItemResponse, 0, len(items))
			for _, item := range items {
				mapped, ok := mapRecordInboxItem(item)
				if !ok {
					writeRecordInternalError(w)
					return
				}
				response = append(response, mapped)
			}
			writeJSON(w, http.StatusOK, recordInboxListResponse{Items: response})
			return
		}

		itemRequest := recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: notificationID}
		switch action {
		case "":
			if request.Method != http.MethodGet {
				writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
				return
			}
			item, err := application.GetInboxItem(request.Context(), itemRequest)
			if err != nil {
				writeRecordInboxError(w, err)
				return
			}
			mapped, ok := mapRecordInboxItem(item)
			if !ok {
				writeRecordInternalError(w)
				return
			}
			writeJSON(w, http.StatusOK, mapped)
		case "target":
			if request.Method != http.MethodGet {
				writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
				return
			}
			target, err := application.GetInboxDeepLink(request.Context(), itemRequest)
			if err != nil {
				writeRecordInboxError(w, err)
				return
			}
			if target.Validate() != nil {
				writeRecordInternalError(w)
				return
			}
			writeJSON(w, http.StatusOK, recordInboxTargetResponse{RecordID: target.RecordID, SubjectKind: target.SubjectKind, SubjectID: target.SubjectID})
		case "read", "unread", "dismiss":
			if request.Method != http.MethodPut {
				writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
				return
			}
			if !recordInboxTransitionBodyEmpty(request) {
				writeRecordError(w, http.StatusBadRequest, "invalid_request", "transition body must be empty", nil)
				return
			}
			item, err := application.TransitionInbox(request.Context(), recordcollaboration.InboxTransitionRequest{
				Actor: actor, NotificationID: notificationID, Kind: recordcollaboration.InboxTransitionKind(action),
			})
			if err != nil {
				writeRecordInboxError(w, err)
				return
			}
			mapped, ok := mapRecordInboxItem(item)
			if !ok {
				writeRecordInternalError(w)
				return
			}
			writeJSON(w, http.StatusOK, mapped)
		default:
			writeRecordNotFound(w)
		}
	})
}

func recordInboxTransitionBodyEmpty(request *http.Request) bool {
	if request == nil || request.Body == nil {
		return request != nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1))
	return err == nil && len(body) == 0
}

func recordInboxRoute(path string) (string, string, bool) {
	const prefix = "/api/record-notifications"
	if path == prefix {
		return "", "", true
	}
	if path == prefix+"/unread-count" {
		return "", "unread-count", true
	}
	if !strings.HasPrefix(path, prefix+"/") {
		return "", "", false
	}
	segments := strings.Split(strings.TrimPrefix(path, prefix+"/"), "/")
	if len(segments) < 1 || len(segments) > 2 || segments[0] == "" {
		return "", "", false
	}
	if recordcollaboration.ValidateInboxNotificationID(segments[0]) != nil {
		return "", "", false
	}
	action := ""
	if len(segments) == 2 {
		action = segments[1]
		if action != "target" && action != "read" && action != "unread" && action != "dismiss" {
			return "", "", false
		}
	}
	return segments[0], action, true
}

func mapRecordInboxItem(item recordcollaboration.InboxItem) (recordInboxItemResponse, bool) {
	if item.Validate() != nil {
		return recordInboxItemResponse{}, false
	}
	return recordInboxItemResponse{
		NotificationID: item.NotificationID, RecordID: item.RecordID, EventKind: item.EventKind,
		SubjectKind: item.SubjectKind, SubjectID: item.SubjectID, SourceVersion: item.SourceVersion,
		Reason: item.Reason, Mandatory: item.Mandatory, EventAt: item.EventAt.UTC(),
		ReadAt: item.ReadAt, DismissedAt: item.DismissedAt,
	}, true
}

func writeRecordInboxError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordcollaboration.ErrInboxNotFound), errors.Is(err, recordauth.ErrDenied),
		errors.Is(err, records.ErrRecordNotFound), errors.Is(err, records.ErrRecordDeletionReserved):
		writeRecordNotFound(w)
	case errors.Is(err, recordcollaboration.ErrInvalidInboxRequest):
		writeRecordError(w, http.StatusUnprocessableEntity, "inbox_invalid", "inbox request is invalid", nil)
	case errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable), errors.Is(err, store.ErrRecordSubjectUnavailable),
		errors.Is(err, recordcollaboration.ErrMembershipUnavailable), errors.Is(err, recordcollaboration.ErrInboxUnavailable),
		errors.Is(err, recordplatform.ErrDeletionReservationUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "record_service_unavailable", "record service unavailable", nil)
	default:
		writeRecordInternalError(w)
	}
}
