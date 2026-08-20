package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterRegistersSubjectActivityWhenRecordsEnabled(t *testing.T) {
	var calls int
	router := centerhttp.New(centerhttp.RouterOptions{
		RecordsEnabled: true,
		SubjectActivityHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthMiddleware: func(next http.Handler) http.Handler { return next },
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/subjects/vps/vps_7c2a4e18b09d5f31/activity", nil))
	if recorder.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("status=%d calls=%d", recorder.Code, calls)
	}
}

func TestRouterOmitsSubjectActivityWhenRecordsModeIsOff(t *testing.T) {
	var calls int
	router := centerhttp.New(centerhttp.RouterOptions{
		RecordsEnabled: false,
		SubjectActivityHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			calls++
		}),
		AuthMiddleware: func(next http.Handler) http.Handler { return next },
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/subjects/vps/vps_7c2a4e18b09d5f31/activity", nil))
	if calls != 0 {
		t.Fatalf("handler called when records mode off")
	}
}
