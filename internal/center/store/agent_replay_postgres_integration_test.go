package store

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

// RunAgentReplayWindowAcceptance exposes a test-only fixture to the external
// package, allowing real HTTP handlers (which import store) without a cycle.
func RunAgentReplayWindowAcceptance(t *testing.T, httpAcceptance func(*pgxpool.Pool, agentapi.SyncRequest)) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	assertAgentReplayRuntimePrivileges(t, ctx, fixture)
	const prefix = "replay_window"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecyclePendingEnrollment)
	pool := fixture.openDirectRuntimePool(t, ctx, "agent-replay-window", 4)
	base, _ := syncInterleavingBatches(prefix)
	base.Observations.HostSamples = nil
	base.Observations.ProbeObservations = nil
	base.IPQualityReports = nil
	base = prepareSyncFixture(base)
	original := syncFixtureDTO(base)
	original.SyncToken = base.SyncToken
	makeRequest := func(sequence, allocated, floor, live int64) agentapi.SyncRequest {
		req := original
		req.BatchSequence, req.AllocatedThrough, req.RetiredThrough = sequence, allocated, floor
		req.Heartbeats = append([]agentapi.MonitoringInstanceHeartbeat(nil), original.Heartbeats...)
		if sequence == 0 {
			req.Heartbeats = nil
		} else {
			req.Heartbeats[0].SyncBatchID = req.ProducerID + ":" + strconv.FormatInt(sequence, 10)
		}
		req.LiveSignal = nil
		if live > 0 {
			req.LiveSignal = &agentapi.LiveSignal{Sequence: live, Fingerprint: "fp_" + prefix}
		}
		return req
	}
	apply := func(repo *PostgresSyncRepository, req agentapi.SyncRequest, want syncing.ResultDisposition) syncing.Result {
		t.Helper()
		batch := syncFixtureBatch(req)
		result, err := repo.ApplyBatch(ctx, batch)
		if err != nil {
			t.Fatalf("apply sequence %d: %v", req.BatchSequence, err)
		}
		if result.Disposition != want || result.SessionID != req.SessionID || result.ProducerID != req.ProducerID || result.BatchSequence != req.BatchSequence || result.BatchDigest != hex.EncodeToString(batch.PayloadDigest[:]) {
			t.Fatalf("ack mismatch: %#v want disposition %q", result, want)
		}
		if want == syncing.ResultDispositionDeferred || want == syncing.ResultDispositionExactDuplicate {
			if result.Plan.PendingAction != nil || len(result.Plan.ProbeAssignments) != 0 || result.Plan.IPQualityPlan != nil {
				t.Fatalf("non-recorded plan: %#v", result.Plan)
			}
		}
		return result
	}
	repo := NewPostgresSyncRepository(pool)
	first := makeRequest(1, 131073, 0, 100)
	apply(repo, first, syncing.ResultDispositionRecorded)
	var firstOnline time.Time
	if err := pool.QueryRow(ctx, `select last_trusted_online_at from monitoring_agent_sessions where session_id=$1`, first.SessionID).Scan(&firstOnline); err != nil {
		t.Fatal(err)
	}
	// A lost response or failed durable local deletion resends the identical
	// DTO through a newly constructed repository after process restart.
	retry := first
	retry.Heartbeats = append([]agentapi.MonitoringInstanceHeartbeat(nil), first.Heartbeats...)
	retry.Heartbeats[0].IsBackfilled = true
	retry.LiveSignal = &agentapi.LiveSignal{Sequence: 99, Fingerprint: "fp_" + prefix}
	apply(NewPostgresSyncRepository(pool), retry, syncing.ResultDispositionExactDuplicate)
	var retryOnline time.Time
	if err := pool.QueryRow(ctx, `select last_trusted_online_at from monitoring_agent_sessions where session_id=$1`, first.SessionID).Scan(&retryOnline); err != nil {
		t.Fatal(err)
	}
	if !retryOnline.Equal(firstOnline) {
		t.Fatal("old live sequence refreshed trusted online")
	}

	// Terminal admission failures cannot advance controls, receipts or online.
	for _, tc := range []struct {
		name   string
		change func(*agentapi.SyncRequest)
		want   error
	}{
		{"revision", func(r *agentapi.SyncRequest) { r.ProtocolRevision = 1 }, syncing.ErrSyncProtocolRequired},
		{"producer", func(r *agentapi.SyncRequest) {
			r.ProducerID = "99999999-9999-4999-8999-999999999999"
			r.Heartbeats[0].SyncBatchID = r.ProducerID + ":1"
		}, syncing.ErrSyncProducerMismatch},
		{"payload", func(r *agentapi.SyncRequest) { r.Heartbeats[0].AgentVersion = "changed" }, syncing.ErrSyncBatchIdentityConflict},
		{"commands", func(r *agentapi.SyncRequest) {
			r.CommandResults = []agentapi.CommandResult{{ActionID: "changed", CommandID: "uptime", Stdout: "new output"}}
		}, syncing.ErrSyncBatchIdentityConflict},
		{"token", func(r *agentapi.SyncRequest) { r.SyncToken = "wrong" }, syncing.ErrInvalidSyncToken},
		{"fingerprint", func(r *agentapi.SyncRequest) { r.LiveSignal.Fingerprint = "wrong" }, syncing.ErrBindingNotAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := first
			req.Heartbeats = append([]agentapi.MonitoringInstanceHeartbeat(nil), first.Heartbeats...)
			req.LiveSignal = &agentapi.LiveSignal{Sequence: 1000, Fingerprint: "fp_" + prefix}
			req.AllocatedThrough = 200000
			tc.change(&req)
			if _, err := repo.ApplyBatch(ctx, syncFixtureBatch(req)); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
			var high, allocated int64
			if err := pool.QueryRow(ctx, `select live_sequence_high,allocated_through from agent_replay_states where session_id=$1`, first.SessionID).Scan(&high, &allocated); err != nil {
				t.Fatal(err)
			}
			if high != 100 || allocated != 131073 {
				t.Fatalf("rejected request persisted state: high=%d allocated=%d", high, allocated)
			}
		})
	}
	apply(repo, makeRequest(131072, 131073, 0, 0), syncing.ResultDispositionRecorded)
	if _, err := pool.Exec(ctx, `update monitoring_instances set pending_action_id='replay-pending',pending_action_command_id='uptime' where monitoring_instance_id=$1`, first.MonitoringInstanceID); err != nil {
		t.Fatal(err)
	}
	deferred := makeRequest(131073, 131073, 0, 101)
	deferred.CommandResults = []agentapi.CommandResult{{ActionID: "must-not-complete", CommandID: "uptime", Stdout: "not persisted"}}
	apply(repo, deferred, syncing.ResultDispositionDeferred)
	var pending string
	var count int
	if err := pool.QueryRow(ctx, `select pending_action_id,(select count(*)::int from monitoring_instance_heartbeats where monitoring_instance_id=$1) from monitoring_instances where monitoring_instance_id=$1`, first.MonitoringInstanceID).Scan(&pending, &count); err != nil {
		t.Fatal(err)
	}
	if pending != "replay-pending" || count != 2 {
		t.Fatalf("deferred executed facts/commands: pending=%q facts=%d", pending, count)
	}
	if _, err := pool.Exec(ctx, `update monitoring_instances set pending_action_id=null,pending_action_command_id=null where monitoring_instance_id=$1`, first.MonitoringInstanceID); err != nil {
		t.Fatal(err)
	}
	// New current and old backlog can arrive in either order without moving the
	// window anchor away from the durable retirement floor.
	apply(repo, makeRequest(2, 131073, 0, 0), syncing.ResultDispositionRecorded)
	apply(repo, makeRequest(131073, 131073, 1, 102), syncing.ResultDispositionRecorded)
	var floor int64
	if err := pool.QueryRow(ctx, `select retired_through,(select count(*)::int from agent_batch_receipts where session_id=$1 and batch_sequence<=1) from agent_replay_states where session_id=$1`, first.SessionID).Scan(&floor, &count); err != nil {
		t.Fatal(err)
	}
	if floor != 1 || count != 0 {
		t.Fatalf("retirement trigger did not reclaim receipt: floor=%d count=%d", floor, count)
	}
	apply(repo, makeRequest(262148, 262148, 2, 103), syncing.ResultDispositionDeferred)
	if err := pool.QueryRow(ctx, `select retired_through,(select count(*)::int from agent_batch_receipts where session_id=$1 and batch_sequence<=2) from agent_replay_states where session_id=$1`, first.SessionID).Scan(&floor, &count); err != nil {
		t.Fatal(err)
	}
	if floor != 2 || count != 0 {
		t.Fatalf("deferred request failed to commit retirement: floor=%d count=%d", floor, count)
	}
	if _, err := repo.ApplyBatch(ctx, syncFixtureBatch(first)); !errors.Is(err, syncing.ErrSyncBatchRetired) {
		t.Fatalf("retired replay error=%v", err)
	}
	// A late control may advertise older watermarks but cannot move them back.
	apply(repo, makeRequest(0, 0, 0, 0), syncing.ResultDispositionSuppressed)
	if _, err := pool.Exec(ctx, `update monitoring_instances set monitoring_status=$2 where monitoring_instance_id=$1`, first.MonitoringInstanceID, monitoringinstances.MonitoringPaused); err != nil {
		t.Fatal(err)
	}
	stopped := apply(repo, makeRequest(131074, 131074, 131073, 103), syncing.ResultDispositionSuppressed)
	if !stopped.StopCollection {
		t.Fatal("paused batch did not stop collection")
	}
	apply(repo, makeRequest(0, 131074, 131074, 104), syncing.ResultDispositionSuppressed)
	for _, lifecycle := range []string{"evidence_only", "retired", "archived"} {
		stoppedPrefix := prefix + "_" + lifecycle
		seedSyncInterleavingFixture(t, ctx, fixture, stoppedPrefix, monitoringinstances.LifecycleInUse)
		batch, _ := syncInterleavingBatches(stoppedPrefix)
		batch.Observations.HostSamples, batch.Observations.ProbeObservations, batch.IPQualityReports = nil, nil, nil
		batch.BatchSequence, batch.AllocatedThrough = 1, 1
		batch = prepareSyncFixture(batch)
		req := syncFixtureDTO(batch)
		apply(repo, req, syncing.ResultDispositionRecorded)
		var err error
		switch lifecycle {
		case "evidence_only":
			_, err = fixture.db.Exec(ctx, `update monitoring_agent_sessions set capability='evidence_only',ended_at=now() where session_id=$1`, req.SessionID)
		case "retired":
			_, err = fixture.db.Exec(ctx, `update monitoring_instances set lifecycle_status=$2 where monitoring_instance_id=$1`, req.MonitoringInstanceID, monitoringinstances.LifecycleRetired)
		case "archived":
			_, err = fixture.db.Exec(ctx, `update vps_assets set lifecycle_status='archived' where vps_id=$1`, "vps_"+stoppedPrefix)
		}
		if err != nil {
			t.Fatal(err)
		}
		req.BatchSequence, req.RetiredThrough = 0, 1
		req.Heartbeats = nil
		req.LiveSignal = &agentapi.LiveSignal{Sequence: 2, Fingerprint: "fp_" + stoppedPrefix}
		result := apply(repo, req, syncing.ResultDispositionSuppressed)
		if !result.StopCollection || result.RetiredThrough != 1 {
			t.Fatalf("stopped control=%#v", result)
		}
		var receipts int
		if err := pool.QueryRow(ctx, `select count(*)::int from agent_batch_receipts where session_id=$1`, req.SessionID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if receipts != 0 {
			t.Fatalf("%s retained %d receipts after control", lifecycle, receipts)
		}
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from agent_batch_receipts where session_id=$1`, first.SessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("suppressed control retained %d receipts", count)
	}
	assertAgentReplaySQLGuards(t, ctx, fixture, pool, first.SessionID, first.ProducerID)
	httpAcceptance(pool, makeRequest(131077, 262148, 131075, 105))
}

func assertAgentReplayRuntimePrivileges(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	for _, tc := range []struct {
		table                                  string
		selectOK, insertOK, updateOK, deleteOK bool
	}{
		{"agent_replay_states", true, true, true, false}, {"agent_batch_receipts", true, true, false, true},
	} {
		var s, i, u, d bool
		if err := fixture.db.QueryRow(ctx, `select has_table_privilege($1::name,$2,'SELECT'),has_table_privilege($1::name,$2,'INSERT'),has_table_privilege($1::name,$2,'UPDATE'),has_table_privilege($1::name,$2,'DELETE')`, fixture.runtime, "public."+tc.table).Scan(&s, &i, &u, &d); err != nil {
			t.Fatal(err)
		}
		if s != tc.selectOK || i != tc.insertOK || u != tc.updateOK || d != tc.deleteOK {
			t.Fatalf("%s privilege vector=%v/%v/%v/%v", tc.table, s, i, u, d)
		}
	}
}

func assertAgentReplaySQLGuards(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, pool *pgxpool.Pool, session, producer string) {
	t.Helper()
	const sequence int64 = 131075
	if _, err := pool.Exec(ctx, `update agent_replay_states set allocated_through=$2 where session_id=$1`, session, sequence+131073); err != nil {
		t.Fatal(err)
	}
	digest, err := agentapi.CanonicalSyncBatchDigest(agentapi.SyncRequest{SessionID: session, ProducerID: producer, BatchSequence: sequence})
	if err != nil {
		t.Fatal(err)
	}
	// Insert's actual BEFORE guard has acquired FOR SHARE; keep that transaction
	// open at this barrier while another runtime connection attempts retirement.
	insertTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer insertTx.Rollback(ctx)
	var insertPID int
	if err := insertTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&insertPID); err != nil {
		t.Fatal(err)
	}
	if _, err := insertTx.Exec(ctx, `insert into agent_batch_receipts(session_id,batch_sequence,payload_digest) values($1,$2,$3)`, session, sequence, digest[:]); err != nil {
		t.Fatal(err)
	}
	floorTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer floorTx.Rollback(ctx)
	var floorPID int
	if err := floorTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&floorPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := floorTx.Exec(ctx, `update agent_replay_states set retired_through=$2 where session_id=$1`, session, sequence)
		if err == nil {
			err = floorTx.Commit(ctx)
		}
		result <- err
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, floorPID, insertPID)
	if err := insertTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*)::int from agent_batch_receipts where session_id=$1 and batch_sequence<=$2`, session, sequence).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("concurrent retirement left retired receipt")
	}
	if _, err := pool.Exec(ctx, `insert into agent_batch_receipts(session_id,batch_sequence,payload_digest) values($1,$2,$3)`, session, sequence+1, digest[:]); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sql, state string
		args       []any
	}{
		{`update agent_replay_states set retired_through=retired_through-1 where session_id=$1`, "23514", []any{session}},
		{`update agent_replay_states set allocated_through=allocated_through-1 where session_id=$1`, "23514", []any{session}},
		{`update agent_replay_states set live_sequence_high=0 where session_id=$1`, "23514", []any{session}},
		{`update agent_replay_states set producer_id='11111111-1111-4111-8111-111111111111' where session_id=$1`, "23514", []any{session}},
		{`delete from agent_batch_receipts where session_id=$1`, "23514", []any{session}},
		{`insert into agent_batch_receipts(session_id,batch_sequence,payload_digest) values($1,$2,$3)`, "23514", []any{session, sequence + 131073, digest[:]}},
		{`update agent_batch_receipts set payload_digest=$2 where session_id=$1`, "42501", []any{session, digest[:]}},
		{`delete from agent_replay_states where session_id=$1`, "42501", []any{session}},
	} {
		_, err := pool.Exec(ctx, tc.sql, tc.args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != tc.state {
			t.Fatalf("guard SQL %q: %v want %s", tc.sql, err, tc.state)
		}
	}
	// Reverse the ordering as well: an insert waiting behind an uncommitted
	// floor update must re-read the new floor and reject, not leave a receipt.
	retireTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer retireTx.Rollback(ctx)
	var retirePID int
	if err := retireTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&retirePID); err != nil {
		t.Fatal(err)
	}
	if _, err := retireTx.Exec(ctx, `update agent_replay_states set retired_through=$2 where session_id=$1`, session, sequence+1); err != nil {
		t.Fatal(err)
	}
	insertConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer insertConn.Release()
	var waiterPID int
	if err := insertConn.QueryRow(ctx, `select pg_backend_pid()`).Scan(&waiterPID); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := insertConn.Exec(ctx, `insert into agent_batch_receipts(session_id,batch_sequence,payload_digest) values($1,$2,$3)`, session, sequence+1, digest[:])
		result <- err
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, waiterPID, retirePID)
	if err := retireTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("insert after concurrent retirement: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from agent_batch_receipts where session_id=$1 and batch_sequence<=$2`, session, sequence+1).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("reverse-order retirement left retired receipt")
	}
}
