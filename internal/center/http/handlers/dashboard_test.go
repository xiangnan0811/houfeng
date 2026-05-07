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
		SnapshotGeneratedAt:        now,
		TotalNodeCount:             5,
		TotalTargetCount:           4,
		AbnormalNodeCount:          2,
		PendingOnboardingNodeCount: 1,
		PausedNodeCount:            1,
		RetiredNodeCount:           1,
		PausedTargetCount:          1,
		ArchivedTargetCount:        1,
		RecentNewIncidentCount:     3,
		GroupSummaries: []incidents.DashboardGroupSummary{{
			Group:                  "production",
			NodeCount:              3,
			TargetCount:            2,
			AbnormalNodeCount:      1,
			AbnormalTargetCount:    1,
			SevereNodeCount:        0,
			SevereTargetCount:      1,
			MaintenanceNodeCount:   1,
			MaintenanceTargetCount: 0,
		}},
		NotificationStatus: incidents.DashboardNotificationStatus{
			TelegramConfigured:         true,
			TelegramRuntimeManaged:     true,
			TelegramRuntimeApplyActive: true,
			FeishuConfigured:           false,
		},
		RecentEvents: []incidents.StateChangeEventRecord{{
			IncidentID:    "inc_001",
			IncidentClass: incidents.IncidentNodeDiskPressure,
			ObjectType:    incidents.ObjectTypeNode,
			ObjectID:      "nd_001",
			EventType:     incidents.EventIncidentStarted,
			Severity:      incidents.SeverityAlert,
			Summary:       "磁盘使用率 92.0%",
			CreatedAt:     now,
		}},
		AbnormalNodes: []incidents.DashboardNodeSummary{{
			NodeID:                     "nd_001",
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
	if body["total_node_count"] != float64(5) {
		t.Fatalf("body = %#v, want total_node_count=5", body)
	}
	if body["total_target_count"] != float64(4) {
		t.Fatalf("body = %#v, want total_target_count=4", body)
	}
	if body["abnormal_node_count"] != float64(2) {
		t.Fatalf("body = %#v, want abnormal_node_count=2", body)
	}
	if body["snapshot_generated_at"] != "2026-04-25T12:00:00Z" {
		t.Fatalf("body = %#v, want snapshot_generated_at", body)
	}
	if body["pending_onboarding_node_count"] != float64(1) || body["paused_node_count"] != float64(1) || body["retired_node_count"] != float64(1) {
		t.Fatalf("body = %#v, want node completeness counts", body)
	}
	if body["paused_target_count"] != float64(1) || body["archived_target_count"] != float64(1) {
		t.Fatalf("body = %#v, want target completeness counts", body)
	}
	if _, ok := body["AbnormalNodeCount"]; ok {
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
	if _, ok := groupSummary["NodeCount"]; ok {
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
	abnormalNodes, ok := body["abnormal_nodes"].([]any)
	if !ok || len(abnormalNodes) != 1 {
		t.Fatalf("body = %#v, want one abnormal node", body)
	}
	abnormalNode, ok := abnormalNodes[0].(map[string]any)
	if !ok {
		t.Fatalf("abnormalNodes[0] = %#v, want object", abnormalNodes[0])
	}
	if abnormalNode["node_id"] != "nd_001" || abnormalNode["current_primary_issue_summary"] != "磁盘使用率 92.0%" {
		t.Fatalf("abnormal node = %#v, want snake_case node summary", abnormalNode)
	}
	if _, ok := abnormalNode["NodeID"]; ok {
		t.Fatalf("abnormal node = %#v, want snake_case keys only", abnormalNode)
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
