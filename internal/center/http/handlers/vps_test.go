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
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

type fakeVPSAssetRepository struct {
	listVPSAssetsResult  []vpsassets.Record
	listVPSAssetsErr     error
	listVPSAssetsFilter  vpsassets.ListFilters
	getVPSAssetResult    vpsassets.Record
	getVPSAssetErr       error
	getVPSAssetID        string
	createVPSAssetResult vpsassets.Record
	createVPSAssetErr    error
	createVPSAssetInput  vpsassets.CreateInput
	patchVPSAssetResult  vpsassets.Record
	patchVPSAssetErr     error
	patchVPSAssetID      string
	patchVPSAssetInput   vpsassets.PatchInput
}

func (f *fakeVPSAssetRepository) ListVPSAssets(_ context.Context, filters vpsassets.ListFilters) ([]vpsassets.Record, error) {
	f.listVPSAssetsFilter = filters
	return f.listVPSAssetsResult, f.listVPSAssetsErr
}

func (f *fakeVPSAssetRepository) GetVPSAsset(_ context.Context, vpsID string) (vpsassets.Record, error) {
	f.getVPSAssetID = vpsID
	if f.getVPSAssetErr != nil {
		return vpsassets.Record{}, f.getVPSAssetErr
	}
	return f.getVPSAssetResult, nil
}

func (f *fakeVPSAssetRepository) CreateVPSAsset(_ context.Context, input vpsassets.CreateInput) (vpsassets.Record, error) {
	f.createVPSAssetInput = input
	if f.createVPSAssetErr != nil {
		return vpsassets.Record{}, f.createVPSAssetErr
	}
	return f.createVPSAssetResult, nil
}

func (f *fakeVPSAssetRepository) PatchVPSAsset(_ context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
	f.patchVPSAssetID = vpsID
	f.patchVPSAssetInput = input
	if f.patchVPSAssetErr != nil {
		return vpsassets.Record{}, f.patchVPSAssetErr
	}
	return f.patchVPSAssetResult, nil
}

type fakeVPSAssetRenewalLinkageRepository struct {
	fakeVPSAssetRepository
	linkageResult vpsassets.RenewalSubscriptionLinkage
	linkageInput  vpsassets.PatchInput
	linkageVPSID  string
}

func (f *fakeVPSAssetRenewalLinkageRepository) PatchVPSAssetWithSubscriptionRenewalLinkage(_ context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, vpsassets.RenewalSubscriptionLinkage, error) {
	f.linkageVPSID = vpsID
	f.linkageInput = input
	if f.patchVPSAssetErr != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, f.patchVPSAssetErr
	}
	return f.patchVPSAssetResult, f.linkageResult, nil
}

type fakeRenewalTimelineRepository struct {
	timeline             renewals.VPSTimeline
	err                  error
	requestedVPSID       string
	createLogResult      renewals.ExperienceLogRecord
	createLogErr         error
	createLogInput       renewals.CreateExperienceLogInput
	listLogsResult       []renewals.ExperienceLogRecord
	listLogsErr          error
	listLogsRequestedVPS string
}

func (f *fakeRenewalTimelineRepository) GetVPSTimeline(_ context.Context, vpsID string) (renewals.VPSTimeline, error) {
	f.requestedVPSID = vpsID
	if f.err != nil {
		return renewals.VPSTimeline{}, f.err
	}
	return f.timeline, nil
}

func (f *fakeRenewalTimelineRepository) CreateExperienceLog(_ context.Context, input renewals.CreateExperienceLogInput) (renewals.ExperienceLogRecord, error) {
	f.createLogInput = input
	if f.createLogErr != nil {
		return renewals.ExperienceLogRecord{}, f.createLogErr
	}
	return f.createLogResult, nil
}

func (f *fakeRenewalTimelineRepository) ListExperienceLogsForVPS(_ context.Context, vpsID string) ([]renewals.ExperienceLogRecord, error) {
	f.listLogsRequestedVPS = vpsID
	if f.listLogsErr != nil {
		return nil, f.listLogsErr
	}
	return f.listLogsResult, nil
}

func TestVPSCollectionListsAssetsWithFilters(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	providerID := "pv_001"
	repo := &fakeVPSAssetRepository{listVPSAssetsResult: []vpsassets.Record{{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge",
		ProviderID:      &providerID,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalKeep,
		SSHPort:         22,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}}

	handler := handlers.VPSCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps?provider_id=+pv_001+&lifecycle_status=+active+&usage_status=in_use&renewal_decision=keep", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listVPSAssetsFilter.ProviderID != "pv_001" ||
		repo.listVPSAssetsFilter.LifecycleStatus != vpsassets.LifecycleActive ||
		repo.listVPSAssetsFilter.UsageStatus != vpsassets.UsageInUse ||
		repo.listVPSAssetsFilter.RenewalDecision != vpsassets.RenewalKeep {
		t.Fatalf("filters = %#v, want normalized query filters", repo.listVPSAssetsFilter)
	}
	var body []vpsassets.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].VPSID != "vps_001" {
		t.Fatalf("body = %#v, want vps asset list", body)
	}
}

func TestVPSCollectionAddsActiveNodeLinkCountsWhenAvailable(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeVPSAssetRepository{listVPSAssetsResult: []vpsassets.Record{{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge",
		SSHPort:         22,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalKeep,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}}
	linkRepo := &fakeAssetLinkRepository{countActiveLinksForVPSVal: 2}

	handler := handlers.VPSCollection(repo, linkRepo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if linkRepo.countActiveLinksForVPSID != "vps_001" {
		t.Fatalf("count vps id = %q, want vps_001", linkRepo.countActiveLinksForVPSID)
	}
	var body []vpsassets.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].ActiveNodeLinkCount != 2 {
		t.Fatalf("body = %#v, want active_node_link_count 2", body)
	}
}

func TestVPSCollectionCreatesAsset(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	providerID := "pv_001"
	repo := &fakeVPSAssetRepository{createVPSAssetResult: vpsassets.Record{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge",
		ProviderID:      &providerID,
		ProviderName:    "Hetzner",
		SSHPort:         22,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalUnreviewed,
		Importance:      "normal",
		Labels:          []string{"edge"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}}

	handler := handlers.VPSCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps", strings.NewReader(`{
		"display_name":" Tokyo Edge ",
		"provider_id":" pv_001 ",
		"provider_name":" Hetzner ",
		"lifecycle_status":"active",
		"usage_status":"in_use",
		"labels":[" edge ","","edge"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createVPSAssetInput.DisplayName != "Tokyo Edge" {
		t.Fatalf("create display name = %q, want trimmed name", repo.createVPSAssetInput.DisplayName)
	}
	if repo.createVPSAssetInput.ProviderID == nil || *repo.createVPSAssetInput.ProviderID != "pv_001" {
		t.Fatalf("create provider id = %#v, want pv_001", repo.createVPSAssetInput.ProviderID)
	}
	if repo.createVPSAssetInput.SSHPort != vpsassets.DefaultSSHPort {
		t.Fatalf("create ssh port = %d, want default %d", repo.createVPSAssetInput.SSHPort, vpsassets.DefaultSSHPort)
	}
	if len(repo.createVPSAssetInput.Labels) != 1 || repo.createVPSAssetInput.Labels[0] != "edge" {
		t.Fatalf("create labels = %#v, want normalized labels", repo.createVPSAssetInput.Labels)
	}
	var body vpsassets.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.VPSID != "vps_001" {
		t.Fatalf("vps_id = %q, want vps_001", body.VPSID)
	}
}

func TestVPSCollectionRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body string
		path string
	}{
		{name: "blank display name", body: `{"display_name":" ","lifecycle_status":"active","usage_status":"in_use"}`, path: "/api/vps"},
		{name: "invalid lifecycle", body: `{"display_name":"Tokyo","lifecycle_status":"online","usage_status":"in_use"}`, path: "/api/vps"},
		{name: "invalid usage", body: `{"display_name":"Tokyo","lifecycle_status":"active","usage_status":"busy"}`, path: "/api/vps"},
		{name: "invalid renewal", body: `{"display_name":"Tokyo","lifecycle_status":"active","usage_status":"in_use","renewal_decision":"later"}`, path: "/api/vps"},
		{name: "invalid ssh port", body: `{"display_name":"Tokyo","lifecycle_status":"active","usage_status":"in_use","ssh_port":65536}`, path: "/api/vps"},
		{name: "unknown field", body: `{"display_name":"Tokyo","lifecycle_status":"active","usage_status":"in_use","unexpected":true}`, path: "/api/vps"},
		{name: "invalid filter", body: ``, path: "/api/vps?lifecycle_status=online"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.VPSCollection(&fakeVPSAssetRepository{})
			method := http.MethodPost
			if tt.body == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestVPSItemGetsAsset(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeVPSAssetRepository{getVPSAssetResult: vpsassets.Record{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge",
		SSHPort:         22,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalKeep,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}

	handler := handlers.VPSItem(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.getVPSAssetID != "vps_001" {
		t.Fatalf("get vps id = %q, want vps_001", repo.getVPSAssetID)
	}
	var body vpsassets.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.VPSID != "vps_001" {
		t.Fatalf("vps_id = %q, want vps_001", body.VPSID)
	}
}

func TestVPSItemReturnsNodeLinksWhenAvailable(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeVPSAssetRepository{getVPSAssetResult: vpsassets.Record{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge",
		SSHPort:         22,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalKeep,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}
	linkRepo := &fakeAssetLinkRepository{listNodesForVPSResult: []assetlinks.NodeSummary{{
		NodeID:              "nd_001",
		DisplayName:         "Tokyo Node",
		CurrentHealthStatus: "正常",
		LinkedAt:            now,
	}}}

	handler := handlers.VPSItem(repo, linkRepo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if linkRepo.listNodesForVPSID != "vps_001" {
		t.Fatalf("list nodes vps id = %q, want vps_001", linkRepo.listNodesForVPSID)
	}
	var body struct {
		VPSID               string                   `json:"vps_id"`
		ActiveNodeLinkCount int                      `json:"active_node_link_count"`
		NodeLinks           []assetlinks.NodeSummary `json:"node_links"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.VPSID != "vps_001" || body.ActiveNodeLinkCount != 1 || len(body.NodeLinks) != 1 || body.NodeLinks[0].NodeID != "nd_001" {
		t.Fatalf("body = %#v, want vps detail with node link summary", body)
	}
}

func TestVPSItemPatchesAsset(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	archivedAt := now.Add(-time.Hour)
	repo := &fakeVPSAssetRepository{patchVPSAssetResult: vpsassets.Record{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge Archived",
		ProviderID:      nil,
		SSHPort:         2200,
		LifecycleStatus: vpsassets.LifecycleArchived,
		UsageStatus:     vpsassets.UsageIdle,
		RenewalDecision: vpsassets.RenewalCancel,
		Labels:          []string{"edge", "backup"},
		CreatedAt:       now.Add(-2 * time.Hour),
		UpdatedAt:       now,
		ArchivedAt:      &archivedAt,
	}}

	handler := handlers.VPSItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/vps/vps_001", strings.NewReader(`{
		"display_name":" Tokyo Edge Archived ",
		"provider_id":null,
		"ssh_port":2200,
		"lifecycle_status":"archived",
		"usage_status":"idle",
		"renewal_decision":"cancel",
		"labels":[" edge ","backup","edge"]
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.patchVPSAssetID != "vps_001" {
		t.Fatalf("patch vps id = %q, want vps_001", repo.patchVPSAssetID)
	}
	if !repo.patchVPSAssetInput.DisplayName.Set || repo.patchVPSAssetInput.DisplayName.Value != "Tokyo Edge Archived" {
		t.Fatalf("patch display name = %#v, want trimmed set name", repo.patchVPSAssetInput.DisplayName)
	}
	if !repo.patchVPSAssetInput.ProviderID.Set || repo.patchVPSAssetInput.ProviderID.Value != nil {
		t.Fatalf("patch provider id = %#v, want explicit clear", repo.patchVPSAssetInput.ProviderID)
	}
	if !repo.patchVPSAssetInput.SSHPort.Set || repo.patchVPSAssetInput.SSHPort.Value != 2200 {
		t.Fatalf("patch ssh port = %#v, want 2200", repo.patchVPSAssetInput.SSHPort)
	}
	if !repo.patchVPSAssetInput.LifecycleStatus.Set || repo.patchVPSAssetInput.LifecycleStatus.Value != vpsassets.LifecycleArchived {
		t.Fatalf("patch lifecycle = %#v, want archived", repo.patchVPSAssetInput.LifecycleStatus)
	}
	if !repo.patchVPSAssetInput.RenewalDecision.Set || repo.patchVPSAssetInput.RenewalDecision.Value != vpsassets.RenewalCancel {
		t.Fatalf("patch renewal = %#v, want cancel", repo.patchVPSAssetInput.RenewalDecision)
	}
	if !repo.patchVPSAssetInput.Labels.Set || len(repo.patchVPSAssetInput.Labels.Values) != 2 {
		t.Fatalf("patch labels = %#v, want normalized labels", repo.patchVPSAssetInput.Labels)
	}
	var body vpsassets.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.DisplayName != "Tokyo Edge Archived" || body.ArchivedAt == nil {
		t.Fatalf("body = %#v, want archived asset", body)
	}
}

func TestVPSItemPatchesCancellationDecisionWithSubscriptionLinkage(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeVPSAssetRenewalLinkageRepository{
		fakeVPSAssetRepository: fakeVPSAssetRepository{patchVPSAssetResult: vpsassets.Record{
			VPSID:           "vps_001",
			DisplayName:     "Tokyo Edge",
			SSHPort:         22,
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     vpsassets.UsageInUse,
			RenewalDecision: vpsassets.RenewalCancel,
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
		linkageResult: vpsassets.RenewalSubscriptionLinkage{
			Status:         vpsassets.RenewalSubscriptionLinkageUpdated,
			CandidateCount: 1,
			SubscriptionID: "sub_001",
			Updated:        true,
			Message:        "已同步取消关联订阅的自动续费。",
		},
	}

	handler := handlers.VPSItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/vps/vps_001", strings.NewReader(`{
		"renewal_decision":"cancel",
		"renewal_reason":" too expensive "
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.linkageVPSID != "vps_001" {
		t.Fatalf("linkage vps id = %q, want vps_001", repo.linkageVPSID)
	}
	if !repo.linkageInput.RenewalDecision.Set || repo.linkageInput.RenewalDecision.Value != vpsassets.RenewalCancel {
		t.Fatalf("linkage renewal decision = %#v, want cancel", repo.linkageInput.RenewalDecision)
	}
	var body struct {
		VPSID                      string                               `json:"vps_id"`
		RenewalSubscriptionLinkage vpsassets.RenewalSubscriptionLinkage `json:"renewal_subscription_linkage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.VPSID != "vps_001" || body.RenewalSubscriptionLinkage.Status != vpsassets.RenewalSubscriptionLinkageUpdated || body.RenewalSubscriptionLinkage.SubscriptionID != "sub_001" {
		t.Fatalf("body = %#v, want vps patch response with linkage summary", body)
	}
}

func TestVPSItemPatchesRenewalDecisionReason(t *testing.T) {
	repo := &fakeVPSAssetRepository{patchVPSAssetResult: vpsassets.Record{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge",
		SSHPort:         22,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalCancel,
		CreatedAt:       time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC),
	}}

	handler := handlers.VPSItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/vps/vps_001", strings.NewReader(`{
		"renewal_decision":"cancel",
		"renewal_reason":" too expensive "
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !repo.patchVPSAssetInput.RenewalDecision.Set || repo.patchVPSAssetInput.RenewalDecision.Value != vpsassets.RenewalCancel {
		t.Fatalf("patch renewal decision = %#v, want cancel", repo.patchVPSAssetInput.RenewalDecision)
	}
	if !repo.patchVPSAssetInput.RenewalReason.Set || repo.patchVPSAssetInput.RenewalReason.Value != "too expensive" {
		t.Fatalf("patch renewal reason = %#v, want trimmed reason", repo.patchVPSAssetInput.RenewalReason)
	}
}

func TestVPSTimelineReturnsAssetHistory(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	fromDecision := vpsassets.RenewalKeep
	fromRenewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC))
	toRenewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 8, 0, 0, 0, time.UTC))
	repo := &fakeRenewalTimelineRepository{timeline: renewals.VPSTimeline{
		VPSID: "vps_001",
		RenewalDecisions: []renewals.DecisionRecord{{
			DecisionID:   "rdec_001",
			VPSID:        "vps_001",
			FromDecision: &fromDecision,
			ToDecision:   vpsassets.RenewalCancel,
			Reason:       "too expensive",
			DecidedAt:    now,
			CreatedAt:    now,
		}},
		PriceHistories: []renewals.PriceHistoryRecord{{
			PriceHistoryID:         "ph_001",
			SubscriptionID:         "sub_001",
			VPSID:                  "vps_001",
			FromPrice:              120,
			ToPrice:                240,
			FromCurrency:           "USD",
			ToCurrency:             "USD",
			FromBillingCycle:       "annual",
			ToBillingCycle:         "biennial",
			FromBillingMonths:      12,
			ToBillingMonths:        24,
			FromMonthlyPrice:       10,
			ToMonthlyPrice:         10,
			FromRenewAt:            &fromRenewAt,
			ToRenewAt:              &toRenewAt,
			FromAutoRenew:          true,
			ToAutoRenew:            false,
			FromAutoRenewCancelled: false,
			ToAutoRenewCancelled:   true,
			FromStatus:             subscriptions.StatusActive,
			ToStatus:               subscriptions.StatusPaused,
			ChangedAt:              now,
			CreatedAt:              now,
		}},
		IPHistories: []renewals.IPHistoryRecord{{
			IPHistoryID: "iph_001",
			VPSID:       "vps_001",
			FromIPv4:    "192.0.2.1",
			ToIPv4:      "198.51.100.5",
			FromIPv6:    "2001:db8::1",
			ToIPv6:      "2001:db8::5",
			ChangedAt:   now,
			CreatedAt:   now,
		}},
		SpecSnapshots: []renewals.SpecSnapshotRecord{{
			SnapshotID:     "vss_001",
			VPSID:          "vps_001",
			ProductName:    "CPX31",
			SSHHost:        "edge.example",
			SSHPort:        2222,
			SSHUser:        "deploy",
			OSName:         "Ubuntu 24.04",
			Virtualization: "kvm",
			CapturedAt:     now,
			CreatedAt:      now,
		}},
		ExperienceLogs: []renewals.ExperienceLogRecord{{
			ExperienceLogID: "elog_001",
			VPSID:           "vps_001",
			Category:        renewals.ExperienceNetwork,
			Severity:        renewals.ExperienceSeverityWarning,
			Summary:         "packet loss",
			Details:         "opened provider ticket",
			OccurredAt:      now,
			CreatedAt:       now,
		}},
	}}

	handler := handlers.VPSTimeline(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/timeline", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.requestedVPSID != "vps_001" {
		t.Fatalf("requested vps id = %q, want vps_001", repo.requestedVPSID)
	}
	var body renewals.VPSTimeline
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.VPSID != "vps_001" ||
		len(body.RenewalDecisions) != 1 ||
		len(body.PriceHistories) != 1 ||
		len(body.IPHistories) != 1 ||
		len(body.SpecSnapshots) != 1 ||
		len(body.ExperienceLogs) != 1 ||
		body.RenewalDecisions[0].FromDecision == nil ||
		*body.RenewalDecisions[0].FromDecision != vpsassets.RenewalKeep ||
		body.PriceHistories[0].PriceHistoryID != "ph_001" ||
		body.IPHistories[0].ToIPv4 != "198.51.100.5" ||
		body.SpecSnapshots[0].ProductName != "CPX31" ||
		body.ExperienceLogs[0].Summary != "packet loss" {
		t.Fatalf("timeline body = %#v, want all asset history arrays", body)
	}
}

func TestVPSExperienceLogsListsAndCreates(t *testing.T) {
	now := time.Date(2026, time.May, 10, 9, 30, 0, 0, time.UTC)
	repo := &fakeRenewalTimelineRepository{
		listLogsResult: []renewals.ExperienceLogRecord{{
			ExperienceLogID: "elog_001",
			VPSID:           "vps_001",
			Category:        renewals.ExperienceNetwork,
			Severity:        renewals.ExperienceSeverityWarning,
			Summary:         "packet loss",
			Details:         "opened provider ticket",
			OccurredAt:      now,
			CreatedAt:       now,
		}},
		createLogResult: renewals.ExperienceLogRecord{
			ExperienceLogID: "elog_002",
			VPSID:           "vps_001",
			Category:        renewals.ExperienceSupport,
			Severity:        renewals.ExperienceSeverityInfo,
			Summary:         "support response improved",
			Details:         "new ticket was answered quickly",
			OccurredAt:      now,
			CreatedAt:       now,
		},
	}

	listRecorder := httptest.NewRecorder()
	handlers.VPSExperienceLogs(repo).ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/experience-logs", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	if repo.listLogsRequestedVPS != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listLogsRequestedVPS)
	}
	var listBody []renewals.ExperienceLogRecord
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(listBody) != 1 || listBody[0].ExperienceLogID != "elog_001" {
		t.Fatalf("list body = %#v, want one experience log", listBody)
	}

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/experience-logs", strings.NewReader(`{
		"category":" support ",
		"severity":" info ",
		"summary":" support response improved ",
		"details":" new ticket was answered quickly ",
		"occurred_at":"2026-05-10T09:30:00Z"
	}`))
	handlers.VPSExperienceLogs(repo).ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	if repo.createLogInput.VPSID != "vps_001" ||
		repo.createLogInput.Category != renewals.ExperienceSupport ||
		repo.createLogInput.Severity != renewals.ExperienceSeverityInfo ||
		repo.createLogInput.Summary != "support response improved" ||
		repo.createLogInput.Details != "new ticket was answered quickly" ||
		repo.createLogInput.OccurredAt == nil {
		t.Fatalf("create input = %#v, want normalized experience log input", repo.createLogInput)
	}
	var createBody renewals.ExperienceLogRecord
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if createBody.ExperienceLogID != "elog_002" {
		t.Fatalf("create body = %#v, want created experience log", createBody)
	}
}

func TestVPSExperienceLogsMapsErrorsAndMethods(t *testing.T) {
	tests := []struct {
		name   string
		repo   *fakeRenewalTimelineRepository
		method string
		path   string
		body   string
		want   int
	}{
		{name: "list missing vps", repo: &fakeRenewalTimelineRepository{listLogsErr: renewals.ErrAssetTimelineNotFound}, method: http.MethodGet, path: "/api/vps/vps_missing/experience-logs", want: http.StatusNotFound},
		{name: "list invalid input", repo: &fakeRenewalTimelineRepository{listLogsErr: renewals.ErrInvalidAssetHistoryInput}, method: http.MethodGet, path: "/api/vps/vps_001/experience-logs", want: http.StatusBadRequest},
		{name: "list repo failure", repo: &fakeRenewalTimelineRepository{listLogsErr: errors.New("query failed")}, method: http.MethodGet, path: "/api/vps/vps_001/experience-logs", want: http.StatusInternalServerError},
		{name: "create invalid json", repo: &fakeRenewalTimelineRepository{}, method: http.MethodPost, path: "/api/vps/vps_001/experience-logs", body: `{`, want: http.StatusBadRequest},
		{name: "create invalid input", repo: &fakeRenewalTimelineRepository{}, method: http.MethodPost, path: "/api/vps/vps_001/experience-logs", body: `{"category":"network","severity":"warning","summary":" "}`, want: http.StatusBadRequest},
		{name: "create missing vps", repo: &fakeRenewalTimelineRepository{createLogErr: renewals.ErrAssetTimelineNotFound}, method: http.MethodPost, path: "/api/vps/vps_missing/experience-logs", body: `{"category":"network","severity":"warning","summary":"packet loss"}`, want: http.StatusNotFound},
		{name: "create repo failure", repo: &fakeRenewalTimelineRepository{createLogErr: errors.New("insert failed")}, method: http.MethodPost, path: "/api/vps/vps_001/experience-logs", body: `{"category":"network","severity":"warning","summary":"packet loss"}`, want: http.StatusInternalServerError},
		{name: "wrong method", repo: &fakeRenewalTimelineRepository{}, method: http.MethodPatch, path: "/api/vps/vps_001/experience-logs", body: `{}`, want: http.StatusMethodNotAllowed},
		{name: "malformed path", repo: &fakeRenewalTimelineRepository{}, method: http.MethodGet, path: "/api/vps/vps_001/experience-logs/extra", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handlers.VPSExperienceLogs(tt.repo).ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestVPSTimelineMapsErrorsAndMethods(t *testing.T) {
	tests := []struct {
		name   string
		repo   *fakeRenewalTimelineRepository
		method string
		path   string
		want   int
	}{
		{name: "missing vps", repo: &fakeRenewalTimelineRepository{err: renewals.ErrRenewalTimelineNotFound}, method: http.MethodGet, path: "/api/vps/vps_missing/timeline", want: http.StatusNotFound},
		{name: "invalid input", repo: &fakeRenewalTimelineRepository{err: renewals.ErrInvalidRenewalDecisionInput}, method: http.MethodGet, path: "/api/vps/vps_001/timeline", want: http.StatusBadRequest},
		{name: "repo failure", repo: &fakeRenewalTimelineRepository{err: errors.New("query failed")}, method: http.MethodGet, path: "/api/vps/vps_001/timeline", want: http.StatusInternalServerError},
		{name: "wrong method", repo: &fakeRenewalTimelineRepository{}, method: http.MethodPost, path: "/api/vps/vps_001/timeline", want: http.StatusMethodNotAllowed},
		{name: "malformed path", repo: &fakeRenewalTimelineRepository{}, method: http.MethodGet, path: "/api/vps/vps_001/timeline/extra", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			handlers.VPSTimeline(tt.repo).ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestVPSItemReturnsNotFound(t *testing.T) {
	tests := []struct {
		name   string
		method string
		repo   *fakeVPSAssetRepository
	}{
		{name: "get", method: http.MethodGet, repo: &fakeVPSAssetRepository{getVPSAssetErr: vpsassets.ErrVPSAssetNotFound}},
		{name: "patch", method: http.MethodPatch, repo: &fakeVPSAssetRepository{patchVPSAssetErr: vpsassets.ErrVPSAssetNotFound}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.VPSItem(tt.repo)
			body := strings.NewReader(`{"display_name":"New Name"}`)
			req := httptest.NewRequest(tt.method, "/api/vps/vps_missing", body)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
			var response map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if response["error"] != "vps asset not found" {
				t.Fatalf("error = %q, want vps asset not found", response["error"])
			}
		})
	}
}

func TestVPSItemRejectsInvalidPatchAndDeeperPaths(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "blank display name", method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"display_name":" "}`, want: http.StatusBadRequest},
		{name: "invalid lifecycle", method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"lifecycle_status":"online"}`, want: http.StatusBadRequest},
		{name: "invalid usage", method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"usage_status":"busy"}`, want: http.StatusBadRequest},
		{name: "invalid renewal", method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"renewal_decision":"later"}`, want: http.StatusBadRequest},
		{name: "invalid ssh port", method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"ssh_port":0}`, want: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"note":"ok","extra":true}`, want: http.StatusBadRequest},
		{name: "deeper path", method: http.MethodGet, path: "/api/vps/vps_001/links", body: ``, want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.VPSItem(&fakeVPSAssetRepository{})
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestVPSMapsInvalidProviderReferenceToBadRequest(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{name: "create", handler: handlers.VPSCollection(&fakeVPSAssetRepository{createVPSAssetErr: vpsassets.ErrInvalidVPSAssetInput}), method: http.MethodPost, path: "/api/vps", body: `{"display_name":"Tokyo","provider_id":"pv_missing","lifecycle_status":"active","usage_status":"in_use"}`},
		{name: "patch", handler: handlers.VPSItem(&fakeVPSAssetRepository{patchVPSAssetErr: vpsassets.ErrInvalidVPSAssetInput}), method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"provider_id":"pv_missing"}`},
		{name: "list", handler: handlers.VPSCollection(&fakeVPSAssetRepository{listVPSAssetsErr: vpsassets.ErrInvalidVPSAssetInput}), method: http.MethodGet, path: "/api/vps", body: ``},
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

func TestVPSUnsupportedMethodsReturnMethodNotAllowed(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
	}{
		{name: "collection", handler: handlers.VPSCollection(&fakeVPSAssetRepository{}), method: http.MethodDelete, path: "/api/vps"},
		{name: "item", handler: handlers.VPSItem(&fakeVPSAssetRepository{}), method: http.MethodPost, path: "/api/vps/vps_001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestVPSMapRepositoryFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{name: "list", handler: handlers.VPSCollection(&fakeVPSAssetRepository{listVPSAssetsErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/vps"},
		{name: "create", handler: handlers.VPSCollection(&fakeVPSAssetRepository{createVPSAssetErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/vps", body: `{"display_name":"Tokyo","lifecycle_status":"active","usage_status":"in_use"}`},
		{name: "get", handler: handlers.VPSItem(&fakeVPSAssetRepository{getVPSAssetErr: errors.New("get failed")}), method: http.MethodGet, path: "/api/vps/vps_001"},
		{name: "patch", handler: handlers.VPSItem(&fakeVPSAssetRepository{patchVPSAssetErr: errors.New("patch failed")}), method: http.MethodPatch, path: "/api/vps/vps_001", body: `{"display_name":"Tokyo"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestVPSMapsLinkRepositoryFailures(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
	}{
		{
			name: "list count failure",
			handler: handlers.VPSCollection(&fakeVPSAssetRepository{listVPSAssetsResult: []vpsassets.Record{{
				VPSID:           "vps_001",
				DisplayName:     "Tokyo Edge",
				SSHPort:         22,
				LifecycleStatus: vpsassets.LifecycleActive,
				UsageStatus:     vpsassets.UsageInUse,
				RenewalDecision: vpsassets.RenewalKeep,
				CreatedAt:       now,
				UpdatedAt:       now,
			}}}, &fakeAssetLinkRepository{countActiveLinksForVPSErr: errors.New("count failed")}),
			method: http.MethodGet,
			path:   "/api/vps",
		},
		{
			name: "item node links failure",
			handler: handlers.VPSItem(&fakeVPSAssetRepository{getVPSAssetResult: vpsassets.Record{
				VPSID:           "vps_001",
				DisplayName:     "Tokyo Edge",
				SSHPort:         22,
				LifecycleStatus: vpsassets.LifecycleActive,
				UsageStatus:     vpsassets.UsageInUse,
				RenewalDecision: vpsassets.RenewalKeep,
				CreatedAt:       now,
				UpdatedAt:       now,
			}}, &fakeAssetLinkRepository{listNodesForVPSErr: errors.New("links failed")}),
			method: http.MethodGet,
			path:   "/api/vps/vps_001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
			}
		})
	}
}
