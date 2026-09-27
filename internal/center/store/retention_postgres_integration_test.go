package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/retention"
	storemigrate "houfeng/internal/center/store/migrate"
)

// Uses a separately authenticated, admitted runtime role, not the migration
// owner. The fixture fails (rather than skips) if a configured database fails.
func TestPostgresIntegrationRetentionPreservesHistoryAndFinalizedBuckets(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresBaseFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator, "retention-migrator", 1)
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("converge current retention schema: %v", err)
	}
	runtime := fixture.openDirectRuntimePool(t, ctx, "retention-runtime", 1)
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("admit retention runtime role: %v", err)
	}
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.FixedZone("local", 8*60*60)).UTC()
	rawCutoff := now.AddDate(0, 0, -30)
	aggregateCutoff := startOfUTCDay(now.AddDate(0, 0, -365))
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := fixture.db.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed retention fixture: %v", err)
		}
	}
	exec(`insert into vps_assets (vps_id, display_name, lifecycle_status, archived_at) values ('vps_retention','Archived retention','archived',$1)`, now.Add(-time.Hour))
	exec(`insert into monitoring_instances (monitoring_instance_id, vps_id, display_name, region, city, provider, lifecycle_status, monitoring_status, binding_status, last_action)
		values ('mi_retention','vps_retention','Retention','','','','已退役','暂停','已绑定',jsonb_build_object('status','done','stdout','sensitive output','stderr','error','output_expired',false,'output_expires_at',$1::timestamptz,'action_id','act_retention'))`, now)
	exec(`insert into targets (target_id,name,target_type,host,run_status) values ('tgt_retention','Retention','service','example.test','暂停')`)
	exec(`insert into probe_items (probe_item_id,target_id,probe_kind,frequency_tier,timeout_seconds) values ('prb_retention','tgt_retention','http','1m',5)`)
	insertRaw := func(at time.Time, cpu float64) {
		t.Helper()
		exec(`insert into host_samples (monitoring_instance_id, observed_at, received_at, cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes, swap_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec, net_out_bytes_per_sec, cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec, disk_busy_pct, uptime_seconds, sync_batch_id)
			values ('mi_retention',$1,$2,$3,1,1,1,20,1,0,10,10,0,0,0,0,0,0,0,1,'batch_retention')`, at, now, cpu)
		exec(`insert into monitoring_instance_heartbeats (monitoring_instance_id,observed_at,received_at,agent_version,fingerprint,sync_batch_id) values ('mi_retention',$1,$2,'test','fp','batch_retention')`, at, now)
		exec(`insert into probe_observations (monitoring_instance_id,target_id,probe_item_id,observed_at,received_at,result_kind,latency_ms,sync_batch_id) values ('mi_retention','tgt_retention','prb_retention',$1,$2,'success',$3,'batch_retention')`, at, now, int(cpu))
	}
	insertRaw(rawCutoff.Add(-time.Second), 10)
	insertRaw(rawCutoff, 30)
	insertRaw(aggregateCutoff.Add(-time.Second), 40)
	insertRaw(aggregateCutoff, 50)
	insertRaw(now, 60)
	old := now.AddDate(-5, 0, 0)
	exec(`insert into state_change_events (event_id,object_type,object_id,event_type,summary,created_at) values ('evt_retention','monitoring_instance','mi_retention','test','permanent',$1)`, old)
	exec(`insert into notification_records (notification_id,object_type,object_id,channel,delivery_status,summary,created_at) values ('ntf_retention','monitoring_instance','mi_retention','telegram','sent','permanent',$1)`, old)
	exec(`insert into ip_quality_reports (report_id,monitoring_instance_id,observed_at,agent_version,fingerprint,sync_batch_id,ip_address,ip_version,status,raw_json) values ('ipq_retention','mi_retention',$1,'test','fp','batch_ipq','192.0.2.1',4,'success','{"safe":true}')`, old)

	repo := NewPostgresRetentionRepository(runtime)
	policy := retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}
	result, err := repo.ApplyRetention(ctx, policy, now)
	if err != nil {
		t.Fatalf("ApplyRetention(runtime) = %v", err)
	}
	if result.DeletedHostSamples != 3 || result.DeletedHeartbeats != 3 || result.DeletedProbeObservations != 3 || result.DeletedMonitoringInstanceAggregates != 1 || result.DeletedTargetAggregates != 1 || result.ClearedCommandActionOutputs != 1 {
		t.Fatalf("retention boundary counts = %+v", result)
	}
	assertCount := func(sql string, want int) {
		t.Helper()
		var got int
		if err := fixture.db.QueryRow(ctx, sql).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d (%v), want %d", sql, got, err, want)
		}
	}
	assertCount(`select count(*) from host_samples`, 2)
	assertCount(`select count(*) from monitoring_instance_host_sample_daily_aggregates`, 2)
	assertCount(`select count(*) from target_probe_daily_aggregates`, 2)
	assertCount(`select count(*) from state_change_events`, 1)
	assertCount(`select count(*) from notification_records`, 1)
	assertCount(`select count(*) from ip_quality_reports where raw_json is not null`, 1)
	assertCount(`select count(*) from monitoring_instances where last_action ? 'stdout' or last_action ? 'stderr'`, 0)

	// Partial survivors and a late arrival in the already pruned day must not
	// overwrite the complete two-sample aggregate (mean 20, max 30).
	insertRaw(rawCutoff.Add(time.Second), 99)
	if _, err := repo.ApplyRetention(ctx, policy, now.Add(time.Hour)); err != nil {
		t.Fatalf("second ApplyRetention(runtime) = %v", err)
	}
	var count int
	var avg, max float64
	var finalized bool
	if err := fixture.db.QueryRow(ctx, `select sample_count, avg_cpu_usage_pct, max_cpu_usage_pct, finalized from monitoring_instance_host_sample_daily_aggregates where bucket_date = $1::date`, startOfUTCDay(rawCutoff)).Scan(&count, &avg, &max, &finalized); err != nil {
		t.Fatal(err)
	}
	if count != 2 || avg != 20 || max != 30 || !finalized {
		t.Fatalf("finalized host bucket = count %d avg %v max %v finalized %v", count, avg, max, finalized)
	}
	if err := fixture.db.QueryRow(ctx, `select observation_count, avg_latency_ms, finalized from target_probe_daily_aggregates where bucket_date = $1::date`, startOfUTCDay(rawCutoff)).Scan(&count, &avg, &finalized); err != nil {
		t.Fatal(err)
	}
	if count != 2 || avg != 20 || !finalized {
		t.Fatalf("finalized probe bucket = count %d avg %v finalized %v", count, avg, finalized)
	}
	assertCount(`select count(*) from vps_assets where lifecycle_status='archived'`, 1)

	// A cleanup failure must roll back aggregate writes and earlier heartbeat
	// deletion together, so retrying cannot observe a half-pruned day.
	insertRaw(now.Add(-24*time.Hour), 70)
	exec(`create function retention_test_reject_delete() returns trigger language plpgsql as $$ begin raise exception 'injected retention failure'; end $$`)
	exec(`create trigger retention_test_reject_delete before delete on host_samples for each row execute function retention_test_reject_delete()`)
	if _, err := repo.ApplyRetention(ctx, policy, now.AddDate(0, 0, 31)); err == nil {
		t.Fatal("ApplyRetention() succeeded despite injected cleanup failure")
	}
	assertCount(`select count(*) from host_samples`, 2)
	assertCount(`select count(*) from monitoring_instance_heartbeats`, 2)
	assertCount(`select count(*) from monitoring_instance_host_sample_daily_aggregates`, 2)
}
