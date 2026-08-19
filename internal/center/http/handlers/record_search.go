package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
)

type recordSearchService interface {
	Search(context.Context, recordsearch.SearchRequest) (recordsearch.Result, error)
}

// recordSearchQueryKeys is the closed set of accepted parameters. An unknown key
// is refused rather than ignored: silently dropping a misspelled filter would
// answer a narrower question with a wider result set and look like a match.
var recordSearchQueryKeys = map[string]struct{}{
	"q": {}, "type": {}, "status": {}, "status_group": {}, "lifecycle": {},
	"owner": {}, "participant": {}, "tag": {}, "subject": {},
	"follow_up": {}, "action": {},
	"occurred_from": {}, "occurred_to": {}, "updated_from": {}, "updated_to": {},
	"sort": {}, "limit": {}, "cursor": {},
}

func RecordSearch(service recordSearchService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		if nilRecordSearchHandlerDependency(service) {
			writeRecordError(w, http.StatusServiceUnavailable, "search_unavailable", "record search is unavailable", nil)
			return
		}
		if request.Method != http.MethodGet {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}

		values, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			writeRecordError(w, http.StatusBadRequest, "query_invalid", "invalid search query", nil)
			return
		}
		for key := range values {
			if _, allowed := recordSearchQueryKeys[key]; !allowed {
				writeRecordError(w, http.StatusBadRequest, "query_invalid", "unknown search parameter", nil)
				return
			}
		}
		query, err := recordSearchQueryFromValues(values)
		if err != nil {
			writeRecordError(w, http.StatusBadRequest, "query_invalid", "invalid search query", nil)
			return
		}

		result, err := service.Search(request.Context(), recordsearch.SearchRequest{
			Actor:  actor,
			Query:  query,
			Cursor: values.Get("cursor"),
		})
		if err != nil {
			writeRecordSearchError(w, err)
			return
		}
		response := recordSearchResponse{
			Items:      make([]recordResponse, 0, len(result.Records)),
			NextCursor: result.NextCursor,
			Generation: result.Generation,
		}
		for _, record := range result.Records {
			response.Items = append(response.Items, newRecordResponse(record))
		}
		writeJSON(w, http.StatusOK, response)
	})
}

type recordSearchResponse struct {
	Items      []recordResponse `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Generation uint64           `json:"generation"`
}

func recordSearchQueryFromValues(values url.Values) (recordsearch.Query, error) {
	pageSize, err := recordSearchPageSize(values.Get("limit"))
	if err != nil {
		return recordsearch.Query{}, err
	}
	occurred, err := recordSearchTimeRange(values, "occurred_from", "occurred_to")
	if err != nil {
		return recordsearch.Query{}, err
	}
	updated, err := recordSearchTimeRange(values, "updated_from", "updated_to")
	if err != nil {
		return recordsearch.Query{}, err
	}
	subjects, err := recordSearchSubjectFilters(values["subject"])
	if err != nil {
		return recordsearch.Query{}, err
	}
	// Every remaining value goes to the domain unchecked on purpose: normalization
	// owns the closed vocabularies, so the transport cannot drift from them by
	// keeping its own copy of the valid values.
	return recordsearch.NormalizeQuery(recordsearch.QueryValues{
		Text:           values.Get("q"),
		Types:          recordSearchTypedValues[records.RecordType](values["type"]),
		Statuses:       recordSearchTypedValues[records.BusinessStatus](values["status"]),
		StatusGroups:   recordSearchTypedValues[records.StatusGroup](values["status_group"]),
		Lifecycles:     recordSearchTypedValues[records.Lifecycle](values["lifecycle"]),
		Subjects:       subjects,
		OwnerIDs:       values["owner"],
		ParticipantIDs: values["participant"],
		Tags:           values["tag"],
		FollowUp:       recordsearch.FollowUpState(values.Get("follow_up")),
		Action:         recordsearch.ActionState(values.Get("action")),
		Occurred:       occurred,
		Updated:        updated,
		Sort:           recordsearch.Sort(values.Get("sort")),
		PageSize:       pageSize,
	})
}

func recordSearchPageSize(raw string) (uint32, error) {
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != raw {
		return 0, errors.New("invalid search limit")
	}
	return uint32(parsed), nil
}

func recordSearchTimeRange(values url.Values, fromKey, toKey string) (recordsearch.TimeRange, error) {
	from, err := recordSearchInstant(values.Get(fromKey))
	if err != nil {
		return recordsearch.TimeRange{}, err
	}
	to, err := recordSearchInstant(values.Get(toKey))
	if err != nil {
		return recordsearch.TimeRange{}, err
	}
	return recordsearch.TimeRange{From: from, To: to}, nil
}

func recordSearchInstant(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	return &utc, nil
}

// recordSearchSubjectFilters reads the positional form kind:source_id:role:placement.
// An empty segment means "any", so vps::context asks for every record where some
// VPS is context. Colons are safe separators because every segment is drawn from
// a lowercase alphanumeric vocabulary.
func recordSearchSubjectFilters(raw []string) ([]recordsearch.SubjectFilter, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	filters := make([]recordsearch.SubjectFilter, 0, len(raw))
	for _, value := range raw {
		segments := strings.Split(value, ":")
		if len(segments) > 4 {
			return nil, errors.New("invalid subject filter")
		}
		for len(segments) < 4 {
			segments = append(segments, "")
		}
		filters = append(filters, recordsearch.SubjectFilter{
			Kind:      records.SubjectKind(segments[0]),
			SourceID:  segments[1],
			Role:      records.RelationRole(segments[2]),
			Placement: recordsearch.SubjectPlacement(segments[3]),
		})
	}
	return filters, nil
}

func recordSearchTypedValues[Value ~string](raw []string) []Value {
	if len(raw) == 0 {
		return nil
	}
	converted := make([]Value, 0, len(raw))
	for _, value := range raw {
		converted = append(converted, Value(value))
	}
	return converted
}

func writeRecordSearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordsearch.ErrInvalidQuery), errors.Is(err, recordsearch.ErrInvalidSearchRequest):
		writeRecordError(w, http.StatusBadRequest, "query_invalid", "invalid search query", nil)
	case errors.Is(err, recordsearch.ErrInvalidCursor):
		writeRecordError(w, http.StatusBadRequest, "cursor_invalid", "invalid search cursor", nil)
	// A republished index is a recoverable conflict rather than a bad request: the
	// caller's cursor was valid when minted, and restarting from page one is the
	// whole remedy.
	case errors.Is(err, recordsearch.ErrGenerationSuperseded):
		writeRecordError(w, http.StatusConflict, "search_generation_superseded", "search index was republished", nil)
	case errors.Is(err, recordsearch.ErrIndexUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "search_unavailable", "record search is unavailable", nil)
	default:
		writeRecordsApplicationError(w, err)
	}
}

func nilRecordSearchHandlerDependency(service recordSearchService) bool {
	return service == nil || nilEvidenceHandlerDependency(service)
}
