package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/commandaudits"
	"houfeng/internal/center/monitoringinstances"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationCommandActionAuditWritePathsAndCleanup(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryCommandActionPostgresSchema(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	if _, err := db.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name)
		values ('usr_audit', 'audit-admin', 'hash', '审计管理员')
	`); err != nil {
		t.Fatalf("insert audit actor: %v", err)
	}

	repo := NewPostgresMonitoringInstanceRepository(db)
	record, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Tokyo Audit",
		Group:           "production",
		Region:          "ap-northeast",
		City:            "Tokyo",
		Provider:        "test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{"audit"},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance() error = %v", err)
	}
	setCommandAuditMonitoringInstanceExecutable(t, ctx, db, record.MonitoringInstanceID)

	queuedAt := time.Date(2026, time.July, 12, 8, 30, 0, 0, time.UTC)
	if err := repo.QueueCommandAction(ctx, record.MonitoringInstanceID, monitoringinstances.QueueCommandActionInput{
		ActionID:    "act_valid",
		CommandID:   "uptime",
		Sensitivity: "standard",
		ActorUserID: "usr_audit",
		Source:      monitoringinstances.CommandActionSourceWeb,
		QueuedAt:    queuedAt,
	}); err != nil {
		t.Fatalf("QueueCommandAction() error = %v", err)
	}
	if err := repo.RecordRejectedCommandAction(ctx, record.MonitoringInstanceID, monitoringinstances.RejectedCommandActionInput{
		CommandID:   "systemctl_status",
		Sensitivity: "sensitive",
		ActorUserID: "usr_audit",
		Source:      monitoringinstances.CommandActionSourceWeb,
		OccurredAt:  queuedAt.Add(time.Minute),
	}); err != nil {
		t.Fatalf("RecordRejectedCommandAction() error = %v", err)
	}

	var auditCount int
	var instanceName, actorUsername, actorDisplayName string
	if err := db.QueryRow(ctx, `
		select count(*)::int,
		       min(monitoring_instance_name_snapshot),
		       min(actor_username_snapshot),
		       min(actor_display_name_snapshot)
		from monitoring_instance_command_action_audit
		where monitoring_instance_id = $1
	`, record.MonitoringInstanceID).Scan(&auditCount, &instanceName, &actorUsername, &actorDisplayName); err != nil {
		t.Fatalf("query command audit snapshots: %v", err)
	}
	if auditCount != 2 || instanceName != "Tokyo Audit" || actorUsername != "audit-admin" || actorDisplayName != "审计管理员" {
		t.Fatalf("audit rows/snapshots = (%d, %q, %q, %q)", auditCount, instanceName, actorUsername, actorDisplayName)
	}

	err = repo.QueueCommandAction(ctx, record.MonitoringInstanceID, monitoringinstances.QueueCommandActionInput{
		ActionID:    "act_forged_actor",
		CommandID:   "uptime",
		Sensitivity: "standard",
		ActorUserID: "usr_missing",
		Source:      monitoringinstances.CommandActionSourceWeb,
		QueuedAt:    queuedAt.Add(2 * time.Minute),
	})
	if err == nil || !strings.Contains(err.Error(), "inserted exactly one row") {
		t.Fatalf("QueueCommandAction() forged actor error = %v, want integrity error", err)
	}
	var pendingActionID string
	if err := db.QueryRow(ctx, `
		select pending_action_id
		from monitoring_instances
		where monitoring_instance_id = $1
	`, record.MonitoringInstanceID).Scan(&pendingActionID); err != nil {
		t.Fatalf("query pending action after rollback: %v", err)
	}
	if pendingActionID != "act_valid" {
		t.Fatalf("pending action after forged actor = %q, want rolled back act_valid", pendingActionID)
	}
	if err := db.QueryRow(ctx, `
		select count(*)::int
		from monitoring_instance_command_action_audit
		where monitoring_instance_id = $1
	`, record.MonitoringInstanceID).Scan(&auditCount); err != nil {
		t.Fatalf("count command audits after rollback: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("audit count after forged actor = %d, want 2", auditCount)
	}

	err = repo.RecordRejectedCommandAction(ctx, "mi_never_existed", monitoringinstances.RejectedCommandActionInput{
		CommandID:   "systemctl_status",
		Sensitivity: "sensitive",
		ActorUserID: "usr_audit",
		Source:      monitoringinstances.CommandActionSourceWeb,
		OccurredAt:  queuedAt.Add(3 * time.Minute),
	})
	if err == nil || !strings.Contains(err.Error(), "inserted exactly one row") {
		t.Fatalf("RecordRejectedCommandAction() missing instance error = %v, want integrity error", err)
	}

	if _, err := db.Exec(ctx, `
		update monitoring_instances
		set archived_at = now(), archived_reason = 'integration cleanup'
		where monitoring_instance_id = $1
	`, record.MonitoringInstanceID); err != nil {
		t.Fatalf("archive monitoring instance: %v", err)
	}
	cleanup, err := repo.PermanentCleanupMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.PermanentCleanupInput{
		Reason:           "integration cleanup",
		ConfirmationName: "Tokyo Audit",
	})
	if err != nil {
		t.Fatalf("PermanentCleanupMonitoringInstance() error = %v", err)
	}
	if !cleanup.Deleted || cleanup.Counts.CommandActionAuditCount != 2 || cleanup.DeletedReferenceCount != 0 {
		t.Fatalf("cleanup result = %#v, want two preserved audits excluded from deleted references", cleanup)
	}
	if err := db.QueryRow(ctx, `
		select count(*)::int
		from monitoring_instance_command_action_audit
		where monitoring_instance_id = $1
	`, record.MonitoringInstanceID).Scan(&auditCount); err != nil {
		t.Fatalf("count command audits after cleanup: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("audit count after cleanup = %d, want 2", auditCount)
	}
}

func TestPostgresIntegrationRejectedAuditRequiresCurrentlyExecutableInstance(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryCommandActionPostgresSchema(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name)
		values ('usr_rejection_gate', 'rejection-gate', 'hash', '拒绝审计操作者')
	`); err != nil {
		t.Fatalf("insert rejection audit actor: %v", err)
	}

	repo := NewPostgresMonitoringInstanceRepository(db)
	record, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Rejection Gate",
		Group:           "review",
		Region:          "test",
		City:            "test",
		Provider:        "test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{"review"},
	})
	if err != nil {
		t.Fatalf("create rejection gate monitoring instance: %v", err)
	}

	setState := func(bindingStatus, monitoringStatus string, archivedAt any) {
		t.Helper()
		if _, err := db.Exec(ctx, `
			update monitoring_instances
			set binding_status = $2,
			    monitoring_status = $3,
			    archived_at = $4,
			    archived_reason = case when $4::timestamptz is null then '' else 'review gate' end
			where monitoring_instance_id = $1
		`, record.MonitoringInstanceID, bindingStatus, monitoringStatus, archivedAt); err != nil {
			t.Fatalf("set rejection gate state: %v", err)
		}
	}
	input := monitoringinstances.RejectedCommandActionInput{
		CommandID:   "systemctl_status",
		Sensitivity: "sensitive",
		ActorUserID: "usr_rejection_gate",
		Source:      monitoringinstances.CommandActionSourceWeb,
		OccurredAt:  time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC),
	}

	setState(monitoringinstances.BindingBound, monitoringinstances.MonitoringEnabled, nil)
	if err := repo.RecordRejectedCommandAction(ctx, record.MonitoringInstanceID, input); err != nil {
		t.Fatalf("record executable rejection audit: %v", err)
	}

	nonExecutableStates := []struct {
		name             string
		bindingStatus    string
		monitoringStatus string
		archivedAt       any
	}{
		{name: "unbound", bindingStatus: monitoringinstances.BindingUnbound, monitoringStatus: monitoringinstances.MonitoringEnabled},
		{name: "paused", bindingStatus: monitoringinstances.BindingBound, monitoringStatus: monitoringinstances.MonitoringPaused},
		{name: "archived", bindingStatus: monitoringinstances.BindingBound, monitoringStatus: monitoringinstances.MonitoringEnabled, archivedAt: time.Date(2026, time.July, 12, 9, 5, 0, 0, time.UTC)},
	}
	for _, state := range nonExecutableStates {
		t.Run(state.name, func(t *testing.T) {
			setState(state.bindingStatus, state.monitoringStatus, state.archivedAt)
			err := repo.RecordRejectedCommandAction(ctx, record.MonitoringInstanceID, input)
			if err == nil || !strings.Contains(err.Error(), "inserted exactly one row") {
				t.Fatalf("RecordRejectedCommandAction() error = %v, want fail-closed integrity error", err)
			}
		})
	}

	var auditCount int
	if err := db.QueryRow(ctx, `
		select count(*)::int
		from monitoring_instance_command_action_audit
		where monitoring_instance_id = $1
	`, record.MonitoringInstanceID).Scan(&auditCount); err != nil {
		t.Fatalf("count rejection gate audits: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("rejection gate audit count = %d, want only the executable attempt", auditCount)
	}
}

func TestPostgresIntegrationCommandAuditReadModelFiltersOutcomesAndKeyset(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryCommandActionPostgresSchema(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name)
		values ('usr_audit', 'audit%_admin', 'hash', '审计 %_ 管理员')
	`); err != nil {
		t.Fatalf("insert audit actor: %v", err)
	}

	monitoringRepo := NewPostgresMonitoringInstanceRepository(db)
	active, err := monitoringRepo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Tokyo Active",
		Group:           "production",
		Region:          "ap-northeast",
		City:            "Tokyo",
		Provider:        "test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{"audit"},
	})
	if err != nil {
		t.Fatalf("create active monitoring instance: %v", err)
	}
	deleted, err := monitoringRepo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Literal %_ Edge",
		Group:           "retired",
		Region:          "eu-west",
		City:            "Paris",
		Provider:        "test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{"audit"},
	})
	if err != nil {
		t.Fatalf("create deleted monitoring instance: %v", err)
	}
	setCommandAuditMonitoringInstanceExecutable(t, ctx, db, active.MonitoringInstanceID)
	setCommandAuditMonitoringInstanceExecutable(t, ctx, db, deleted.MonitoringInstanceID)

	upperBound := time.Date(2026, time.July, 12, 12, 0, 10, 0, time.UTC)
	sameStartedAt := upperBound.Add(-10 * time.Second)
	seedQueuedCommandAudit(t, ctx, monitoringRepo, active.MonitoringInstanceID, "act_success", "uptime", sameStartedAt)
	seedCommandAuditEvent(t, ctx, db, commandActionAuditEvent{
		ActionID: "act_success", MonitoringInstanceID: active.MonitoringInstanceID, CommandID: "uptime", Sensitivity: "standard", EventType: "dispatched", Source: monitoringinstances.CommandActionSourceAgentSync, OccurredAt: sameStartedAt.Add(time.Second),
	})
	successExitCode := 0
	seedCommandAuditEvent(t, ctx, db, commandActionAuditEvent{
		ActionID: "act_success", MonitoringInstanceID: active.MonitoringInstanceID, CommandID: "uptime", Sensitivity: "standard", EventType: "completed", Source: monitoringinstances.CommandActionSourceAgentSync, ExitCode: &successExitCode, OccurredAt: sameStartedAt.Add(2 * time.Second),
	})

	seedQueuedCommandAudit(t, ctx, monitoringRepo, active.MonitoringInstanceID, "act_failure", "uptime", sameStartedAt)
	failureExitCode := 7
	seedCommandAuditEvent(t, ctx, db, commandActionAuditEvent{
		ActionID: "act_failure", MonitoringInstanceID: active.MonitoringInstanceID, CommandID: "uptime", Sensitivity: "standard", EventType: "completed", Source: monitoringinstances.CommandActionSourceAgentSync, ExitCode: &failureExitCode, OccurredAt: sameStartedAt.Add(3 * time.Second),
	})

	dispatchedAt := sameStartedAt.Add(-time.Second)
	seedQueuedCommandAudit(t, ctx, monitoringRepo, active.MonitoringInstanceID, "act_dispatched", "uptime", dispatchedAt)
	seedCommandAuditEvent(t, ctx, db, commandActionAuditEvent{
		ActionID: "act_dispatched", MonitoringInstanceID: active.MonitoringInstanceID, CommandID: "uptime", Sensitivity: "standard", EventType: "dispatched", Source: monitoringinstances.CommandActionSourceAgentSync, OccurredAt: dispatchedAt.Add(500 * time.Millisecond),
	})

	queuedAt := dispatchedAt.Add(-time.Second)
	seedQueuedCommandAudit(t, ctx, monitoringRepo, active.MonitoringInstanceID, "act_queued", "uptime", queuedAt)
	seedCommandAuditEvent(t, ctx, db, commandActionAuditEvent{
		ActionID: "act_queued", MonitoringInstanceID: active.MonitoringInstanceID, CommandID: "uptime", Sensitivity: "standard", EventType: "completed", Source: monitoringinstances.CommandActionSourceAgentSync, ExitCode: &successExitCode, OccurredAt: upperBound.Add(time.Second),
	})

	rejectedAt := queuedAt.Add(-time.Second)
	if err := monitoringRepo.RecordRejectedCommandAction(ctx, deleted.MonitoringInstanceID, monitoringinstances.RejectedCommandActionInput{
		CommandID:   "systemctl_status",
		Sensitivity: "sensitive",
		ActorUserID: "usr_audit",
		Source:      monitoringinstances.CommandActionSourceWeb,
		OccurredAt:  rejectedAt,
	}); err != nil {
		t.Fatalf("record rejected command audit: %v", err)
	}
	if _, err := db.Exec(ctx, `delete from monitoring_instances where monitoring_instance_id = $1`, deleted.MonitoringInstanceID); err != nil {
		t.Fatalf("delete monitoring instance after snapshot: %v", err)
	}
	if _, err := db.Exec(ctx, `delete from users where user_id = 'usr_audit'`); err != nil {
		t.Fatalf("delete audit actor after snapshot: %v", err)
	}

	repo := NewPostgresCommandAuditRepository(db)
	startedFrom := upperBound.Add(-time.Hour)
	page, err := repo.ListCommandAudits(ctx, commandaudits.Query{
		StartedFrom: &startedFrom,
		StartedTo:   upperBound,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListCommandAudits() error = %v", err)
	}
	if page.HasMore || len(page.Items) != 5 {
		t.Fatalf("page = %#v, want five outcomes", page)
	}
	wantIDs := []string{"act_success", "act_failure", "act_dispatched", "act_queued"}
	wantOutcomes := []string{"succeeded", "failed", "dispatched", "queued", "rejected"}
	for i, wantOutcome := range wantOutcomes {
		if page.Items[i].Outcome != wantOutcome {
			t.Fatalf("item[%d] = %#v, want outcome %q", i, page.Items[i], wantOutcome)
		}
		if i < len(wantIDs) && page.Items[i].ID != wantIDs[i] {
			t.Fatalf("item[%d].ID = %q, want %q", i, page.Items[i].ID, wantIDs[i])
		}
		for eventIndex := 1; eventIndex < len(page.Items[i].Events); eventIndex++ {
			previous := page.Items[i].Events[eventIndex-1]
			current := page.Items[i].Events[eventIndex]
			if current.OccurredAt.Before(previous.OccurredAt) || (current.OccurredAt.Equal(previous.OccurredAt) && current.AuditID < previous.AuditID) {
				t.Fatalf("item[%d] events are not stable ascending: %#v", i, page.Items[i].Events)
			}
		}
	}
	if len(page.Items[3].Events) != 1 {
		t.Fatalf("queued action events = %#v, future completion must be excluded", page.Items[3].Events)
	}
	rejected := page.Items[4]
	if rejected.ActionID != "" || !rejected.MonitoringInstance.Deleted || rejected.MonitoringInstance.Name != "Literal %_ Edge" {
		t.Fatalf("rejected deleted identity = %#v", rejected)
	}
	if rejected.Actor == nil || rejected.Actor.Username != "audit%_admin" || rejected.Actor.DisplayName != "审计 %_ 管理员" {
		t.Fatalf("rejected actor snapshot = %#v", rejected.Actor)
	}

	firstPage, err := repo.ListCommandAudits(ctx, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 2})
	if err != nil {
		t.Fatalf("first keyset page: %v", err)
	}
	if !firstPage.HasMore || len(firstPage.Items) != 2 || firstPage.Items[0].ID != "act_success" || firstPage.Items[1].ID != "act_failure" {
		t.Fatalf("first keyset page = %#v", firstPage)
	}
	before := firstPage.Items[1].StartedAt
	secondPage, err := repo.ListCommandAudits(ctx, commandaudits.Query{
		StartedFrom:     &startedFrom,
		StartedTo:       upperBound,
		Limit:           2,
		BeforeStartedAt: &before,
		BeforeID:        firstPage.Items[1].ID,
	})
	if err != nil {
		t.Fatalf("second keyset page: %v", err)
	}
	if !secondPage.HasMore || len(secondPage.Items) != 2 || secondPage.Items[0].ID != "act_dispatched" || secondPage.Items[1].ID != "act_queued" {
		t.Fatalf("second keyset page = %#v", secondPage)
	}

	for _, outcome := range []string{"rejected", "queued", "dispatched", "succeeded", "failed"} {
		assertCommandAuditFilterCount(t, ctx, repo, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 10, Outcome: outcome}, 1)
	}
	assertCommandAuditFilterCount(t, ctx, repo, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 10, CommandID: "systemctl_status"}, 1)
	assertCommandAuditFilterCount(t, ctx, repo, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 10, Sensitivity: "sensitive"}, 1)
	assertCommandAuditFilterCount(t, ctx, repo, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 10, MonitoringInstance: `%_`}, 1)
	assertCommandAuditFilterCount(t, ctx, repo, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 10, Actor: `audit%_admin`}, 5)
	assertCommandAuditFilterCount(t, ctx, repo, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 10, Actor: `auditXadmin`}, 0)
	assertCommandAuditFilterCount(t, ctx, repo, commandaudits.Query{StartedFrom: &startedFrom, StartedTo: upperBound, Limit: 10, ActionID: "act_dispatched"}, 1)
}

func TestPostgresIntegrationCommandAuditQueryPlanIsWindowAndLimitBounded(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryCommandActionPostgresSchema(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	monitoringRepo := NewPostgresMonitoringInstanceRepository(db)
	record, err := monitoringRepo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Command Audit Explain",
		Group:           "performance",
		Region:          "test",
		City:            "test",
		Provider:        "test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{"command-audit-explain"},
	})
	if err != nil {
		t.Fatalf("create explain monitoring instance: %v", err)
	}

	upperBound := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	if _, err := db.Exec(ctx, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, monitoring_instance_name_snapshot,
			command_id, sensitivity, event_type, source, occurred_at
		)
		select
			'cmd_aud_perf_old_' || series,
			'act_perf_old_' || series,
			$1,
			$2,
			'uptime',
			'standard',
			'queued',
			'web',
			$3::timestamptz - interval '120 days' + series * interval '1 second'
		from generate_series(1, 8000) as series
	`, record.MonitoringInstanceID, record.DisplayName, upperBound); err != nil {
		t.Fatalf("seed out-of-window command audits: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, monitoring_instance_name_snapshot,
			command_id, sensitivity, event_type, source, occurred_at
		)
		select
			'cmd_aud_perf_live_' || series,
			'act_perf_live_' || series,
			$1,
			$2,
			'uptime',
			'standard',
			'queued',
			'web',
			$3::timestamptz - interval '29 days' + series * interval '2 hours'
		from generate_series(1, 240) as series
	`, record.MonitoringInstanceID, record.DisplayName, upperBound); err != nil {
		t.Fatalf("seed in-window command audits: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, monitoring_instance_name_snapshot,
			command_id, sensitivity, event_type, source, exit_code, occurred_at
		)
		select
			'cmd_aud_perf_completed_' || series,
			'act_perf_live_' || series,
			$1,
			$2,
			'uptime',
			'standard',
			'completed',
			'agent_sync',
			case when series % 2 = 0 then 0 else 7 end,
			$3::timestamptz - interval '29 days' + series * interval '2 hours' + interval '1 second'
		from generate_series(1, 120) as series
	`, record.MonitoringInstanceID, record.DisplayName, upperBound); err != nil {
		t.Fatalf("seed completed command audits: %v", err)
	}
	if _, err := db.Exec(ctx, `analyze monitoring_instance_command_action_audit`); err != nil {
		t.Fatalf("analyze command audit table: %v", err)
	}

	startedFrom := upperBound.Add(-30 * 24 * time.Hour)
	countingDB := &countingCommandAuditPostgresQueryer{db: db}
	page, err := newPostgresCommandAuditRepository(countingDB).ListCommandAudits(ctx, commandaudits.Query{
		StartedFrom: &startedFrom,
		StartedTo:   upperBound,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("run representative command audit page: %v", err)
	}
	if countingDB.calls != 2 || len(page.Items) != 20 || !page.HasMore {
		t.Fatalf("representative page calls/items/has_more = (%d, %d, %t), want exactly two queries and bounded 20-row page", countingDB.calls, len(page.Items), page.HasMore)
	}

	for _, outcome := range []string{"", "failed"} {
		name := "default-window"
		if outcome != "" {
			name = "outcome-" + outcome
		}
		t.Run(name, func(t *testing.T) {
			plan := explainCommandAuditActions(t, ctx, db, startedFrom, upperBound, outcome, 21)
			limitNode := findCommandAuditExplainNode(plan.Plan, func(node commandAuditExplainNode) bool {
				return node.NodeType == "Limit"
			})
			if limitNode == nil || limitNode.ActualRows > 21 {
				t.Fatalf("EXPLAIN limit node = %#v, want at most 21 rows", limitNode)
			}
			globalIndexNode := findCommandAuditExplainNode(plan.Plan, func(node commandAuditExplainNode) bool {
				return node.IndexName == "idx_monitoring_instance_command_action_audit_global_time"
			})
			if globalIndexNode == nil {
				t.Fatalf("EXPLAIN did not use the global time index: %#v", plan.Plan)
			}
			if globalIndexNode.ActualRows > 400 {
				t.Fatalf("global time index candidates = %.0f, want window-bounded <= 400", globalIndexNode.ActualRows)
			}
			if sequentialAuditScan := findCommandAuditExplainNode(plan.Plan, func(node commandAuditExplainNode) bool {
				return node.NodeType == "Seq Scan" && node.RelationName == "monitoring_instance_command_action_audit"
			}); sequentialAuditScan != nil {
				t.Fatalf("EXPLAIN used an unbounded command audit sequential scan: %#v", *sequentialAuditScan)
			}
			if plan.ExecutionTimeMS > 500 {
				t.Fatalf("EXPLAIN execution time = %.3fms, want <= 500ms", plan.ExecutionTimeMS)
			}
			t.Logf(
				"%s EXPLAIN: execution=%.3fms, limit_rows=%.0f, global_index_rows=%.0f",
				name,
				plan.ExecutionTimeMS,
				limitNode.ActualRows,
				globalIndexNode.ActualRows,
			)
		})
	}
}

type countingCommandAuditPostgresQueryer struct {
	db    *pgxpool.Pool
	calls int
}

func (q *countingCommandAuditPostgresQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.calls++
	return q.db.Query(ctx, sql, args...)
}

type commandAuditExplainDocument struct {
	Plan            commandAuditExplainNode `json:"Plan"`
	ExecutionTimeMS float64                 `json:"Execution Time"`
}

type commandAuditExplainNode struct {
	NodeType     string                    `json:"Node Type"`
	RelationName string                    `json:"Relation Name"`
	IndexName    string                    `json:"Index Name"`
	ActualRows   float64                   `json:"Actual Rows"`
	Plans        []commandAuditExplainNode `json:"Plans"`
}

func explainCommandAuditActions(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	startedFrom time.Time,
	startedTo time.Time,
	outcome string,
	databaseLimit int,
) commandAuditExplainDocument {
	t.Helper()
	var raw []byte
	if err := db.QueryRow(ctx, "explain (analyze, buffers, format json) "+listCommandAuditActionsSQL,
		startedFrom,
		startedTo,
		"",
		"",
		"",
		outcome,
		"",
		"",
		nil,
		"",
		databaseLimit,
	).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN command audit actions: %v", err)
	}
	var documents []commandAuditExplainDocument
	if err := json.Unmarshal(raw, &documents); err != nil {
		t.Fatalf("decode command audit EXPLAIN JSON: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("EXPLAIN documents = %d, want 1", len(documents))
	}
	return documents[0]
}

func findCommandAuditExplainNode(root commandAuditExplainNode, matches func(commandAuditExplainNode) bool) *commandAuditExplainNode {
	if matches(root) {
		matched := root
		return &matched
	}
	for _, child := range root.Plans {
		if matched := findCommandAuditExplainNode(child, matches); matched != nil {
			return matched
		}
	}
	return nil
}

func seedQueuedCommandAudit(t *testing.T, ctx context.Context, repo *PostgresMonitoringInstanceRepository, monitoringInstanceID, actionID, commandID string, occurredAt time.Time) {
	t.Helper()
	if err := repo.QueueCommandAction(ctx, monitoringInstanceID, monitoringinstances.QueueCommandActionInput{
		ActionID:    actionID,
		CommandID:   commandID,
		Sensitivity: "standard",
		ActorUserID: "usr_audit",
		Source:      monitoringinstances.CommandActionSourceWeb,
		QueuedAt:    occurredAt,
	}); err != nil {
		t.Fatalf("queue command audit %q: %v", actionID, err)
	}
}

func setCommandAuditMonitoringInstanceExecutable(t *testing.T, ctx context.Context, db *pgxpool.Pool, monitoringInstanceID string) {
	t.Helper()
	tag, err := db.Exec(ctx, `
		update monitoring_instances
		set binding_status = $2,
		    monitoring_status = $3
		where monitoring_instance_id = $1
	`, monitoringInstanceID, monitoringinstances.BindingBound, monitoringinstances.MonitoringEnabled)
	if err != nil {
		t.Fatalf("make command audit monitoring instance %q executable: %v", monitoringInstanceID, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("make command audit monitoring instance %q executable: updated %d rows", monitoringInstanceID, tag.RowsAffected())
	}
}

func seedCommandAuditEvent(t *testing.T, ctx context.Context, db *pgxpool.Pool, event commandActionAuditEvent) {
	t.Helper()
	if err := insertCommandActionAudit(ctx, db, event); err != nil {
		t.Fatalf("insert command audit event %q/%q: %v", event.ActionID, event.EventType, err)
	}
}

func assertCommandAuditFilterCount(t *testing.T, ctx context.Context, repo *PostgresCommandAuditRepository, query commandaudits.Query, want int) {
	t.Helper()
	page, err := repo.ListCommandAudits(ctx, query)
	if err != nil {
		t.Fatalf("ListCommandAudits(%#v) error = %v", query, err)
	}
	if len(page.Items) != want {
		t.Fatalf("ListCommandAudits(%#v) items = %#v, want %d", query, page.Items, want)
	}
}

func openTemporaryCommandActionPostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for command audit PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for command audit PostgreSQL integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_cmd_audit_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres pool for schema setup: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres database %q: %v", databaseName, err)
	}
	return testPool
}
