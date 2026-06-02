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
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

type fakeAssetLifecycleRepository struct {
	previewResult                 assetlifecycle.CancellationPreview
	previewErr                    error
	previewVPSID                  string
	applyResult                   assetlifecycle.LifecycleActionResult
	applyErr                      error
	applyVPSID                    string
	applyInput                    assetlifecycle.ApplyCancellationInput
	extendResult                  assetlifecycle.LifecycleActionResult
	extendErr                     error
	extendVPSID                   string
	extendInput                   assetlifecycle.ExtendValidityInput
	monitoringInstanceContexts    []assetlifecycle.AssetContextForMonitoringInstance
	monitoringInstanceContextsErr error
	targetContexts                []assetlifecycle.AssetContextForTarget
	targetContextsErr             error
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

func (f *fakeAssetLifecycleRepository) ListMonitoringInstanceAssetContexts(context.Context) ([]assetlifecycle.AssetContextForMonitoringInstance, error) {
	return f.monitoringInstanceContexts, f.monitoringInstanceContextsErr
}

func (f *fakeAssetLifecycleRepository) ListTargetAssetContexts(context.Context) ([]assetlifecycle.AssetContextForTarget, error) {
	return f.targetContexts, f.targetContextsErr
}

func TestVPSCancellationPreviewReturnsImpactGraph(t *testing.T) {
	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	repo := &fakeAssetLifecycleRepository{previewResult: assetlifecycle.CancellationPreview{
		VPS: vpsassets.Record{
			VPSID:           "vps_001",
			DisplayName:     "Tokyo Edge",
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     vpsassets.UsageInUse,
			RenewalDecision: vpsassets.RenewalUnreviewed,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Subscriptions: []assetlifecycle.SubscriptionImpact{{
			Record:            subscriptions.Record{SubscriptionID: "sub_001", VPSID: "vps_001", Status: subscriptions.StatusExpired, CreatedAt: now, UpdatedAt: now},
			Role:              "inactive",
			RecommendedAction: "keep_inactive",
			Message:           "订阅账单记录已无续费动作，仍需处理 VPS、MonitoringInstance 与入口探测状态。",
		}},
		Warnings: []string{"关联订阅账单记录已无续费动作；这不是“没有关联订阅”，仍需处理 VPS、MonitoringInstance 与入口探测状态。"},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/cancellation-preview", nil)
	recorder := httptest.NewRecorder()
	handlers.VPSCancellationPreview(repo).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.previewVPSID != "vps_001" {
		t.Fatalf("preview vps id = %q, want vps_001", repo.previewVPSID)
	}
	var body assetlifecycle.CancellationPreview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.VPS.VPSID != "vps_001" || len(body.Subscriptions) != 1 || body.Subscriptions[0].Role != "inactive" {
		t.Fatalf("preview body = %#v, want inactive subscription impact", body)
	}
	if len(body.Warnings) != 1 || !strings.Contains(body.Warnings[0], "不是“没有关联订阅”") {
		t.Fatalf("warnings = %#v, want inactive subscription evidence", body.Warnings)
	}
}

func TestVPSCancellationAppliesConfirmedSelection(t *testing.T) {
	completedAt := time.Date(2026, time.May, 30, 9, 0, 0, 0, time.UTC)
	repo := &fakeAssetLifecycleRepository{applyResult: assetlifecycle.LifecycleActionResult{
		Action: assetlifecycle.LifecycleActionRecord{
			ActionID:    "ala_001",
			VPSID:       "vps_001",
			ActionType:  assetlifecycle.ActionTypeCancelVPS,
			Status:      assetlifecycle.ActionStatusCompleted,
			Reason:      "expired and will not renew",
			CompletedAt: &completedAt,
		},
		Steps: []assetlifecycle.LifecycleActionStep{{
			StepID:     "als_001",
			ActionID:   "ala_001",
			ObjectType: assetlifecycle.ObjectTypeVPS,
			ObjectID:   "vps_001",
			StepType:   assetlifecycle.StepTypeVPSLifecycle,
			Status:     assetlifecycle.StepStatusCompleted,
		}},
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/cancellation", strings.NewReader(`{
		"reason":" expired and will not renew ",
		"effective_date":"2026-05-30",
		"subscription_ids":[" sub_001 "],
		"vps_lifecycle_status":"cancelled",
		"monitoring_instance_actions":[{"monitoring_instance_id":" mi_001 ","lifecycle_status":"已退役","monitoring_status":"暂停"}],
		"target_actions":[{"target_id":" tg_001 ","run_status":"已归档"}]
	}`))
	recorder := httptest.NewRecorder()

	handlers.VPSCancellation(repo).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.applyVPSID != "vps_001" {
		t.Fatalf("apply vps id = %q, want vps_001", repo.applyVPSID)
	}
	if repo.applyInput.Reason != "expired and will not renew" {
		t.Fatalf("reason = %q, want trimmed", repo.applyInput.Reason)
	}
	if len(repo.applyInput.SubscriptionIDs) != 1 || repo.applyInput.SubscriptionIDs[0] != "sub_001" {
		t.Fatalf("subscription ids = %#v, want trimmed explicit selection", repo.applyInput.SubscriptionIDs)
	}
	if len(repo.applyInput.MonitoringInstanceActions) != 1 || repo.applyInput.MonitoringInstanceActions[0].MonitoringInstanceID != "mi_001" || repo.applyInput.MonitoringInstanceActions[0].LifecycleStatus != monitoringinstances.LifecycleRetired {
		t.Fatalf("monitoringInstance actions = %#v, want normalized action", repo.applyInput.MonitoringInstanceActions)
	}
	if len(repo.applyInput.TargetActions) != 1 || repo.applyInput.TargetActions[0].TargetID != "tg_001" || repo.applyInput.TargetActions[0].RunStatus != targets.RunStatusArchived {
		t.Fatalf("target actions = %#v, want normalized action", repo.applyInput.TargetActions)
	}

	var body assetlifecycle.LifecycleActionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Action.ActionID != "ala_001" || len(body.Steps) != 1 {
		t.Fatalf("response = %#v, want action and steps", body)
	}
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

func TestAssetLifecycleHandlersValidateInputAndMapErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "preview wrong method", handler: handlers.VPSCancellationPreview(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/cancellation-preview", want: http.StatusMethodNotAllowed},
		{name: "preview malformed path", handler: handlers.VPSCancellationPreview(&fakeAssetLifecycleRepository{}), method: http.MethodGet, path: "/api/vps/vps_001/cancellation-preview/extra", want: http.StatusNotFound},
		{name: "preview missing vps", handler: handlers.VPSCancellationPreview(&fakeAssetLifecycleRepository{previewErr: vpsassets.ErrVPSAssetNotFound}), method: http.MethodGet, path: "/api/vps/vps_missing/cancellation-preview", want: http.StatusNotFound},
		{name: "apply invalid json", handler: handlers.VPSCancellation(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/cancellation", body: `{`, want: http.StatusBadRequest},
		{name: "apply missing reason", handler: handlers.VPSCancellation(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/cancellation", body: `{"vps_lifecycle_status":"cancelled"}`, want: http.StatusBadRequest},
		{name: "apply invalid monitoringInstance action", handler: handlers.VPSCancellation(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/cancellation", body: `{"reason":"done","vps_lifecycle_status":"cancelled","monitoring_instance_actions":[{"monitoring_instance_id":"mi_001","lifecycle_status":"online"}]}`, want: http.StatusBadRequest},
		{name: "apply invalid associated object", handler: handlers.VPSCancellation(&fakeAssetLifecycleRepository{applyErr: assetlifecycle.ErrInvalidLifecycleActionInput}), method: http.MethodPost, path: "/api/vps/vps_001/cancellation", body: `{"reason":"done","vps_lifecycle_status":"cancelled"}`, want: http.StatusBadRequest},
		{name: "apply blocked lifecycle action", handler: handlers.VPSCancellation(&fakeAssetLifecycleRepository{applyErr: assetlifecycle.ErrLifecycleActionBlocked}), method: http.MethodPost, path: "/api/vps/vps_archived/cancellation", body: `{"reason":"done","vps_lifecycle_status":"cancelled"}`, want: http.StatusConflict},
		{name: "apply missing vps", handler: handlers.VPSCancellation(&fakeAssetLifecycleRepository{applyErr: vpsassets.ErrVPSAssetNotFound}), method: http.MethodPost, path: "/api/vps/vps_missing/cancellation", body: `{"reason":"done","vps_lifecycle_status":"cancelled"}`, want: http.StatusNotFound},
		{name: "apply repo failure", handler: handlers.VPSCancellation(&fakeAssetLifecycleRepository{applyErr: errors.New("boom")}), method: http.MethodPost, path: "/api/vps/vps_001/cancellation", body: `{"reason":"done","vps_lifecycle_status":"cancelled"}`, want: http.StatusInternalServerError},
		{name: "extend invalid json", handler: handlers.VPSExtendValidity(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/extend-validity", body: `{`, want: http.StatusBadRequest},
		{name: "extend missing date", handler: handlers.VPSExtendValidity(&fakeAssetLifecycleRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/extend-validity", body: `{"reason":"outage"}`, want: http.StatusBadRequest},
		{name: "extend blocked lifecycle action", handler: handlers.VPSExtendValidity(&fakeAssetLifecycleRepository{extendErr: assetlifecycle.ErrLifecycleActionBlocked}), method: http.MethodPost, path: "/api/vps/vps_001/extend-validity", body: `{"extend_to":"2026-12-01","reason":"outage"}`, want: http.StatusConflict},
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
		monitoringInstanceContexts: []assetlifecycle.AssetContextForMonitoringInstance{{
			MonitoringInstanceID:  "mi_001",
			LinkedVPSCount:        1,
			CancellationAttention: true,
		}},
		targetContexts: []assetlifecycle.AssetContextForTarget{{
			TargetID:              "tg_001",
			LinkedVPSCount:        1,
			CancellationAttention: true,
		}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/asset-context/monitoring-instances", nil)
	recorder := httptest.NewRecorder()
	handlers.AssetContextMonitoringInstances(repo).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("monitoringInstance context status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var monitoringInstanceBody []assetlifecycle.AssetContextForMonitoringInstance
	if err := json.Unmarshal(recorder.Body.Bytes(), &monitoringInstanceBody); err != nil {
		t.Fatalf("unmarshal monitoringInstance contexts: %v", err)
	}
	if len(monitoringInstanceBody) != 1 || monitoringInstanceBody[0].MonitoringInstanceID != "mi_001" || !monitoringInstanceBody[0].CancellationAttention {
		t.Fatalf("monitoringInstance contexts = %#v, want attention context", monitoringInstanceBody)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/asset-context/targets", nil)
	recorder = httptest.NewRecorder()
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
