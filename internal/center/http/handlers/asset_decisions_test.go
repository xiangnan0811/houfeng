package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/assetdecisions"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/vpsassets"
)

type fakeAssetDecisionRepository struct {
	overviewResult  assetdecisions.Overview
	overviewErr     error
	overviewFilters assetdecisions.ListFilters
	groupsResult    []assetdecisions.GroupSummary
	groupsErr       error
	groupsFilters   assetdecisions.ListFilters
	groupResult     assetdecisions.GroupDetail
	groupErr        error
	groupID         string
	groupFilters    assetdecisions.ListFilters
}

func (f *fakeAssetDecisionRepository) GetOverview(_ context.Context, filters assetdecisions.ListFilters) (assetdecisions.Overview, error) {
	f.overviewFilters = filters
	if f.overviewErr != nil {
		return assetdecisions.Overview{}, f.overviewErr
	}
	return f.overviewResult, nil
}

func (f *fakeAssetDecisionRepository) ListGroups(_ context.Context, filters assetdecisions.ListFilters) ([]assetdecisions.GroupSummary, error) {
	f.groupsFilters = filters
	if f.groupsErr != nil {
		return nil, f.groupsErr
	}
	return f.groupsResult, nil
}

func (f *fakeAssetDecisionRepository) GetGroup(_ context.Context, groupID string, filters assetdecisions.ListFilters) (assetdecisions.GroupDetail, error) {
	f.groupID = groupID
	f.groupFilters = filters
	if f.groupErr != nil {
		return assetdecisions.GroupDetail{}, f.groupErr
	}
	return f.groupResult, nil
}

func TestAssetDecisionOverviewReturnsSummary(t *testing.T) {
	repo := &fakeAssetDecisionRepository{overviewResult: assetdecisions.Overview{
		SnapshotGeneratedAt: time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC),
		RenewWithinDays:     45,
		GroupCount:          2,
	}}
	handler := handlers.AssetDecisionOverview(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/asset-decisions/overview?renew_within_days=45", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.overviewFilters.RenewWithinDays != 45 {
		t.Fatalf("filters = %#v, want renew window 45", repo.overviewFilters)
	}
	var body assetdecisions.Overview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.GroupCount != 2 || body.RenewWithinDays != 45 {
		t.Fatalf("body = %#v, want overview counts", body)
	}
}

func TestAssetDecisionGroupsFiltersView(t *testing.T) {
	repo := &fakeAssetDecisionRepository{groupsResult: []assetdecisions.GroupSummary{{
		GroupID:   "adg_auto_001",
		GroupType: assetdecisions.GroupRegionPortfolio,
		View:      assetdecisions.ViewRegion,
		Title:     "德国 · 同区取舍",
	}}}
	handler := handlers.AssetDecisionGroups(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/asset-decisions/groups?view=region&renew_within_days=60", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.groupsFilters.View != assetdecisions.ViewRegion || repo.groupsFilters.RenewWithinDays != 60 {
		t.Fatalf("filters = %#v, want region/60", repo.groupsFilters)
	}
	var body []assetdecisions.GroupSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(body) != 1 || body[0].GroupID != "adg_auto_001" {
		t.Fatalf("body = %#v, want group summary", body)
	}
}

func TestAssetDecisionGroupReturnsDetail(t *testing.T) {
	repo := &fakeAssetDecisionRepository{groupResult: assetdecisions.GroupDetail{
		GroupSummary: assetdecisions.GroupSummary{
			GroupID: "adg_auto_abc",
			Title:   "预算压力与弱承载",
		},
		Members: []assetdecisions.GroupMember{{
			VPS: vpsassets.Record{VPSID: "vps_001", DisplayName: "Frankfurt Worker"},
		}},
	}}
	handler := handlers.AssetDecisionGroup(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/asset-decisions/groups/adg_auto_abc?renew_within_days=30", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.groupID != "adg_auto_abc" || repo.groupFilters.RenewWithinDays != 30 {
		t.Fatalf("group request = (%q,%#v), want id and filters", repo.groupID, repo.groupFilters)
	}
	var body assetdecisions.GroupDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(body.Members) != 1 || body.Members[0].VPS.VPSID != "vps_001" {
		t.Fatalf("body = %#v, want group detail member", body)
	}
}

func TestAssetDecisionHandlersMapErrors(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.Handler
		request  *http.Request
		wantCode int
	}{
		{
			name:     "overview invalid view",
			handler:  handlers.AssetDecisionOverview(&fakeAssetDecisionRepository{}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/overview?view=bad", nil),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "groups invalid window",
			handler:  handlers.AssetDecisionGroups(&fakeAssetDecisionRepository{}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/groups?renew_within_days=-1", nil),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "group missing",
			handler:  handlers.AssetDecisionGroup(&fakeAssetDecisionRepository{groupErr: assetdecisions.ErrAssetDecisionGroupNotFound}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/groups/adg_auto_missing", nil),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "repo failure",
			handler:  handlers.AssetDecisionGroups(&fakeAssetDecisionRepository{groupsErr: errors.New("boom")}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/groups", nil),
			wantCode: http.StatusInternalServerError,
		},
		{
			name:     "method not allowed",
			handler:  handlers.AssetDecisionGroups(&fakeAssetDecisionRepository{}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/groups", nil),
			wantCode: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tt.handler.ServeHTTP(recorder, tt.request)
			if recorder.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.wantCode, recorder.Body.String())
			}
		})
	}
}
