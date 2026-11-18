package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recorddeletion"
	storemigrate "houfeng/internal/center/store/migrate"
)

type v2PreparedRunner struct {
	id, volume, system, run, owner string
}

func v2PreparedCoordinates(t *testing.T) v2PreparedRunner {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for record platform PostgreSQL integration tests")
	}
	r := v2PreparedRunner{os.Getenv("HOUFENG_RECORD_PLATFORM_APP_CONTAINER_ID"), os.Getenv("HOUFENG_RECORD_PLATFORM_APP_VOLUME"), os.Getenv("HOUFENG_RECORD_PLATFORM_APP_SYSTEM_IDENTIFIER"), os.Getenv("HOUFENG_RECORDS_RUN_ID"), os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER")}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_V2_LOCKS") != "1" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(r.id) || !regexp.MustCompile(`^[0-9]+$`).MatchString(r.system) || !regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,80}$`).MatchString(r.run) || r.owner == "" || !strings.HasPrefix(r.volume, "houfeng-rp-v2locks-"+r.run+"-") || strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL")) == "" {
		t.Fatal("missing or invalid owned postgres-v2-locks runner coordinates")
	}
	return r
}

// Inspect only the fields needed for ownership and connectivity; Config.Env is never requested.
func (r v2PreparedRunner) inspect(t *testing.T, config *pgxpool.Config) {
	t.Helper()
	if config.ConnConfig.Host != "127.0.0.1" || config.ConnConfig.Port == 0 {
		t.Fatal("APP fixture must connect to a loopback TCP port")
	}
	format := `{{json .Id}}|{{json .Config.Labels}}|{{json .HostConfig.NetworkMode}}|{{json .Mounts}}|{{json .Config.Cmd}}`
	out, err := exec.Command("docker", "container", "inspect", "--format", format, r.id).Output()
	if err != nil {
		t.Fatal("cannot inspect owned APP container")
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 5 {
		t.Fatal("invalid APP container inspection")
	}
	var id, network string
	var labels map[string]string
	var mounts []struct{ Type, Name, Destination string }
	var cmd []string
	if json.Unmarshal([]byte(parts[0]), &id) != nil || json.Unmarshal([]byte(parts[1]), &labels) != nil || json.Unmarshal([]byte(parts[2]), &network) != nil || json.Unmarshal([]byte(parts[3]), &mounts) != nil || json.Unmarshal([]byte(parts[4]), &cmd) != nil {
		t.Fatal("invalid APP inspection fields")
	}
	checkLabels := func(m map[string]string) bool {
		return m["com.houfeng.records.runner"] == "record-platform" && m["com.houfeng.records.run"] == r.run && m["com.houfeng.records.owner"] == r.owner
	}
	if id != r.id || network != "host" || !checkLabels(labels) {
		t.Fatal("APP container ownership or network mismatch")
	}
	mounted := false
	for _, mount := range mounts {
		if mount.Destination == "/var/lib/postgresql/data" && mount.Type == "volume" && mount.Name == r.volume {
			mounted = true
		}
	}
	port, loopback := false, false
	for _, arg := range cmd {
		if arg == "port="+strconv.Itoa(int(config.ConnConfig.Port)) {
			port = true
		}
		if arg == "listen_addresses=127.0.0.1" {
			loopback = true
		}
	}
	if !mounted || !port || !loopback {
		t.Fatal("APP mount or PostgreSQL command does not match fixture")
	}
	out, err = exec.Command("docker", "volume", "inspect", "--format", `{{json .Name}}|{{json .Labels}}`, r.volume).Output()
	if err != nil {
		t.Fatal("cannot inspect owned APP volume")
	}
	parts = strings.Split(strings.TrimSpace(string(out)), "|")
	var name string
	labels = nil
	if len(parts) != 2 || json.Unmarshal([]byte(parts[0]), &name) != nil || json.Unmarshal([]byte(parts[1]), &labels) != nil || name != r.volume || !checkLabels(labels) {
		t.Fatal("APP volume ownership mismatch")
	}
}

func (r v2PreparedRunner) checkDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var system string
	var maximum int
	if err := pool.QueryRow(ctx, `select (pg_control_system()).system_identifier::text, current_setting('max_prepared_transactions')::int`).Scan(&system, &maximum); err != nil || system != r.system || maximum != 16 {
		t.Fatal("APP PostgreSQL identity or prepared transaction setting mismatch")
	}
}

func v2PreparedGID(t *testing.T, run string) string {
	t.Helper()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	gid := "g2v2_" + run + "_" + hex.EncodeToString(random[:])
	if len(gid) >= 200 || !regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`).MatchString(gid) {
		t.Fatal("runner ID cannot form an owned prepared transaction identifier")
	}
	return gid
}

func v2PreparedPresent(ctx context.Context, pool *pgxpool.Pool, gid, database, owner string) (bool, error) {
	var present bool
	err := pool.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_prepared_xacts where gid=$1 and database=$2 and owner=$3)`, gid, database, owner).Scan(&present)
	return present, err
}

// Cleanup is restricted to the exact fixture GID, database and owner, and runs
// before the fixture drops its database and roles even when an assertion fails.
func v2PreparedCleanup(t *testing.T, config *pgxpool.Config, gid, database, owner string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		pool, err := pgxpool.NewWithConfig(ctx, config.Copy())
		if err != nil {
			t.Error("cannot create prepared cleanup pool")
			return
		}
		defer pool.Close()
		for {
			present, err := v2PreparedPresent(ctx, pool, gid, database, owner)
			if err == nil {
				if present {
					if _, err := pool.Exec(ctx, `rollback prepared '`+gid+`'`); err != nil {
						t.Errorf("cannot resolve owned prepared transaction: %v", err)
					}
				}
				return
			}
			select {
			case <-ctx.Done():
				t.Error("cannot inspect owned prepared transaction during cleanup")
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	})
}

type v2PreparedWaitResult struct {
	claim *recorddeletion.ClaimedDeletionWork
	err   error
}
type v2PreparedWaiter struct {
	cancel context.CancelFunc
	done   chan v2PreparedWaitResult
}

func v2PreparedStartWaiter(t *testing.T, ctx context.Context, observer, worker *pgxpool.Pool, gid string) v2PreparedWaiter {
	t.Helper()
	conn, err := worker.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire takeover worker")
	}
	var pid int
	if err := conn.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		conn.Release()
		t.Fatal(err)
	}
	body, err := encodeRecordPurgeCommand(struct {
		OwnerID                string `json:"owner_id"`
		OwnerLeaseMicroseconds int64  `json:"owner_lease_microseconds"`
	}{"v2_prepared_takeover", int64(time.Minute / time.Microsecond)})
	if err != nil {
		conn.Release()
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithCancel(ctx)
	done := make(chan v2PreparedWaitResult, 1)
	go func() {
		defer conn.Release()
		claim, err := scanV2DeletionWork(conn.QueryRow(waitCtx, `select * from public.record_deletion_claim_v2($1)`, body), "v2_prepared_takeover")
		done <- v2PreparedWaitResult{claim, err}
	}()
	for {
		var blocked bool
		err := observer.QueryRow(ctx, `select exists(select 1 from pg_stat_activity a where a.pid=$1 and a.wait_event_type='Lock' and 0=any(pg_blocking_pids(a.pid))) and (select count(*) from pg_prepared_xacts)=1 and exists(select 1 from pg_prepared_xacts where gid=$2)`, pid, gid).Scan(&blocked)
		if err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case result := <-done:
			cancel()
			t.Fatalf("takeover completed before prepared transaction resolved: %v", result.err)
		case <-ctx.Done():
			cancel()
			<-done
			t.Fatal("timed out waiting for prepared lock")
		case <-time.After(20 * time.Millisecond):
		}
	}
	return v2PreparedWaiter{cancel, done}
}

func (w v2PreparedWaiter) stop(t *testing.T) {
	t.Helper()
	w.cancel()
	select {
	case <-w.done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled takeover did not finish")
	}
}

func RunV2PreparedLocks(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) {
	runner := v2PreparedCoordinates(t)
	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil {
		t.Fatal("cannot parse APP fixture connection")
	}
	runner.inspect(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bootstrap, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("cannot open APP bootstrap pool")
	}
	runner.checkDatabase(t, ctx, bootstrap)
	bootstrap.Close()
	for _, terminal := range []bool{false, true} {
		for _, commit := range []bool{true, false} {
			name := "activity"
			if terminal {
				name = "finish"
			}
			if commit {
				name += "_commit_prepared"
			} else {
				name += "_rollback_prepared"
			}
			t.Run(name, func(t *testing.T) { runV2PreparedCase(t, runner, newPortabilityAdapter, terminal, commit) })
		}
	}
}

func runV2PreparedCase(t *testing.T, runner v2PreparedRunner, factory func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error), terminal, commit bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	var fixture recordPlatformPostgresFixture
	var workers, runtime *pgxpool.Pool
	var claim *recorddeletion.ClaimedDeletionWork
	var receipt recorddeletion.OnlinePurgeReceipt
	var physical int64
	if terminal {
		f := setupV2FinishFixture(t, factory)
		fixture, workers, runtime, claim, receipt, physical = f.fixture, f.workerPool, f.runtime, f.claim, f.receipt, f.physicalAfter
		short, err := f.repository.RenewV2DeletionWork(ctx, *claim, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		claim = &short
	} else {
		fixture = newAdmittedCoreRecordFixture(t, ctx)
		runtime = fixture.openDirectRuntimePool(t, ctx, "v2-prepared-runtime", 2)
		workers = fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-prepared-worker", 3)
		if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workers); err != nil {
			t.Fatal(err)
		}
		record := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-prepared-record")
		if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection_heads (project_id,projection_generation,head_state) values('default',1,'active')`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection (activity_id,project_id,projection_generation,ingest_sequence,event_kind,event_at,recorded_at,source_kind,source_event_id,source_version,record_id,revision_id,presentation_version,presentation_json,auth_scope_digest,canonical_hash)
			values('act_v2prepared','default',1,1,'record_created',transaction_timestamp(),transaction_timestamp(),'record_domain','event_v2prepared',1,$1,$2,1,'{}'::jsonb,decode(repeat('11',32),'hex'),decode(repeat('11',32),'hex'))`, record.RecordID, record.RevisionID); err != nil {
			t.Fatalf("seed activity projection: %v", err)
		}
		expiry := issueV2WorkerFixture(t, ctx, fixture, runtime, record.RecordID, "prepared")
		waitServingTestFenceExpired(t, ctx, fixture, expiry)
		var err error
		claim, err = NewPostgresRecordDeletionRepository(workers, allowRecordPlatformAdmissionGate).ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_prepared_owner", OwnerLeaseDuration: 10 * time.Second})
		if err != nil || claim == nil {
			t.Fatalf("claim: %v", err)
		}
		if err := fixture.db.QueryRow(ctx, `select coalesce((select physical_bytes from public.attachment_quota_accounts where project_id='default'),0)`).Scan(&physical); err != nil {
			t.Fatal(err)
		}
	}
	bootstrapConfig, workerConfig := fixture.db.Config().Copy(), workers.Config().Copy()
	runner.inspect(t, bootstrapConfig)
	runner.checkDatabase(t, ctx, fixture.db)
	gid := v2PreparedGID(t, runner.run)
	var gidExists bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from pg_prepared_xacts where gid=$1)`, gid).Scan(&gidExists); err != nil || gidExists {
		t.Fatal("generated prepared transaction identifier already exists or cannot be verified")
	}
	v2PreparedCleanup(t, bootstrapConfig, gid, fixture.databaseName, fixture.deletionWorker)
	tx, err := workers.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var activityReceipt []byte
	if terminal {
		owner, err := deletionV2Owner(*claim)
		if err != nil {
			t.Fatal(err)
		}
		body, err := encodeRecordPurgeCommand(struct {
			deletionV2OwnerEnvelope
			ReceiptDigest string `json:"receipt_digest"`
		}{owner, hex.EncodeToString(receipt.ReceiptDigest[:])})
		if err != nil {
			t.Fatal(err)
		}
		var finished bool
		if err := tx.QueryRow(ctx, `select public.record_deletion_finish_v2($1)`, body).Scan(&finished); err != nil || !finished {
			t.Fatalf("finish before prepare: %t, %v", finished, err)
		}
	} else {
		target := recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}
		body, err := deletionAdapterEnvelope(ctx, tx, target, recorddeletion.RecordActivitySurfaceDigest())
		if err != nil {
			t.Fatal(err)
		}
		var surface []byte
		var count int64
		var verified time.Time
		if err := tx.QueryRow(ctx, `select removed_surface_digest,receipt_digest,removed_row_count,verified_absent_at from public.record_activity_purge_v2($1)`, body).Scan(&surface, &activityReceipt, &count, &verified); err != nil {
			t.Fatalf("activity before prepare: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `prepare transaction '`+gid+`'`); err != nil {
		t.Fatalf("prepare transaction: %v", err)
	}
	// PREPARE has ended the SQL transaction; release the pgx transaction's
	// checked-out connection before closing the old pool.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("release prepared worker connection: %v", err)
	}
	present, err := v2PreparedPresent(ctx, fixture.db, gid, fixture.databaseName, fixture.deletionWorker)
	if err != nil || !present {
		t.Fatal("prepared transaction identity mismatch")
	}
	waitTx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitV2DeadlineInTransaction(t, ctx, fixture, waitTx, claim.Owner.ExpiresAt)
	if err := waitTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	waiter := v2PreparedStartWaiter(t, ctx, fixture.db, workers, gid)
	waiter.stop(t)
	workers.Close()
	runtime.Close()
	fixture.db.Reset()
	runner.inspect(t, bootstrapConfig)
	if err := exec.CommandContext(ctx, "docker", "restart", runner.id).Run(); err != nil {
		t.Fatal("owned APP restart failed")
	}
	fresh, err := pgxpool.NewWithConfig(ctx, bootstrapConfig.Copy())
	if err != nil {
		t.Fatal("cannot create fresh bootstrap pool")
	}
	defer fresh.Close()
	for {
		if fresh.Ping(ctx) == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("restarted APP did not become ready")
		case <-time.After(200 * time.Millisecond):
		}
	}
	runner.checkDatabase(t, ctx, fresh)
	present, err = v2PreparedPresent(ctx, fresh, gid, fixture.databaseName, fixture.deletionWorker)
	if err != nil || !present {
		t.Fatal("prepared transaction lost or changed after APP restart")
	}
	freshWorkers, err := pgxpool.NewWithConfig(ctx, workerConfig.Copy())
	if err != nil {
		t.Fatal("cannot create fresh worker pool")
	}
	defer freshWorkers.Close()
	waiter = v2PreparedStartWaiter(t, ctx, fresh, freshWorkers, gid)
	verb := "rollback"
	if commit {
		verb = "commit"
	}
	if _, err := freshWorkers.Exec(ctx, verb+` prepared '`+gid+`'`); err != nil {
		waiter.stop(t)
		t.Fatalf("%s prepared as direct worker: %v", verb, err)
	}
	var takeover v2PreparedWaitResult
	select {
	case takeover = <-waiter.done:
		waiter.cancel()
	case <-ctx.Done():
		waiter.stop(t)
		t.Fatal("takeover did not finish after prepared resolution")
	}
	result, err := takeover.claim, takeover.err
	if err != nil {
		t.Fatalf("claim after prepared resolution: %v", err)
	}
	if terminal && commit {
		if result != nil {
			t.Fatal("terminal commit returned work")
		}
	} else if result == nil || result.Owner.Generation != claim.Owner.Generation+1 {
		t.Fatal("takeover did not advance the original generation")
	}
	var state string
	var terminalValid, receiptPresent, projectionPresent, takeoverValid bool
	var quota int64
	if err := fresh.QueryRow(ctx, `select o.operation_state,
		coalesce(o.completed_at is not null and o.receipt_digest=$2 and o.owner_id='' and o.owner_generation=0 and o.owner_expires_at is null and r.state='committed' and r.owner_id='' and r.owner_generation=0 and r.owner_expires_at is null and exists(select 1 from public.record_deletion_gates g where g.operation_id=o.operation_id and g.project_id=o.project_id and g.record_id=r.object_id and g.fence_epoch=r.fence_epoch) and not exists(select 1 from public.deletion_fence_leases f where f.project_id=r.project_id and f.object_kind='record' and f.object_id=r.object_id),false),
		exists(select 1 from public.record_activity_purge_receipts a where a.operation_id=o.operation_id),
		exists(select 1 from public.record_activity_projection a where a.record_id=r.object_id),
		coalesce(r.state='fenced' and o.owner_id='v2_prepared_takeover' and o.owner_generation=$3 and r.owner_id=o.owner_id and r.owner_generation=o.owner_generation and r.owner_expires_at=o.owner_expires_at and exists(select 1 from public.record_deletion_gates g where g.operation_id=o.operation_id and g.project_id=r.project_id and g.record_id=r.object_id and g.fence_epoch=r.fence_epoch) and exists(select 1 from public.deletion_fence_leases f where f.project_id=r.project_id and f.object_kind='record' and f.object_id=r.object_id and f.owner_id=o.owner_id and f.owner_generation=o.owner_generation and f.expires_at=o.owner_expires_at),false),
		coalesce((select physical_bytes from public.attachment_quota_accounts where project_id='default'),0)
		from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id where o.operation_id=$1`, claim.Operation.OperationID, receipt.ReceiptDigest[:], claim.Owner.Generation+1).Scan(&state, &terminalValid, &receiptPresent, &projectionPresent, &takeoverValid, &quota); err != nil {
		t.Fatal(err)
	}
	if quota != physical || terminalValid != (terminal && commit) || takeoverValid == (terminal && commit) || (terminal && commit && state != "online_purged") || (!(terminal && commit) && state != "online_purging") {
		t.Fatalf("prepared result state=%s terminal=%t takeover=%t quota=%d expected=%d", state, terminalValid, takeoverValid, quota, physical)
	}
	if !terminal {
		if receiptPresent != commit {
			t.Fatalf("activity receipt presence=%t expected=%t", receiptPresent, commit)
		}
		if projectionPresent == commit {
			t.Fatalf("activity projection presence=%t after commit=%t", projectionPresent, commit)
		}
		if commit {
			target := recorddeletion.PurgeTarget{Operation: result.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: result.Operation.OperationID, Owner: result.Owner}}
			replay, err := NewPostgresRecordDeletionRepository(freshWorkers, allowRecordPlatformAdmissionGate).PurgeRecordActivity(ctx, activity.DeletionCommand{PurgeTarget: target, SurfaceDigest: recorddeletion.RecordActivitySurfaceDigest()})
			if err != nil || hex.EncodeToString(replay.ReceiptDigest[:]) != hex.EncodeToString(activityReceipt) {
				t.Fatalf("activity replay failed: %v", err)
			}
		}
	} else if commit {
		if err := NewPostgresRecordDeletionRepository(freshWorkers, allowRecordPlatformAdmissionGate).CompleteV2OnlinePurge(ctx, *claim, receipt); err != nil {
			t.Fatalf("terminal readback: %v", err)
		}
		var after int64
		if err := fresh.QueryRow(ctx, `select physical_bytes from public.attachment_quota_accounts where project_id='default'`).Scan(&after); err != nil || after != physical {
			t.Fatalf("terminal readback changed quota: %v", err)
		}
	} else {
		var intact bool
		if err := fresh.QueryRow(ctx, `select exists(select 1 from public.record_core_purge_receipts where operation_id=$1) and exists(select 1 from public.record_activity_purge_receipts where operation_id=$1) and not exists(select 1 from public.records where record_id=$2) and (select receipt_digest is null and completed_at is null from public.record_purge_operations where operation_id=$1)`, claim.Operation.OperationID, claim.Operation.Object.ObjectID).Scan(&intact); err != nil || !intact {
			t.Fatalf("rollback changed prior purge effects: %v", err)
		}
	}
}
