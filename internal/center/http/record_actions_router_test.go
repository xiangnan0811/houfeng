package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterPrefersProtectedRecordActionsRoutesOverGenericRecords(t *testing.T) {
	recordsCalls, actionCalls, authCalls := 0, 0, 0
	router := centerhttp.New(centerhttp.RouterOptions{
		RecordsEnabled: true,
		RecordsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			recordsCalls++
			w.WriteHeader(http.StatusTeapot)
		}),
		RecordActionsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			actionCalls++
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				authCalls++
				next.ServeHTTP(w, request)
			})
		},
	})
	for _, test := range []struct{ method, path string }{
		{http.MethodPost, "/api/records/rec_actionparent1/actions"},
		{http.MethodPatch, "/api/records/rec_actionparent1/actions/ract_action1"},
		{http.MethodPost, "/api/records/rec_actionparent1/actions/ract_action1/complete"},
		{http.MethodPost, "/api/records/rec_actionparent1/actions/ract_action1/cancel"},
		{http.MethodPost, "/api/records/rec_actionparent1/actions/ract_action1/reopen"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s %s status=%d", test.method, test.path, recorder.Code)
		}
	}
	if actionCalls != 5 || authCalls != 5 || recordsCalls != 0 {
		t.Fatalf("calls action=%d auth=%d records=%d", actionCalls, authCalls, recordsCalls)
	}
}

func TestRouterOmitsRecordActionsWhenRecordsModeIsOff(t *testing.T) {
	actionCalls := 0
	router := newTestRouter(centerhttp.RouterOptions{
		RecordsEnabled:       false,
		RecordActionsHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { actionCalls++ }),
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/records/rec_actionparent1/actions", nil))
	if recorder.Code != http.StatusNotFound || actionCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, actionCalls, recorder.Body.String())
	}
}
