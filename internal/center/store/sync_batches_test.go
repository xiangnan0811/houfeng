package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresSyncRepositoryRollsBackHeartbeatWhenObservationWriteFails(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus:      agentapi.BindingStatusBound,
		nodeFingerprint:        "fp-001",
		nodeSyncTokenHash:      hashSyncToken("sync-token-001"),
		probeMetadataByItemID:  map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
		execErrForSQLSubstring: "insert into probe_observations",
		execErr:                errors.New("probe write boom"),
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	_, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if err == nil {
		t.Fatal("ApplyBatch() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "insert probe observation") {
		t.Fatalf("ApplyBatch() error = %q, want insert probe observation context", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls == 0 {
		t.Fatal("rollbackCalls = 0, want rollback on observation write failure")
	}
	if !containsSQL(tx.execSQL, "insert into node_heartbeats") {
		t.Fatal("expected heartbeat insert before probe write failure")
	}
	if containsSQL(tx.execSQL, "update nodes") {
		t.Fatal("node sync state should not update when probe write fails")
	}
}

func TestPostgresSyncRepositoryRejectsProbeMetadataMismatchBeforeWritingBatch(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus:     agentapi.BindingStatusBound,
		nodeFingerprint:       "fp-001",
		nodeSyncTokenHash:     hashSyncToken("sync-token-001"),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_wrong", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	_, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if !errors.Is(err, observations.ErrInvalidProbeObservation) {
		t.Fatalf("ApplyBatch() error = %v, want ErrInvalidProbeObservation", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls == 0 {
		t.Fatal("rollbackCalls = 0, want rollback on probe metadata mismatch")
	}
	if containsSQL(tx.execSQL, "insert into node_heartbeats") {
		t.Fatal("probe metadata mismatch should fail before writing heartbeats")
	}
}

func TestPostgresSyncRepositoryRejectsInvalidProbeObservationSemanticsBeforeWritingBatch(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus:     agentapi.BindingStatusBound,
		nodeFingerprint:       "fp-001",
		nodeSyncTokenHash:     hashSyncToken("sync-token-001"),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.Observations.ProbeObservations[0].ResultKind = "maybe"

	_, err := repo.ApplyBatch(context.Background(), batch)
	if !errors.Is(err, observations.ErrInvalidProbeObservation) {
		t.Fatalf("ApplyBatch() error = %v, want ErrInvalidProbeObservation", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls == 0 {
		t.Fatal("rollbackCalls = 0, want rollback on invalid probe observation semantics")
	}
	if containsSQL(tx.execSQL, "insert into node_heartbeats") {
		t.Fatal("invalid probe observation semantics should fail before writing heartbeats")
	}
}

func TestPostgresSyncRepositoryRejectsObservationBatchWithoutHeartbeatCarrier(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.Heartbeats = nil

	_, err := repo.ApplyBatch(context.Background(), batch)
	if !errors.Is(err, syncing.ErrHeartbeatRequired) {
		t.Fatalf("ApplyBatch() error = %v, want ErrHeartbeatRequired", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls != 0 {
		t.Fatalf("rollbackCalls = %d, want 0", tx.rollbackCalls)
	}
	if len(tx.execSQL) != 0 {
		t.Fatalf("len(execSQL) = %d, want 0", len(tx.execSQL))
	}
}

func testSyncBatch() syncing.Batch {
	observedAt := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	return syncing.Batch{
		NodeID:    "nd_001",
		SyncToken: "sync-token-001",
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt:   observedAt,
			AgentVersion: "agent/v0.1.0",
			Fingerprint:  "fp-001",
			SyncBatchID:  "sync_001",
		}},
		Observations: observations.BatchWrite{
			NodeID: "nd_001",
			ProbeObservations: []observations.ProbeObservationWrite{{
				NodeID:       "nd_001",
				TargetID:     "tg_001",
				ProbeItemID:  "pb_001",
				ProbeKind:    agentapi.ProbeKindHTTP,
				ObservedAt:   observedAt,
				AgentVersion: "agent/v0.1.0",
				Fingerprint:  "fp-001",
				ResultKind:   agentapi.ProbeResultSuccess,
				SyncBatchID:  "sync_001",
			}},
		},
	}
}

type fakeSyncBatchTx struct {
	nodeBindingStatus string
	nodeFingerprint   string
	nodeSyncTokenHash string
	nodeLabels        []string

	probeMetadataByItemID map[string]observations.ProbeMetadata
	probeMetadataErr      map[string]error
	planRows              []fakeAgentPlanScan

	execErrForSQLSubstring string
	execErr                error
	execSQL                []string
	execArgs               [][]any
	commitCalls            int
	rollbackCalls          int
}

func (f *fakeSyncBatchTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, append([]any(nil), args...))
	if f.execErr != nil && strings.Contains(sql, f.execErrForSQLSubstring) {
		return pgconn.CommandTag{}, f.execErr
	}
	if strings.Contains(sql, "update nodes") {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeSyncBatchTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "select labels"):
		return fakeRow{scan: func(dest ...any) error {
			*(dest[0].(*[]string)) = append([]string(nil), f.nodeLabels...)
			return nil
		}}
	case strings.Contains(sql, "from nodes"):
		return fakeRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = f.nodeBindingStatus
			*(dest[1].(*string)) = f.nodeFingerprint
			*(dest[2].(*string)) = f.nodeSyncTokenHash
			return nil
		}}
	case strings.Contains(sql, "from probe_items"):
		probeItemID, _ := args[0].(string)
		if err := f.probeMetadataErr[probeItemID]; err != nil {
			return fakeRow{scan: func(dest ...any) error { return err }}
		}
		metadata, ok := f.probeMetadataByItemID[probeItemID]
		if !ok {
			return fakeRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		return fakeRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = metadata.TargetID
			*(dest[1].(*string)) = metadata.ProbeKind
			return nil
		}}
	default:
		return fakeRow{scan: func(dest ...any) error { return errors.New("unexpected query") }}
	}
}

func (f *fakeSyncBatchTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "from targets t") {
		return &fakeAgentPlanRows{rows: f.planRows}, nil
	}
	return nil, errors.New("unexpected query")
}

func (f *fakeSyncBatchTx) Commit(context.Context) error {
	f.commitCalls++
	return nil
}

func (f *fakeSyncBatchTx) Rollback(context.Context) error {
	f.rollbackCalls++
	return nil
}

func TestPostgresSyncRepositoryPersistsBackfilledHeartbeatFlag(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus: agentapi.BindingStatusBound,
		nodeFingerprint:   "fp-001",
		nodeSyncTokenHash: hashSyncToken("sync-token-001"),
		nodeLabels:        []string{"核心", "edge"},
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
		planRows: []fakeAgentPlanScan{{
			scan: func(dest ...any) error {
				*(dest[0].(*string)) = "tg_001"
				*(dest[1].(*string)) = "api.example.test"
				port := 443
				*(dest[2].(**int)) = &port
				*(dest[3].(*string)) = "启用"
				*(dest[4].(*string)) = "pb_001"
				*(dest[5].(*string)) = agentapi.ProbeKindHTTP
				*(dest[6].(*string)) = agentapi.FrequencyTier1m
				*(dest[7].(*int)) = 5
				*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
				return nil
			},
		}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.Heartbeats[0].IsBackfilled = true

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	heartbeatArgs := tx.argsForSQL("insert into node_heartbeats")
	if got, ok := heartbeatArgs[6].(bool); !ok || !got {
		t.Fatalf("heartbeat is_backfilled arg = %#v, want true", heartbeatArgs[6])
	}
}

func TestSyncBatchPlanReturnsAcceptedAtAndDerivedPlan(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus: agentapi.BindingStatusBound,
		nodeFingerprint:   "fp-001",
		nodeSyncTokenHash: hashSyncToken("sync-token-001"),
		nodeLabels:        []string{"核心", "edge"},
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
		planRows: []fakeAgentPlanScan{{
			scan: func(dest ...any) error {
				*(dest[0].(*string)) = "tg_001"
				*(dest[1].(*string)) = "api.example.test"
				port := 443
				*(dest[2].(**int)) = &port
				*(dest[3].(*string)) = "启用"
				*(dest[4].(*string)) = "pb_001"
				*(dest[5].(*string)) = agentapi.ProbeKindHTTP
				*(dest[6].(*string)) = agentapi.FrequencyTier1m
				*(dest[7].(*int)) = 5
				*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
				return nil
			},
		}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	result, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if result.AcceptedAt.IsZero() {
		t.Fatal("AcceptedAt is zero, want non-zero")
	}
	if result.Plan.HostSampleFrequencyTier != agentapi.FrequencyTier1m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", result.Plan.HostSampleFrequencyTier, agentapi.FrequencyTier1m)
	}
	if len(result.Plan.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(result.Plan.ProbeAssignments))
	}
	if result.Plan.ProbeAssignments[0].TargetID != "tg_001" {
		t.Fatalf("TargetID = %q, want %q", result.Plan.ProbeAssignments[0].TargetID, "tg_001")
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
}

type fakeRow struct {
	scan func(dest ...any) error
}

func (f fakeRow) Scan(dest ...any) error {
	return f.scan(dest...)
}

func containsSQL(sqls []string, want string) bool {
	for _, sql := range sqls {
		if strings.Contains(sql, want) {
			return true
		}
	}
	return false
}

func (f *fakeSyncBatchTx) argsForSQL(want string) []any {
	for i, sql := range f.execSQL {
		if strings.Contains(sql, want) {
			return f.execArgs[i]
		}
	}
	return nil
}
