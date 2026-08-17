package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterPrefersProtectedRecordWatchRouteOverGenericRecords(t *testing.T) {
	recordsCalls, watchCalls, authCalls := 0, 0, 0
	router := centerhttp.New(centerhttp.RouterOptions{
		RecordsEnabled:       true,
		RecordsHandler:       http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { recordsCalls++; w.WriteHeader(http.StatusTeapot) }),
		RecordWatchesHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { watchCalls++; w.WriteHeader(http.StatusNoContent) }),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) { authCalls++; next.ServeHTTP(w, request) })
		},
	})
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/records/rec_watchparent1/watch", nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s status=%d", method, recorder.Code)
		}
	}
	if watchCalls != 2 || authCalls != 2 || recordsCalls != 0 {
		t.Fatalf("calls watch=%d auth=%d records=%d", watchCalls, authCalls, recordsCalls)
	}
}
