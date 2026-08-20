package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/vpsoverview"
)

type vpsOverviewStub struct {
	overview vpsoverview.Overview
	err      error
	requests []vpsoverview.Request
}

func (stub *vpsOverviewStub) Get(_ context.Context, request vpsoverview.Request) (vpsoverview.Overview, error) {
	stub.requests = append(stub.requests, request)
	if stub.err != nil {
		return vpsoverview.Overview{}, stub.err
	}
	return stub.overview, nil
}

func TestVPSOverviewHandlerReturnsPayload(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	stub := &vpsOverviewStub{overview: vpsoverview.Overview{
		GeneratedAt:  now,
		Identity:     vpsoverview.Identity{VPSID: "vps_7c2a4e18b09d5f31", DisplayName: "Alpha", Labels: []string{}},
		Anomalies:    []vpsoverview.Anomaly{},
		Facts:        []vpsoverview.Fact{},
		Relations:    []vpsoverview.RelationSummary{},
		Capabilities: []string{vpsoverview.CapabilityRecordsV2Read},
	}}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("actor: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/vps/vps_7c2a4e18b09d5f31/overview", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	VPSOverview(stub).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(stub.requests) != 1 || stub.requests[0].VPSID != "vps_7c2a4e18b09d5f31" {
		t.Fatalf("requests = %#v", stub.requests)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := payload["anomalies"]; !ok {
		t.Fatal("missing anomalies")
	}
}

func TestVPSOverviewHandlerMapsNotFound(t *testing.T) {
	stub := &vpsOverviewStub{err: vpsoverview.ErrVPSNotFound}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("actor: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/vps/vps_7c2a4e18b09d5f31/overview", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	VPSOverview(stub).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "resource_not_found") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
