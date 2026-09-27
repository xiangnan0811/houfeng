package store

import (
	"context"
	"testing"
	"time"
)

func TestPostgresIntegrationMonitoringAttentionProjectsBindingAndEvidenceBeforeStoredHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "monitoring-attention-projection", 4)
	for _, tc := range []struct {
		id, binding, control string
		trusted              bool
	}{
		{"pending", "指纹变更待确认", "启用", true},
		{"missing", "已绑定", "启用", false},
		{"healthy", "已绑定", "启用", true},
		{"paused", "指纹变更待确认", "暂停", true},
		{"maintenance", "指纹变更待确认", "维护中", true},
	} {
		if _, err := fixture.db.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values($1,$1,'active')`, "vps_"+tc.id); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,"group",region,city,provider,lifecycle_status,monitoring_status,binding_status,current_health_status,last_heartbeat_at,last_trusted_online_at)
		values($1,$2,$1,'projection-test','','','','已接入',$3,$4,'正常',now(),case when $5 then now() else null end)`, "mi_"+tc.id, "vps_"+tc.id, tc.control, tc.binding, tc.trusted); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewPostgresDashboardRepository(pool).GetDashboardOverview(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalMonitoringInstanceCount != 5 || got.AbnormalMonitoringInstanceCount != 2 || got.SevereMonitoringInstanceCount != 0 || got.AssetSummary.AbnormalLinkedVPSCount != 2 {
		t.Fatalf("stale stored normal escaped dashboard attention: %#v", got)
	}
	if len(got.GroupSummaries) != 1 || got.GroupSummaries[0].AbnormalMonitoringInstanceCount != 2 {
		t.Fatalf("group aggregate disagrees with attention queue: %#v", got.GroupSummaries)
	}
	health := map[string]string{}
	for _, row := range got.AbnormalMonitoringInstances {
		health[row.MonitoringInstanceID] = row.CurrentHealthStatus
	}
	if len(health) != 2 || health["mi_pending"] != "绑定待确认" || health["mi_missing"] != "数据不可用" {
		t.Fatalf("attention queue omitted or labelled unconfirmed evidence healthy: %#v", health)
	}
	facts, err := NewPostgresAssetDecisionRepository(pool).loadFacts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		switch fact.VPS.VPSID {
		case "vps_pending", "vps_missing":
			if fact.AbnormalMonitoringCount != 1 || fact.RunningMonitoringCount != 0 {
				t.Fatalf("decision projection hid unconfirmed monitoring: %#v", fact)
			}
		case "vps_healthy":
			if fact.AbnormalMonitoringCount != 0 || fact.RunningMonitoringCount != 1 {
				t.Fatalf("confirmed monitoring lost normal evidence: %#v", fact)
			}
		default:
			if fact.AbnormalMonitoringCount != 0 || fact.RunningMonitoringCount != 0 {
				t.Fatalf("control state entered operational abnormal counts: %#v", fact)
			}
		}
	}
}

func TestPostgresIntegrationDashboardHistoricalInventoryUsesLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "dashboard-historical-inventory", 4)
	for _, sql := range []string{
		`insert into vps_assets(vps_id,display_name,lifecycle_status) values ('vps_history_counts','History counts','active')`,
		`insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status,current_health_status)
		values ('mi_history_counts','vps_history_counts','Historical monitor','','','','已退役','暂停','已绑定','严重')`,
		`insert into targets(target_id,name,target_type,host,lifecycle_status,run_status,current_health_status)
		values ('tg_history_counts','Historical target','service','example.test','retired','暂停','严重'),
		('tg_current_counts','Current target','service','current.example.test','active','启用','正常')`,
	} {
		if _, err := fixture.db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewPostgresDashboardRepository(pool).GetDashboardOverview(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedTargetCount != 1 || got.RetiredMonitoringInstanceCount != 1 {
		t.Fatalf("historical lifecycle inventory vanished from counts: %#v", got)
	}
	if got.TotalMonitoringInstanceCount != 0 || got.TotalTargetCount != 1 || got.AbnormalMonitoringInstanceCount != 0 || got.AbnormalTargetCount != 0 || len(got.AbnormalMonitoringInstances) != 0 || len(got.AbnormalTargets) != 0 {
		t.Fatalf("historical inventory contaminated current runtime: %#v", got)
	}
	if len(got.GroupSummaries) != 1 || got.GroupSummaries[0].TargetCount != 1 || got.GroupSummaries[0].MonitoringInstanceCount != 0 {
		t.Fatalf("historical inventory contaminated runtime groups: %#v", got.GroupSummaries)
	}
}
