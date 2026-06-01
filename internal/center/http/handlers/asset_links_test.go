package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/http/handlers"
)

type fakeAssetLinkRepository struct {
	linkMonitoringInstanceResult        assetlinks.Record
	linkMonitoringInstanceErr           error
	linkMonitoringInstanceVPSID         string
	linkMonitoringInstanceInput         assetlinks.LinkInput
	unlinkMonitoringInstanceResult      assetlinks.Record
	unlinkMonitoringInstanceErr         error
	unlinkMonitoringInstanceVPSID       string
	unlinkMonitoringInstanceInput       assetlinks.UnlinkInput
	listMonitoringInstancesForVPSResult []assetlinks.MonitoringInstanceSummary
	listMonitoringInstancesForVPSErr    error
	listMonitoringInstancesForVPSID     string
	listVPSForMonitoringInstanceResult  []assetlinks.VPSSummary
	listVPSForMonitoringInstanceErr     error
	listVPSForMonitoringInstanceID      string
	countActiveLinksForVPSVal           int
	countActiveLinksForVPSErr           error
	countActiveLinksForVPSID            string
}

func (f *fakeAssetLinkRepository) LinkMonitoringInstance(_ context.Context, vpsID string, input assetlinks.LinkInput) (assetlinks.Record, error) {
	f.linkMonitoringInstanceVPSID = vpsID
	f.linkMonitoringInstanceInput = input
	if f.linkMonitoringInstanceErr != nil {
		return assetlinks.Record{}, f.linkMonitoringInstanceErr
	}
	return f.linkMonitoringInstanceResult, nil
}

func (f *fakeAssetLinkRepository) UnlinkMonitoringInstance(_ context.Context, vpsID string, input assetlinks.UnlinkInput) (assetlinks.Record, error) {
	f.unlinkMonitoringInstanceVPSID = vpsID
	f.unlinkMonitoringInstanceInput = input
	if f.unlinkMonitoringInstanceErr != nil {
		return assetlinks.Record{}, f.unlinkMonitoringInstanceErr
	}
	return f.unlinkMonitoringInstanceResult, nil
}

func (f *fakeAssetLinkRepository) ListMonitoringInstancesForVPS(_ context.Context, vpsID string) ([]assetlinks.MonitoringInstanceSummary, error) {
	f.listMonitoringInstancesForVPSID = vpsID
	return f.listMonitoringInstancesForVPSResult, f.listMonitoringInstancesForVPSErr
}

func (f *fakeAssetLinkRepository) ListVPSForMonitoringInstance(_ context.Context, monitoringInstanceID string) ([]assetlinks.VPSSummary, error) {
	f.listVPSForMonitoringInstanceID = monitoringInstanceID
	return f.listVPSForMonitoringInstanceResult, f.listVPSForMonitoringInstanceErr
}

func (f *fakeAssetLinkRepository) CountActiveLinksForVPS(_ context.Context, vpsID string) (int, error) {
	f.countActiveLinksForVPSID = vpsID
	return f.countActiveLinksForVPSVal, f.countActiveLinksForVPSErr
}

func TestVPSLinkMonitoringInstanceCreatesLink(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{linkMonitoringInstanceResult: assetlinks.Record{
		LinkID:               "vnl_001",
		VPSID:                "vps_001",
		MonitoringInstanceID: "mi_001",
		LinkedAt:             now,
		Note:                 "primary",
	}}

	handler := handlers.VPSLinkMonitoringInstance(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/link-monitoring-instance", strings.NewReader(`{"monitoring_instance_id":" mi_001 ","note":" primary "}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.linkMonitoringInstanceVPSID != "vps_001" || repo.linkMonitoringInstanceInput.MonitoringInstanceID != "mi_001" || repo.linkMonitoringInstanceInput.Note != "primary" {
		t.Fatalf("link input = vps:%q input:%#v, want normalized values", repo.linkMonitoringInstanceVPSID, repo.linkMonitoringInstanceInput)
	}
	var body assetlinks.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.LinkID != "vnl_001" {
		t.Fatalf("link_id = %q, want vnl_001", body.LinkID)
	}
}

func TestVPSUnlinkMonitoringInstanceEndsActiveLink(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	unlinkedAt := now.Add(time.Minute)
	repo := &fakeAssetLinkRepository{unlinkMonitoringInstanceResult: assetlinks.Record{
		LinkID:               "vnl_001",
		VPSID:                "vps_001",
		MonitoringInstanceID: "mi_001",
		LinkedAt:             now,
		UnlinkedAt:           &unlinkedAt,
		Note:                 "rotated",
	}}

	handler := handlers.VPSUnlinkMonitoringInstance(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/unlink-monitoring-instance", strings.NewReader(`{"monitoring_instance_id":" mi_001 ","note":" rotated "}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.unlinkMonitoringInstanceVPSID != "vps_001" || repo.unlinkMonitoringInstanceInput.MonitoringInstanceID != "mi_001" || repo.unlinkMonitoringInstanceInput.Note != "rotated" {
		t.Fatalf("unlink input = vps:%q input:%#v, want normalized values", repo.unlinkMonitoringInstanceVPSID, repo.unlinkMonitoringInstanceInput)
	}
	var body assetlinks.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.UnlinkedAt == nil {
		t.Fatalf("unlinked_at = nil, want historical unlink timestamp")
	}
}

func TestVPSMonitoringInstancesListsActiveMonitoringInstanceSummaries(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{listMonitoringInstancesForVPSResult: []assetlinks.MonitoringInstanceSummary{{
		MonitoringInstanceID:       "mi_001",
		DisplayName:                "Tokyo MonitoringInstance",
		Provider:                   "MonitoringInstance Hint",
		MonitoringStatus:           "启用",
		CurrentHealthStatus:        "关注",
		CurrentActiveIncidentCount: 1,
		LinkedAt:                   now,
	}}}

	handler := handlers.VPSMonitoringInstances(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/monitoring-instances", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listMonitoringInstancesForVPSID != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listMonitoringInstancesForVPSID)
	}
	var body []assetlinks.MonitoringInstanceSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].MonitoringInstanceID != "mi_001" || body[0].CurrentHealthStatus != "关注" {
		t.Fatalf("body = %#v, want active monitoringInstance summary", body)
	}
}

func TestMonitoringInstanceVPSListsActiveVPSSummaries(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{listVPSForMonitoringInstanceResult: []assetlinks.VPSSummary{{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo VPS",
		ProviderName:    "Asset Provider",
		LifecycleStatus: "active",
		UsageStatus:     "in_use",
		RenewalDecision: "keep",
		LinkedAt:        now,
	}}}

	handler := handlers.MonitoringInstanceVPS(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/vps", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listVPSForMonitoringInstanceID != "mi_001" {
		t.Fatalf("list monitoringInstance id = %q, want mi_001", repo.listVPSForMonitoringInstanceID)
	}
	var body []assetlinks.VPSSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].VPSID != "vps_001" || body[0].ProviderName != "Asset Provider" {
		t.Fatalf("body = %#v, want active vps summary", body)
	}
}

func TestAssetLinkHandlersRejectInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{name: "link blank monitoringInstance", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":" "}`},
		{name: "unlink blank monitoringInstance", handler: handlers.VPSUnlinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/unlink-monitoring-instance", body: `{"monitoring_instance_id":" "}`},
		{name: "link unknown field", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":"mi_001","extra":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestAssetLinkHandlersMapDomainErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "link conflict", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{linkMonitoringInstanceErr: assetlinks.ErrVPSMonitoringInstanceLinkConflict}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":"mi_001"}`, want: http.StatusConflict},
		{name: "link missing vps or monitoring instance", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{linkMonitoringInstanceErr: assetlinks.ErrVPSMonitoringInstanceLinkNotFound}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":"mi_001"}`, want: http.StatusNotFound},
		{name: "unlink missing active link", handler: handlers.VPSUnlinkMonitoringInstance(&fakeAssetLinkRepository{unlinkMonitoringInstanceErr: assetlinks.ErrVPSMonitoringInstanceLinkNotFound}), method: http.MethodPost, path: "/api/vps/vps_001/unlink-monitoring-instance", body: `{"monitoring_instance_id":"mi_001"}`, want: http.StatusNotFound},
		{name: "list monitoringInstances repo failure", handler: handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{listMonitoringInstancesForVPSErr: errors.New("query failed")}), method: http.MethodGet, path: "/api/vps/vps_001/monitoring-instances", want: http.StatusInternalServerError},
		{name: "list vps repo failure", handler: handlers.MonitoringInstanceVPS(&fakeAssetLinkRepository{listVPSForMonitoringInstanceErr: errors.New("query failed")}), method: http.MethodGet, path: "/api/monitoring-instances/mi_001/vps", want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestAssetLinkHandlersRejectWrongMethodsAndMalformedPaths(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		want    int
	}{
		{name: "vps monitoringInstances wrong method", handler: handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/monitoring-instances", want: http.StatusMethodNotAllowed},
		{name: "monitoringInstance vps wrong method", handler: handlers.MonitoringInstanceVPS(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/vps", want: http.StatusMethodNotAllowed},
		{name: "malformed vps path", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance/extra", want: http.StatusNotFound},
		{name: "malformed monitoringInstance path", handler: handlers.MonitoringInstanceVPS(&fakeAssetLinkRepository{}), method: http.MethodGet, path: "/api/monitoring-instances/mi_001/vps/extra", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}
