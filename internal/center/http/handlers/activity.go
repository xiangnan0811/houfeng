package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/records"
)

type subjectActivityService interface {
	List(context.Context, activity.ListRequest) (activity.ListResult, error)
}

var subjectActivityQueryKeys = map[string]struct{}{
	"view": {}, "source": {}, "event_kind": {}, "from": {}, "to": {},
	"versions": {}, "limit": {}, "cursor": {},
}

// SubjectActivity serves GET /api/subjects/{type}/{id}/activity.
func SubjectActivity(service subjectActivityService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		if nilSubjectActivityDependency(service) {
			writeRecordError(w, http.StatusServiceUnavailable, "activity_projection_unavailable", "activity projection is unavailable", nil)
			return
		}
		if request.Method != http.MethodGet {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}

		subject, err := subjectRefFromActivityPath(request.URL.Path)
		if err != nil {
			writeRecordError(w, http.StatusNotFound, "resource_not_found", "subject not found", nil)
			return
		}

		values, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			writeRecordError(w, http.StatusBadRequest, "query_invalid", "invalid activity query", nil)
			return
		}
		for key := range values {
			if _, allowed := subjectActivityQueryKeys[key]; !allowed {
				writeRecordError(w, http.StatusBadRequest, "query_invalid", "unknown activity parameter", nil)
				return
			}
		}
		query, err := subjectActivityQueryFromValues(subject, values)
		if err != nil {
			writeRecordError(w, http.StatusBadRequest, "query_invalid", "invalid activity query", nil)
			return
		}

		result, err := service.List(request.Context(), activity.ListRequest{
			Actor:  actor,
			Query:  query,
			Cursor: values.Get("cursor"),
		})
		if err != nil {
			writeSubjectActivityError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func subjectRefFromActivityPath(path string) (activity.SubjectRef, error) {
	const prefix = "/api/subjects/"
	const suffix = "/activity"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return activity.SubjectRef{}, activity.ErrInvalidQuery
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return activity.SubjectRef{}, activity.ErrInvalidQuery
	}
	kind := records.SubjectKind(parts[0])
	if !records.ValidSubjectKind(kind) {
		return activity.SubjectRef{}, activity.ErrInvalidQuery
	}
	if !records.ValidSubjectSourceID(kind, parts[1]) {
		return activity.SubjectRef{}, activity.ErrInvalidQuery
	}
	return activity.SubjectRef{Kind: kind, SourceID: parts[1]}, nil
}

func subjectActivityQueryFromValues(subject activity.SubjectRef, values url.Values) (activity.Query, error) {
	query := activity.Query{Subject: subject}
	if view := values.Get("view"); view != "" {
		query.View = activity.View(view)
	}
	if versions := values.Get("versions"); versions != "" {
		query.Versions = activity.VersionScope(versions)
	}
	if rawLimit := values.Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return activity.Query{}, activity.ErrInvalidQuery
		}
		query.Limit = limit
	}
	for _, source := range values["source"] {
		query.Sources = append(query.Sources, activity.SourceKind(source))
	}
	for _, kind := range values["event_kind"] {
		query.EventKinds = append(query.EventKinds, activity.EventKind(kind))
	}
	from, err := parseOptionalActivityRFC3339(values.Get("from"))
	if err != nil {
		return activity.Query{}, err
	}
	to, err := parseOptionalActivityRFC3339(values.Get("to"))
	if err != nil {
		return activity.Query{}, err
	}
	query.From = from
	query.To = to
	return query, nil
}

func parseOptionalActivityRFC3339(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, activity.ErrInvalidQuery
	}
	return parsed, nil
}

func writeSubjectActivityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, activity.ErrInvalidQuery), errors.Is(err, activity.ErrInvalidListRequest):
		writeRecordError(w, http.StatusBadRequest, "query_invalid", "invalid activity query", nil)
	case errors.Is(err, activity.ErrCursorInvalid):
		writeRecordError(w, http.StatusBadRequest, "cursor_invalid", "invalid activity cursor", nil)
	case errors.Is(err, activity.ErrCursorExpired):
		writeRecordError(w, http.StatusConflict, "cursor_expired", "activity cursor expired", nil)
	case errors.Is(err, activity.ErrSubjectNotFound):
		writeRecordError(w, http.StatusNotFound, "resource_not_found", "subject not found", nil)
	case errors.Is(err, activity.ErrProjectionUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "activity_projection_unavailable", "activity projection is unavailable", nil)
	default:
		writeRecordsApplicationError(w, err)
	}
}

func nilSubjectActivityDependency(service subjectActivityService) bool {
	if service == nil {
		return true
	}
	return false
}
