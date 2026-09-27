package store

import (
	"context"
	"encoding/json"

	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"

	"houfeng/internal/center/vpsassets"
)

func createVPSStateRepairTransitionVPS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, lifecycle vpsassets.LifecycleStatus, usage vpsassets.UsageStatus, renewal vpsassets.RenewalDecision) vpsassets.Record {
	t.Helper()
	vps, err := NewPostgresVPSAssetRepository(pool).CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     name,
		LifecycleStatus: lifecycle,
		UsageStatus:     usage,
		RenewalDecision: renewal,
	})
	if err != nil {
		t.Fatalf("create VPS %q in %s/%s/%s: %v", name, lifecycle, usage, renewal, err)
	}
	return vps
}

func prepareVPSStateRepairCancelledVPS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, usage vpsassets.UsageStatus, renewal vpsassets.RenewalDecision) {
	t.Helper()
	if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status = $2, usage_status = $3, renewal_decision = $4 where vps_id = $1`, vpsID, vpsassets.LifecycleCancelled, usage, renewal); err != nil {
		t.Fatalf("prepare cancelled VPS %q: %v", vpsID, err)
	}
}

func assertVPSStateRepairStoredVPSState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, wantLifecycle vpsassets.LifecycleStatus, wantUsage vpsassets.UsageStatus, wantRenewal vpsassets.RenewalDecision, wantArchived bool) {
	t.Helper()
	var lifecycle vpsassets.LifecycleStatus
	var usage vpsassets.UsageStatus
	var renewal vpsassets.RenewalDecision
	var archivedAt *time.Time
	var snapshotJSON []byte
	if err := pool.QueryRow(ctx, `select lifecycle_status, usage_status, renewal_decision, archived_at, archived_state_snapshot from vps_assets where vps_id = $1`, vpsID).Scan(&lifecycle, &usage, &renewal, &archivedAt, &snapshotJSON); err != nil {
		t.Fatalf("read VPS state for %q: %v", vpsID, err)
	}
	if lifecycle != wantLifecycle || usage != wantUsage || renewal != wantRenewal || (archivedAt != nil) != wantArchived {
		t.Fatalf("stored VPS state = lifecycle:%q usage:%q renewal:%q archived_at:%v, want lifecycle:%q usage:%q renewal:%q archived:%t", lifecycle, usage, renewal, archivedAt, wantLifecycle, wantUsage, wantRenewal, wantArchived)
	}
	if wantArchived && (len(snapshotJSON) == 0 || string(snapshotJSON) == "null") {
		t.Fatalf("archived VPS %q has no retained state snapshot", vpsID)
	}
	if !wantArchived && len(snapshotJSON) != 0 && string(snapshotJSON) != "null" {
		t.Fatalf("unarchived VPS %q unexpectedly has archived snapshot %s", vpsID, snapshotJSON)
	}
}

type vpsStateRepairExpectedAuditState struct {
	lifecycle     vpsassets.LifecycleStatus
	usage         vpsassets.UsageStatus
	renewal       vpsassets.RenewalDecision
	hasArchivedAt bool
	snapshot      *vpsStateRepairSnapshotExpectation
}

type vpsStateRepairSnapshotExpectation struct {
	lifecycle vpsassets.LifecycleStatus
	usage     vpsassets.UsageStatus
	renewal   vpsassets.RenewalDecision
	source    string
}

func assertVPSStateRepairLifecycleAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, actionType assetlifecycle.ActionType, reason string, beforeWant, afterWant vpsStateRepairExpectedAuditState) {
	t.Helper()
	var actionStatus, actionReason, objectType, objectID, stepType, stepStatus, message string
	var beforeJSON, afterJSON []byte
	if err := pool.QueryRow(ctx, `
		select a.status, a.reason, s.object_type, s.object_id, s.step_type, s.status, s.message, s.before_state, s.after_state
		from asset_lifecycle_actions a
		join asset_lifecycle_action_steps s using (action_id)
		where a.vps_id = $1 and a.action_type = $2
		order by a.created_at desc limit 1`, vpsID, actionType).Scan(
		&actionStatus, &actionReason, &objectType, &objectID, &stepType, &stepStatus, &message, &beforeJSON, &afterJSON,
	); err != nil {
		t.Fatalf("read %s lifecycle audit: %v", actionType, err)
	}
	if actionStatus != assetlifecycle.ActionStatusCompleted || actionReason != reason || objectType != assetlifecycle.ObjectTypeVPS || objectID != vpsID || stepType != assetlifecycle.StepTypeVPSLifecycle || stepStatus != assetlifecycle.StepStatusCompleted || message != reason {
		t.Fatalf("%s audit = action(%q,%q) step(%q,%q,%q,%q), want completed VPS audit with full reason", actionType, actionStatus, actionReason, objectType, stepType, stepStatus, message)
	}
	assertVPSStateRepairAuditState(t, decodeVPSStateRepairAuditJSON(t, beforeJSON), beforeWant)
	assertVPSStateRepairAuditState(t, decodeVPSStateRepairAuditJSON(t, afterJSON), afterWant)
}

func decodeVPSStateRepairAuditJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode lifecycle audit state %q: %v", raw, err)
	}
	return state
}

func assertVPSStateRepairAuditState(t *testing.T, state map[string]any, want vpsStateRepairExpectedAuditState) {
	t.Helper()
	if state["lifecycle_status"] != string(want.lifecycle) || state["usage_status"] != string(want.usage) || state["renewal_decision"] != string(want.renewal) {
		t.Fatalf("lifecycle audit state = %#v, want %s/%s/%s", state, want.lifecycle, want.usage, want.renewal)
	}
	archivedAt := state["archived_at"]
	if (archivedAt != nil) != want.hasArchivedAt {
		t.Fatalf("lifecycle audit archived_at = %#v, want present:%t", archivedAt, want.hasArchivedAt)
	}
	if archivedAt != nil {
		value, ok := archivedAt.(string)
		if !ok {
			t.Fatalf("lifecycle audit archived_at type = %T, want string", archivedAt)
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			t.Fatalf("parse lifecycle audit archived_at %q: %v", value, err)
		}
	}
	snapshotValue := state["archived_state_snapshot"]
	if want.snapshot == nil {
		if snapshotValue != nil {
			t.Fatalf("lifecycle audit snapshot = %#v, want null", snapshotValue)
		}
		return
	}
	snapshot, ok := snapshotValue.(map[string]any)
	if !ok {
		t.Fatalf("lifecycle audit snapshot = %#v, want archived snapshot object", snapshotValue)
	}
	if snapshot["lifecycle_status"] != string(want.snapshot.lifecycle) || snapshot["usage_status"] != string(want.snapshot.usage) || snapshot["renewal_decision"] != string(want.snapshot.renewal) || snapshot["source"] != want.snapshot.source {
		t.Fatalf("lifecycle audit snapshot = %#v, want %s/%s/%s source:%s", snapshot, want.snapshot.lifecycle, want.snapshot.usage, want.snapshot.renewal, want.snapshot.source)
	}
	capturedAt, ok := snapshot["captured_at"].(string)
	if !ok || capturedAt == "" {
		t.Fatalf("lifecycle audit snapshot captured_at = %#v, want timestamp", snapshot["captured_at"])
	}
	if _, err := time.Parse(time.RFC3339Nano, capturedAt); err != nil {
		t.Fatalf("parse lifecycle audit snapshot captured_at %q: %v", capturedAt, err)
	}
}

func assertVPSStateRepairSnapshot(t *testing.T, snapshot *vpsassets.ArchivedStateSnapshot, lifecycle vpsassets.LifecycleStatus, usage vpsassets.UsageStatus, renewal vpsassets.RenewalDecision, source string) {
	t.Helper()
	if snapshot == nil {
		t.Fatal("archived state snapshot is nil")
	}
	if snapshot.LifecycleStatus != lifecycle || snapshot.UsageStatus != usage || snapshot.RenewalDecision != renewal || snapshot.Source != source || snapshot.CapturedAt.IsZero() {
		t.Fatalf("archived state snapshot = %#v, want %s/%s/%s source:%s with capture time", snapshot, lifecycle, usage, renewal, source)
	}
}

func containsVPSStateRepairString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.Contains(value, wanted) {
			return true
		}
	}
	return false
}
