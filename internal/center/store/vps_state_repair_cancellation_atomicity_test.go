package store

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairCancellationAtomicityFaultInjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepository := NewPostgresVPSAssetRepository(pool)
	subscriptionRepository := NewPostgresSubscriptionRepository(pool)
	monitoringInstanceRepository := NewPostgresMonitoringInstanceRepository(pool)
	linkRepository := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	targetRepository := NewPostgresTargetRepository(pool)
	serviceRepository := NewPostgresAssetServiceRepository(pool)
	lifecycleRepository := NewPostgresAssetLifecycleRepository(pool)

	failures := []struct {
		name               string
		triggerName        string
		functionName       string
		tableName          string
		triggerTiming      string
		triggerEvent       string
		triggerBody        string
		failureMessage     string
		failedObjectType   string
		failedStepType     string
		completedStepCount int
	}{
		{
			name:             "VPS row change",
			triggerName:      "reject_atomicity_vps_change",
			functionName:     "reject_atomicity_vps_change_fn",
			tableName:        "vps_assets",
			triggerTiming:    "after",
			triggerEvent:     "update",
			triggerBody:      "if new.lifecycle_status = 'to_cancel' then raise exception 'injected cancellation failure at VPS update' using errcode = 'P0001'; end if;",
			failureMessage:   "injected cancellation failure at VPS update",
			failedObjectType: assetlifecycle.ObjectTypeVPS,
			failedStepType:   assetlifecycle.StepTypeVPSLifecycle,
		},
		{
			name:               "subscription row change",
			triggerName:        "reject_atomicity_subscription_change",
			functionName:       "reject_atomicity_subscription_change_fn",
			tableName:          "subscriptions",
			triggerTiming:      "after",
			triggerEvent:       "update",
			triggerBody:        "if new.status = 'cancelled' and new.auto_renew = false and new.auto_renew_cancelled = true then raise exception 'injected cancellation failure at subscription update' using errcode = 'P0001'; end if;",
			failureMessage:     "injected cancellation failure at subscription update",
			failedObjectType:   assetlifecycle.ObjectTypeSubscription,
			failedStepType:     assetlifecycle.StepTypeSubscriptionStatus,
			completedStepCount: 1,
		},
		{
			name:               "MI lifecycle event after row change",
			triggerName:        "reject_atomicity_mi_lifecycle_event",
			functionName:       "reject_atomicity_mi_lifecycle_event_fn",
			tableName:          "state_change_events",
			triggerTiming:      "before",
			triggerEvent:       "insert",
			triggerBody:        "if new.object_type = 'monitoring_instance' and new.event_type = 'monitoring_instance_retired' then raise exception 'injected cancellation failure at MI lifecycle event' using errcode = 'P0001'; end if;",
			failureMessage:     "injected cancellation failure at MI lifecycle event",
			failedObjectType:   assetlifecycle.ObjectTypeMonitoringInstance,
			failedStepType:     assetlifecycle.StepTypeMonitoringInstanceLifecycle,
			completedStepCount: 2,
		},
		{
			name:               "Target event after MI retirement",
			triggerName:        "reject_atomicity_target_event",
			functionName:       "reject_atomicity_target_event_fn",
			tableName:          "state_change_events",
			triggerTiming:      "before",
			triggerEvent:       "insert",
			triggerBody:        "if new.object_type = 'target' and new.event_type = 'target_paused' then raise exception 'injected cancellation failure at Target event' using errcode = 'P0001'; end if;",
			failureMessage:     "injected cancellation failure at Target event",
			failedObjectType:   assetlifecycle.ObjectTypeTarget,
			failedStepType:     assetlifecycle.StepTypeTargetRunStatus,
			completedStepCount: 3,
		},
		{
			name:               "completed Target step audit after all writes",
			triggerName:        "reject_atomicity_completed_target_step",
			functionName:       "reject_atomicity_completed_target_step_fn",
			tableName:          "asset_lifecycle_action_steps",
			triggerTiming:      "before",
			triggerEvent:       "insert",
			triggerBody:        "if new.status = 'completed' and new.object_type = 'target' and new.step_type = 'target_run_status' then raise exception 'injected cancellation failure at completed Target step audit' using errcode = 'P0001'; end if;",
			failureMessage:     "injected cancellation failure at completed Target step audit",
			failedObjectType:   assetlifecycle.ObjectTypeTarget,
			failedStepType:     assetlifecycle.StepTypeTargetRunStatus,
			completedStepCount: 3,
		},
	}

	for index := range failures {
		failure := failures[index]
		t.Run(failure.name, func(t *testing.T) {
			fixture := createCancellationAtomicityFixture(t, ctx, pool, vpsRepository, subscriptionRepository, monitoringInstanceRepository, linkRepository, targetRepository, serviceRepository, "atomicity-"+strings.ToLower(strings.ReplaceAll(failure.name, " ", "-")))
			preview, err := lifecycleRepository.GetVPSCancellationPreview(ctx, fixture.vps.VPSID)
			if err != nil {
				t.Fatalf("GetVPSCancellationPreview: %v", err)
			}
			before := readCancellationAtomicityState(t, ctx, pool, fixture)
			installCancellationAtomicityTrigger(t, ctx, pool, failure.triggerName, failure.functionName, failure.tableName, failure.triggerTiming, failure.triggerEvent, failure.triggerBody)

			input := cancellationAtomicityInput(fixture, preview.PreviewDigest)
			_, err = lifecycleRepository.ApplyVPSCancellation(ctx, fixture.vps.VPSID, input)
			if err == nil || !strings.Contains(err.Error(), failure.failureMessage) {
				t.Fatalf("ApplyVPSCancellation error = %v, want injected failure %q", err, failure.failureMessage)
			}

			assertCancellationAtomicityStateUnchanged(t, ctx, pool, fixture, before)
			assertCancellationAtomicityFailedAudit(t, ctx, pool, fixture.vps.VPSID, input.Reason, failure.failedObjectType, cancellationAtomicityObjectID(fixture, failure.failedObjectType), failure.failedStepType, failure.completedStepCount, failure.failureMessage)
		})
	}

	t.Run("failed audit rejection is returned", func(t *testing.T) {
		fixture := createCancellationAtomicityFixture(t, ctx, pool, vpsRepository, subscriptionRepository, monitoringInstanceRepository, linkRepository, targetRepository, serviceRepository, "atomicity-failed-audit-rejection")
		preview, err := lifecycleRepository.GetVPSCancellationPreview(ctx, fixture.vps.VPSID)
		if err != nil {
			t.Fatalf("GetVPSCancellationPreview: %v", err)
		}
		before := readCancellationAtomicityState(t, ctx, pool, fixture)
		installCancellationAtomicityTrigger(t, ctx, pool, "reject_atomicity_vps_update_for_audit", "reject_atomicity_vps_update_for_audit_fn", "vps_assets", "after", "update", "if new.lifecycle_status = 'to_cancel' then raise exception 'injected cancellation failure before audit rejection' using errcode = 'P0001'; end if;")
		installCancellationAtomicityTrigger(t, ctx, pool, "reject_atomicity_failed_action_audit", "reject_atomicity_failed_action_audit_fn", "asset_lifecycle_actions", "before", "insert", "if new.status = 'failed' then raise exception 'injected failed cancellation audit rejection' using errcode = 'P0001'; end if;")

		input := cancellationAtomicityInput(fixture, preview.PreviewDigest)
		_, err = lifecycleRepository.ApplyVPSCancellation(ctx, fixture.vps.VPSID, input)
		if err == nil || !strings.Contains(err.Error(), "injected cancellation failure before audit rejection") || !strings.Contains(err.Error(), "injected failed cancellation audit rejection") {
			t.Fatalf("ApplyVPSCancellation error = %v, want original failure and rejected failed-audit errors", err)
		}

		assertCancellationAtomicityStateUnchanged(t, ctx, pool, fixture, before)
		var actions, steps int
		if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, fixture.vps.VPSID, assetlifecycle.ActionTypeCancelVPS).Scan(&actions); err != nil {
			t.Fatalf("count cancellation actions after audit rejection: %v", err)
		}
		if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_action_steps s join asset_lifecycle_actions a using (action_id) where a.vps_id = $1 and a.action_type = $2`, fixture.vps.VPSID, assetlifecycle.ActionTypeCancelVPS).Scan(&steps); err != nil {
			t.Fatalf("count cancellation steps after audit rejection: %v", err)
		}
		if actions != 0 || steps != 0 {
			t.Fatalf("cancellation audit after rejected failure audit = %d actions, %d steps; want no persisted audit", actions, steps)
		}
	})
}

type cancellationAtomicityFixture struct {
	vps          vpsassets.Record
	subscription subscriptions.Record
	monitoring   monitoringinstances.Record
	target       targets.TargetRecord
	service      assetservices.Record
}

func createCancellationAtomicityFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	vpsRepository *PostgresVPSAssetRepository,
	subscriptionRepository *PostgresSubscriptionRepository,
	monitoringInstanceRepository *PostgresMonitoringInstanceRepository,
	linkRepository *PostgresVPSMonitoringInstanceLinkRepository,
	targetRepository *PostgresTargetRepository,
	serviceRepository *PostgresAssetServiceRepository,
	name string,
) cancellationAtomicityFixture {
	t.Helper()

	vps, err := vpsRepository.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     name,
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: vpsassets.RenewalKeep,
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset: %v", err)
	}
	subscription, err := subscriptionRepository.CreateSubscription(ctx, subscriptions.CreateInput{
		VPSID:         vps.VPSID,
		Price:         10,
		BillingMonths: 1,
		Currency:      "USD",
		Status:        subscriptions.StatusActive,
		AutoRenew:     true,
		RenewalMode:   string(subscriptions.RenewalModeAuto),
		DisplayName:   name + " subscription",
		Labels:        []string{},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	monitoringInstance, err := monitoringInstanceRepository.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     name + " monitoring instance",
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance: %v", err)
	}
	if _, err := linkRepository.LinkMonitoringInstance(ctx, vps.VPSID, assetlinks.LinkInput{MonitoringInstanceID: monitoringInstance.MonitoringInstanceID}); err != nil {
		t.Fatalf("LinkMonitoringInstance: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set lifecycle_status = $2,
			monitoring_status = $3,
			binding_status = $4,
			binding_fingerprint = $5,
			binding_epoch_started_at = now(),
			enrollment_token_hash = $6,
			enrollment_token_issued_at = now(),
			enrollment_token_consumed_at = now(),
			sync_token_hash = $7,
			pending_binding_fingerprint = $8,
			pending_binding_first_seen_at = now(),
			pending_binding_last_seen_at = now(),
			pending_binding_attempt_count = 2,
			pending_action_id = $9,
			pending_action_command_id = $10,
			last_action = $11::jsonb
		where monitoring_instance_id = $1`,
		monitoringInstance.MonitoringInstanceID,
		monitoringinstances.LifecycleInUse,
		monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingBound,
		name+"-binding-fingerprint",
		hashEnrollmentToken(name+"-enrollment-token"),
		hashSyncToken(name+"-sync-token"),
		name+"-pending-fingerprint",
		name+"-pending-action",
		name+"-pending-command",
		`{"action_id":"atomicity_action","command_id":"atomicity_command","status":"pending"}`); err != nil {
		t.Fatalf("seed in-use MI credentials and pending state: %v", err)
	}
	target, err := targetRepository.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              name + " Target",
		TargetType:                        targets.TargetTypeService,
		Host:                              strings.ReplaceAll(name, "_", "-") + ".example.test",
		ExecutionMonitoringInstanceLabels: []string{"edge"},
		RunStatus:                         targets.RunStatusEnabled,
		Labels:                            []string{},
	})
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	service, err := serviceRepository.CreateAssetService(ctx, assetservices.CreateInput{
		VPSID:       vps.VPSID,
		TargetID:    new(target.TargetID),
		Name:        name + " active service",
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      assetservices.ServiceStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateAssetService: %v", err)
	}
	return cancellationAtomicityFixture{vps: vps, subscription: subscription, monitoring: monitoringInstance, target: target, service: service}
}

func cancellationAtomicityInput(fixture cancellationAtomicityFixture, previewDigest string) assetlifecycle.ApplyCancellationInput {
	return assetlifecycle.ApplyCancellationInput{
		Reason:             "fault injection cancellation atomicity",
		VPSLifecycleStatus: vpsassets.LifecycleToCancel,
		PreviewDigest:      previewDigest,
		SubscriptionIDs:    []string{fixture.subscription.SubscriptionID},
		MonitoringInstanceActions: []assetlifecycle.MonitoringInstanceActionInput{{
			MonitoringInstanceID: fixture.monitoring.MonitoringInstanceID,
			LifecycleStatus:      monitoringinstances.LifecycleRetired,
		}},
		TargetActions: []assetlifecycle.TargetActionInput{{
			TargetID:  fixture.target.TargetID,
			RunStatus: targets.RunStatusPaused,
		}},
	}
}

func installCancellationAtomicityTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, triggerName, functionName, tableName, timing, event, body string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `create function `+functionName+`() returns trigger language plpgsql as $$ begin `+body+` return new; end $$`); err != nil {
		t.Fatalf("create injected trigger function %s: %v", functionName, err)
	}
	if _, err := pool.Exec(ctx, `create trigger `+triggerName+` `+timing+` `+event+` on `+tableName+` for each row execute function `+functionName+`()`); err != nil {
		t.Fatalf("create injected trigger %s: %v", triggerName, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `drop trigger if exists `+triggerName+` on `+tableName); err != nil {
			t.Errorf("drop injected trigger %s: %v", triggerName, err)
		}
		if _, err := pool.Exec(context.Background(), `drop function if exists `+functionName+`()`); err != nil {
			t.Errorf("drop injected trigger function %s: %v", functionName, err)
		}
	})
}

type cancellationAtomicityState struct {
	vps              []byte
	subscription     []byte
	monitoring       []byte
	link             []byte
	target           []byte
	service          []byte
	renewalDecisions []byte
	events           []byte
}

func readCancellationAtomicityState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture cancellationAtomicityFixture) cancellationAtomicityState {
	t.Helper()
	var state cancellationAtomicityState
	queries := []struct {
		query string
		args  []any
		dest  *[]byte
	}{
		{`select to_jsonb(v) from vps_assets v where vps_id = $1`, []any{fixture.vps.VPSID}, &state.vps},
		{`select to_jsonb(s) from subscriptions s where subscription_id = $1`, []any{fixture.subscription.SubscriptionID}, &state.subscription},
		{`select to_jsonb(m) from monitoring_instances m where monitoring_instance_id = $1`, []any{fixture.monitoring.MonitoringInstanceID}, &state.monitoring},
		{`select coalesce(jsonb_agg(to_jsonb(l)), '[]'::jsonb) from vps_monitoring_instance_links l where vps_id = $1 and monitoring_instance_id = $2`, []any{fixture.vps.VPSID, fixture.monitoring.MonitoringInstanceID}, &state.link},
		{`select to_jsonb(t) from targets t where target_id = $1`, []any{fixture.target.TargetID}, &state.target},
		{`select to_jsonb(s) from asset_services s where service_id = $1`, []any{fixture.service.ServiceID}, &state.service},
		{`select coalesce(jsonb_agg(to_jsonb(d) order by d.created_at, d.decision_id), '[]'::jsonb) from renewal_decisions d where vps_id = $1`, []any{fixture.vps.VPSID}, &state.renewalDecisions},
		{`select coalesce(jsonb_agg(to_jsonb(e) order by e.created_at, e.event_id), '[]'::jsonb) from state_change_events e where object_id = any($1::text[])`, []any{[]string{fixture.vps.VPSID, fixture.monitoring.MonitoringInstanceID, fixture.target.TargetID}}, &state.events},
	}
	for _, item := range queries {
		if err := pool.QueryRow(ctx, item.query, item.args...).Scan(item.dest); err != nil {
			t.Fatalf("read cancellation state snapshot: %v", err)
		}
	}
	return state
}

func assertCancellationAtomicityStateUnchanged(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture cancellationAtomicityFixture, before cancellationAtomicityState) {
	t.Helper()
	after := readCancellationAtomicityState(t, ctx, pool, fixture)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("cancellation failure changed business state:\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func assertCancellationAtomicityFailedAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID, reason, objectType, objectID, stepType string, completedStepCount int, failureMessage string) {
	t.Helper()
	var actionCount, completedActionCount int
	if err := pool.QueryRow(ctx, `select count(*), count(*) filter (where status = 'completed') from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, vpsID, assetlifecycle.ActionTypeCancelVPS).Scan(&actionCount, &completedActionCount); err != nil {
		t.Fatalf("read cancellation action counts: %v", err)
	}
	if actionCount != 1 || completedActionCount != 0 {
		t.Fatalf("cancellation actions = %d total, %d completed; want one failed action and no completed action", actionCount, completedActionCount)
	}
	var actionStatus, storedReason, storedCompletedStepCount, storedFailure string
	if err := pool.QueryRow(ctx, `select status, reason, summary->>'completed_step_count', summary->>'failure_reason' from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, vpsID, assetlifecycle.ActionTypeCancelVPS).Scan(&actionStatus, &storedReason, &storedCompletedStepCount, &storedFailure); err != nil {
		t.Fatalf("read failed cancellation action: %v", err)
	}
	if actionStatus != assetlifecycle.ActionStatusFailed || storedReason != reason || storedCompletedStepCount != strconv.Itoa(completedStepCount) {
		t.Fatalf("failed cancellation action = status %q, reason %q, completed_step_count %q; want failed status, preserved reason, and count %d", actionStatus, storedReason, storedCompletedStepCount, completedStepCount)
	}
	if !strings.Contains(storedFailure, failureMessage) {
		t.Fatalf("failed cancellation summary failure %q does not contain injected error %q", storedFailure, failureMessage)
	}

	var stepCount, completedStepRows int
	if err := pool.QueryRow(ctx, `select count(*), count(*) filter (where s.status = 'completed') from asset_lifecycle_action_steps s join asset_lifecycle_actions a using (action_id) where a.vps_id = $1 and a.action_type = $2`, vpsID, assetlifecycle.ActionTypeCancelVPS).Scan(&stepCount, &completedStepRows); err != nil {
		t.Fatalf("read cancellation step counts: %v", err)
	}
	if stepCount != 1 || completedStepRows != 0 {
		t.Fatalf("cancellation steps = %d total, %d completed; want only one failed step", stepCount, completedStepRows)
	}
	var failedStepStatus, failedStepObjectType, failedStepObjectID, failedStepType string
	var failedStepAfterState map[string]any
	if err := pool.QueryRow(ctx, `select s.status, s.object_type, s.object_id, s.step_type, s.after_state from asset_lifecycle_action_steps s join asset_lifecycle_actions a using (action_id) where a.vps_id = $1 and a.action_type = $2`, vpsID, assetlifecycle.ActionTypeCancelVPS).Scan(&failedStepStatus, &failedStepObjectType, &failedStepObjectID, &failedStepType, &failedStepAfterState); err != nil {
		t.Fatalf("read failed cancellation step: %v", err)
	}
	if failedStepStatus != assetlifecycle.StepStatusFailed || failedStepObjectType != objectType || failedStepObjectID != objectID || failedStepType != stepType {
		t.Fatalf("failed cancellation step = (%q,%q,%q,%q), want (%q,%q,%q,%q)", failedStepStatus, failedStepObjectType, failedStepObjectID, failedStepType, assetlifecycle.StepStatusFailed, objectType, objectID, stepType)
	}
	if _, ok := failedStepAfterState["error"].(string); !ok {
		t.Fatalf("failed cancellation step after_state = %#v, want error detail", failedStepAfterState)
	}
}

func cancellationAtomicityObjectID(fixture cancellationAtomicityFixture, objectType string) string {
	switch objectType {
	case assetlifecycle.ObjectTypeVPS:
		return fixture.vps.VPSID
	case assetlifecycle.ObjectTypeSubscription:
		return fixture.subscription.SubscriptionID
	case assetlifecycle.ObjectTypeMonitoringInstance:
		return fixture.monitoring.MonitoringInstanceID
	case assetlifecycle.ObjectTypeTarget:
		return fixture.target.TargetID
	default:
		return ""
	}
}
