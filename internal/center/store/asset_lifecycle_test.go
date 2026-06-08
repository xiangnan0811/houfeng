package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresAssetLifecycleMigrationDefinesAuditTablesAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0029_rename_nodes_to_monitoring_instances.sql"))
	if err != nil {
		t.Fatalf("ReadFile(asset lifecycle migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"update asset_lifecycle_action_steps",
		"where object_type = 'node'",
		"when 'node_lifecycle' then 'monitoring_instance_lifecycle'",
		"when 'node_monitoring' then 'monitoring_instance_monitoring'",
		"drop constraint if exists asset_lifecycle_action_steps_object_type_allowed",
		"object_type in ('vps', 'subscription', 'monitoring_instance', 'target')",
		"drop constraint if exists asset_lifecycle_action_steps_step_type_allowed",
		"step_type in ('vps_lifecycle', 'subscription_status', 'monitoring_instance_lifecycle', 'monitoring_instance_monitoring', 'target_run_status')",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("asset lifecycle migration missing %q", snippet)
		}
	}
	objectDrop := strings.Index(text, "drop constraint if exists asset_lifecycle_action_steps_object_type_allowed")
	objectUpdate := strings.Index(text, "update asset_lifecycle_action_steps\nset object_type = 'monitoring_instance'")
	if objectDrop < 0 || objectUpdate < 0 || objectDrop > objectUpdate {
		t.Fatalf("asset lifecycle migration must drop object_type constraint before rewriting node object_type")
	}
	stepDrop := strings.Index(text, "drop constraint if exists asset_lifecycle_action_steps_step_type_allowed")
	stepUpdate := strings.Index(text, "when 'node_lifecycle' then 'monitoring_instance_lifecycle'")
	if stepDrop < 0 || stepUpdate < 0 || stepDrop > stepUpdate {
		t.Fatalf("asset lifecycle migration must drop step_type constraint before rewriting node step_type")
	}
}

func TestPostgresVPSFirstStatusMigrationNormalizesLegacyStateIntoVPSAudit(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0030_vps_first_status_semantics.sql"))
	if err != nil {
		t.Fatalf("ReadFile(vps-first migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"set status = 'unknown'",
		"not in ('active', 'paused', 'cancelled', 'expired', 'unknown')",
		"set lifecycle_status = '待接入'",
		"not in ('待接入', '在用', '观察中', '不续费', '已退役')",
		"subscription_evidence as",
		"monitoring_evidence as",
		"cancelled_auto_renew_count",
		"new_lifecycle_status",
		"new_renewal_decision",
		"update vps_assets",
		"insert into asset_lifecycle_actions",
		"ala_mig0030_",
		"insert into asset_lifecycle_action_steps",
		"als_mig0030_",
		"insert into renewal_decisions",
		"rdec_mig0030_",
		"old_lifecycle_status",
		"new_lifecycle_status",
		"legacy_evidence",
		"subscription_evidence",
		"monitoring_evidence",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("vps-first status migration missing %q", snippet)
		}
	}
	if strings.Contains(text, "set status = 'active'") {
		t.Fatalf("vps-first status migration must not coerce invalid legacy subscription status to active")
	}
	statusNormalize := strings.Index(text, "update subscriptions")
	classify := strings.Index(text, "classified as")
	if statusNormalize < 0 || classify < 0 || statusNormalize > classify {
		t.Fatalf("vps-first status migration must normalize legacy subscription status before classification")
	}
	actionInsert := strings.Index(text, "insert into asset_lifecycle_actions")
	stepInsert := strings.Index(text, "insert into asset_lifecycle_action_steps")
	renewalInsert := strings.Index(text, "insert into renewal_decisions")
	if actionInsert < 0 || stepInsert < 0 || renewalInsert < 0 || actionInsert > stepInsert || actionInsert > renewalInsert {
		t.Fatalf("vps-first status migration must create action audit before steps and renewal history")
	}
}

func TestApplyVPSCancellationRejectsArchivedVPSBeforeMutation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	archivedAt := now.Add(-time.Hour)
	businessTx := &fakeAssetLifecycleTx{
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "from vps_assets") {
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, assetLifecycleTestVPSRecord("vps_archived", vpsassets.LifecycleArchived, vpsassets.RenewalCancel, now, &archivedAt))
					return nil
				})
			}
			return fakeAssetLifecycleRowFunc(func(dest ...any) error {
				return pgx.ErrNoRows
			})
		},
	}
	repo := &PostgresAssetLifecycleRepository{db: &fakeAssetLifecycleDB{txs: []*fakeAssetLifecycleTx{businessTx}}}

	_, err := repo.ApplyVPSCancellation(context.Background(), "vps_archived", assetlifecycle.ApplyCancellationInput{
		Reason:             "expired and no renewal",
		VPSLifecycleStatus: vpsassets.LifecycleCancelled,
	})

	if !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
		t.Fatalf("ApplyVPSCancellation error = %v, want ErrLifecycleActionBlocked", err)
	}
	if len(businessTx.execCalls) != 0 {
		t.Fatalf("business tx exec calls = %d, want no mutation/audit insert before blocker", len(businessTx.execCalls))
	}
	if businessTx.commitCount != 0 {
		t.Fatalf("business tx commit count = %d, want 0", businessTx.commitCount)
	}
	if businessTx.rollbackCount == 0 {
		t.Fatalf("business tx rollback count = %d, want rollback", businessTx.rollbackCount)
	}
}

func TestListMonitoringInstanceAssetContextsExcludesArchivedAndCancelledVPS(t *testing.T) {
	t.Parallel()

	var seenSQL string
	repo := &PostgresAssetLifecycleRepository{db: &fakeAssetLifecycleDB{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			seenSQL = sql
			return &fakeSubscriptionRows{}, nil
		},
	}}

	if _, err := repo.ListMonitoringInstanceAssetContexts(context.Background()); err != nil {
		t.Fatalf("ListMonitoringInstanceAssetContexts() error = %v", err)
	}
	for _, snippet := range []string{
		"join vps_assets v on v.vps_id = l.vps_id",
		"v.lifecycle_status not in ('cancelled', 'archived')",
	} {
		if !strings.Contains(seenSQL, snippet) {
			t.Fatalf("ListMonitoringInstanceAssetContexts SQL missing %q in %s", snippet, seenSQL)
		}
	}
}

func TestListTargetAssetContextsExcludesArchivedAndCancelledVPS(t *testing.T) {
	t.Parallel()

	var seenSQL string
	repo := &PostgresAssetLifecycleRepository{db: &fakeAssetLifecycleDB{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			seenSQL = sql
			return &fakeSubscriptionRows{}, nil
		},
	}}

	if _, err := repo.ListTargetAssetContexts(context.Background()); err != nil {
		t.Fatalf("ListTargetAssetContexts() error = %v", err)
	}
	for _, snippet := range []string{
		"join vps_assets v on v.vps_id = ta.vps_id",
		"v.lifecycle_status not in ('cancelled', 'archived')",
	} {
		if !strings.Contains(seenSQL, snippet) {
			t.Fatalf("ListTargetAssetContexts SQL missing %q in %s", snippet, seenSQL)
		}
	}
}

func TestApplyVPSCancellationPersistsFailedAuditAfterRollback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	businessTx := &fakeAssetLifecycleTx{
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "from vps_assets"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, assetLifecycleTestVPSRecord("vps_001", vpsassets.LifecycleActive, vpsassets.RenewalCancel, now, nil))
					return nil
				})
			case strings.Contains(sql, "update vps_assets"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, assetLifecycleTestVPSRecord("vps_001", vpsassets.LifecycleCancelled, vpsassets.RenewalCancel, now, nil))
					return nil
				})
			case strings.Contains(sql, "from subscriptions"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					return pgx.ErrNoRows
				})
			default:
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					return pgx.ErrNoRows
				})
			}
		},
	}
	auditTx := &fakeAssetLifecycleTx{}
	db := &fakeAssetLifecycleDB{txs: []*fakeAssetLifecycleTx{businessTx, auditTx}}
	repo := &PostgresAssetLifecycleRepository{db: db}

	_, err := repo.ApplyVPSCancellation(context.Background(), "vps_001", assetlifecycle.ApplyCancellationInput{
		Reason:                    "expired and no renewal",
		VPSLifecycleStatus:        vpsassets.LifecycleCancelled,
		SubscriptionIDs:           []string{"sub_missing"},
		MonitoringInstanceActions: []assetlifecycle.MonitoringInstanceActionInput{},
		TargetActions:             []assetlifecycle.TargetActionInput{},
	})

	if !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("ApplyVPSCancellation error = %v, want ErrInvalidLifecycleActionInput", err)
	}
	if db.beginCount != 2 {
		t.Fatalf("begin tx count = %d, want business tx plus failed audit tx", db.beginCount)
	}
	if businessTx.commitCount != 0 {
		t.Fatalf("business tx commit count = %d, want 0", businessTx.commitCount)
	}
	if businessTx.rollbackCount == 0 {
		t.Fatalf("business tx rollback count = %d, want rollback before failed audit", businessTx.rollbackCount)
	}
	if auditTx.commitCount != 1 {
		t.Fatalf("audit tx commit count = %d, want 1", auditTx.commitCount)
	}
	failedActions, failedSteps := filterAssetLifecycleAuditExecs(auditTx.execCalls)
	if len(failedActions) != 1 {
		t.Fatalf("failed action inserts = %d, want 1; calls=%#v", len(failedActions), auditTx.execCalls)
	}
	if failedActions[0].args[3] != assetlifecycle.ActionStatusFailed {
		t.Fatalf("failed action status arg = %#v, want failed", failedActions[0].args[3])
	}
	var summary map[string]any
	if err := json.Unmarshal(failedActions[0].args[6].([]byte), &summary); err != nil {
		t.Fatalf("unmarshal failed action summary: %v", err)
	}
	if summary["completed_step_count"] != float64(1) {
		t.Fatalf("completed_step_count = %#v, want 1", summary["completed_step_count"])
	}
	if !strings.Contains(summary["failure_reason"].(string), "sub_missing") {
		t.Fatalf("failure_reason = %#v, want missing subscription evidence", summary["failure_reason"])
	}
	if len(failedSteps) != 1 {
		t.Fatalf("failed step inserts = %d, want 1; calls=%#v", len(failedSteps), auditTx.execCalls)
	}
	if failedSteps[0].args[2] != assetlifecycle.ObjectTypeSubscription {
		t.Fatalf("failed step object type = %#v, want subscription", failedSteps[0].args[2])
	}
	if failedSteps[0].args[3] != "sub_missing" {
		t.Fatalf("failed step object id = %#v, want sub_missing", failedSteps[0].args[3])
	}
	if failedSteps[0].args[4] != assetlifecycle.StepTypeSubscriptionStatus {
		t.Fatalf("failed step type = %#v, want subscription_status", failedSteps[0].args[4])
	}
	if failedSteps[0].args[5] != assetlifecycle.StepStatusFailed {
		t.Fatalf("failed step status = %#v, want failed", failedSteps[0].args[5])
	}
	if !strings.Contains(failedSteps[0].args[8].(string), "sub_missing") {
		t.Fatalf("failed step message = %#v, want missing subscription evidence", failedSteps[0].args[8])
	}
}

func TestExtendVPSValidityUpdatesSingleActiveSubscriptionAndAuditsAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	oldRenewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	newRenewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC))
	tx := &fakeAssetLifecycleTx{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "from subscriptions") {
				return nil, errors.New("unexpected query")
			}
			return &fakeSubscriptionRows{rows: []fakeSubscriptionScan{{
				scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      "sub_001",
						VPSID:               "vps_001",
						Price:               120,
						Currency:            "USD",
						BillingCycle:        "annual",
						BillingMonths:       12,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
						BillingPeriodLength: 1,
						MonthlyPrice:        10,
						RenewAt:             &oldRenewAt,
						AutoRenew:           true,
						RenewalMode:         string(subscriptions.RenewalModeAuto),
						Status:              subscriptions.StatusActive,
						CreatedAt:           now,
						UpdatedAt:           now,
					})
					return nil
				},
			}}}, nil
		},
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "from vps_assets"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, assetLifecycleTestVPSRecord("vps_001", vpsassets.LifecycleActive, vpsassets.RenewalKeep, now, nil))
					return nil
				})
			case strings.Contains(sql, "update subscriptions"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      "sub_001",
						VPSID:               "vps_001",
						Price:               120,
						Currency:            "USD",
						BillingCycle:        "annual",
						BillingMonths:       12,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
						BillingPeriodLength: 1,
						MonthlyPrice:        10,
						RenewAt:             &newRenewAt,
						AutoRenew:           true,
						RenewalMode:         string(subscriptions.RenewalModeAuto),
						Status:              subscriptions.StatusActive,
						CreatedAt:           now,
						UpdatedAt:           now,
					})
					return nil
				})
			case strings.Contains(sql, "insert into price_histories"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanPriceHistoryRecordDestinations(dest, priceHistoryFixture("ph_001", now, oldRenewAt, newRenewAt))
					return nil
				})
			default:
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					return errors.New("unexpected query row")
				})
			}
		},
	}
	repo := &PostgresAssetLifecycleRepository{db: &fakeAssetLifecycleDB{txs: []*fakeAssetLifecycleTx{tx}}}

	result, err := repo.ExtendVPSValidity(context.Background(), "vps_001", assetlifecycle.ExtendValidityInput{
		ExtendTo:    &newRenewAt,
		Reason:      "outage compensation",
		Fee:         0,
		FeeCurrency: "USD",
		SourceType:  "outage",
	})
	if err != nil {
		t.Fatalf("ExtendVPSValidity() error = %v", err)
	}
	if tx.commitCount != 1 {
		t.Fatalf("commit count = %d, want 1", tx.commitCount)
	}
	if result.Action.ActionType != assetlifecycle.ActionTypeExtendValidity || len(result.Steps) != 1 {
		t.Fatalf("result = %#v, want validity extension action and one step", result)
	}
	actions, steps := filterAssetLifecycleAuditExecs(tx.execCalls)
	if len(actions) != 1 || len(steps) != 1 {
		t.Fatalf("audit inserts actions=%d steps=%d calls=%#v", len(actions), len(steps), tx.execCalls)
	}
	if actions[0].args[2] != string(assetlifecycle.ActionTypeExtendValidity) {
		t.Fatalf("action type arg = %#v, want extend_validity", actions[0].args[2])
	}
	if steps[0].args[4] != assetlifecycle.StepTypeSubscriptionRenewAt {
		t.Fatalf("step type arg = %#v, want subscription_renew_at", steps[0].args[4])
	}
	var summary map[string]any
	if err := json.Unmarshal(actions[0].args[6].([]byte), &summary); err != nil {
		t.Fatalf("unmarshal extension summary: %v", err)
	}
	if summary["subscription_id"] != "sub_001" || summary["source_type"] != "outage" {
		t.Fatalf("summary = %#v, want subscription and source", summary)
	}
}

func TestExtendVPSValidityRejectsShorteningCurrentSubscription(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 30, 8, 0, 0, 0, time.UTC)
	oldRenewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC))
	newRenewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	tx := &fakeAssetLifecycleTx{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "from subscriptions") {
				return nil, errors.New("unexpected query")
			}
			return &fakeSubscriptionRows{rows: []fakeSubscriptionScan{{
				scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      "sub_001",
						VPSID:               "vps_001",
						Price:               120,
						Currency:            "USD",
						BillingCycle:        "annual",
						BillingMonths:       12,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
						BillingPeriodLength: 1,
						MonthlyPrice:        10,
						RenewAt:             &oldRenewAt,
						AutoRenew:           true,
						RenewalMode:         string(subscriptions.RenewalModeAuto),
						Status:              subscriptions.StatusActive,
						CreatedAt:           now,
						UpdatedAt:           now,
					})
					return nil
				},
			}}}, nil
		},
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "from vps_assets"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, assetLifecycleTestVPSRecord("vps_001", vpsassets.LifecycleActive, vpsassets.RenewalKeep, now, nil))
					return nil
				})
			default:
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					return errors.New("unexpected query row")
				})
			}
		},
	}
	repo := &PostgresAssetLifecycleRepository{db: &fakeAssetLifecycleDB{txs: []*fakeAssetLifecycleTx{tx}}}

	_, err := repo.ExtendVPSValidity(context.Background(), "vps_001", assetlifecycle.ExtendValidityInput{
		ExtendTo:    &newRenewAt,
		Reason:      "outage compensation",
		Fee:         0,
		FeeCurrency: "USD",
		SourceType:  "outage",
	})
	if !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("ExtendVPSValidity() error = %v, want invalid lifecycle action input", err)
	}
	if tx.commitCount != 0 {
		t.Fatalf("commit count = %d, want 0", tx.commitCount)
	}
	actions, steps := filterAssetLifecycleAuditExecs(tx.execCalls)
	if len(actions) != 1 || len(steps) != 0 {
		t.Fatalf("audit inserts actions=%d steps=%d calls=%#v, want action only before rollback", len(actions), len(steps), tx.execCalls)
	}
}

func TestCancellationPreviewFindingsTreatPausedAndUnknownSubscriptionsAsInactiveEvidence(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		status subscriptions.Status
	}{
		{name: "paused", status: subscriptions.StatusPaused},
		{name: "unknown", status: subscriptions.StatusUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			preview := assetlifecycle.CancellationPreview{
				VPS: vpsassets.Record{
					VPSID:           "vps_001",
					LifecycleStatus: vpsassets.LifecycleActive,
					RenewalDecision: vpsassets.RenewalUnreviewed,
				},
				Subscriptions: buildSubscriptionImpacts([]subscriptions.Record{{
					SubscriptionID: "sub_001",
					VPSID:          "vps_001",
					Status:         tt.status,
				}}),
			}

			if got := preview.Subscriptions[0].Role; got != "inactive" {
				t.Fatalf("subscription impact role = %q, want inactive", got)
			}

			warnings, blockers := buildCancellationPreviewFindings(preview)
			if len(blockers) != 0 {
				t.Fatalf("blockers = %#v, want none", blockers)
			}
			if !containsString(warnings, "不是“没有关联订阅”") {
				t.Fatalf("warnings = %#v, want inactive subscription evidence warning", warnings)
			}
			if !containsString(warnings, "存在状态割裂") {
				t.Fatalf("warnings = %#v, want status split warning", warnings)
			}
		})
	}
}

func assetLifecycleTestVPSRecord(vpsID string, lifecycle vpsassets.LifecycleStatus, renewal vpsassets.RenewalDecision, now time.Time, archivedAt *time.Time) vpsassets.Record {
	return vpsassets.Record{
		VPSID:           vpsID,
		DisplayName:     "Frankfurt Legacy",
		ProviderName:    "Hetzner",
		ProductName:     "CX21",
		Country:         "DE",
		Region:          "Hesse",
		City:            "Frankfurt",
		IPv4:            "192.0.2.10",
		SSHHost:         "192.0.2.10",
		SSHPort:         22,
		SSHUser:         "root",
		OSName:          "Debian 12",
		Virtualization:  "kvm",
		LifecycleStatus: lifecycle,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: renewal,
		Importance:      "normal",
		CreatedAt:       now,
		UpdatedAt:       now,
		ArchivedAt:      archivedAt,
	}
}

func filterAssetLifecycleAuditExecs(calls []fakeAssetLifecycleExecCall) ([]fakeAssetLifecycleExecCall, []fakeAssetLifecycleExecCall) {
	actions := []fakeAssetLifecycleExecCall{}
	steps := []fakeAssetLifecycleExecCall{}
	for _, call := range calls {
		switch {
		case strings.Contains(call.sql, "insert into asset_lifecycle_actions"):
			actions = append(actions, call)
		case strings.Contains(call.sql, "insert into asset_lifecycle_action_steps"):
			steps = append(steps, call)
		}
	}
	return actions, steps
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

type fakeAssetLifecycleDB struct {
	txs          []*fakeAssetLifecycleTx
	queryFunc    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRowFunc func(context.Context, string, ...any) pgx.Row
	beginCount   int
}

func (f *fakeAssetLifecycleDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFunc != nil {
		return f.queryFunc(ctx, sql, args...)
	}
	return nil, errors.New("unexpected query on fake asset lifecycle db")
}

func (f *fakeAssetLifecycleDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFunc != nil {
		return f.queryRowFunc(ctx, sql, args...)
	}
	return fakeAssetLifecycleRowFunc(func(dest ...any) error {
		return errors.New("unexpected query row on fake asset lifecycle db")
	})
}

func (f *fakeAssetLifecycleDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	if f.beginCount >= len(f.txs) {
		return nil, errors.New("unexpected BeginTx call")
	}
	tx := f.txs[f.beginCount]
	f.beginCount++
	return tx, nil
}

type fakeAssetLifecycleTx struct {
	queryRowFunc  func(context.Context, string, ...any) pgx.Row
	queryFunc     func(context.Context, string, ...any) (pgx.Rows, error)
	execFunc      func(context.Context, string, ...any) (pgconn.CommandTag, error)
	execCalls     []fakeAssetLifecycleExecCall
	commitCount   int
	rollbackCount int
}

type fakeAssetLifecycleExecCall struct {
	sql  string
	args []any
}

func (f *fakeAssetLifecycleTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested Begin on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) Commit(context.Context) error {
	f.commitCount++
	return nil
}

func (f *fakeAssetLifecycleTx) Rollback(context.Context) error {
	f.rollbackCount++
	return nil
}

func (f *fakeAssetLifecycleTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (f *fakeAssetLifecycleTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (f *fakeAssetLifecycleTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, fakeAssetLifecycleExecCall{sql: sql, args: append([]any(nil), args...)})
	if f.execFunc != nil {
		return f.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (f *fakeAssetLifecycleTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFunc != nil {
		return f.queryFunc(ctx, sql, args...)
	}
	return nil, errors.New("unexpected Query on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFunc != nil {
		return f.queryRowFunc(ctx, sql, args...)
	}
	return fakeAssetLifecycleRowFunc(func(dest ...any) error {
		return errors.New("unexpected QueryRow on fake asset lifecycle tx")
	})
}

func (f *fakeAssetLifecycleTx) Conn() *pgx.Conn {
	return nil
}

type fakeAssetLifecycleRowFunc func(dest ...any) error

func (f fakeAssetLifecycleRowFunc) Scan(dest ...any) error {
	return f(dest...)
}
