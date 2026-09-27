package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestTrustedLiveSignalsRemainAvailableWhenCollectionStopped(t *testing.T) {
	for _, capability := range []string{"full", "evidence_only"} {
		t.Run(capability, func(t *testing.T) {
			tx := &fakeSyncBatchTx{monitoringInstanceBindingStatus: agentapi.BindingStatusBound, monitoringInstanceFingerprint: "fp-001", monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"), monitoringInstanceMonitoring: monitoringinstances.MonitoringPaused, sessionCapability: capability}
			now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
			tx.receiverClock = now
			repo := &PostgresSyncRepository{beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) { return tx, nil }, now: func() time.Time { return now }}
			batch := testSyncBatch()
			batch.LiveSignal = &agentapi.LiveSignal{ID: "signal-1", Fingerprint: "fp-001"}
			batch.SessionID = "mas_1"
			result, err := repo.ApplyBatch(context.Background(), batch)
			if err != nil {
				t.Fatal(err)
			}
			if !result.StopCollection {
				t.Fatal("must explicitly stop collection")
			}
			args := tx.argsForSQL("insert into agent_live_signals")
			if len(args) != 3 || args[2] != now {
				t.Fatalf("signal must use receiver time: %#v", args)
			}
			if !containsSQL(tx.execSQL, "update monitoring_agent_sessions set last_trusted_online_at") {
				t.Fatal("missing durable evidence")
			}
			if containsSQL(tx.execSQL, "insert into host_samples") || containsSQL(tx.execSQL, "insert into monitoring_instance_heartbeats") {
				t.Fatal("suppressed observation entered normal pipeline")
			}
		})
	}
}

func TestDuplicateLiveSignalAndBackfillNeverRefreshDurableOnlineTime(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "backfill", true: "duplicate"}[duplicate], func(t *testing.T) {
			tx := &fakeSyncBatchTx{duplicateLiveSignal: duplicate}
			batch := syncing.Batch{MonitoringInstanceID: "mi_1", SessionID: "mas_1"}
			if duplicate {
				batch.LiveSignal = &agentapi.LiveSignal{ID: "old-signal", Fingerprint: "fp"}
			}
			if err := recordTrustedLiveSignal(context.Background(), tx, batch, acceptedSyncBatchState{}, time.Now()); err != nil {
				t.Fatal(err)
			}
			if containsSQL(tx.execSQL, "update monitoring_agent_sessions") || containsSQL(tx.execSQL, "update monitoring_instances") {
				t.Fatal("old evidence refreshed online time")
			}
		})
	}
}
