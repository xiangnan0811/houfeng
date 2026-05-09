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
	linkNodeResult            assetlinks.Record
	linkNodeErr               error
	linkNodeVPSID             string
	linkNodeInput             assetlinks.LinkInput
	unlinkNodeResult          assetlinks.Record
	unlinkNodeErr             error
	unlinkNodeVPSID           string
	unlinkNodeInput           assetlinks.UnlinkInput
	listNodesForVPSResult     []assetlinks.NodeSummary
	listNodesForVPSErr        error
	listNodesForVPSID         string
	listVPSForNodeResult      []assetlinks.VPSSummary
	listVPSForNodeErr         error
	listVPSForNodeID          string
	countActiveLinksForVPSVal int
	countActiveLinksForVPSErr error
	countActiveLinksForVPSID  string
}

func (f *fakeAssetLinkRepository) LinkNode(_ context.Context, vpsID string, input assetlinks.LinkInput) (assetlinks.Record, error) {
	f.linkNodeVPSID = vpsID
	f.linkNodeInput = input
	if f.linkNodeErr != nil {
		return assetlinks.Record{}, f.linkNodeErr
	}
	return f.linkNodeResult, nil
}

func (f *fakeAssetLinkRepository) UnlinkNode(_ context.Context, vpsID string, input assetlinks.UnlinkInput) (assetlinks.Record, error) {
	f.unlinkNodeVPSID = vpsID
	f.unlinkNodeInput = input
	if f.unlinkNodeErr != nil {
		return assetlinks.Record{}, f.unlinkNodeErr
	}
	return f.unlinkNodeResult, nil
}

func (f *fakeAssetLinkRepository) ListNodesForVPS(_ context.Context, vpsID string) ([]assetlinks.NodeSummary, error) {
	f.listNodesForVPSID = vpsID
	return f.listNodesForVPSResult, f.listNodesForVPSErr
}

func (f *fakeAssetLinkRepository) ListVPSForNode(_ context.Context, nodeID string) ([]assetlinks.VPSSummary, error) {
	f.listVPSForNodeID = nodeID
	return f.listVPSForNodeResult, f.listVPSForNodeErr
}

func (f *fakeAssetLinkRepository) CountActiveLinksForVPS(_ context.Context, vpsID string) (int, error) {
	f.countActiveLinksForVPSID = vpsID
	return f.countActiveLinksForVPSVal, f.countActiveLinksForVPSErr
}

func TestVPSLinkNodeCreatesLink(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{linkNodeResult: assetlinks.Record{
		LinkID:   "vnl_001",
		VPSID:    "vps_001",
		NodeID:   "nd_001",
		LinkedAt: now,
		Note:     "primary",
	}}

	handler := handlers.VPSLinkNode(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/link-node", strings.NewReader(`{"node_id":" nd_001 ","note":" primary "}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.linkNodeVPSID != "vps_001" || repo.linkNodeInput.NodeID != "nd_001" || repo.linkNodeInput.Note != "primary" {
		t.Fatalf("link input = vps:%q input:%#v, want normalized values", repo.linkNodeVPSID, repo.linkNodeInput)
	}
	var body assetlinks.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.LinkID != "vnl_001" {
		t.Fatalf("link_id = %q, want vnl_001", body.LinkID)
	}
}

func TestVPSUnlinkNodeEndsActiveLink(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	unlinkedAt := now.Add(time.Minute)
	repo := &fakeAssetLinkRepository{unlinkNodeResult: assetlinks.Record{
		LinkID:     "vnl_001",
		VPSID:      "vps_001",
		NodeID:     "nd_001",
		LinkedAt:   now,
		UnlinkedAt: &unlinkedAt,
		Note:       "rotated",
	}}

	handler := handlers.VPSUnlinkNode(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/unlink-node", strings.NewReader(`{"node_id":" nd_001 ","note":" rotated "}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.unlinkNodeVPSID != "vps_001" || repo.unlinkNodeInput.NodeID != "nd_001" || repo.unlinkNodeInput.Note != "rotated" {
		t.Fatalf("unlink input = vps:%q input:%#v, want normalized values", repo.unlinkNodeVPSID, repo.unlinkNodeInput)
	}
	var body assetlinks.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.UnlinkedAt == nil {
		t.Fatalf("unlinked_at = nil, want historical unlink timestamp")
	}
}

func TestVPSNodesListsActiveNodeSummaries(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{listNodesForVPSResult: []assetlinks.NodeSummary{{
		NodeID:                     "nd_001",
		DisplayName:                "Tokyo Node",
		Provider:                   "Node Hint",
		MonitoringStatus:           "启用",
		CurrentHealthStatus:        "关注",
		CurrentActiveIncidentCount: 1,
		LinkedAt:                   now,
	}}}

	handler := handlers.VPSNodes(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listNodesForVPSID != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listNodesForVPSID)
	}
	var body []assetlinks.NodeSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].NodeID != "nd_001" || body[0].CurrentHealthStatus != "关注" {
		t.Fatalf("body = %#v, want active node summary", body)
	}
}

func TestNodeVPSListsActiveVPSSummaries(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{listVPSForNodeResult: []assetlinks.VPSSummary{{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo VPS",
		ProviderName:    "Asset Provider",
		LifecycleStatus: "active",
		UsageStatus:     "in_use",
		RenewalDecision: "keep",
		LinkedAt:        now,
	}}}

	handler := handlers.NodeVPS(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_001/vps", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listVPSForNodeID != "nd_001" {
		t.Fatalf("list node id = %q, want nd_001", repo.listVPSForNodeID)
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
		{name: "link blank node", handler: handlers.VPSLinkNode(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-node", body: `{"node_id":" "}`},
		{name: "unlink blank node", handler: handlers.VPSUnlinkNode(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/unlink-node", body: `{"node_id":" "}`},
		{name: "link unknown field", handler: handlers.VPSLinkNode(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-node", body: `{"node_id":"nd_001","extra":true}`},
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
		{name: "link conflict", handler: handlers.VPSLinkNode(&fakeAssetLinkRepository{linkNodeErr: assetlinks.ErrVPSNodeLinkConflict}), method: http.MethodPost, path: "/api/vps/vps_001/link-node", body: `{"node_id":"nd_001"}`, want: http.StatusConflict},
		{name: "link missing vps or node", handler: handlers.VPSLinkNode(&fakeAssetLinkRepository{linkNodeErr: assetlinks.ErrVPSNodeLinkNotFound}), method: http.MethodPost, path: "/api/vps/vps_001/link-node", body: `{"node_id":"nd_001"}`, want: http.StatusNotFound},
		{name: "unlink missing active link", handler: handlers.VPSUnlinkNode(&fakeAssetLinkRepository{unlinkNodeErr: assetlinks.ErrVPSNodeLinkNotFound}), method: http.MethodPost, path: "/api/vps/vps_001/unlink-node", body: `{"node_id":"nd_001"}`, want: http.StatusNotFound},
		{name: "list nodes repo failure", handler: handlers.VPSNodes(&fakeAssetLinkRepository{listNodesForVPSErr: errors.New("query failed")}), method: http.MethodGet, path: "/api/vps/vps_001/nodes", want: http.StatusInternalServerError},
		{name: "list vps repo failure", handler: handlers.NodeVPS(&fakeAssetLinkRepository{listVPSForNodeErr: errors.New("query failed")}), method: http.MethodGet, path: "/api/nodes/nd_001/vps", want: http.StatusInternalServerError},
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
		{name: "vps nodes wrong method", handler: handlers.VPSNodes(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/nodes", want: http.StatusMethodNotAllowed},
		{name: "node vps wrong method", handler: handlers.NodeVPS(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/nodes/nd_001/vps", want: http.StatusMethodNotAllowed},
		{name: "malformed vps path", handler: handlers.VPSLinkNode(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-node/extra", want: http.StatusNotFound},
		{name: "malformed node path", handler: handlers.NodeVPS(&fakeAssetLinkRepository{}), method: http.MethodGet, path: "/api/nodes/nd_001/vps/extra", want: http.StatusNotFound},
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
