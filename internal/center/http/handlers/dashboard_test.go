package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/incidents"
)

type fakeDashboardRepository struct {
	limit  int
	result incidents.DashboardOverview
	err    error
}

func (f *fakeDashboardRepository) GetDashboardOverview(_ context.Context, limit int) (incidents.DashboardOverview, error) {
	f.limit = limit
	return f.result, f.err
}

func TestDashboardHandlerReturnsOverview(t *testing.T) {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	repo := &fakeDashboardRepository{result: incidents.DashboardOverview{
		SnapshotGeneratedAt:                      now,
		TotalMonitoringInstanceCount:             5,
		TotalTargetCount:                         4,
		AbnormalMonitoringInstanceCount:          2,
		SevereMonitoringInstanceCount:            1,
		PendingOnboardingMonitoringInstanceCount: 1,
		PausedMonitoringInstanceCount:            1,
		RetiredMonitoringInstanceCount:           1,
		PausedTargetCount:                        1,
		ArchivedTargetCount:                      1,
		RecentNewIncidentCount:                   3,
		GroupSummaries: []incidents.DashboardGroupSummary{{
			Group:                              "production",
			MonitoringInstanceCount:            3,
			TargetCount:                        2,
			AbnormalMonitoringInstanceCount:    1,
			AbnormalTargetCount:                1,
			SevereMonitoringInstanceCount:      0,
			SevereTargetCount:                  1,
			MaintenanceMonitoringInstanceCount: 1,
			MaintenanceTargetCount:             0,
		}},
		NotificationStatus: incidents.DashboardNotificationStatus{
			TelegramConfigured:         true,
			TelegramRuntimeManaged:     true,
			TelegramRuntimeApplyActive: true,
			FeishuConfigured:           false,
		},
		AssetSummary: incidents.DashboardAssetSummary{
			RenewalDue30dSubscriptionCount: 3,
			RenewalDue30dVPSCount:          2,
			UnreviewedVPSCount:             4,
			ToCancelVPSCount:               1,
			CancelledVPSCount:              2,
			CancellationAttentionVPSCount:  3,
			RunningCancelledAssetCount:     4,
			ToMigrateVPSCount:              2,
			UnlinkedVPSCount:               5,
			AbnormalLinkedVPSCount:         1,
			CostByCurrency: []incidents.DashboardAssetCostByCurrency{{
				Currency:     "USD",
				MonthlyTotal: 42.5,
				YearlyTotal:  510,
			}},
		},
		RecentEvents: []incidents.StateChangeEventRecord{{
			IncidentID:    "inc_001",
			IncidentClass: incidents.IncidentMonitoringInstanceDiskPressure,
			ObjectType:    incidents.ObjectTypeMonitoringInstance,
			ObjectID:      "mi_001",
			EventType:     incidents.EventIncidentStarted,
			Severity:      incidents.SeverityAlert,
			Summary:       "磁盘使用率 92.0%",
			CreatedAt:     now,
		}},
		AbnormalMonitoringInstances: []incidents.DashboardMonitoringInstanceSummary{{
			MonitoringInstanceID:       "mi_001",
			DisplayName:                "Tokyo Edge",
			Region:                     "ap-northeast-1",
			City:                       "Tokyo",
			Provider:                   "aws",
			LifecycleStatus:            "在用",
			MonitoringStatus:           "启用",
			CurrentHealthStatus:        string(incidents.SeverityAlert),
			LastHeartbeatAt:            &now,
			CurrentActiveIncidentCount: 2,
			CurrentPrimaryIssueSummary: "磁盘使用率 92.0%",
		}},
		AbnormalTargets: []incidents.DashboardTargetSummary{{
			TargetID:                   "tg_001",
			Name:                       "Blog",
			TargetType:                 "service",
			Host:                       "blog.example.com",
			RunStatus:                  "启用",
			CurrentHealthStatus:        string(incidents.SeverityCritical),
			LastFailureAt:              &now,
			CurrentActiveIncidentCount: 1,
			CurrentPrimaryIssueSummary: "HTTPS 探测连续失败",
		}},
	}}

	handler := handlers.Dashboard(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?limit=5", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.limit != 5 {
		t.Fatalf("limit = %d, want 5", repo.limit)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body["total_monitoring_instance_count"] != float64(5) {
		t.Fatalf("body = %#v, want total_monitoring_instance_count=5", body)
	}
	if body["total_target_count"] != float64(4) {
		t.Fatalf("body = %#v, want total_target_count=4", body)
	}
	if body["abnormal_monitoring_instance_count"] != float64(2) {
		t.Fatalf("body = %#v, want abnormal_monitoring_instance_count=2", body)
	}
	if body["severe_monitoring_instance_count"] != float64(1) {
		t.Fatalf("body = %#v, want severe_monitoring_instance_count=1", body)
	}
	if body["severe_monitoring_instance_count"].(float64) > body["abnormal_monitoring_instance_count"].(float64) {
		t.Fatalf("body = %#v, severe monitoring instances must remain a subset of abnormal monitoring instances", body)
	}
	if body["snapshot_generated_at"] != "2026-04-25T12:00:00Z" {
		t.Fatalf("body = %#v, want snapshot_generated_at", body)
	}
	if body["pending_onboarding_monitoring_instance_count"] != float64(1) || body["paused_monitoring_instance_count"] != float64(1) || body["retired_monitoring_instance_count"] != float64(1) {
		t.Fatalf("body = %#v, want monitoring instance completeness counts", body)
	}
	if body["paused_target_count"] != float64(1) || body["archived_target_count"] != float64(1) {
		t.Fatalf("body = %#v, want target completeness counts", body)
	}
	if _, ok := body["AbnormalMonitoringInstanceCount"]; ok {
		t.Fatalf("body = %#v, want snake_case keys only", body)
	}
	groupSummaries, ok := body["group_summaries"].([]any)
	if !ok || len(groupSummaries) != 1 {
		t.Fatalf("body = %#v, want one group summary", body)
	}
	groupSummary, ok := groupSummaries[0].(map[string]any)
	if !ok {
		t.Fatalf("groupSummaries[0] = %#v, want object", groupSummaries[0])
	}
	if groupSummary["group"] != "production" || groupSummary["target_count"] != float64(2) {
		t.Fatalf("group summary = %#v, want snake_case group summary", groupSummary)
	}
	if _, ok := groupSummary["MonitoringInstanceCount"]; ok {
		t.Fatalf("group summary = %#v, want snake_case keys only", groupSummary)
	}
	notificationStatus, ok := body["notification_status"].(map[string]any)
	if !ok {
		t.Fatalf("body = %#v, want notification_status object", body)
	}
	if notificationStatus["telegram_configured"] != true || notificationStatus["telegram_runtime_apply_active"] != true || notificationStatus["feishu_configured"] != false {
		t.Fatalf("notification status = %#v, want boolean-only snake_case status", notificationStatus)
	}
	for _, secretKey := range []string{"telegram_bot_token", "telegram_chat_id", "feishu_webhook_url", "TelegramConfigured"} {
		if _, ok := notificationStatus[secretKey]; ok {
			t.Fatalf("notification status = %#v, want no secret or Go struct keys", notificationStatus)
		}
	}
	assetSummary, ok := body["asset_summary"].(map[string]any)
	if !ok {
		t.Fatalf("body = %#v, want asset_summary object", body)
	}
	for key, want := range map[string]float64{
		"renewal_due_30d_subscription_count": 3,
		"renewal_due_30d_vps_count":          2,
		"unreviewed_vps_count":               4,
		"to_cancel_vps_count":                1,
		"to_migrate_vps_count":               2,
		"unlinked_vps_count":                 5,
		"abnormal_linked_vps_count":          1,
	} {
		if assetSummary[key] != want {
			t.Fatalf("asset summary = %#v, want %s=%v", assetSummary, key, want)
		}
	}
	if _, ok := assetSummary["VPSAssets"]; ok {
		t.Fatalf("asset summary = %#v, want no asset detail dump", assetSummary)
	}
	costs, ok := assetSummary["cost_by_currency"].([]any)
	if !ok || len(costs) != 1 {
		t.Fatalf("asset summary = %#v, want one cost row", assetSummary)
	}
	cost, ok := costs[0].(map[string]any)
	if !ok {
		t.Fatalf("asset cost = %#v, want object", costs[0])
	}
	if cost["currency"] != "USD" || cost["monthly_total"] != 42.5 || cost["yearly_total"] != float64(510) {
		t.Fatalf("asset cost = %#v, want USD monthly/yearly totals", cost)
	}
	if _, ok := cost["MonthlyTotal"]; ok {
		t.Fatalf("asset cost = %#v, want snake_case keys only", cost)
	}
	abnormalMonitoringInstances, ok := body["abnormal_monitoring_instances"].([]any)
	if !ok || len(abnormalMonitoringInstances) != 1 {
		t.Fatalf("body = %#v, want one abnormal monitoringInstance", body)
	}
	abnormalMonitoringInstance, ok := abnormalMonitoringInstances[0].(map[string]any)
	if !ok {
		t.Fatalf("abnormalMonitoringInstances[0] = %#v, want object", abnormalMonitoringInstances[0])
	}
	if abnormalMonitoringInstance["monitoring_instance_id"] != "mi_001" || abnormalMonitoringInstance["current_primary_issue_summary"] != "磁盘使用率 92.0%" {
		t.Fatalf("abnormal monitoringInstance = %#v, want snake_case monitoringInstance summary", abnormalMonitoringInstance)
	}
	if _, ok := abnormalMonitoringInstance["MonitoringInstanceID"]; ok {
		t.Fatalf("abnormal monitoringInstance = %#v, want snake_case keys only", abnormalMonitoringInstance)
	}
	abnormalTargets, ok := body["abnormal_targets"].([]any)
	if !ok || len(abnormalTargets) != 1 {
		t.Fatalf("body = %#v, want one abnormal target", body)
	}
	abnormalTarget, ok := abnormalTargets[0].(map[string]any)
	if !ok {
		t.Fatalf("abnormalTargets[0] = %#v, want object", abnormalTargets[0])
	}
	if abnormalTarget["target_id"] != "tg_001" || abnormalTarget["current_primary_issue_summary"] != "HTTPS 探测连续失败" {
		t.Fatalf("abnormal target = %#v, want snake_case target summary", abnormalTarget)
	}
	recentEvents, ok := body["recent_events"].([]any)
	if !ok || len(recentEvents) != 1 {
		t.Fatalf("body = %#v, want one recent event", body)
	}
	event, ok := recentEvents[0].(map[string]any)
	if !ok {
		t.Fatalf("recentEvents[0] = %#v, want object", recentEvents[0])
	}
	for _, key := range []string{"incident_id", "incident_class", "object_type", "object_id", "event_type", "severity", "summary", "created_at"} {
		if _, ok := event[key]; !ok {
			t.Fatalf("event = %#v, want key %q", event, key)
		}
	}
	if _, ok := event["IncidentID"]; ok {
		t.Fatalf("event = %#v, want snake_case nested keys only", event)
	}
}

func TestDashboardHandlerRejectsInvalidLimit(t *testing.T) {
	handler := handlers.Dashboard(&fakeDashboardRepository{})
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?limit=abc", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
