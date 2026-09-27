package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
)

func TestVPSStateRepairMIUnlinkedCreateRejectsEveryLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	for _, lifecycle := range []string{monitoringinstances.LifecyclePendingEnrollment, monitoringinstances.LifecycleEnrolled, monitoringinstances.LifecycleRetired} {
		_, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{DisplayName: "Orphan", LifecycleStatus: lifecycle})
		if !errors.Is(err, monitoringinstances.ErrInvalidCreateInput) {
			t.Fatalf("orphan %q: %v", lifecycle, err)
		}
	}
	assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from monitoring_instances where display_name=$1", "Orphan", 0)
}

func TestVPSStateRepairMIRetirementPreservesEvidenceAndRequiresExplicitReenrollment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	record := createVPSStateRepairOwnedMI(t, ctx, pool, repo, "vps_retirement", "MI retirement")
	issue, err := repo.IssueMonitoringInstanceEnrollmentToken(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	_, oldToken, err := repo.ApplyEnrollment(ctx, enrollment.EnrollInput{Token: issue.Token, Fingerprint: "fingerprint-current"})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _, _ := strings.Cut(oldToken, ".")
	if _, err := pool.Exec(ctx, `
  update monitoring_instances set binding_status='指纹变更待确认',
   pending_binding_fingerprint='fingerprint-pending',pending_binding_first_seen_at=now(),
   pending_binding_last_seen_at=now(),pending_binding_attempt_count=2,
   pending_action_id='act_pending_retire',pending_action_command_id='uptime',
   last_action='{"action_id":"act_pending_retire","command_id":"uptime","status":"pending"}'::jsonb
  where monitoring_instance_id=$1`, record.MonitoringInstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into monitoring_instance_command_action_audit
  (audit_id,action_id,monitoring_instance_id,command_id,sensitivity,event_type,source,exit_code,occurred_at)
  values('audit_completed','act_completed',$1,'uptime','standard','completed','agent_sync',0,now())`, record.MonitoringInstanceID); err != nil {
		t.Fatal(err)
	}
	input := monitoringinstances.LifecycleActionInput{Reason: "end monitoring", IdempotencyKey: "retire-once"}
	retired, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, input)
	if err != nil {
		t.Fatal(err)
	}
	if retired.LifecycleStatus != monitoringinstances.LifecycleRetired || retired.MonitoringStatus != monitoringinstances.MonitoringPaused ||
		retired.BindingStatus != monitoringinstances.BindingBound || retired.EnrollmentTokenHash != "" ||
		retired.EnrollmentTokenIssuedAt != nil || retired.EnrollmentTokenConsumedAt != nil ||
		retired.PendingBindingFingerprint != "" || retired.PendingBindingFirstSeenAt != nil ||
		retired.PendingBindingLastSeenAt != nil || retired.PendingBindingAttemptCount != 0 ||
		retired.PendingActionID != "" || retired.PendingActionCommandID != "" || retired.LastAction != nil {
		t.Fatalf("retirement invariants: %#v", retired)
	}
	var capability, tokenHash string
	var ended bool
	if err := pool.QueryRow(ctx, `select capability,token_hash,ended_at is not null from monitoring_agent_sessions where session_id=$1`, sessionID).Scan(&capability, &tokenHash, &ended); err != nil {
		t.Fatal(err)
	}
	if capability != "evidence_only" || tokenHash != hashSyncToken(oldToken) || !ended {
		t.Fatal("retirement discarded or restored old session authority")
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 1)
	assertVPSStateRepairMICommandAuditCount(t, ctx, pool, record.MonitoringInstanceID, 2)
	assertVPSStateRepairMIIntValue(t, ctx, pool, `select count(*)::int from monitoring_instance_command_action_audit where monitoring_instance_id=$1 and event_type='cancelled'`, record.MonitoringInstanceID, 1)
	if _, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, input); err != nil {
		t.Fatalf("same-request replay: %v", err)
	}
	input.IdempotencyKey = "retire-again"
	if _, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, input); !errors.Is(err, monitoringinstances.ErrManagementActionBlocked) {
		t.Fatalf("new repeat request: %v", err)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 1)
	assertVPSStateRepairMICommandAuditCount(t, ctx, pool, record.MonitoringInstanceID, 2)
	var payload string
	if err := pool.QueryRow(ctx, `select payload::text from state_change_events where object_id=$1 and event_type=$2`, record.MonitoringInstanceID, string(incidents.EventMonitoringInstanceRetired)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, oldToken) || strings.Contains(payload, "fingerprint-pending") {
		t.Fatal("lifecycle audit leaked identity credential")
	}
	for name, call := range map[string]func() error{
		"resume": func() error {
			_, e := repo.ResumeMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID)
			return e
		},
		"maintenance": func() error {
			_, e := repo.SetMonitoringInstanceMonitoringMaintenance(ctx, record.MonitoringInstanceID)
			return e
		},
		"enrollment token": func() error {
			_, e := repo.IssueMonitoringInstanceEnrollmentToken(ctx, record.MonitoringInstanceID)
			return e
		},
		"sync token": func() error { _, e := repo.IssueSyncToken(ctx, record.MonitoringInstanceID); return e },
	} {
		if err := call(); !errors.Is(err, monitoringinstances.ErrRetiredMonitoringInstance) {
			t.Fatalf("%s on retired: %v", name, err)
		}
	}
	pending, err := repo.ResetMonitoringInstanceBinding(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.MonitoringInstanceID != record.MonitoringInstanceID || pending.VPSID != record.VPSID || pending.LifecycleStatus != monitoringinstances.LifecyclePendingEnrollment || pending.BindingStatus != monitoringinstances.BindingUnbound || pending.SyncTokenHash != "" || pending.PendingActionID != "" {
		t.Fatalf("explicit reenrollment: %#v", pending)
	}
	issue, err = repo.IssueMonitoringInstanceEnrollmentToken(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	_, newToken, err := repo.ApplyEnrollment(ctx, enrollment.EnrollInput{Token: issue.Token, Fingerprint: "fingerprint-current"})
	if err != nil {
		t.Fatal(err)
	}
	newSession, _, _ := strings.Cut(newToken, ".")
	if newSession == sessionID {
		t.Fatal("reenrollment reused old session")
	}
	if err := pool.QueryRow(ctx, `select capability from monitoring_agent_sessions where session_id=$1`, sessionID).Scan(&capability); err != nil {
		t.Fatal(err)
	}
	if capability != "evidence_only" {
		t.Fatal("reenrollment reactivated old session")
	}
	assertVPSStateRepairMICommandAuditCount(t, ctx, pool, record.MonitoringInstanceID, 2)
}

func TestVPSStateRepairMILinkedCreateRejectsRetiredLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	if _, err := pool.Exec(ctx, "insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_retired_create','Retired guard','active')"); err != nil {
		t.Fatal(err)
	}
	_, _, err := repo.CreateLinkedMonitoringInstance(ctx, "vps_retired_create", monitoringinstances.CreateInput{DisplayName: "Retired linked MI", LifecycleStatus: monitoringinstances.LifecycleRetired}, "")
	if !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
		t.Fatalf("retired create: %v", err)
	}
	assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from monitoring_instances where vps_id=$1", "vps_retired_create", 0)
	assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from vps_monitoring_instance_links where vps_id=$1", "vps_retired_create", 0)
}

func createVPSStateRepairOwnedMI(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *PostgresMonitoringInstanceRepository, vpsID, name string) monitoringinstances.Record {
	t.Helper()
	if _, err := pool.Exec(ctx, "insert into vps_assets(vps_id,display_name,provider_name,region,city,lifecycle_status) values($1,$2,'Fixture','Region','City','active')", vpsID, name); err != nil {
		t.Fatal(err)
	}
	record, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, vpsID, monitoringinstances.LinkedCreateWireIdentity{DisplayName: name, Provider: "Fixture", Region: "Region", City: "City"}, "create-"+vpsID)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func assertVPSStateRepairMIEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, event incidents.EventType, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, "select count(*)::int from state_change_events where object_type='monitoring_instance' and object_id=$1 and event_type=$2", id, string(event)).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("event %q count=%d want=%d", event, got, want)
	}
}

func assertVPSStateRepairMICommandAuditCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, want int) {
	t.Helper()
	assertVPSStateRepairMIIntValue(t, ctx, pool, "select count(*)::int from monitoring_instance_command_action_audit where monitoring_instance_id=$1", id, want)
}
