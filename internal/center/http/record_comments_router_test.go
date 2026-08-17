package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterPrefersProtectedRecordCommentRoutesOverGenericRecords(t *testing.T) {
	recordsCalls, commentCalls, authCalls := 0, 0, 0
	router := centerhttp.New(centerhttp.RouterOptions{
		RecordsEnabled: true,
		RecordsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			recordsCalls++
			w.WriteHeader(http.StatusTeapot)
		}),
		RecordCommentsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			commentCalls++
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
		{http.MethodGet, "/api/records/rec_commentparent1/comments"},
		{http.MethodPost, "/api/records/rec_commentparent1/comments"},
		{http.MethodPatch, "/api/records/rec_commentparent1/comments/rcm_comment1"},
		{http.MethodPost, "/api/records/rec_commentparent1/comments/rcm_comment1/redact"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s %s status=%d", test.method, test.path, recorder.Code)
		}
	}
	if commentCalls != 4 || authCalls != 4 || recordsCalls != 0 {
		t.Fatalf("calls comments=%d auth=%d records=%d", commentCalls, authCalls, recordsCalls)
	}
}

func TestRouterOmitsRecordCommentsWhenRecordsModeIsOff(t *testing.T) {
	commentCalls := 0
	router := newTestRouter(centerhttp.RouterOptions{
		RecordsEnabled:        false,
		RecordCommentsHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { commentCalls++ }),
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/records/rec_commentparent1/comments", nil))
	if recorder.Code != http.StatusNotFound || commentCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, commentCalls, recorder.Body.String())
	}
}
