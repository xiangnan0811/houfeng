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
	pendingActionID   string
	pendingCommandID  string

	probeMetadataByItemID map[string]observations.ProbeMetadata
	probeMetadataErr      map[string]error
	planRows              []fakeAgentPlanScan

	execErrForSQLSubstring    string
	execErr                   error
	commandResultRowsAffected *int
	execSQL                   []string
	execArgs                  [][]any
	commitCalls               int
	rollbackCalls             int
}

func (f *fakeSyncBatchTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, append([]any(nil), args...))
	if f.execErr != nil && strings.Contains(sql, f.execErrForSQLSubstring) {
		return pgconn.CommandTag{}, f.execErr
	}
	if strings.Contains(sql, "last_action->>'status'") {
		if f.commandResultRowsAffected != nil && *f.commandResultRowsAffected == 0 {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
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
	case strings.Contains(sql, "pending_action_id"):
		if f.pendingActionID == "" {
			return fakeRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		actionID := f.pendingActionID
		commandID := f.pendingCommandID
		return fakeRow{scan: func(dest ...any) error {
			*(dest[0].(**string)) = &actionID
			*(dest[1].(**string)) = &commandID
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
	if result.Plan.HostSampleFrequencyTier != agentapi.FrequencyTier5s {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", result.Plan.HostSampleFrequencyTier, agentapi.FrequencyTier5s)
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

func TestSyncBatchDispatchesPendingActionAsDurableLastAction(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus: agentapi.BindingStatusBound,
		nodeFingerprint:   "fp-001",
		nodeSyncTokenHash: hashSyncToken("sync-token-001"),
		pendingActionID:   "act_001",
		pendingCommandID:  "uptime",
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
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
	if result.Plan.PendingAction == nil {
		t.Fatal("PendingAction = nil, want dispatched action")
	}
	if result.Plan.PendingAction.ActionID != "act_001" || result.Plan.PendingAction.CommandID != "uptime" {
		t.Fatalf("PendingAction = %#v, want act_001/uptime", result.Plan.PendingAction)
	}

	args := tx.argsForSQL("pending_action_id = NULL")
	if len(args) != 4 {
		t.Fatalf("dispatch args = %#v, want node id, payload, action id, command id", args)
	}
	payload, ok := args[1].([]byte)
	if !ok {
		t.Fatalf("pending payload arg = %#v, want []byte JSON", args[1])
	}
	for _, want := range []string{`"action_id":"act_001"`, `"command_id":"uptime"`, `"status":"pending"`} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("pending last_action = %s, missing %s", payload, want)
		}
	}
	if args[2] != "act_001" || args[3] != "uptime" {
		t.Fatalf("dispatch guard args = %#v, want act_001/uptime", args[2:4])
	}
}

func TestSyncBatchStoresMatchingCommandResultWithCommandID(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus: agentapi.BindingStatusBound,
		nodeFingerprint:   "fp-001",
		nodeSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_001",
		CommandID: "uptime",
		Stdout:    "up 1 day",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	args := tx.argsForSQL("last_action->>'status'")
	if len(args) != 5 {
		t.Fatalf("result args = %#v, want payload, node id, status, action id, command id", args)
	}
	payload, ok := args[0].([]byte)
	if !ok {
		t.Fatalf("result payload arg = %#v, want []byte JSON", args[0])
	}
	for _, want := range []string{`"action_id":"act_001"`, `"command_id":"uptime"`, `"status":"done"`, `"stdout":"up 1 day"`} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("result last_action = %s, missing %s", payload, want)
		}
	}
	if args[1] != "nd_001" || args[2] != commandActionStatusPending || args[3] != "act_001" || args[4] != "uptime" {
		t.Fatalf("result guard args = %#v, want node/status/action/command guard", args[1:5])
	}
}

func TestSyncBatchIgnoresMismatchedCommandResultRows(t *testing.T) {
	t.Parallel()

	zeroRows := 0
	tx := &fakeSyncBatchTx{
		nodeBindingStatus:         agentapi.BindingStatusBound,
		nodeFingerprint:           "fp-001",
		nodeSyncTokenHash:         hashSyncToken("sync-token-001"),
		commandResultRowsAffected: &zeroRows,
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_stale",
		CommandID: "uptime",
		Stdout:    "stale",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v, want mismatch to be ignored", err)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
}

func TestSyncBatchStoresCommandResultBeforeDispatchingQueuedAction(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		nodeBindingStatus: agentapi.BindingStatusBound,
		nodeFingerprint:   "fp-001",
		nodeSyncTokenHash: hashSyncToken("sync-token-001"),
		pendingActionID:   "act_next",
		pendingCommandID:  "df_h",
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_prev",
		CommandID: "uptime",
		Stdout:    "up 1 day",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	resultIndex := sqlIndex(tx.execSQL, "last_action->>'status'")
	dispatchIndex := sqlIndex(tx.execSQL, "pending_action_id = NULL")
	if resultIndex == -1 || dispatchIndex == -1 {
		t.Fatalf("exec SQL missing result or dispatch update: %#v", tx.execSQL)
	}
	if resultIndex > dispatchIndex {
		t.Fatalf("command result update index %d should run before dispatch index %d", resultIndex, dispatchIndex)
	}
}

type fakeRow struct {
	scan func(dest ...any) error
}

func (f fakeRow) Scan(dest ...any) error {
	return f.scan(dest...)
}

func containsSQL(sqls []string, want string) bool {
	return sqlIndex(sqls, want) != -1
}

func sqlIndex(sqls []string, want string) int {
	for i, sql := range sqls {
		if strings.Contains(sql, want) {
			return i
		}
	}
	return -1
}

func (f *fakeSyncBatchTx) argsForSQL(want string) []any {
	for i, sql := range f.execSQL {
		if strings.Contains(sql, want) {
			return f.execArgs[i]
		}
	}
	return nil
}
