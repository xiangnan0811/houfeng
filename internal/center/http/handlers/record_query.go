package handlers

import (
	"net/http"
	"net/url"
	"strconv"
)

func recordListLimit(request *http.Request, defaultLimit, maxLimit uint64) (uint64, bool) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return 0, false
	}
	for key := range query {
		if key != "limit" {
			return 0, false
		}
	}
	values, ok := query["limit"]
	if !ok {
		return defaultLimit, true
	}
	if len(values) != 1 || values[0] == "" {
		return 0, false
	}
	limit, err := strconv.ParseUint(values[0], 10, 64)
	return limit, err == nil && limit > 0 && limit <= maxLimit && strconv.FormatUint(limit, 10) == values[0]
}
