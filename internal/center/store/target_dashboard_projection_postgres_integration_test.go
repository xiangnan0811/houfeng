package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
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

func TestPostgresIntegrationTargetFreshness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "target-freshness-dashboard", 4)
	const (
		activeVPS        = "vps_target_freshness_active"
		archivedVPS      = "vps_target_freshness_archived"
		monitoringID     = "mi_target_freshness"
		mixedTarget      = "tg_target_freshness_mixed"
		unobservedTarget = "tg_target_freshness_unobserved"
		uncoveredTarget  = "tg_target_freshness_uncovered"
		inactiveTarget   = "tg_target_freshness_inactive"
		archivedTarget   = "tg_target_freshness_archived_only"
		mixedHTTPProbe   = "pb_target_freshness_http"
		mixedTLSProbe    = "pb_target_freshness_tls"
		mixedDisabled    = "pb_target_freshness_disabled"
		inactiveProbe    = "pb_target_freshness_inactive"
		unobservedProbe  = "pb_target_freshness_unobserved"
	)

	if _, err := fixture.db.Exec(ctx, `
		insert into vps_assets(vps_id, display_name, lifecycle_status, usage_status)
		values
			($1, 'Freshness active executor', 'active', 'in_use'),
			($2, 'Freshness archived executor', 'archived', 'unknown')
	`, activeVPS, archivedVPS); err != nil {
		t.Fatalf("insert freshness VPS fixtures: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_instances(
			monitoring_instance_id, vps_id, display_name, "group", region, city, provider,
			labels, lifecycle_status, monitoring_status, binding_status, current_health_status,
			ever_connected, last_trusted_online_at, archived_at
		) values (
			$1, $2, 'Freshness executor', 'edge', '', '', '',
			array['edge'], '已接入', '启用', '已绑定', '正常',
			true, now(), null
		)
	`, monitoringID, activeVPS); err != nil {
		t.Fatalf("insert freshness monitoring instance: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into targets(
			target_id, name, target_type, host, execution_monitoring_instance_labels,
			lifecycle_status, run_status, "group", labels, current_health_status
		) values
			($1, 'Mixed freshness target', 'service', 'mixed.example.test', array['edge'], 'active', '启用', 'edge', array['edge'], '严重'),
			($2, 'Unobserved freshness target', 'service', 'unobserved-freshness.example.test', array['edge'], 'active', '启用', 'edge', array['edge'], '严重'),
			($3, 'Uncovered freshness target', 'service', 'uncovered-freshness.example.test', array['edge'], 'active', '启用', '', array['edge'], '正常'),
			($4, 'Inactive freshness target', 'service', 'inactive-freshness.example.test', array['edge'], 'active', '维护中', 'edge', array['edge'], '严重'),
			($5, 'Archived-only freshness target', 'service', 'archived-freshness.example.test', array['edge'], 'active', '启用', 'history', array['edge'], '关注')
	`, mixedTarget, unobservedTarget, uncoveredTarget, inactiveTarget, archivedTarget); err != nil {
		t.Fatalf("insert freshness targets: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into probe_items(probe_item_id, target_id, probe_kind, enabled, frequency_tier, timeout_seconds, config)
		values
			($1, $2, 'http', true, '1m', 5, '{}'::jsonb),
			($3, $2, 'tls', true, '6h', 5, '{}'::jsonb),
			($4, $2, 'http', false, '1m', 5, '{}'::jsonb),
			($5, $6, 'http', true, '1m', 5, '{}'::jsonb),
			($7, $8, 'http', true, '1m', 5, '{}'::jsonb)
	`, mixedHTTPProbe, mixedTarget, mixedTLSProbe, mixedDisabled, inactiveProbe, inactiveTarget, unobservedProbe, unobservedTarget); err != nil {
		t.Fatalf("insert freshness probe fixtures: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into asset_services(service_id, vps_id, target_id, name, service_type)
		values ('svc_target_freshness_archived', $1, $2, 'Archived freshness service', 'web')
	`, archivedVPS, archivedTarget); err != nil {
		t.Fatalf("insert archived freshness service: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into asset_service_associations(id, service_id, vps_id, target_id, address, port)
		values ('assoc_target_freshness_archived', 'svc_target_freshness_archived', $1, $2, 'archived-freshness.example.test', 443)
	`, archivedVPS, archivedTarget); err != nil {
		t.Fatalf("insert archived freshness association: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	oldReset := now.Add(-2 * time.Hour)
	if _, err := fixture.db.Exec(ctx, `
		update targets
		set freshness_reset_at = $1
		where target_id in ($2, $3, $4)
	`, oldReset, mixedTarget, inactiveTarget, unobservedTarget); err != nil {
		t.Fatalf("set freshness target generations: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update probe_items
		set freshness_reset_at = $1
		where probe_item_id in ($2, $3, $4, $5)
	`, oldReset, mixedHTTPProbe, mixedTLSProbe, inactiveProbe, unobservedProbe); err != nil {
		t.Fatalf("set freshness probe generations: %v", err)
	}

	observationsRepo := NewPostgresObservationRepository(pool)
	httpObservedAt := now.Add(-200 * time.Second)
	tlsObservedAt := now.Add(-20 * time.Second)
	if err := observationsRepo.RecordBatch(ctx, observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{
			targetFreshnessObservation(monitoringID, mixedTarget, mixedHTTPProbe, httpObservedAt, agentapi.ProbeResultFailure, false),
			targetFreshnessObservation(monitoringID, mixedTarget, mixedTLSProbe, tlsObservedAt, agentapi.ProbeResultSuccess, false),
			targetFreshnessObservation(monitoringID, mixedTarget, mixedDisabled, now.Add(-10*time.Second), agentapi.ProbeResultSuccess, false),
			targetFreshnessObservation(monitoringID, inactiveTarget, inactiveProbe, now.Add(-10*time.Second), agentapi.ProbeResultSuccess, true),
		},
	}); err != nil {
		t.Fatalf("record freshness observations: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into center_settings(settings_id, incident_defaults, override_rules)
		values ('center', $1::jsonb, $2::jsonb)
		on conflict (settings_id) do update
		set incident_defaults = excluded.incident_defaults,
		    override_rules = excluded.override_rules,
		    updated_at = now()
	`,
		`{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true,"cpu_warning_pct":70,"cpu_alert_pct":80,"cpu_critical_pct":90,"mem_warning_pct":85,"mem_alert_pct":92,"mem_critical_pct":95,"disk_warning_pct":85,"disk_alert_pct":92,"disk_critical_pct":97,"inode_warning_pct":80,"inode_alert_pct":90,"inode_critical_pct":95,"iowait_warning_pct":20,"iowait_critical_pct":50,"load5_warning":4,"load5_critical":8}`,
		`{"monitoring_instance_labels":[],"target_types":[],"target_labels":[{"label":"edge","overrides":{"incident_defaults":{"cpu_alert_pct":75}}}]}`,
	); err != nil {
		t.Fatalf("set non-default incident defaults and partial target override: %v", err)
	}

	targetRepo := NewPostgresTargetRepository(pool)
	current, err := targetRepo.ListTargetsByScope(ctx, targets.ListScopeCurrent)
	if err != nil {
		t.Fatalf("list freshness current targets: %v", err)
	}
	currentByID := make(map[string]targets.TargetRecord, len(current))
	for _, record := range current {
		currentByID[record.TargetID] = record
	}
	if _, ok := currentByID[archivedTarget]; ok {
		t.Fatalf("archived-only target unexpectedly visible in current list")
	}
	mixed := currentByID[mixedTarget]
	if mixed.ObservationFreshness.State != "partial" ||
		mixed.ObservationFreshness.FreshProbeCount != 1 ||
		mixed.ObservationFreshness.StaleProbeCount != 1 ||
		mixed.ObservationFreshness.PendingProbeCount != 0 ||
		mixed.ObservationFreshness.EnabledProbeCount != 2 ||
		len(mixed.ObservationFreshness.Probes) != 2 {
		t.Fatalf("mixed freshness = %#v, want partial with one fresh and one stale enabled probe", mixed.ObservationFreshness)
	}
	if mixed.LastFailureAt == nil || mixed.CurrentHealthStatus != "严重" {
		t.Fatalf("mixed health = (%v,%q), want preserved failure and severe health", mixed.LastFailureAt, mixed.CurrentHealthStatus)
	}
	if currentByID[unobservedTarget].ObservationFreshness.State != "unobserved" {
		t.Fatalf("unobserved freshness = %#v, want unobserved", currentByID[unobservedTarget].ObservationFreshness)
	}
	if currentByID[uncoveredTarget].ObservationFreshness.State != "uncovered" {
		t.Fatalf("uncovered freshness = %#v, want uncovered", currentByID[uncoveredTarget].ObservationFreshness)
	}
	if inactive := currentByID[inactiveTarget].ObservationFreshness; inactive.State != "inactive" || inactive.FreshProbeCount != 0 || inactive.StaleProbeCount != 0 || len(inactive.Probes) != 0 {
		t.Fatalf("inactive freshness = %#v, want inactive with empty freshness counts", inactive)
	}

	detail, err := targetRepo.GetTarget(ctx, mixedTarget)
	if err != nil {
		t.Fatalf("get mixed freshness target: %v", err)
	}
	if detail.ObservationFreshness.State != mixed.ObservationFreshness.State ||
		detail.ObservationFreshness.EvaluatedAt.IsZero() {
		t.Fatalf("mixed detail freshness = %#v, want list projection with evaluation time", detail.ObservationFreshness)
	}
	for _, probe := range detail.ObservationFreshness.Probes {
		if probe.ProbeItemID == mixedDisabled {
			t.Fatalf("disabled probe unexpectedly included in freshness projection: %#v", detail.ObservationFreshness.Probes)
		}
	}

	var incidentsBefore int
	if err := pool.QueryRow(ctx, `
		select count(*)::int
		from active_incidents
		where object_type = 'target' and object_id = $1
	`, mixedTarget).Scan(&incidentsBefore); err != nil {
		t.Fatalf("count mixed target incidents before dashboard: %v", err)
	}
	dashboardRepo := NewPostgresDashboardRepository(pool)
	limited, err := dashboardRepo.GetDashboardOverview(ctx, 1)
	if err != nil {
		t.Fatalf("load limited freshness dashboard: %v", err)
	}
	unlimited, err := dashboardRepo.GetDashboardOverview(ctx, 10)
	if err != nil {
		t.Fatalf("load unlimited freshness dashboard: %v", err)
	}
	if limited.StaleTargetCount != 1 || unlimited.StaleTargetCount != 1 {
		t.Fatalf("dashboard stale target count = (%d,%d), want one independent of preview limit", limited.StaleTargetCount, unlimited.StaleTargetCount)
	}
	if limited.StaleTargetCount != unlimited.StaleTargetCount || limited.TotalTargetCount != unlimited.TotalTargetCount {
		t.Fatalf("dashboard limit changed global freshness/counts: limited=%#v unlimited=%#v", limited, unlimited)
	}
	if limited.TotalTargetCount != 4 {
		t.Fatalf("dashboard current target total = %d, want four visible targets excluding archived-only target", limited.TotalTargetCount)
	}
	if limited.UnobservedTargetCount != 2 || limited.AbnormalTargetCount != 1 || limited.SevereTargetCount != 1 {
		t.Fatalf("dashboard preserved target health counts = %#v, want unobserved=2 (including uncovered legacy count) abnormal=1 severe=1", limited)
	}
	if len(limited.AbnormalTargets) != 1 || limited.AbnormalTargets[0].TargetID != mixedTarget ||
		limited.AbnormalTargets[0].ObservationFreshness.State != "partial" {
		t.Fatalf("limited abnormal target projection = %#v, want mixed target freshness attached", limited.AbnormalTargets)
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
	if limitedGroups["edge"].StaleTargetCount != 1 || unlimitedGroups["edge"].StaleTargetCount != 1 {
		t.Fatalf("dashboard edge stale group counts = (%#v,%#v), want one", limitedGroups["edge"], unlimitedGroups["edge"])
	}
	var incidentsAfter int
	if err := pool.QueryRow(ctx, `
		select count(*)::int
		from active_incidents
		where object_type = 'target' and object_id = $1
	`, mixedTarget).Scan(&incidentsAfter); err != nil {
		t.Fatalf("count mixed target incidents after dashboard: %v", err)
	}
	if incidentsAfter != incidentsBefore {
		t.Fatalf("freshness read changed target incident count from %d to %d", incidentsBefore, incidentsAfter)
	}

	if err := targetRepo.DeleteProbeItem(ctx, mixedTarget, mixedDisabled); err != nil {
		t.Fatalf("delete disabled freshness probe: %v", err)
	}
	afterDelete, err := targetRepo.GetTarget(ctx, mixedTarget)
	if err != nil {
		t.Fatalf("get mixed target after disabled probe deletion: %v", err)
	}
	if len(afterDelete.ObservationFreshness.Probes) != 2 || afterDelete.ObservationFreshness.EnabledProbeCount != 2 {
		t.Fatalf("mixed freshness after disabled probe deletion = %#v, want two enabled probes", afterDelete.ObservationFreshness)
	}

	created, err := targetRepo.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "Generation freshness target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "generation-freshness.example.test",
		ExecutionMonitoringInstanceLabels: []string{"edge"},
		RunStatus:                         targets.RunStatusEnabled,
		Group:                             "edge",
		Labels:                            []string{"edge"},
	})
	if err != nil {
		t.Fatalf("create freshness generation target: %v", err)
	}
	if created.ObservationFreshness.State != "uncovered" || created.ObservationFreshness.EvaluatedAt.IsZero() {
		t.Fatalf("created target freshness = %#v, want evaluated uncovered projection", created.ObservationFreshness)
	}
	createdProbe, err := targetRepo.CreateProbeItem(ctx, created.TargetID, targets.CreateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 5,
		Config:         []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create freshness generation probe: %v", err)
	}
	generationProbeID := createdProbe.ProbeItemID
	if _, err := fixture.db.Exec(ctx, `
		update targets set freshness_reset_at = $1 where target_id = $2
	`, oldReset, created.TargetID); err != nil {
		t.Fatalf("set generation target reset: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update probe_items set freshness_reset_at = $1 where probe_item_id = $2
	`, oldReset, generationProbeID); err != nil {
		t.Fatalf("set generation probe reset: %v", err)
	}
	generationObservedAt := now.Add(-10 * time.Minute)
	if err := observationsRepo.RecordBatch(ctx, observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{
			targetFreshnessObservation(monitoringID, created.TargetID, generationProbeID, generationObservedAt, agentapi.ProbeResultSuccess, false),
		},
	}); err != nil {
		t.Fatalf("record generation target observation: %v", err)
	}
	generationBeforeResume, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target before resume: %v", err)
	}
	if generationBeforeResume.ObservationFreshness.State != "stale" {
		t.Fatalf("generation freshness before resume = %#v, want stale", generationBeforeResume.ObservationFreshness)
	}
	if _, err := targetRepo.PauseTargetRun(ctx, created.TargetID); err != nil {
		t.Fatalf("pause generation target: %v", err)
	}
	resumed, err := targetRepo.ResumeTargetRun(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("resume generation target: %v", err)
	}
	if resumed.ObservationFreshness.State != "pending" {
		t.Fatalf("generation freshness after resume = %#v, want pending after reset", resumed.ObservationFreshness)
	}
	if len(resumed.ObservationFreshness.Probes) != 1 {
		t.Fatalf("generation freshness after resume = %#v, want one probe", resumed.ObservationFreshness)
	}
	expectedSince := resumed.ObservationFreshness.Probes[0].ExpectedSince
	resumedAgain, err := targetRepo.ResumeTargetRun(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("repeat resume generation target: %v", err)
	}
	if resumedAgain.ObservationFreshness.Probes[0].ExpectedSince != expectedSince {
		t.Fatalf("repeat resume changed freshness generation: first=%s repeat=%s", expectedSince, resumedAgain.ObservationFreshness.Probes[0].ExpectedSince)
	}

	if _, err := targetRepo.UpdateTargetMetadata(ctx, created.TargetID, targets.UpdateMetadataInput{
		Group:  new("edge"),
		Labels: []string{"edge"},
		Note:   "metadata does not reset observation generation",
	}); err != nil {
		t.Fatalf("update generation metadata: %v", err)
	}
	afterMetadata, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target after metadata update: %v", err)
	}
	if afterMetadata.ObservationFreshness.Probes[0].ExpectedSince != expectedSince {
		t.Fatalf("metadata update changed freshness generation: got=%s want=%s", afterMetadata.ObservationFreshness.Probes[0].ExpectedSince, expectedSince)
	}

	if _, err := targetRepo.UpdateProbeItem(ctx, created.TargetID, generationProbeID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 5,
		Config:         []byte(`{"path":"/health"}`),
	}); err != nil {
		t.Fatalf("change generation probe config: %v", err)
	}
	afterConfig, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target after probe config update: %v", err)
	}
	configExpectedSince := afterConfig.ObservationFreshness.Probes[0].ExpectedSince
	if !configExpectedSince.After(expectedSince) || afterConfig.ObservationFreshness.State != "pending" {
		t.Fatalf("probe config update freshness = %#v, want new pending generation", afterConfig.ObservationFreshness)
	}
	if _, err := targetRepo.UpdateProbeItem(ctx, created.TargetID, generationProbeID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 20,
		Config:         []byte(`{"path":"/health"}`),
	}); err != nil {
		t.Fatalf("change generation probe timeout: %v", err)
	}
	afterTimeout, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target after timeout update: %v", err)
	}
	if afterTimeout.ObservationFreshness.Probes[0].ExpectedSince != configExpectedSince ||
		afterTimeout.ObservationFreshness.Probes[0].StaleAfterSeconds != 200 {
		t.Fatalf("timeout update freshness = %#v, want unchanged generation and recalculated deadline", afterTimeout.ObservationFreshness)
	}
	if _, err := targetRepo.UpdateProbeItem(ctx, created.TargetID, generationProbeID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 20,
		Config:         []byte(`{"path":"/health"}`),
	}); err != nil {
		t.Fatalf("repeat same generation probe update: %v", err)
	}
	afterSameUpdate, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target after same probe update: %v", err)
	}
	if afterSameUpdate.ObservationFreshness.Probes[0].ExpectedSince != configExpectedSince {
		t.Fatalf("same probe update changed freshness generation: got=%s want=%s", afterSameUpdate.ObservationFreshness.Probes[0].ExpectedSince, configExpectedSince)
	}
	if _, err := targetRepo.UpdateProbeItem(ctx, created.TargetID, generationProbeID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        false,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 20,
		Config:         []byte(`{"path":"/health"}`),
	}); err != nil {
		t.Fatalf("disable generation probe: %v", err)
	}
	disabledGeneration, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target after disabling probe: %v", err)
	}
	if disabledGeneration.ObservationFreshness.State != "uncovered" ||
		disabledGeneration.ObservationFreshness.EnabledProbeCount != 0 ||
		len(disabledGeneration.ObservationFreshness.Probes) != 0 {
		t.Fatalf("disabled generation freshness = %#v, want uncovered without enabled probes", disabledGeneration.ObservationFreshness)
	}
	if _, err := targetRepo.UpdateProbeItem(ctx, created.TargetID, generationProbeID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 20,
		Config:         []byte(`{"path":"/health"}`),
	}); err != nil {
		t.Fatalf("re-enable generation probe: %v", err)
	}
	reEnabledGeneration, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target after re-enabling probe: %v", err)
	}
	if reEnabledGeneration.ObservationFreshness.State != "pending" ||
		!reEnabledGeneration.ObservationFreshness.Probes[0].ExpectedSince.After(configExpectedSince) {
		t.Fatalf("re-enabled generation freshness = %#v, want new pending generation", reEnabledGeneration.ObservationFreshness)
	}
	newGenerationObservationAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := observationsRepo.RecordBatch(ctx, observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{
			targetFreshnessObservation(monitoringID, created.TargetID, generationProbeID, newGenerationObservationAt, agentapi.ProbeResultSuccess, false),
		},
	}); err != nil {
		t.Fatalf("record post-reset generation observation: %v", err)
	}
	afterNewObservation, err := targetRepo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("get generation target after post-reset observation: %v", err)
	}
	if afterNewObservation.ObservationFreshness.State != "fresh" {
		t.Fatalf("generation freshness after post-reset observation = %#v, want fresh", afterNewObservation.ObservationFreshness)
	}

	if _, err := fixture.db.Exec(ctx, `
		insert into center_settings(settings_id, override_rules)
		values ('center', $1::jsonb)
		on conflict (settings_id) do update set override_rules = excluded.override_rules, updated_at = now()
	`, `{"monitoring_instance_labels":[],"target_types":[],"target_labels":[{"label":"edge","overrides":{"incident_defaults":{"cpu_alert_pct":75},"probe_frequency_defaults":{"http":"5m"}}}]}`); err != nil {
		t.Fatalf("set freshness frequency override: %v", err)
	}
	overridden, err := targetRepo.GetTarget(ctx, mixedTarget)
	if err != nil {
		t.Fatalf("get target after freshness frequency override: %v", err)
	}
	var overriddenHTTP *targets.ProbeObservationFreshness
	for index := range overridden.ObservationFreshness.Probes {
		probe := &overridden.ObservationFreshness.Probes[index]
		if probe.ProbeItemID == mixedHTTPProbe {
			overriddenHTTP = probe
		}
	}
	if overriddenHTTP == nil || overriddenHTTP.EffectiveFrequencyTier != targets.FrequencyTier5m ||
		overriddenHTTP.StaleAfterSeconds != 905 || overridden.ObservationFreshness.State != "fresh" {
		t.Fatalf("overridden mixed freshness = %#v, want 5m fresh HTTP projection", overridden.ObservationFreshness)
	}
	afterOverride, err := dashboardRepo.GetDashboardOverview(ctx, 1)
	if err != nil {
		t.Fatalf("load dashboard after freshness override: %v", err)
	}
	if afterOverride.StaleTargetCount != 0 {
		t.Fatalf("dashboard stale count after target-label override = %d, want zero", afterOverride.StaleTargetCount)
	}
	if afterOverride.AbnormalTargetCount != 1 || afterOverride.SevereTargetCount != 1 {
		t.Fatalf("dashboard health counts after freshness override = (%d,%d), want preserved abnormal/severe 1/1", afterOverride.AbnormalTargetCount, afterOverride.SevereTargetCount)
	}
}

func TestPostgresIntegrationTargetFreshnessRepeatableReadSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "dashboard-repeatable-read", 4)
	const (
		vpsID    = "vps_dashboard_repeatable_read"
		targetID = "tg_dashboard_repeatable_read"
		probeID  = "pb_dashboard_repeatable_read"
	)

	if _, err := fixture.db.Exec(ctx, `
		insert into vps_assets(vps_id, display_name, lifecycle_status, usage_status)
		values ($1, 'Dashboard repeatable-read VPS', 'active', 'in_use')
	`, vpsID); err != nil {
		t.Fatalf("insert repeatable-read VPS: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into targets(
			target_id, name, target_type, host, execution_monitoring_instance_labels,
			lifecycle_status, run_status, "group", current_health_status,
			last_failure_at
		) values (
			$1, 'Dashboard repeatable-read target', 'service', 'repeatable-read.example.test',
			array[]::text[], 'active', '启用', 'snapshot-a', '严重', now() - interval '1 hour'
		)
	`, targetID); err != nil {
		t.Fatalf("insert repeatable-read target: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into probe_items(
			probe_item_id, target_id, probe_kind, enabled, frequency_tier,
			timeout_seconds, config
		) values ($1, $2, 'http', true, '1m', 5, '{}'::jsonb)
	`, probeID, targetID); err != nil {
		t.Fatalf("insert repeatable-read probe: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update targets
		set freshness_reset_at = now() - interval '1 hour'
		where target_id = $1
	`, targetID); err != nil {
		t.Fatalf("seed repeatable-read target stale evidence: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update probe_items
		set freshness_reset_at = now() - interval '1 hour',
		    last_live_observed_at = now() - interval '1 hour'
		where probe_item_id = $1
	`, probeID); err != nil {
		t.Fatalf("seed repeatable-read probe stale evidence: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var txOptions pgx.TxOptions
	repo := NewPostgresDashboardRepository(pool)
	repo.beginTx = func(ctx context.Context, options pgx.TxOptions) (dashboardReadTx, error) {
		txOptions = options
		tx, err := pool.BeginTx(ctx, options)
		if err != nil {
			return nil, err
		}
		return &blockingDashboardReadTx{
			dashboardReadTx: tx,
			entered:         entered,
			release:         release,
		}, nil
	}

	type dashboardResult struct {
		overview incidents.DashboardOverview
		err      error
	}
	resultCh := make(chan dashboardResult, 1)
	go func() {
		overview, err := repo.GetDashboardOverview(ctx, 10)
		resultCh <- dashboardResult{overview: overview, err: err}
	}()

	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatalf("dashboard freshness query did not start: %v", ctx.Err())
	}
	if _, err := fixture.db.Exec(ctx, `
		update targets
		set lifecycle_status = 'retired', "group" = 'snapshot-b'
		where target_id = $1
	`, targetID); err != nil {
		t.Fatalf("move target out of current visibility: %v", err)
	}
	close(release)

	var result dashboardResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		t.Fatalf("dashboard overview did not finish: %v", ctx.Err())
	}
	if result.err != nil {
		t.Fatalf("dashboard overview error: %v", result.err)
	}
	if txOptions.IsoLevel != pgx.RepeatableRead || txOptions.AccessMode != pgx.ReadOnly {
		t.Fatalf("dashboard transaction options = %#v, want repeatable-read read-only", txOptions)
	}
	overview := result.overview
	if overview.TotalTargetCount != 1 || overview.StaleTargetCount != 1 ||
		overview.AbnormalTargetCount != 1 || overview.SevereTargetCount != 1 {
		t.Fatalf("repeatable-read overview counts = (total=%d stale=%d abnormal=%d severe=%d), want (1,1,1,1)", overview.TotalTargetCount, overview.StaleTargetCount, overview.AbnormalTargetCount, overview.SevereTargetCount)
	}
	if len(overview.AbnormalTargets) != 1 || overview.AbnormalTargets[0].TargetID != targetID ||
		overview.AbnormalTargets[0].ObservationFreshness.State != "stale" {
		t.Fatalf("repeatable-read abnormal target projection = %#v, want the visible stale target", overview.AbnormalTargets)
	}
	var snapshotGroup *incidents.DashboardGroupSummary
	for index := range overview.GroupSummaries {
		if overview.GroupSummaries[index].Group == "snapshot-a" {
			snapshotGroup = &overview.GroupSummaries[index]
			break
		}
		if overview.GroupSummaries[index].Group == "snapshot-b" {
			t.Fatalf("repeatable-read groups contain post-move group: %#v", overview.GroupSummaries)
		}
	}
	if snapshotGroup == nil || snapshotGroup.TargetCount != 1 || snapshotGroup.StaleTargetCount != 1 {
		t.Fatalf("repeatable-read snapshot group = %#v, want target/stale count 1", snapshotGroup)
	}
}

type blockingDashboardReadTx struct {
	dashboardReadTx
	entered chan<- struct{}
	release <-chan struct{}
}

func (tx *blockingDashboardReadTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.dashboardReadTx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if strings.Contains(sql, "last_live_observed_at") {
		close(tx.entered)
		select {
		case <-tx.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return rows, nil
}

func targetFreshnessObservation(
	monitoringInstanceID, targetID, probeItemID string,
	observedAt time.Time,
	resultKind string,
	maintenance bool,
) observations.ProbeObservationWrite {
	observation := observations.ProbeObservationWrite{
		MonitoringInstanceID: monitoringInstanceID,
		TargetID:             targetID,
		ProbeItemID:          probeItemID,
		ProbeKind:            agentapi.ProbeKindHTTP,
		ObservedAt:           observedAt,
		ReceivedAt:           observedAt.Add(time.Second),
		AgentVersion:         "target-freshness-fixture",
		Fingerprint:          "target-freshness-fixture",
		ResultKind:           resultKind,
		MaintenanceContext:   maintenance,
		SyncBatchID:          "sync_target_freshness_" + probeItemID + "_" + observedAt.Format("150405"),
	}
	if resultKind == agentapi.ProbeResultFailure {
		observation.ErrorCode = agentapi.ProbeErrorHTTPStatus
		observation.ErrorSummary = "fixture failure"
	}
	return observation
}
