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

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/http/handlers"

	"houfeng/internal/center/subscriptions"

	"houfeng/internal/center/vpsassets"
)

type fakeAssetLifecycleRepository struct {
	previewResult       assetlifecycle.CancellationPreview
	previewErr          error
	previewVPSID        string
	applyResult         assetlifecycle.LifecycleActionResult
	applyErr            error
	applyVPSID          string
	applyInput          assetlifecycle.ApplyCancellationInput
	extendResult        assetlifecycle.LifecycleActionResult
	extendErr           error
	extendVPSID         string
	extendInput         assetlifecycle.ExtendValidityInput
	archiveReviewResult assetlifecycle.ArchiveReview
	archiveReviewErr    error
	archiveReviewVPSID  string
	archiveResult       assetlifecycle.ArchiveReview
	archiveErr          error
	archiveVPSID        string
	archiveInput        assetlifecycle.ApplyArchiveInput
	restoreResult       vpsassets.Record
	restoreErr          error
	restoreVPSID        string
	targetContexts      []assetlifecycle.AssetContextForTarget
	targetContextsErr   error
}

func (f *fakeAssetLifecycleRepository) GetVPSCancellationPreview(_ context.Context, vpsID string) (assetlifecycle.CancellationPreview, error) {
	f.previewVPSID = vpsID
	if f.previewErr != nil {
		return assetlifecycle.CancellationPreview{}, f.previewErr
	}
	return f.previewResult, nil
}

func (f *fakeAssetLifecycleRepository) ApplyVPSCancellation(_ context.Context, vpsID string, input assetlifecycle.ApplyCancellationInput) (assetlifecycle.LifecycleActionResult, error) {
	f.applyVPSID = vpsID
	f.applyInput = input
	if f.applyErr != nil {
		return assetlifecycle.LifecycleActionResult{}, f.applyErr
	}
	return f.applyResult, nil
}

func (f *fakeAssetLifecycleRepository) ExtendVPSValidity(_ context.Context, vpsID string, input assetlifecycle.ExtendValidityInput) (assetlifecycle.LifecycleActionResult, error) {
	f.extendVPSID = vpsID
	f.extendInput = input
	if f.extendErr != nil {
		return assetlifecycle.LifecycleActionResult{}, f.extendErr
	}
	return f.extendResult, nil
}

func (f *fakeAssetLifecycleRepository) GetVPSArchiveReview(_ context.Context, vpsID string) (assetlifecycle.ArchiveReview, error) {
	f.archiveReviewVPSID = vpsID
	if f.archiveReviewErr != nil {
		return assetlifecycle.ArchiveReview{}, f.archiveReviewErr
	}
	return f.archiveReviewResult, nil
}

func (f *fakeAssetLifecycleRepository) ApplyVPSArchive(_ context.Context, vpsID string, input assetlifecycle.ApplyArchiveInput) (assetlifecycle.ArchiveReview, error) {
	f.archiveVPSID = vpsID
	f.archiveInput = input
	if f.archiveErr != nil {
		return assetlifecycle.ArchiveReview{}, f.archiveErr
	}
	return f.archiveResult, nil
}

func (f *fakeAssetLifecycleRepository) RestoreVPSFromArchive(_ context.Context, vpsID string, _ assetlifecycle.RestoreArchiveInput) (vpsassets.Record, error) {
	f.restoreVPSID = vpsID
	if f.restoreErr != nil {
		return vpsassets.Record{}, f.restoreErr
	}
	return f.restoreResult, nil
}

func (f *fakeAssetLifecycleRepository) ListTargetAssetContexts(context.Context) ([]assetlifecycle.AssetContextForTarget, error) {
	return f.targetContexts, f.targetContextsErr
}

func TestVPSExtendValidityUpdatesActiveSubscription(t *testing.T) {
	completedAt := time.Date(2026, time.May, 30, 9, 0, 0, 0, time.UTC)
	extendTo := subscriptions.NewDate(time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC))
	repo := &fakeAssetLifecycleRepository{extendResult: assetlifecycle.LifecycleActionResult{
		Action: assetlifecycle.LifecycleActionRecord{
			ActionID:      "ala_extend",
			VPSID:         "vps_001",
			ActionType:    assetlifecycle.ActionTypeExtendValidity,
			Status:        assetlifecycle.ActionStatusCompleted,
			Reason:        "provider outage compensation",
			EffectiveDate: &extendTo,
			CompletedAt:   &completedAt,
		},
		Steps: []assetlifecycle.LifecycleActionStep{{
			StepID:     "als_extend",
			ActionID:   "ala_extend",
			ObjectType: assetlifecycle.ObjectTypeSubscription,
			ObjectID:   "sub_001",
			StepType:   assetlifecycle.StepTypeSubscriptionRenewAt,
			Status:     assetlifecycle.StepStatusCompleted,
		}},
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/extend-validity", strings.NewReader(`{
		"extend_to":"2026-12-01",
		"reason":" provider outage compensation ",
		"fee":0,
		"fee_currency":" usd ",
		"source_type":" outage_compensation "
	}`))
	recorder := httptest.NewRecorder()

	handlers.VPSExtendValidity(repo).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.extendVPSID != "vps_001" {
		t.Fatalf("extend vps id = %q, want vps_001", repo.extendVPSID)
	}
	if repo.extendInput.ExtendTo == nil || repo.extendInput.ExtendTo.Time.Format(subscriptions.DateLayout) != "2026-12-01" {
		t.Fatalf("extend_to = %#v, want 2026-12-01", repo.extendInput.ExtendTo)
	}
	if repo.extendInput.Reason != "provider outage compensation" || repo.extendInput.FeeCurrency != "USD" || repo.extendInput.SourceType != "outage_compensation" {
		t.Fatalf("extend input = %#v, want normalized values", repo.extendInput)
	}

	var body assetlifecycle.LifecycleActionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Action.ActionType != assetlifecycle.ActionTypeExtendValidity || len(body.Steps) != 1 {
		t.Fatalf("response = %#v, want validity extension action", body)
	}
}

func TestVPSArchiveReviewReturnsEligibilityAndBlockers(t *testing.T) {
	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	repo := &fakeAssetLifecycleRepository{archiveReviewResult: assetlifecycle.ArchiveReview{
		VPS: vpsassets.Record{
			VPSID:           "vps_001",
			DisplayName:     "Tokyo Edge",
			LifecycleStatus: vpsassets.LifecycleCancelled,
			UsageStatus:     vpsassets.UsageIdle,
			RenewalDecision: vpsassets.RenewalCancel,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Subscriptions: []assetlifecycle.SubscriptionImpact{{
			Record:            subscriptions.Record{SubscriptionID: "sub_001", VPSID: "vps_001", Status: subscriptions.StatusCancelled, CreatedAt: now, UpdatedAt: now},
			Role:              "inactive",
			RecommendedAction: "keep_inactive",
			Message:           "订阅账单记录已无续费动作。",
		}},
		Eligible: true,
		Blockers: []string{},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/archive-review", nil)
	recorder := httptest.NewRecorder()
	handlers.VPSArchiveReview(repo).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.archiveReviewVPSID != "vps_001" {
		t.Fatalf("archive review vps id = %q, want vps_001", repo.archiveReviewVPSID)
	}
	var body assetlifecycle.ArchiveReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.VPS.VPSID != "vps_001" || !body.Eligible || len(body.Subscriptions) != 1 {
		t.Fatalf("archive review body = %#v, want eligible review with subscription evidence", body)
	}
}

func TestVPSArchiveAppliesStrongConfirmation(t *testing.T) {
	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	repo := &fakeAssetLifecycleRepository{archiveResult: assetlifecycle.ArchiveReview{
		VPS: vpsassets.Record{
			VPSID:           "vps_001",
			DisplayName:     "Tokyo Edge",
			LifecycleStatus: vpsassets.LifecycleArchived,
			UsageStatus:     vpsassets.UsageIdle,
			RenewalDecision: vpsassets.RenewalCancel,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Eligible: false,
		Blockers: []string{"VPS 已归档，只读保留历史。"},
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/archive", strings.NewReader(`{
		"preview_digest":"preview", "idempotency_key":"request", "confirmation_name":" Tokyo Edge ", "reason":"账单与运行残留已整理"
	}`))
	recorder := httptest.NewRecorder()

	handlers.VPSArchive(repo).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.archiveVPSID != "vps_001" {
		t.Fatalf("archive vps id = %q, want vps_001", repo.archiveVPSID)
	}
	if repo.archiveInput.ConfirmationName != "Tokyo Edge" {
		t.Fatalf("confirmation name = %q, want trimmed display name", repo.archiveInput.ConfirmationName)
	}
	var body assetlifecycle.ArchiveReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.VPS.LifecycleStatus != vpsassets.LifecycleArchived {
		t.Fatalf("archive response = %#v, want archived review", body)
	}
}

func TestVPSRestoreFromArchiveReturnsRestoredAsset(t *testing.T) {
	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	repo := &fakeAssetLifecycleRepository{restoreResult: vpsassets.Record{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo Edge",
		LifecycleStatus: vpsassets.LifecycleIdle,
		UsageStatus:     vpsassets.UsageIdle,
		RenewalDecision: vpsassets.RenewalCancel,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/restore-from-archive", strings.NewReader(`{"reason":"重新整理用途"}`))
	recorder := httptest.NewRecorder()

	handlers.VPSRestoreFromArchive(repo).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.restoreVPSID != "vps_001" {
		t.Fatalf("restore vps id = %q, want vps_001", repo.restoreVPSID)
	}
	var body vpsassets.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.VPSID != "vps_001" || body.LifecycleStatus != vpsassets.LifecycleIdle {
		t.Fatalf("restore response = %#v, want idle vps", body)
	}
}

func TestAssetLifecycleHandlersValidateInputAndMapErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "extend invalid json", handler: handlers.VPSExtendValidity(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/extend-validity", body: `{`, want: http.StatusBadRequest},
		{name: "extend missing date", handler: handlers.VPSExtendValidity(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/extend-validity", body: `{"reason":"outage"}`, want: http.StatusBadRequest},
		{name: "extend blocked lifecycle action", handler: handlers.VPSExtendValidity(&fakeAssetLifecycleRepository{extendErr: assetlifecycle.ErrLifecycleActionBlocked}), method: http.MethodPost, path: "/api/vps/vps_001/extend-validity", body: `{"extend_to":"2026-12-01","reason":"outage"}`, want: http.StatusConflict},
		{name: "archive review wrong method", handler: handlers.VPSArchiveReview(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/archive-review", want: http.StatusMethodNotAllowed},
		{name: "archive review malformed path", handler: handlers.VPSArchiveReview(&fakeAssetLifecycleRepository{}), method: http.MethodGet, path: "/api/vps/vps_001/archive-review/extra", want: http.StatusNotFound},
		{name: "archive review missing vps", handler: handlers.VPSArchiveReview(&fakeAssetLifecycleRepository{archiveReviewErr: vpsassets.ErrVPSAssetNotFound}), method: http.MethodGet, path: "/api/vps/vps_missing/archive-review", want: http.StatusNotFound},
		{name: "archive invalid json", handler: handlers.VPSArchive(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/archive", body: `{`, want: http.StatusBadRequest},
		{name: "archive missing confirmation", handler: handlers.VPSArchive(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/archive", body: `{}`, want: http.StatusBadRequest},
		{name: "archive blocked lifecycle action", handler: handlers.VPSArchive(&fakeAssetLifecycleRepository{archiveErr: assetlifecycle.ErrLifecycleActionBlocked}), method: http.MethodPost, path: "/api/vps/vps_001/archive", body: `{"preview_digest":"preview", "idempotency_key":"request", "confirmation_name":"Tokyo Edge","reason":"done"}`, want: http.StatusConflict},
		{name: "archive missing vps", handler: handlers.VPSArchive(&fakeAssetLifecycleRepository{archiveErr: vpsassets.ErrVPSAssetNotFound}), method: http.MethodPost, path: "/api/vps/vps_missing/archive", body: `{"preview_digest":"preview", "idempotency_key":"request", "confirmation_name":"Tokyo Edge","reason":"done"}`, want: http.StatusNotFound},
		{name: "archive repo failure", handler: handlers.VPSArchive(&fakeAssetLifecycleRepository{archiveErr: errors.New("boom")}), method: http.MethodPost, path: "/api/vps/vps_001/archive", body: `{"preview_digest":"preview", "idempotency_key":"request", "confirmation_name":"Tokyo Edge","reason":"done"}`, want: http.StatusInternalServerError},
		{name: "restore wrong method", handler: handlers.VPSRestoreFromArchive(&fakeAssetLifecycleRepository{}), method: http.MethodGet, path: "/api/vps/vps_001/restore-from-archive", want: http.StatusMethodNotAllowed},
		{name: "restore blocked lifecycle action", handler: handlers.VPSRestoreFromArchive(&fakeAssetLifecycleRepository{restoreErr: assetlifecycle.ErrLifecycleActionBlocked}), method: http.MethodPost, path: "/api/vps/vps_cancelled/restore-from-archive", body: `{"reason":"整理恢复"}`, want: http.StatusConflict},
		{name: "restore missing vps", handler: handlers.VPSRestoreFromArchive(&fakeAssetLifecycleRepository{restoreErr: vpsassets.ErrVPSAssetNotFound}), method: http.MethodPost, path: "/api/vps/vps_missing/restore-from-archive", body: `{"reason":"整理恢复"}`, want: http.StatusNotFound},
		{name: "restore repo failure", handler: handlers.VPSRestoreFromArchive(&fakeAssetLifecycleRepository{restoreErr: errors.New("boom")}), method: http.MethodPost, path: "/api/vps/vps_001/restore-from-archive", body: `{"reason":"整理恢复"}`, want: http.StatusInternalServerError},
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

func TestAssetContextHandlersReturnBatchContexts(t *testing.T) {
	repo := &fakeAssetLifecycleRepository{
		targetContexts: []assetlifecycle.AssetContextForTarget{{
			TargetID:              "tg_001",
			LinkedVPSCount:        1,
			CancellationAttention: true,
		}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/asset-context/targets", nil)
	recorder := httptest.NewRecorder()
	handlers.AssetContextTargets(repo).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("target context status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var targetBody []assetlifecycle.AssetContextForTarget
	if err := json.Unmarshal(recorder.Body.Bytes(), &targetBody); err != nil {
		t.Fatalf("unmarshal target contexts: %v", err)
	}
	if len(targetBody) != 1 || targetBody[0].TargetID != "tg_001" || !targetBody[0].CancellationAttention {
		t.Fatalf("target contexts = %#v, want attention context", targetBody)
	}
}
