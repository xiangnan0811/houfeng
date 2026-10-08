package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/targets"
)

func TestPostgresIntegrationTargetDashboardProjectionAndCoverage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "target-dashboard-projection", 4)

	if _, err := fixture.db.Exec(ctx, `
		insert into vps_assets(vps_id, display_name, lifecycle_status, usage_status)
		values
			('vps_exec_enabled', 'Executor enabled', 'active', 'in_use'),
			('vps_exec_paused', 'Executor paused', 'active', 'in_use'),
			('vps_exec_retired', 'Executor retired', 'active', 'in_use'),
			('vps_exec_archived', 'Executor archived', 'active', 'in_use'),
			('vps_target_archived', 'Archived target host', 'archived', 'unknown')
	`); err != nil {
		t.Fatalf("insert target projection VPS fixtures: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_instances(
			monitoring_instance_id, vps_id, display_name, "group", region, city, provider,
			labels, lifecycle_status, monitoring_status, binding_status, current_health_status,
			ever_connected, last_trusted_online_at, archived_at
		) values
			('mi_exec_enabled', 'vps_exec_enabled', 'Executor enabled', 'edge', '', '', '', array['edge'], '已接入', '启用', '已绑定', '告警', true, now(), null),
			('mi_exec_paused', 'vps_exec_paused', 'Executor paused', 'edge', '', '', '', array['edge'], '已接入', '暂停', '已绑定', '正常', true, now(), null),
			('mi_exec_retired', 'vps_exec_retired', 'Executor retired', 'edge', '', '', '', array['edge'], '已退役', '启用', '已绑定', '正常', true, now(), null),
			('mi_exec_archived', 'vps_exec_archived', 'Executor archived', 'edge', '', '', '', array['edge'], '已接入', '启用', '已绑定', '正常', true, now(), now())
	`); err != nil {
		t.Fatalf("insert target projection monitoring fixtures: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into targets(
			target_id, name, target_type, host, execution_monitoring_instance_labels,
			lifecycle_status, run_status, "group", current_health_status,
			last_success_at, last_failure_at
		) values
			('tg_normal', 'Observed normal', 'service', 'normal.example.test', array['edge'], 'active', '启用', 'normal', '正常', now(), null),
			('tg_unobserved', 'Unobserved target', 'service', 'unobserved.example.test', array['edge'], 'active', '启用', 'edge', '严重', null, null),
			('tg_shared_abnormal', 'Shared abnormal target', 'service', 'shared.example.test', array['edge'], 'active', '启用', 'edge', '关注', null, now()),
			('tg_severe', 'Severe target', 'service', 'severe.example.test', array['edge'], 'active', '启用', 'edge', '严重', null, now()),
			('tg_maintenance', 'Maintenance target', 'service', 'maintenance.example.test', array['edge'], 'active', '维护中', 'controls', '严重', null, null),
			('tg_paused', 'Paused target', 'service', 'paused.example.test', array['edge'], 'active', '暂停', 'controls', '严重', null, null),
			('tg_archived_only', 'Archived-only target', 'service', 'archived.example.test', array['edge'], 'active', '启用', 'history', '关注', null, null),
			('tg_retired', 'Retired target', 'service', 'retired.example.test', array['edge'], 'retired', '启用', 'history', '严重', null, now())
	`); err != nil {
		t.Fatalf("insert target projection target fixtures: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into probe_items(probe_item_id, target_id, probe_kind, enabled, frequency_tier, timeout_seconds, config)
		values
			('pb_shared_http', 'tg_shared_abnormal', 'http', true, '5s', 3, '{}'::jsonb),
			('pb_shared_tcp', 'tg_shared_abnormal', 'tcp', true, '5s', 3, '{}'::jsonb),
			('pb_shared_disabled', 'tg_shared_abnormal', 'tls', false, '5s', 3, '{}'::jsonb),
			('pb_paused_http', 'tg_paused', 'http', true, '5s', 3, '{}'::jsonb),
			('pb_maintenance_http', 'tg_maintenance', 'http', true, '5s', 3, '{}'::jsonb),
			('pb_retired_http', 'tg_retired', 'http', true, '5s', 3, '{}'::jsonb)
	`); err != nil {
		t.Fatalf("insert target projection probe fixtures: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into asset_services(service_id, vps_id, target_id, name, service_type)
		values
			('svc_shared_active', 'vps_exec_enabled', 'tg_shared_abnormal', 'Shared active service', 'web'),
			('svc_shared_archived', 'vps_target_archived', 'tg_shared_abnormal', 'Shared archived service', 'web'),
			('svc_archived_only', 'vps_target_archived', 'tg_archived_only', 'Archived-only service', 'web')
	`); err != nil {
		t.Fatalf("insert target projection services: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into asset_service_associations(id, service_id, vps_id, target_id, address, port)
		values
			('assoc_shared_active', 'svc_shared_active', 'vps_exec_enabled', 'tg_shared_abnormal', 'shared.example.test', 443),
			('assoc_shared_archived', 'svc_shared_archived', 'vps_target_archived', 'tg_shared_abnormal', 'shared.example.test', 443),
			('assoc_archived_only', 'svc_archived_only', 'vps_target_archived', 'tg_archived_only', 'archived.example.test', 443)
	`); err != nil {
		t.Fatalf("insert target projection associations: %v", err)
	}

	targetRepo := NewPostgresTargetRepository(pool)
	current, err := targetRepo.ListTargetsByScope(ctx, targets.ListScopeCurrent)
	if err != nil {
		t.Fatalf("list current targets: %v", err)
	}
	currentIDs := make(map[string]targets.TargetRecord, len(current))
	for _, record := range current {
		currentIDs[record.TargetID] = record
	}
	for _, id := range []string{"tg_normal", "tg_unobserved", "tg_shared_abnormal", "tg_severe", "tg_maintenance", "tg_paused"} {
		if _, ok := currentIDs[id]; !ok {
			t.Fatalf("current target list missing %q: %#v", id, currentIDs)
		}
	}
	for _, id := range []string{"tg_archived_only", "tg_retired"} {
		if _, ok := currentIDs[id]; ok {
			t.Fatalf("current target list unexpectedly contains %q", id)
		}
	}
	healthWant := map[string]string{
		"tg_unobserved":      "数据不可用",
		"tg_shared_abnormal": "关注",
		"tg_maintenance":     "维护中",
		"tg_paused":          "暂停",
	}
	for id, want := range healthWant {
		if currentIDs[id].CurrentHealthStatus != want {
			t.Fatalf("current target %q health = %q, want %q", id, currentIDs[id].CurrentHealthStatus, want)
		}
	}
	retired, err := targetRepo.ListTargetsByScope(ctx, targets.ListScopeRetired)
	if err != nil {
		t.Fatalf("list retired targets: %v", err)
	}
	if len(retired) != 1 || retired[0].TargetID != "tg_retired" {
		t.Fatalf("retired target list = %#v, want only tg_retired", retired)
	}
	all, err := targetRepo.ListTargetsByScope(ctx, targets.ListScopeAll)
	if err != nil {
		t.Fatalf("list all targets: %v", err)
	}
	allByID := make(map[string]targets.TargetRecord, len(all))
	for _, record := range all {
		allByID[record.TargetID] = record
	}
	for id, wantCounts := range map[string][2]int{
		"tg_shared_abnormal": {2, 1},
		"tg_paused":          {1, 0},
		"tg_maintenance":     {1, 1},
		"tg_retired":         {1, 0},
	} {
		record, ok := allByID[id]
		if !ok {
			t.Fatalf("all target list missing %q", id)
		}
		if record.EnabledProbeCount != wantCounts[0] || record.MatchingExecutorCount != wantCounts[1] {
			t.Fatalf("target %q coverage = (%d,%d), want (%d,%d)", id, record.EnabledProbeCount, record.MatchingExecutorCount, wantCounts[0], wantCounts[1])
		}
	}
	shared, err := targetRepo.GetTarget(ctx, "tg_shared_abnormal")
	if err != nil {
		t.Fatalf("get target detail: %v", err)
	}
	if shared.EnabledProbeCount != 2 || shared.MatchingExecutorCount != 1 {
		t.Fatalf("target detail coverage = (%d,%d), want (2,1)", shared.EnabledProbeCount, shared.MatchingExecutorCount)
	}

	dashboardRepo := NewPostgresDashboardRepository(pool)
	limited, err := dashboardRepo.GetDashboardOverview(ctx, 1)
	if err != nil {
		t.Fatalf("load limited dashboard: %v", err)
	}
	unlimited, err := dashboardRepo.GetDashboardOverview(ctx, 10)
	if err != nil {
		t.Fatalf("load unlimited dashboard: %v", err)
	}
	if limited.TotalTargetCount != 6 || unlimited.TotalTargetCount != 6 {
		t.Fatalf("dashboard current target count = (%d,%d), want 6", limited.TotalTargetCount, unlimited.TotalTargetCount)
	}
	if limited.UnobservedTargetCount != 1 || unlimited.UnobservedTargetCount != 1 {
		t.Fatalf("dashboard unobserved count = (%d,%d), want 1", limited.UnobservedTargetCount, unlimited.UnobservedTargetCount)
	}
	if limited.AbnormalTargetCount != 2 || unlimited.AbnormalTargetCount != 2 || limited.SevereTargetCount != 1 || unlimited.SevereTargetCount != 1 {
		t.Fatalf("dashboard abnormal/severe counts = (%d/%d,%d/%d), want abnormal 2 and severe 1", limited.AbnormalTargetCount, limited.SevereTargetCount, unlimited.AbnormalTargetCount, unlimited.SevereTargetCount)
	}
	if len(limited.AbnormalTargets) != 1 || len(unlimited.AbnormalTargets) != 2 {
		t.Fatalf("dashboard abnormal previews = (%d,%d), want limit-dependent 1/2", len(limited.AbnormalTargets), len(unlimited.AbnormalTargets))
	}
	if limited.AbnormalTargets[0].CurrentHealthStatus != "严重" {
		t.Fatalf("limited abnormal target health = %q, want severe first", limited.AbnormalTargets[0].CurrentHealthStatus)
	}
	if limited.AssetSummary.AbnormalLinkedVPSCount != 1 || unlimited.AssetSummary.AbnormalLinkedVPSCount != 1 {
		t.Fatalf("dashboard abnormal linked VPS count = (%d,%d), want deduplicated 1", limited.AssetSummary.AbnormalLinkedVPSCount, unlimited.AssetSummary.AbnormalLinkedVPSCount)
	}

	groupByName := func(summaries []incidents.DashboardGroupSummary) map[string]incidents.DashboardGroupSummary {
		result := make(map[string]incidents.DashboardGroupSummary, len(summaries))
		for _, summary := range summaries {
			result[summary.Group] = summary
		}
		return result
	}
	limitedGroups := groupByName(limited.GroupSummaries)
	unlimitedGroups := groupByName(unlimited.GroupSummaries)
	edgeLimited, ok := limitedGroups["edge"]
	if !ok || edgeLimited.AbnormalTargetCount != 2 || edgeLimited.UnobservedTargetCount != 1 {
		t.Fatalf("limited edge group = %#v, want abnormal=2/unobserved=1", edgeLimited)
	}
	edgeUnlimited, ok := unlimitedGroups["edge"]
	if !ok || edgeUnlimited != edgeLimited {
		t.Fatalf("group summary changed with preview limit: limited=%#v unlimited=%#v", edgeLimited, edgeUnlimited)
	}
}
