package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterProtectsRecordInboxOutsideGenericRecordsRoute(t *testing.T) {
	recordsCalls, inboxCalls, authCalls := 0, 0, 0
	router := centerhttp.New(centerhttp.RouterOptions{
		RecordsEnabled:     true,
		RecordsHandler:     http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { recordsCalls++; w.WriteHeader(http.StatusTeapot) }),
		RecordInboxHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { inboxCalls++; w.WriteHeader(http.StatusNoContent) }),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) { authCalls++; next.ServeHTTP(w, request) })
		},
	})
	for _, path := range []string{
		"/api/record-notifications", "/api/record-notifications/unread-count",
		"/api/record-notifications/rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/target",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
	if inboxCalls != 3 || authCalls != 3 || recordsCalls != 0 {
		t.Fatalf("calls inbox/auth/records = %d/%d/%d", inboxCalls, authCalls, recordsCalls)
	}
}
