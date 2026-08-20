package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterRegistersVPSOverviewSubtree(t *testing.T) {
	var calls int
	router := centerhttp.New(centerhttp.RouterOptions{
		VPSOverviewHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthMiddleware: func(next http.Handler) http.Handler { return next },
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/vps/vps_7c2a4e18b09d5f31/overview", nil))
	if recorder.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("status=%d calls=%d", recorder.Code, calls)
	}
}
