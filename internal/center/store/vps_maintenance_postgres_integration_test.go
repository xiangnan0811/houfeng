package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/vpsmaintenance"
)

func TestVPSMaintenancePostgresSharedOverlapAndManualOverride(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	_, err := pool.Exec(ctx, `
	insert into vps_assets(vps_id,display_name,lifecycle_status) values('maint_a','A','active'),('maint_b','B','active');
	insert into targets(target_id,name,target_type,host,run_status) values('maint_shared','Shared','service','example.test','启用'),('maint_paused','Paused','service','paused.test','暂停');
	insert into asset_services(service_id,name,service_type) values('maint_service','Shared','web'),('maint_service_paused','Paused','web');
	insert into asset_service_associations(id,service_id,vps_id,target_id) values('maint_link_a','maint_service','maint_a','maint_shared'),('maint_link_b','maint_service','maint_b','maint_shared'),('maint_link_paused','maint_service_paused','maint_a','maint_paused');`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresVPSMaintenanceRepository(pool)
	start := func(vps string, confirmed bool) {
		t.Helper()
		review, err := repo.Review(ctx, vps)
		if err != nil {
			t.Fatal(err)
		}
		input := vpsmaintenance.StartInput{Reason: "planned", PreviewDigest: review.PreviewDigest}
		if confirmed {
			input.ConfirmedSharedTargetIDs = []string{"maint_shared"}
		}
		if _, err = repo.Start(ctx, vps, input, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	end := func(vps string) {
		t.Helper()
		if _, err := repo.End(ctx, vps, vpsmaintenance.EndInput{Reason: "done"}, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	status := func(id, want string) {
		t.Helper()
		var got string
		if err := pool.QueryRow(ctx, `select run_status from targets where target_id=$1`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s control=%s want=%s", id, got, want)
		}
	}
	reset := func() time.Time {
		t.Helper()
		var got time.Time
		if err := pool.QueryRow(ctx, `select freshness_reset_at from targets where target_id='maint_shared'`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	start("maint_a", false)
	initialReset := reset()
	status("maint_shared", "启用")
	status("maint_paused", "暂停")
	end("maint_a")
	if got := reset(); !got.Equal(initialReset) {
		t.Fatalf("no-op maintenance exit reset=%v want unchanged %v", got, initialReset)
	}
	start("maint_a", true)
	start("maint_b", true)
	sharedReset := reset()
	end("maint_a")
	status("maint_shared", "维护中")
	if got := reset(); !got.Equal(sharedReset) {
		t.Fatalf("earlier shared maintenance exit reset=%v want unchanged %v", got, sharedReset)
	}
	end("maint_b")
	status("maint_shared", "启用")
	finalReset := reset()
	if !finalReset.After(sharedReset) {
		t.Fatalf("final shared maintenance exit reset=%v want after %v", finalReset, sharedReset)
	}
	start("maint_a", true)
	start("maint_b", true)
	supersededReset := reset()
	// Model a later explicit same-value maintenance setting: it must outlive
	// both VPS-owned holds, just as an explicit pause would.
	if _, err = pool.Exec(ctx, `update targets set control_revision=control_revision+1 where target_id='maint_shared'`); err != nil {
		t.Fatal(err)
	}
	end("maint_a")
	end("maint_b")
	status("maint_shared", "维护中")
	if got := reset(); !got.Equal(supersededReset) {
		t.Fatalf("superseded maintenance exit reset=%v want unchanged %v", got, supersededReset)
	}
	status("maint_paused", "暂停")
	if _, err = repo.End(ctx, "maint_b", vpsmaintenance.EndInput{Reason: "retry"}, "operator"); !errors.Is(err, vpsmaintenance.ErrConflict) {
		t.Fatalf("duplicate end=%v", err)
	}
	var actions, effects int
	if err = pool.QueryRow(ctx, `select count(*) from vps_maintenance_actions where ended_at is not null and ended_by='operator'`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from vps_maintenance_effects`).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if actions != 5 || effects != 4 {
		t.Fatalf("audit actions=%d effects=%d", actions, effects)
	}
}

func TestVPSMaintenancePostgresArchiveNeverResumes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	if _, err := pool.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values('maint_archive','Archive','active');insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,lifecycle_status,monitoring_status,region,city,provider) values('maint_mi','maint_archive','Current','待接入','启用','test','test','test')`); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresVPSMaintenanceRepository(pool)
	review, err := repo.Review(ctx, "maint_archive")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Start(ctx, "maint_archive", vpsmaintenance.StartInput{Reason: "planned", PreviewDigest: review.PreviewDigest}, "operator"); err != nil {
		t.Fatal(err)
	}
	tx, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = endVPSMaintenanceForArchive(ctx, tx, "maint_archive", "archive"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var control string
	if err = pool.QueryRow(ctx, `select monitoring_status from monitoring_instances where monitoring_instance_id='maint_mi'`).Scan(&control); err != nil {
		t.Fatal(err)
	}
	if control != "维护中" {
		t.Fatalf("archive resumed control=%s", control)
	}
	if _, err = repo.End(ctx, "maint_archive", vpsmaintenance.EndInput{Reason: "done"}, "operator"); !errors.Is(err, vpsmaintenance.ErrConflict) {
		t.Fatalf("ended archived maintenance=%v", err)
	}
}

func TestVPSMaintenancePostgresFailureRollsBackControlsAndAudit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	if _, err := pool.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values('maint_failure','Failure','active');insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,lifecycle_status,monitoring_status,region,city,provider) values('maint_fail_mi','maint_failure','Current','待接入','启用','test','test','test');alter table vps_maintenance_effects add constraint force_effect_failure check(resource_id<>'maint_fail_mi')`); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresVPSMaintenanceRepository(pool)
	review, err := repo.Review(ctx, "maint_failure")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Start(ctx, "maint_failure", vpsmaintenance.StartInput{Reason: "planned", PreviewDigest: review.PreviewDigest}, "operator"); err == nil {
		t.Fatal("expected effect persistence failure")
	}
	var control string
	var revision int64
	var actions int
	if err = pool.QueryRow(ctx, `select monitoring_status,control_revision from monitoring_instances where monitoring_instance_id='maint_fail_mi'`).Scan(&control, &revision); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from vps_maintenance_actions`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if control != "启用" || revision != 0 || actions != 0 {
		t.Fatalf("partial mutation: control=%s revision=%d actions=%d", control, revision, actions)
	}
}
