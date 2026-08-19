package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

// The search path sits under the record subtree, which routes every other
// segment by record identifier. Without a more specific pattern the search
// request would be read as a request for a record named "search" and answered
// with a not-found.
func TestRouterPrefersProtectedRecordSearchRouteOverGenericRecords(t *testing.T) {
	recordsCalls, searchCalls, authCalls := 0, 0, 0
	router := centerhttp.New(centerhttp.RouterOptions{
		RecordsEnabled: true,
		RecordsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			recordsCalls++
			w.WriteHeader(http.StatusTeapot)
		}),
		RecordSearchHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			searchCalls++
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				authCalls++
				next.ServeHTTP(w, request)
			})
		},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/records/search?q=disk", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("search status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if searchCalls != 1 || authCalls != 1 || recordsCalls != 0 {
		t.Fatalf("calls search=%d auth=%d records=%d", searchCalls, authCalls, recordsCalls)
	}

	// A record listing and a single record still reach the records handler, so the
	// new pattern narrows exactly one path and nothing else.
	for _, path := range []string{"/api/records", "/api/records/rec_searchneighbour1"} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusTeapot {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusTeapot)
		}
	}
	if searchCalls != 1 || recordsCalls != 2 {
		t.Fatalf("calls after neighbours search=%d records=%d", searchCalls, recordsCalls)
	}
}

func TestRouterOmitsRecordSearchWhenRecordsModeIsOff(t *testing.T) {
	searchCalls := 0
	router := newTestRouter(centerhttp.RouterOptions{
		RecordsEnabled:      false,
		RecordSearchHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { searchCalls++ }),
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/records/search", nil))
	if recorder.Code != http.StatusNotFound || searchCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, searchCalls, recorder.Body.String())
	}
}
