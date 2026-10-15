package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/http/handlers"
)

func TestVPSIPQualityCollectScopesAndConflict(t *testing.T) {
	for _, tc := range []struct {
		body     string
		versions []int
	}{
		{"", []int{4, 6}}, {"{}", []int{4, 6}},
		{`{"ip_versions":[4]}`, []int{4}}, {`{"ip_versions":[6]}`, []int{6}}, {`{"ip_versions":[4,6]}`, []int{4, 6}},
	} {
		t.Run(tc.body, func(t *testing.T) {
			handler := handlers.VPSIPQualityCollect(fakeIPQualityCollectLinks{summaries: []assetlinks.MonitoringInstanceSummary{boundCollectInstance("mi_scope")}}, fakeIPQualityCollectSettings{enabled: true}, newTestCollectRegistry())
			post := func(body string) (int, ipQualityCollectResponse) {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/vps/vps_scope/ip-quality/collect", strings.NewReader(body)))
				var result ipQualityCollectResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return recorder.Code, result
			}
			code, first := post(tc.body)
			if code != http.StatusAccepted || first.Request == nil || !slices.Equal(first.Request.RequestedIPVersions, tc.versions) {
				t.Fatalf("first=%d %#v", code, first)
			}
			code, reused := post(tc.body)
			if code != http.StatusAccepted || reused.Request.RequestID != first.Request.RequestID {
				t.Fatalf("reused=%d %#v", code, reused)
			}
			other := `{"ip_versions":[4]}`
			if slices.Equal(tc.versions, []int{4}) {
				other = `{"ip_versions":[6]}`
			}
			code, conflict := post(other)
			if code != http.StatusConflict || conflict.Reason != "collection_scope_conflict" || conflict.Request == nil || conflict.Request.RequestID != first.Request.RequestID || !slices.Equal(conflict.Request.RequestedIPVersions, tc.versions) {
				t.Fatalf("conflict=%d %#v", code, conflict)
			}
		})
	}
}

func TestVPSIPQualityCollectRejectsMalformedScopeWithoutEnqueuing(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{"ip_versions":null}`, `{"ip_versions":[]}`, `{"ip_versions":[4,4]}`, `{"ip_versions":[6,4]}`, `{"ip_versions":[5]}`, `{"ip_versions":[4,6,4]}`, `{"ip_versions":[4.0]}`, `{"ip_versions":["4"]}`, `{"ip_versions":4}`, `{"ip_versions":[4],"extra":true}`, `{"extra":true}`, `{"ip_versions":[4],"ip_versions":[6]}`, `{} {}`, `{`, strings.Repeat(" ", 1025) + `{}`,
	} {
		t.Run(body, func(t *testing.T) {
			registry := newTestCollectRegistry()
			handler := handlers.VPSIPQualityCollect(fakeIPQualityCollectLinks{summaries: []assetlinks.MonitoringInstanceSummary{boundCollectInstance("mi_scope")}}, fakeIPQualityCollectSettings{enabled: true}, registry)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/vps/vps_scope/ip-quality/collect", strings.NewReader(body)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			_, status := serveIPQualityCollect(t, handler, http.MethodGet, "/api/vps/vps_scope/ip-quality/collect")
			if status.Request != nil {
				t.Fatalf("invalid input enqueued a request: %#v", status.Request)
			}
		})
	}
}
