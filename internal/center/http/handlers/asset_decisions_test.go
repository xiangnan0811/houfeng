package handlers_test

import (
	"bytes"
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
	overviewResult      assetdecisions.Overview
	overviewErr         error
	overviewFilters     assetdecisions.ListFilters
	groupsResult        []assetdecisions.GroupSummary
	groupsErr           error
	groupsFilters       assetdecisions.ListFilters
	groupResult         assetdecisions.GroupDetail
	groupErr            error
	groupID             string
	groupFilters        assetdecisions.ListFilters
	manualGroupsResult  []assetdecisions.ManualGroupSummary
	manualGroupsErr     error
	createManualInput   assetdecisions.CreateManualGroupInput
	createManualResult  assetdecisions.ManualGroupDetail
	createManualErr     error
	manualGroupResult   assetdecisions.ManualGroupDetail
	manualGroupErr      error
	manualGroupID       string
	patchManualID       string
	patchManualInput    assetdecisions.PatchManualGroupInput
	patchManualResult   assetdecisions.ManualGroupDetail
	patchManualErr      error
	addMemberGroupID    string
	addMemberInput      assetdecisions.CreateManualGroupMemberInput
	addMemberResult     assetdecisions.ManualGroupDetail
	addMemberErr        error
	patchMemberGroupID  string
	patchMemberVPSID    string
	patchMemberInput    assetdecisions.PatchManualGroupMemberInput
	patchMemberResult   assetdecisions.ManualGroupDetail
	patchMemberErr      error
	deleteMemberGroupID string
	deleteMemberVPSID   string
	deleteMemberResult  assetdecisions.ManualGroupDetail
	deleteMemberErr     error
	recordsResult       []assetdecisions.RecordSummary
	recordsErr          error
	createInput         assetdecisions.CreateRecordInput
	createResult        assetdecisions.RecordDetail
	createErr           error
	recordResult        assetdecisions.RecordDetail
	recordErr           error
	recordID            string
	patchID             string
	patchInput          assetdecisions.PatchRecordInput
	patchResult         assetdecisions.RecordDetail
	patchErr            error
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

func (f *fakeAssetDecisionRepository) ListManualGroups(context.Context) ([]assetdecisions.ManualGroupSummary, error) {
	if f.manualGroupsErr != nil {
		return nil, f.manualGroupsErr
	}
	return f.manualGroupsResult, nil
}

func (f *fakeAssetDecisionRepository) CreateManualGroup(_ context.Context, input assetdecisions.CreateManualGroupInput) (assetdecisions.ManualGroupDetail, error) {
	f.createManualInput = input
	if f.createManualErr != nil {
		return assetdecisions.ManualGroupDetail{}, f.createManualErr
	}
	return f.createManualResult, nil
}

func (f *fakeAssetDecisionRepository) GetManualGroup(_ context.Context, manualGroupID string) (assetdecisions.ManualGroupDetail, error) {
	f.manualGroupID = manualGroupID
	if f.manualGroupErr != nil {
		return assetdecisions.ManualGroupDetail{}, f.manualGroupErr
	}
	return f.manualGroupResult, nil
}

func (f *fakeAssetDecisionRepository) PatchManualGroup(_ context.Context, manualGroupID string, input assetdecisions.PatchManualGroupInput) (assetdecisions.ManualGroupDetail, error) {
	f.patchManualID = manualGroupID
	f.patchManualInput = input
	if f.patchManualErr != nil {
		return assetdecisions.ManualGroupDetail{}, f.patchManualErr
	}
	return f.patchManualResult, nil
}

func (f *fakeAssetDecisionRepository) AddManualGroupMember(_ context.Context, manualGroupID string, input assetdecisions.CreateManualGroupMemberInput) (assetdecisions.ManualGroupDetail, error) {
	f.addMemberGroupID = manualGroupID
	f.addMemberInput = input
	if f.addMemberErr != nil {
		return assetdecisions.ManualGroupDetail{}, f.addMemberErr
	}
	return f.addMemberResult, nil
}

func (f *fakeAssetDecisionRepository) PatchManualGroupMember(_ context.Context, manualGroupID string, vpsID string, input assetdecisions.PatchManualGroupMemberInput) (assetdecisions.ManualGroupDetail, error) {
	f.patchMemberGroupID = manualGroupID
	f.patchMemberVPSID = vpsID
	f.patchMemberInput = input
	if f.patchMemberErr != nil {
		return assetdecisions.ManualGroupDetail{}, f.patchMemberErr
	}
	return f.patchMemberResult, nil
}

func (f *fakeAssetDecisionRepository) DeleteManualGroupMember(_ context.Context, manualGroupID string, vpsID string) (assetdecisions.ManualGroupDetail, error) {
	f.deleteMemberGroupID = manualGroupID
	f.deleteMemberVPSID = vpsID
	if f.deleteMemberErr != nil {
		return assetdecisions.ManualGroupDetail{}, f.deleteMemberErr
	}
	return f.deleteMemberResult, nil
}

func (f *fakeAssetDecisionRepository) ListRecords(context.Context) ([]assetdecisions.RecordSummary, error) {
	if f.recordsErr != nil {
		return nil, f.recordsErr
	}
	return f.recordsResult, nil
}

func (f *fakeAssetDecisionRepository) CreateRecord(_ context.Context, input assetdecisions.CreateRecordInput) (assetdecisions.RecordDetail, error) {
	f.createInput = input
	if f.createErr != nil {
		return assetdecisions.RecordDetail{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeAssetDecisionRepository) GetRecord(_ context.Context, recordID string) (assetdecisions.RecordDetail, error) {
	f.recordID = recordID
	if f.recordErr != nil {
		return assetdecisions.RecordDetail{}, f.recordErr
	}
	return f.recordResult, nil
}

func (f *fakeAssetDecisionRepository) PatchRecord(_ context.Context, recordID string, input assetdecisions.PatchRecordInput) (assetdecisions.RecordDetail, error) {
	f.patchID = recordID
	f.patchInput = input
	if f.patchErr != nil {
		return assetdecisions.RecordDetail{}, f.patchErr
	}
	return f.patchResult, nil
}

func TestAssetDecisionOverviewReturnsSummary(t *testing.T) {
	repo := &fakeAssetDecisionRepository{overviewResult: assetdecisions.Overview{
		SnapshotGeneratedAt: time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC),
		RenewWithinDays:     60,
		GroupCount:          2,
	}}
	handler := handlers.AssetDecisionOverview(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/asset-decisions/overview?renew_within_days=60", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.overviewFilters.RenewWithinDays != 60 {
		t.Fatalf("filters = %#v, want renew window 60", repo.overviewFilters)
	}
	var body assetdecisions.Overview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.GroupCount != 2 || body.RenewWithinDays != 60 {
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

func TestAssetDecisionManualGroupsListCreateAndMemberMutations(t *testing.T) {
	now := time.Date(2026, time.June, 6, 9, 0, 0, 0, time.UTC)
	baseDetail := assetdecisions.ManualGroupDetail{
		ManualGroupSummary: assetdecisions.ManualGroupSummary{
			ManualGroupID:   "admg_001",
			Status:          assetdecisions.ManualGroupStatusActive,
			Scenario:        assetdecisions.ManualGroupScenarioPrimaryStandby,
			Title:           "德国主备取舍",
			Goal:            "保留一主一备",
			SourceType:      assetdecisions.RecordSourceAutoGroup,
			SourceGroupID:   "adg_auto_001",
			RenewWithinDays: 30,
			MemberCount:     1,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Members: []assetdecisions.ManualGroupMember{{
			VPSID:          "vps_001",
			ManualGroupID:  "admg_001",
			IntendedRole:   assetdecisions.RolePrimaryCandidate,
			IntendedAction: assetdecisions.ActionKeep,
			GroupMember: assetdecisions.GroupMember{
				VPS: vpsassets.Record{VPSID: "vps_001", DisplayName: "Frankfurt Primary"},
			},
			CurrentFactFound: true,
		}},
	}
	repo := &fakeAssetDecisionRepository{
		manualGroupsResult: []assetdecisions.ManualGroupSummary{baseDetail.ManualGroupSummary},
		createManualResult: baseDetail,
		manualGroupResult:  baseDetail,
		patchManualResult:  baseDetail,
		addMemberResult:    baseDetail,
		patchMemberResult:  baseDetail,
		deleteMemberResult: baseDetail,
	}

	collection := handlers.AssetDecisionManualGroups(repo)
	listRecorder := httptest.NewRecorder()
	collection.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/asset-decisions/manual-groups", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	var listBody []assetdecisions.ManualGroupSummary
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listBody) != 1 || listBody[0].ManualGroupID != "admg_001" {
		t.Fatalf("list body = %#v, want manual group", listBody)
	}

	createBody := []byte(`{"source_type":"auto_group","source_group_id":"adg_auto_001","scenario":"primary_standby","title":"德国主备取舍","goal":"保留一主一备","renew_within_days":30}`)
	createRecorder := httptest.NewRecorder()
	collection.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/api/asset-decisions/manual-groups", bytes.NewReader(createBody)))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	if repo.createManualInput.SourceType != assetdecisions.RecordSourceAutoGroup || repo.createManualInput.SourceGroupID != "adg_auto_001" || repo.createManualInput.Scenario != assetdecisions.ManualGroupScenarioPrimaryStandby {
		t.Fatalf("create input = %#v, want decoded manual group create", repo.createManualInput)
	}

	item := handlers.AssetDecisionManualGroup(repo)
	getRecorder := httptest.NewRecorder()
	item.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/asset-decisions/manual-groups/admg_001", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body=%s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	if repo.manualGroupID != "admg_001" {
		t.Fatalf("manualGroupID = %q, want admg_001", repo.manualGroupID)
	}

	patchRecorder := httptest.NewRecorder()
	item.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, "/api/asset-decisions/manual-groups/admg_001", bytes.NewReader([]byte(`{"status":"archived","note":"阶段完成"}`))))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want %d; body=%s", patchRecorder.Code, http.StatusOK, patchRecorder.Body.String())
	}
	if repo.patchManualID != "admg_001" || !repo.patchManualInput.Status.Set || repo.patchManualInput.Status.Value != assetdecisions.ManualGroupStatusArchived || !repo.patchManualInput.Note.Set || repo.patchManualInput.Note.Value != "阶段完成" {
		t.Fatalf("patch manual input = id %q %#v, want archived note", repo.patchManualID, repo.patchManualInput)
	}

	addRecorder := httptest.NewRecorder()
	item.ServeHTTP(addRecorder, httptest.NewRequest(http.MethodPost, "/api/asset-decisions/manual-groups/admg_001/members", bytes.NewReader([]byte(`{"vps_id":"vps_002","intended_role":"standby_candidate","intended_action":"observe","reason":"备用"}`))))
	if addRecorder.Code != http.StatusCreated {
		t.Fatalf("add status = %d, want %d; body=%s", addRecorder.Code, http.StatusCreated, addRecorder.Body.String())
	}
	if repo.addMemberGroupID != "admg_001" || repo.addMemberInput.VPSID != "vps_002" || repo.addMemberInput.IntendedAction != assetdecisions.ActionObserve {
		t.Fatalf("add member input = group %q %#v, want decoded member", repo.addMemberGroupID, repo.addMemberInput)
	}

	memberPatchRecorder := httptest.NewRecorder()
	item.ServeHTTP(memberPatchRecorder, httptest.NewRequest(http.MethodPatch, "/api/asset-decisions/manual-groups/admg_001/members/vps_002", bytes.NewReader([]byte(`{"intended_action":"migrate","sort_order":4}`))))
	if memberPatchRecorder.Code != http.StatusOK {
		t.Fatalf("member patch status = %d, want %d; body=%s", memberPatchRecorder.Code, http.StatusOK, memberPatchRecorder.Body.String())
	}
	if repo.patchMemberGroupID != "admg_001" || repo.patchMemberVPSID != "vps_002" || !repo.patchMemberInput.IntendedAction.Set || repo.patchMemberInput.IntendedAction.Value != assetdecisions.ActionMigrate || !repo.patchMemberInput.SortOrder.Set || repo.patchMemberInput.SortOrder.Value != 4 {
		t.Fatalf("patch member input = group %q vps %q %#v, want decoded patch", repo.patchMemberGroupID, repo.patchMemberVPSID, repo.patchMemberInput)
	}

	deleteRecorder := httptest.NewRecorder()
	item.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/asset-decisions/manual-groups/admg_001/members/vps_002", nil))
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d; body=%s", deleteRecorder.Code, http.StatusOK, deleteRecorder.Body.String())
	}
	if repo.deleteMemberGroupID != "admg_001" || repo.deleteMemberVPSID != "vps_002" {
		t.Fatalf("delete member = group %q vps %q, want decoded path", repo.deleteMemberGroupID, repo.deleteMemberVPSID)
	}
}

func TestAssetDecisionRecordsListAndCreate(t *testing.T) {
	now := time.Date(2026, time.June, 5, 9, 0, 0, 0, time.UTC)
	repo := &fakeAssetDecisionRepository{
		recordsResult: []assetdecisions.RecordSummary{{
			RecordID:        "adr_001",
			Title:           "德国主备取舍",
			Status:          assetdecisions.RecordStatusDraft,
			SourceGroupID:   "adg_auto_001",
			RenewWithinDays: 30,
			MemberCount:     2,
			ExecutionReadback: assetdecisions.RecordExecutionReadback{
				Status:             assetdecisions.ReadbackNeedsEvidence,
				Summary:            "1 台 VPS 仍需补证据",
				NeedsEvidenceCount: 1,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}},
		createResult: assetdecisions.RecordDetail{
			RecordSummary: assetdecisions.RecordSummary{
				RecordID:        "adr_created",
				Title:           "保存德国组",
				Status:          assetdecisions.RecordStatusDecided,
				SourceGroupID:   "adg_auto_001",
				RenewWithinDays: 30,
				MemberCount:     1,
				ExecutionReadback: assetdecisions.RecordExecutionReadback{
					Status:       assetdecisions.ReadbackAligned,
					Summary:      "当前事实与组合判断一致",
					AlignedCount: 1,
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
			Members: []assetdecisions.RecordMember{{
				RecordID:        "adr_created",
				VPSID:           "vps_001",
				DisplayName:     "Frankfurt Primary",
				DecidedRole:     assetdecisions.RolePrimaryCandidate,
				DecidedAction:   assetdecisions.ActionKeep,
				SuggestedRole:   assetdecisions.RolePrimaryCandidate,
				SuggestedAction: assetdecisions.ActionKeep,
				ExecutionReadback: assetdecisions.MemberExecutionReadback{
					Status:  assetdecisions.ReadbackAligned,
					Summary: "当前事实与判断一致",
					CurrentFacts: assetdecisions.ExecutionCurrentFacts{
						Found: true,
					},
				},
			}},
		},
	}
	handler := handlers.AssetDecisionRecords(repo)

	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/asset-decisions/records", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	var listBody []assetdecisions.RecordSummary
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("unmarshal list body: %v", err)
	}
	if len(listBody) != 1 || listBody[0].RecordID != "adr_001" {
		t.Fatalf("list body = %#v, want record summary", listBody)
	}
	if listBody[0].ExecutionReadback.Status != assetdecisions.ReadbackNeedsEvidence || listBody[0].ExecutionReadback.NeedsEvidenceCount != 1 {
		t.Fatalf("list readback = %#v, want serialized execution readback", listBody[0].ExecutionReadback)
	}

	body := []byte(`{"source_group_id":"adg_auto_001","renew_within_days":30,"title":"保存德国组","goal":"保留主力","status":"decided","members":[{"vps_id":"vps_001","decided_role":"primary_candidate","decided_action":"keep","reason":"主力"}]}`)
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/api/asset-decisions/records", bytes.NewReader(body)))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	if repo.createInput.SourceGroupID != "adg_auto_001" || repo.createInput.Status != assetdecisions.RecordStatusDecided || len(repo.createInput.Members) != 1 {
		t.Fatalf("create input = %#v, want decoded payload", repo.createInput)
	}
	var createBody assetdecisions.RecordDetail
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("unmarshal create body: %v", err)
	}
	if createBody.RecordID != "adr_created" || len(createBody.Members) != 1 {
		t.Fatalf("create body = %#v, want created detail", createBody)
	}
	if createBody.ExecutionReadback.Status != assetdecisions.ReadbackAligned || createBody.Members[0].ExecutionReadback.Status != assetdecisions.ReadbackAligned {
		t.Fatalf("create readback = %#v member=%#v, want serialized execution readback", createBody.ExecutionReadback, createBody.Members[0].ExecutionReadback)
	}
}

func TestAssetDecisionRecordGetAndPatch(t *testing.T) {
	now := time.Date(2026, time.June, 5, 9, 0, 0, 0, time.UTC)
	repo := &fakeAssetDecisionRepository{
		recordResult: assetdecisions.RecordDetail{
			RecordSummary: assetdecisions.RecordSummary{
				RecordID:    "adr_001",
				Title:       "服务商组合",
				Status:      assetdecisions.RecordStatusDraft,
				MemberCount: 1,
				ExecutionReadback: assetdecisions.RecordExecutionReadback{
					Status:    assetdecisions.ReadbackOpen,
					Summary:   "1 台 VPS 仍待执行或复核",
					OpenCount: 1,
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
			Members: []assetdecisions.RecordMember{{
				RecordID:    "adr_001",
				VPSID:       "vps_001",
				DisplayName: "Frankfurt Primary",
				ExecutionReadback: assetdecisions.MemberExecutionReadback{
					Status:  assetdecisions.ReadbackOpen,
					Summary: "当前事实仍待执行或复核",
					CurrentFacts: assetdecisions.ExecutionCurrentFacts{
						Found:                   true,
						ActiveSubscriptionCount: 1,
					},
				},
			}},
		},
		patchResult: assetdecisions.RecordDetail{
			RecordSummary: assetdecisions.RecordSummary{
				RecordID:             "adr_001",
				Title:                "服务商组合",
				Status:               assetdecisions.RecordStatusInProgress,
				MemberCount:          1,
				FollowupBlockedCount: 1,
				ExecutionReadback: assetdecisions.RecordExecutionReadback{
					Status:       assetdecisions.ReadbackBlocked,
					Summary:      "1 台 VPS 跟进阻塞",
					BlockedCount: 1,
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
			Members: []assetdecisions.RecordMember{{
				RecordID:       "adr_001",
				VPSID:          "vps_001",
				DisplayName:    "Frankfurt Primary",
				FollowupStatus: assetdecisions.FollowupBlocked,
				FollowupNote:   "",
				ExecutionReadback: assetdecisions.MemberExecutionReadback{
					Status:  assetdecisions.ReadbackBlocked,
					Summary: "成员跟进阻塞",
					CurrentFacts: assetdecisions.ExecutionCurrentFacts{
						Found: true,
					},
				},
			}},
		},
	}
	handler := handlers.AssetDecisionRecord(repo)

	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/asset-decisions/records/adr_001", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body=%s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	if repo.recordID != "adr_001" {
		t.Fatalf("recordID = %q, want adr_001", repo.recordID)
	}
	var getBody assetdecisions.RecordDetail
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("unmarshal get body: %v", err)
	}
	if getBody.ExecutionReadback.Status != assetdecisions.ReadbackOpen || len(getBody.Members) != 1 || !getBody.Members[0].ExecutionReadback.CurrentFacts.Found {
		t.Fatalf("get readback = %#v members=%#v, want serialized execution readback", getBody.ExecutionReadback, getBody.Members)
	}

	patchRecorder := httptest.NewRecorder()
	handler.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, "/api/asset-decisions/records/adr_001", bytes.NewReader([]byte(`{"status":"in_progress","goal":"推进迁移","members":[{"vps_id":"vps_001","followup_status":"blocked","followup_note":""}]}`))))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want %d; body=%s", patchRecorder.Code, http.StatusOK, patchRecorder.Body.String())
	}
	if repo.patchID != "adr_001" || !repo.patchInput.Status.Set || repo.patchInput.Status.Value != assetdecisions.RecordStatusInProgress || !repo.patchInput.Goal.Set || repo.patchInput.Goal.Value != "推进迁移" || len(repo.patchInput.Members) != 1 {
		t.Fatalf("patch request = id %q input %#v, want decoded patch", repo.patchID, repo.patchInput)
	}
	memberPatch := repo.patchInput.Members[0]
	if memberPatch.VPSID != "vps_001" || !memberPatch.FollowupStatus.Set || memberPatch.FollowupStatus.Value != assetdecisions.FollowupBlocked || !memberPatch.FollowupNote.Set || memberPatch.FollowupNote.Value != "" {
		t.Fatalf("member patch = %#v, want blocked status and empty note set", memberPatch)
	}
	var patchBody assetdecisions.RecordDetail
	if err := json.Unmarshal(patchRecorder.Body.Bytes(), &patchBody); err != nil {
		t.Fatalf("unmarshal patch body: %v", err)
	}
	if patchBody.Status != assetdecisions.RecordStatusInProgress || patchBody.FollowupBlockedCount != 1 || len(patchBody.Members) != 1 || patchBody.Members[0].FollowupStatus != assetdecisions.FollowupBlocked {
		t.Fatalf("patch body = %#v, want in_progress with blocked followup", patchBody)
	}
	if patchBody.ExecutionReadback.Status != assetdecisions.ReadbackBlocked || patchBody.Members[0].ExecutionReadback.Status != assetdecisions.ReadbackBlocked {
		t.Fatalf("patch readback = %#v member=%#v, want serialized execution readback", patchBody.ExecutionReadback, patchBody.Members[0].ExecutionReadback)
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
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/groups?renew_within_days=45", nil),
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
			name:     "records invalid json",
			handler:  handlers.AssetDecisionRecords(&fakeAssetDecisionRepository{}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/records", bytes.NewReader([]byte(`{"source_group_id":`))),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "records invalid input",
			handler:  handlers.AssetDecisionRecords(&fakeAssetDecisionRepository{createErr: assetdecisions.ErrInvalidAssetDecisionInput}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/records", bytes.NewReader([]byte(`{"source_group_id":"adg_auto_001"}`))),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "records missing group",
			handler:  handlers.AssetDecisionRecords(&fakeAssetDecisionRepository{createErr: assetdecisions.ErrAssetDecisionGroupNotFound}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/records", bytes.NewReader([]byte(`{"source_group_id":"adg_auto_missing"}`))),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "records missing manual group",
			handler:  handlers.AssetDecisionRecords(&fakeAssetDecisionRepository{createErr: assetdecisions.ErrAssetDecisionManualGroupNotFound}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/records", bytes.NewReader([]byte(`{"source_type":"manual_group","source_group_id":"admg_missing"}`))),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "records list failure",
			handler:  handlers.AssetDecisionRecords(&fakeAssetDecisionRepository{recordsErr: errors.New("boom")}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/records", nil),
			wantCode: http.StatusInternalServerError,
		},
		{
			name:     "record missing",
			handler:  handlers.AssetDecisionRecord(&fakeAssetDecisionRepository{recordErr: assetdecisions.ErrAssetDecisionRecordNotFound}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/records/adr_missing", nil),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "record patch invalid",
			handler:  handlers.AssetDecisionRecord(&fakeAssetDecisionRepository{patchErr: assetdecisions.ErrInvalidAssetDecisionInput}),
			request:  httptest.NewRequest(http.MethodPatch, "/api/asset-decisions/records/adr_001", bytes.NewReader([]byte(`{"status":"bad"}`))),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "record patch failure",
			handler:  handlers.AssetDecisionRecord(&fakeAssetDecisionRepository{patchErr: errors.New("boom")}),
			request:  httptest.NewRequest(http.MethodPatch, "/api/asset-decisions/records/adr_001", bytes.NewReader([]byte(`{"status":"completed"}`))),
			wantCode: http.StatusInternalServerError,
		},
		{
			name:     "method not allowed",
			handler:  handlers.AssetDecisionGroups(&fakeAssetDecisionRepository{}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/groups", nil),
			wantCode: http.StatusMethodNotAllowed,
		},
		{
			name:     "manual groups create invalid json",
			handler:  handlers.AssetDecisionManualGroups(&fakeAssetDecisionRepository{}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/manual-groups", bytes.NewReader([]byte(`{"title":`))),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "manual groups create missing auto source",
			handler:  handlers.AssetDecisionManualGroups(&fakeAssetDecisionRepository{createManualErr: assetdecisions.ErrAssetDecisionGroupNotFound}),
			request:  httptest.NewRequest(http.MethodPost, "/api/asset-decisions/manual-groups", bytes.NewReader([]byte(`{"source_type":"auto_group","source_group_id":"adg_auto_missing","title":"x"}`))),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "manual groups list failure",
			handler:  handlers.AssetDecisionManualGroups(&fakeAssetDecisionRepository{manualGroupsErr: errors.New("boom")}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/manual-groups", nil),
			wantCode: http.StatusInternalServerError,
		},
		{
			name:     "manual group missing",
			handler:  handlers.AssetDecisionManualGroup(&fakeAssetDecisionRepository{manualGroupErr: assetdecisions.ErrAssetDecisionManualGroupNotFound}),
			request:  httptest.NewRequest(http.MethodGet, "/api/asset-decisions/manual-groups/admg_missing", nil),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "manual group member missing",
			handler:  handlers.AssetDecisionManualGroup(&fakeAssetDecisionRepository{patchMemberErr: assetdecisions.ErrAssetDecisionManualGroupMemberNotFound}),
			request:  httptest.NewRequest(http.MethodPatch, "/api/asset-decisions/manual-groups/admg_001/members/vps_missing", bytes.NewReader([]byte(`{"note":"x"}`))),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "manual group member method not allowed",
			handler:  handlers.AssetDecisionManualGroup(&fakeAssetDecisionRepository{}),
			request:  httptest.NewRequest(http.MethodPut, "/api/asset-decisions/manual-groups/admg_001/members/vps_001", nil),
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
