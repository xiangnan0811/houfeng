package store

import (
	"context"
	"errors"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/targets"
	"testing"
	"time"
)

func TestPostgresIntegrationSessionDemotionAfterEarlierTransactionStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "monitoring-session-demotion-clock", 4)
	if _, err := fixture.db.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_clock','Clock','active'); insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status) values('mi_clock','vps_clock','Clock','','','','待接入')`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var transactionStarted time.Time
	if err := tx.QueryRow(ctx, `select transaction_timestamp()`).Scan(&transactionStarted); err != nil {
		t.Fatal(err)
	}
	// Model enrollment committing while a retirement transaction is waiting for
	// the asset graph lock. The retiring transaction's now() is already stale.
	var sessionStarted time.Time
	if err := fixture.db.QueryRow(ctx, `insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash,fingerprint_hash) values('mas_clock','mi_clock','clock-token','clock-fingerprint') returning started_at`).Scan(&sessionStarted); err != nil {
		t.Fatal(err)
	}
	if !sessionStarted.After(transactionStarted) {
		t.Fatalf("fixture requires late session: transaction=%v session=%v", transactionStarted, sessionStarted)
	}
	if err := demoteMonitoringSessionsTx(ctx, tx, monitoringinstances.Record{MonitoringInstanceID: "mi_clock"}); err != nil {
		t.Fatalf("demotion used stale transaction time: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var ended time.Time
	var capability string
	if err := fixture.db.QueryRow(ctx, `select ended_at,capability from monitoring_agent_sessions where session_id='mas_clock'`).Scan(&ended, &capability); err != nil {
		t.Fatal(err)
	}
	if ended.Before(sessionStarted) || capability != "evidence_only" {
		t.Fatalf("ended=%v started=%v capability=%s", ended, sessionStarted, capability)
	}
}

func TestPostgresIntegrationMonitoringRetirementReplayAndReenroll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "monitoring-lifecycle", 4)
	for _, sql := range []string{
		`insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_retire','Retire','active'),('vps_other','Other','active')`,
		`insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status,binding_fingerprint,sync_token_hash,ever_connected,last_trusted_online_at) values('mi_retire','vps_retire','Retire','','','','已接入','启用','已绑定','fp','keep-hash',true,now())`,
		`insert into vps_monitoring_instance_links(link_id,vps_id,monitoring_instance_id) values('link_retire','vps_retire','mi_retire')`,
		`insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash,fingerprint_hash,ever_connected) values('mas_retire','mi_retire','keep-hash','fp',true)`,
	} {
		if _, err := fixture.db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewPostgresMonitoringInstanceRepository(pool)
	if err := repo.QueueCommandAction(ctx, "mi_retire", monitoringinstances.QueueCommandActionInput{ActionID: "act_retire", CommandID: "uptime", Source: monitoringinstances.CommandActionSourceWeb}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into active_incidents(incident_id,object_type,object_id,incident_class,severity,started_at,last_evaluated_at,status,source_summary) values('inc_retire','monitoring_instance','mi_retire','monitoring_instance_disk_pressure','告警',now(),now(),'active','disk full')`); err != nil {
		t.Fatal(err)
	}
	input := monitoringinstances.LifecycleActionInput{Reason: "ended", IdempotencyKey: "retirement-request-one"}
	retired, err := repo.RetireMonitoringInstance(ctx, "mi_retire", input)
	if err != nil {
		t.Fatal(err)
	}
	if retired.LifecycleStatus != monitoringinstances.LifecycleRetired {
		t.Fatalf("lifecycle=%s", retired.LifecycleStatus)
	}
	var capability, token string
	var ended *time.Time
	if err := fixture.db.QueryRow(ctx, `select capability,token_hash,ended_at from monitoring_agent_sessions where session_id='mas_retire'`).Scan(&capability, &token, &ended); err != nil {
		t.Fatal(err)
	}
	if capability != "evidence_only" || token != "keep-hash" || ended == nil {
		t.Fatalf("old session=%s/%s/%v", capability, token, ended)
	}
	var cancelled, closed int
	if err := fixture.db.QueryRow(ctx, `select count(*) from monitoring_instance_command_action_audit where action_id='act_retire' and event_type='cancelled'`).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(ctx, `select count(*) from state_change_events where object_id='mi_retire' and event_type='incident_closed_by_management' and payload->>'natural_recovery'='false' and payload->>'closure_reason'='ended'`).Scan(&closed); err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 || closed != 1 {
		t.Fatalf("cancelled=%d management closures=%d", cancelled, closed)
	}
	replay, err := repo.RetireMonitoringInstance(ctx, "mi_retire", input)
	if err != nil || !replay.UpdatedAt.Equal(retired.UpdatedAt) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	input.IdempotencyKey = "retirement-request-two"
	if _, err := repo.RetireMonitoringInstance(ctx, "mi_retire", input); !errors.Is(err, monitoringinstances.ErrManagementActionBlocked) {
		t.Fatalf("duplicate=%v", err)
	}
	active, err := repo.ListMonitoringInstances(ctx)
	if err != nil || len(active) != 0 {
		t.Fatalf("default list=%+v err=%v", active, err)
	}
	if _, err := fixture.db.Exec(ctx, `update monitoring_instances set vps_id='vps_other' where monitoring_instance_id='mi_retire'`); err == nil {
		t.Fatal("ownership changed")
	}
	reset, err := repo.ResetMonitoringInstanceBinding(ctx, "mi_retire")
	if err != nil {
		t.Fatal(err)
	}
	if reset.LifecycleStatus != monitoringinstances.LifecyclePendingEnrollment || !reset.EverConnected || reset.LastTrustedOnlineAt == nil {
		t.Fatalf("reenroll lost durable facts: %+v", reset)
	}
	if _, err := fixture.db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status) values('mi_competing','vps_retire','Competing','','','','待接入')`); err == nil {
		t.Fatal("second current monitoring instance allowed")
	}
	phases, err := repo.ListMonitoringInstancePhases(ctx, "mi_retire")
	if err != nil || len(phases) != 1 || phases[0].Capability != "evidence_only" {
		t.Fatalf("phases=%+v err=%v", phases, err)
	}
	targetRepo := NewPostgresTargetRepository(pool)
	target, err := targetRepo.CreateTarget(ctx, targets.CreateTargetInput{Name: "retire-target", TargetType: targets.TargetTypeService, Host: "example.com", ExecutionMonitoringInstanceLabels: []string{}, Labels: []string{}, RunStatus: targets.RunStatusEnabled})
	if err != nil {
		t.Fatal(err)
	}
	target, err = targetRepo.ArchiveTarget(ctx, target.TargetID)
	if err != nil || target.LifecycleStatus != targets.LifecycleRetired || target.RunStatus != targets.RunStatusPaused {
		t.Fatalf("target retire=%+v err=%v", target, err)
	}
	for _, scope := range []targets.ListScope{targets.ListScopeCurrent, targets.ListScopeRetired, targets.ListScopeAll} {
		listed, err := targetRepo.ListTargetsByScope(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range listed {
			if item.TargetID == target.TargetID {
				found = true
			}
		}
		if found != (scope != targets.ListScopeCurrent) {
			t.Fatalf("retired target visible=%v in scope %s", found, scope)
		}
	}
	target, err = targetRepo.RestoreArchivedTargetToPaused(ctx, target.TargetID)
	if err != nil || target.LifecycleStatus != targets.LifecycleActive || target.RunStatus != targets.RunStatusPaused {
		t.Fatalf("target restore=%+v err=%v", target, err)
	}
}
