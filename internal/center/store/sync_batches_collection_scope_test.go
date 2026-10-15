package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/contracts/agentapi"
)

func TestSyncCollectionCapabilityRequiresCommittedWritableLivePermission(t *testing.T) {
	for _, tc := range []struct {
		name, capability, monitoring               string
		live, heartbeat, duplicate, archived, fail bool
		want                                       bool
	}{
		{"full recorded", "full", "", true, true, false, false, false, true},
		{"full live only", "full", "", true, false, false, false, false, true},
		{"backfill", "full", "", false, true, false, false, false, false},
		{"evidence only", "evidence_only", "", true, true, false, false, false, false},
		{"paused", "full", monitoringinstances.MonitoringPaused, true, true, false, false, false, false},
		{"archived", "full", "", true, true, false, true, false, false},
		{"duplicate batch", "full", "", true, true, true, false, false, false},
		{"rollback", "full", "", true, true, false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &fakeSyncBatchTx{monitoringInstanceBindingStatus: agentapi.BindingStatusBound, monitoringInstanceFingerprint: "fp-001", monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"), sessionCapability: tc.capability, monitoringInstanceMonitoring: tc.monitoring, monitoringInstanceArchived: tc.archived, duplicateSyncBatch: tc.duplicate}
			if tc.fail {
				tx.execErrForSQLSubstring, tx.execErr = "insert into monitoring_instance_heartbeats", errors.New("write failed")
			}
			repo := &PostgresSyncRepository{beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) { return tx, nil }}
			batch := testSyncBatch()
			batch.Observations = observations.BatchWrite{}
			batch.SessionID = "mas_1"
			batch.IPQualityCollectionRevision = 5
			if tc.live {
				batch.LiveSignal = &agentapi.LiveSignal{ID: "live", Fingerprint: "fp-001"}
			}
			if !tc.heartbeat {
				batch.Heartbeats = nil
			}
			result, err := repo.ApplyBatch(context.Background(), batch)
			if (err != nil) != tc.fail {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if result.LiveCollectionAllowed != tc.want {
				t.Fatalf("untrusted collection permission: %#v", result)
			}
			if tc.want && tx.commitCalls != 1 {
				t.Fatal("collection permission preceded commit")
			}
		})
	}
}
