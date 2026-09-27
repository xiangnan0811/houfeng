package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationSessionLiveEvidenceAndRetirement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "session-live-evidence", 4)
	if _, err := fixture.db.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_live','Live evidence','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status,binding_fingerprint) values('mi_live','vps_live','Live evidence','','','','待接入','暂停','已绑定','fp_live')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash,fingerprint_hash) values('mas_live','mi_live',$1,'fp_live')`, hashSyncToken("mas_live.secret")); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresSyncRepository(pool)
	var dbBefore time.Time
	if err := fixture.db.QueryRow(ctx, `select clock_timestamp()`).Scan(&dbBefore); err != nil {
		t.Fatal(err)
	}
	// Deliberately put the process clock behind PostgreSQL beyond the archive
	// threshold. A newly received signal must still block archive.
	now := dbBefore.Add(-4 * time.Hour)
	repo.now = func() time.Time { return now }
	if _, err := fixture.db.Exec(ctx, `update monitoring_agent_sessions set started_at=clock_timestamp()-interval '1 day'; insert into receiver_health(id,boot_id,healthy_since,checked_at,healthy) values(true,'signal-clock-test',clock_timestamp()-interval '181 minutes',clock_timestamp(),true)`); err != nil {
		t.Fatal(err)
	}
	batch := syncing.Batch{SessionID: "mas_live", MonitoringInstanceID: "mi_live", SyncToken: "mas_live.secret", LiveSignal: &agentapi.LiveSignal{ID: "one", Fingerprint: "fp_live"}}
	result, err := repo.ApplyBatch(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	if !result.StopCollection {
		t.Fatal("paused instance must suppress collection")
	}
	var received time.Time
	var lifecycle string
	var ever bool
	if err := fixture.db.QueryRow(ctx, `select last_trusted_online_at,lifecycle_status,ever_connected from monitoring_instances where monitoring_instance_id='mi_live'`).Scan(&received, &lifecycle, &ever); err != nil {
		t.Fatal(err)
	}
	if received.Before(dbBefore) || lifecycle != "已接入" || !ever {
		t.Fatalf("first heartbeat: %v %s %v", received, lifecycle, ever)
	}
	review, err := NewPostgresAssetLifecycleRepository(pool).GetVPSArchiveReview(ctx, "vps_live")
	if err != nil {
		t.Fatal(err)
	}
	if review.Eligible {
		t.Fatal("process clock skew made fresh signal archivable")
	}
	firstReceived := received
	now = now.Add(time.Hour)
	if _, err := repo.ApplyBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(ctx, `select last_trusted_online_at from monitoring_instances where monitoring_instance_id='mi_live'`).Scan(&received); err != nil {
		t.Fatal(err)
	}
	if !received.Equal(firstReceived) {
		t.Fatal("duplicate refreshed online evidence")
	}
	if _, err := fixture.db.Exec(ctx, `update monitoring_agent_sessions set capability='evidence_only',ended_at=now() where session_id='mas_live'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `update monitoring_instances set lifecycle_status='已退役',binding_status='未绑定',binding_fingerprint=null where monitoring_instance_id='mi_live'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `update vps_assets set lifecycle_status='archived' where vps_id='vps_live'`); err != nil {
		t.Fatal(err)
	}
	batch.LiveSignal.ID = "two"
	if result, err = repo.ApplyBatch(ctx, batch); err != nil || !result.StopCollection {
		t.Fatalf("old session must remain evidence-only: %#v %v", result, err)
	}
	batch.LiveSignal.ID = "three"
	if _, err := repo.ApplyBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := fixture.db.QueryRow(ctx, `select count(*) from vps_followups where vps_id='vps_live' and kind='archived_online'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reappearance followups=%d", count)
	}
}

func TestPostgresIntegrationFreshInstanceEnrollmentAfterRetiredPredecessor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "fresh-instance-after-retirement", 4)
	if _, err := fixture.db.Exec(ctx, `insert into vps_assets(vps_id,display_name,provider_name,region,city,lifecycle_status) values('vps_replacement','Replacement','Fixture','Region','City','active')`); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresMonitoringInstanceRepository(pool)
	wire := monitoringinstances.LinkedCreateWireIdentity{DisplayName: "Old instance", Provider: "Fixture", Region: "Region", City: "City"}
	old, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, "vps_replacement", wire, "create-predecessor")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := repo.IssueMonitoringInstanceEnrollmentToken(ctx, old.MonitoringInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	_, oldToken, err := repo.ApplyEnrollment(ctx, enrollment.EnrollInput{Token: issue.Token, Fingerprint: "same-host"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RetireMonitoringInstance(ctx, old.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "replace predecessor", IdempotencyKey: "retire-predecessor"}); err != nil {
		t.Fatal(err)
	}
	wire.DisplayName = "New instance"
	current, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, "vps_replacement", wire, "create-replacement")
	if err != nil {
		t.Fatal(err)
	}
	if current.MonitoringInstanceID == old.MonitoringInstanceID {
		t.Fatal("new instance reused predecessor identity")
	}
	issue, err = repo.IssueMonitoringInstanceEnrollmentToken(ctx, current.MonitoringInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	_, newToken, err := repo.ApplyEnrollment(ctx, enrollment.EnrollInput{Token: issue.Token, Fingerprint: "same-host"})
	if err != nil {
		t.Fatal(err)
	}
	oldSession, _, _ := strings.Cut(oldToken, ".")
	newSession, _, _ := strings.Cut(newToken, ".")
	syncRepo := NewPostgresSyncRepository(pool)
	oldResult, err := syncRepo.ApplyBatch(ctx, syncing.Batch{SessionID: oldSession, MonitoringInstanceID: old.MonitoringInstanceID, SyncToken: oldToken, LiveSignal: &agentapi.LiveSignal{ID: "old-remaining-process", Fingerprint: "same-host"}})
	if err != nil || !oldResult.StopCollection {
		t.Fatalf("predecessor regained collection: %#v %v", oldResult, err)
	}
	newResult, err := syncRepo.ApplyBatch(ctx, syncing.Batch{SessionID: newSession, MonitoringInstanceID: current.MonitoringInstanceID, SyncToken: newToken, LiveSignal: &agentapi.LiveSignal{ID: "new-process", Fingerprint: "same-host"}})
	if err != nil || newResult.StopCollection {
		t.Fatalf("replacement failed enrollment: %#v %v", newResult, err)
	}
	var currentCount int
	if err := fixture.db.QueryRow(ctx, `select count(*) from monitoring_instances where vps_id='vps_replacement' and lifecycle_status='已接入'`).Scan(&currentCount); err != nil {
		t.Fatal(err)
	}
	if currentCount != 1 {
		t.Fatalf("current enrolled instances=%d", currentCount)
	}
}

func TestPostgresIntegrationReenrollmentNeverReactivatesOldSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "session-reenrollment", 4)
	if _, err := fixture.db.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_reenroll','Reenrollment','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status,enrollment_token_hash,enrollment_token_issued_at) values('mi_reenroll','vps_reenroll','Reenrollment','','','','待接入','启用','未绑定',$1,now())`, hashEnrollmentToken("enroll_first")); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresMonitoringInstanceRepository(pool)
	_, oldToken, err := repo.ApplyEnrollment(ctx, enrollment.EnrollInput{Token: "enroll_first", Fingerprint: "fp_old"})
	if err != nil {
		t.Fatal(err)
	}
	oldID, _, ok := strings.Cut(oldToken, ".")
	if !ok {
		t.Fatal("session credential lacks session identity")
	}
	if _, err := repo.ResetMonitoringInstanceBinding(ctx, "mi_reenroll"); err != nil {
		t.Fatal(err)
	}
	reenrollIssue, err := repo.IssueMonitoringInstanceEnrollmentToken(ctx, "mi_reenroll")
	if err != nil {
		t.Fatal(err)
	}
	_, newToken, err := repo.ApplyEnrollment(ctx, enrollment.EnrollInput{Token: reenrollIssue.Token, Fingerprint: "fp_new"})
	if err != nil {
		t.Fatal(err)
	}
	newID, _, ok := strings.Cut(newToken, ".")
	if !ok || newID == oldID {
		t.Fatal("reenrollment reused old identity")
	}
	syncRepo := NewPostgresSyncRepository(pool)
	oldBatch := syncing.Batch{SessionID: oldID, MonitoringInstanceID: "mi_reenroll", SyncToken: oldToken, LiveSignal: &agentapi.LiveSignal{ID: "old-live", Fingerprint: "fp_old"}}
	if result, err := syncRepo.ApplyBatch(ctx, oldBatch); err != nil || !result.StopCollection {
		t.Fatalf("old credential regained permission or disappeared: %#v %v", result, err)
	}
	newBatch := syncing.Batch{SessionID: newID, MonitoringInstanceID: "mi_reenroll", SyncToken: newToken, LiveSignal: &agentapi.LiveSignal{ID: "new-live", Fingerprint: "fp_new"}}
	if result, err := syncRepo.ApplyBatch(ctx, newBatch); err != nil || result.StopCollection {
		t.Fatalf("new credential failed full permission: %#v %v", result, err)
	}
	var capability string
	var ended bool
	if err := fixture.db.QueryRow(ctx, `select capability,ended_at is not null from monitoring_agent_sessions where session_id=$1`, oldID).Scan(&capability, &ended); err != nil {
		t.Fatal(err)
	}
	if capability != "evidence_only" || !ended {
		t.Fatal("old stage was not ended and downgraded")
	}
}
