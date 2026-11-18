package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordMutationAcquisitionRanksAndLiveFence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "mutation-rank-worker", 1)
	const first, second = "rec_lockrankalpha", "rec_lockrankbeta"
	const rankBarrierLock int64 = 8240054
	if _, err := fixture.db.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values ('default','record',$1,0),('default','record',$2,0)`, first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.deletion_fence_leases(project_id,object_kind,object_id,owner_id,owner_generation,created_at,expires_at)
		values ('default','record',$1,'expired_rank_owner',1,clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '1 minute')`, first); err != nil {
		t.Fatal(err)
	}
	// The epoch's BEFORE INSERT guard uses NOWAIT even for ON CONFLICT. Pause
	// immediately before that guard on beta so the test can observe the locks
	// acquired for alpha without disabling or bypassing the production guard.
	for _, definition := range []string{
		`create function public.test_mutation_rank_epoch_barrier() returns trigger language plpgsql as $$
		begin
			perform pg_catalog.pg_advisory_xact_lock(` + fmt.Sprint(rankBarrierLock) + `);
			return new;
		end; $$`,
		`create trigger aaa_mutation_rank_epoch_barrier before insert on public.content_delivery_epochs
			for each row when (new.object_kind = 'record' and new.object_id = 'rec_lockrankbeta')
			execute function public.test_mutation_rank_epoch_barrier()`,
		`grant execute on function public.test_mutation_rank_epoch_barrier() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	} {
		if _, err := fixture.db.Exec(ctx, definition); err != nil {
			t.Fatalf("install epoch rank barrier: %v", err)
		}
	}
	resources, err := normalizeRecordMutationDeclarations(records.RevisionMutationResources{Records: []records.RevisionMutationRecord{
		{ProjectID: "default", RecordID: second}, {ProjectID: "default", RecordID: first},
	}})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	var holderPID, workerPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock($1::bigint)`, rankBarrierLock); err != nil {
		t.Fatal(err)
	}
	if err := runtime.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			result <- err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err = lockAssetGraphForSync(ctx, tx); err == nil {
			err = lockComparisonDecisionForWriter(ctx, tx)
		}
		if err == nil {
			err = acquireRecordMutationResources(ctx, tx, resources)
		}
		result <- err
	}()
	waitForMutationTestBlocker(t, ctx, fixture.db, workerPID, holderPID, result)

	// Reversed input must already hold alpha's epoch before attempting beta,
	// without having advanced alpha to the later lease rank.
	probe, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var one int
	err = probe.QueryRow(ctx, `select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update nowait`, first).Scan(&one)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		_ = probe.Rollback(ctx)
		t.Fatalf("first epoch was not held while waiting for second: %v", err)
	}
	_ = probe.Rollback(ctx)
	probe, err = fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow(ctx, `select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update nowait`, second).Scan(&one); err != nil {
		_ = probe.Rollback(ctx)
		t.Fatalf("second epoch acquired before its barrier: %v", err)
	}
	_ = probe.Rollback(ctx)
	probe, err = fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow(ctx, `select 1 from public.deletion_fence_leases where project_id='default' and object_kind='record' and object_id=$1 for update nowait`, first).Scan(&one); err != nil {
		_ = probe.Rollback(ctx)
		t.Fatalf("first lease locked before all record epochs: %v", err)
	}
	_ = probe.Rollback(ctx)
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := waitForRecordPlatformResult(t, result, "ranked record mutation acquisition"); err != nil {
		t.Fatal(err)
	}

	// Contention on an existing epoch is reported as a serialization failure
	// by the comparison guard; the caller must restart instead of waiting here.
	busy, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Rollback(context.Background()) }()
	if _, err := busy.Exec(ctx, `select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, second); err != nil {
		t.Fatal(err)
	}
	contender, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = contender.Rollback(context.Background()) }()
	if err := lockAssetGraphForSync(ctx, contender); err != nil {
		t.Fatal(err)
	}
	if err := lockComparisonDecisionForWriter(ctx, contender); err != nil {
		t.Fatal(err)
	}
	err = acquireRecordMutationResources(ctx, contender, resources)
	if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		_ = contender.Rollback(ctx)
		t.Fatalf("contended epoch acquisition = %v, want serialization failure", err)
	}
	if err := contender.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := busy.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// The existing lease API supplies a real, live deletion fence. A missing
	// root must not allow the new writer to bypass it.
	platform := NewPostgresRecordPlatformRepository(runtime, allowRecordPlatformAdmissionGate)
	if _, err := platform.AcquireDeletionFenceLease(ctx, recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first}, recordplatform.LeaseClaimInputV1{OwnerID: "rank_fence_owner", LeaseDuration: time.Minute}); err != nil {
		t.Fatal(err)
	}
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := acquireRecordMutationResources(ctx, tx, resources); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("live deletion fence with missing root = %v, want reserved", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := acquireTerminalRecordMutationResources(ctx, tx, resources); err != nil {
		t.Fatalf("terminal observation behind live fence: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	missing, err := normalizeRecordMutationDeclarations(records.RevisionMutationResources{Records: []records.RevisionMutationRecord{{ProjectID: "default", RecordID: "rec_missingepoch"}}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err = runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := acquireTerminalRecordMutationResources(ctx, tx, missing); !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("terminal observation without epoch = %v, want missing epoch", err)
	}
	var epochExists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id='rec_missingepoch')`).Scan(&epochExists); err != nil || epochExists {
		t.Fatalf("terminal observation created missing epoch: exists=%v err=%v", epochExists, err)
	}
}

// Observe a real PostgreSQL blocker and surface an early worker failure rather
// than waiting for the deadline when the worker never reaches the barrier.
func waitForMutationTestBlocker[T any](t *testing.T, ctx context.Context, db postgresBlockingPIDQueryer, workerPID, holderPID int, result <-chan T) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := db.QueryRow(ctx, `select $2::integer = any(pg_catalog.pg_blocking_pids($1::integer))`, workerPID, holderPID).Scan(&blocked); err != nil {
			t.Fatalf("observe PostgreSQL blocker %d <- %d: %v", workerPID, holderPID, err)
		}
		if blocked {
			return
		}
		select {
		case value := <-result:
			t.Fatalf("worker %d finished before waiting for blocker %d: %#v", workerPID, holderPID, value)
		case <-ctx.Done():
			t.Fatalf("PostgreSQL blocker %d <- %d not observed: %v", workerPID, holderPID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestPostgresIntegrationRecordMutationIdempotencyPhysicalHashOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	fixture := signed.platform
	runtime := revisionBirthBatchRuntimePool(t, ctx, signed.Runtime)

	type scope struct {
		key  recordplatform.IdempotencyKey
		hash int32
	}
	candidates := make([]scope, 64)
	frames := make([]string, len(candidates))
	for index := range candidates {
		candidates[index].key = recordplatform.IdempotencyKey{
			ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate,
			Key: fmt.Sprintf("rank-order-%03d", index),
		}
		var err error
		frames[index], err = recordIdempotencyScopeFrame(candidates[index].key)
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := fixture.db.Query(ctx, `select pg_catalog.hashtext(value) from unnest($1::text[]) with ordinality as keys(value, ordinal) order by ordinal`, frames)
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	for rows.Next() {
		if index >= len(candidates) {
			rows.Close()
			t.Fatal("unexpected physical hash row")
		}
		if err := rows.Scan(&candidates[index].hash); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		index++
	}
	rows.Close()
	if err := rows.Err(); err != nil || index != len(candidates) {
		t.Fatalf("read physical hash rows = %d/%d, error %v", index, len(candidates), err)
	}

	// Choose keys whose lexicographic order differs from signed hash order, so
	// the barrier distinguishes physical lock ordering from key sorting.
	var selected []scope
	for i := 0; i < len(candidates) && len(selected) == 0; i++ {
		for j := i + 1; j < len(candidates) && len(selected) == 0; j++ {
			for k := j + 1; k < len(candidates); k++ {
				a, b, c := candidates[i], candidates[j], candidates[k]
				if a.hash != b.hash && b.hash != c.hash && a.hash != c.hash && (a.hash > b.hash || b.hash > c.hash) {
					selected = []scope{a, b, c}
					break
				}
			}
		}
	}
	if len(selected) != 3 {
		t.Fatal("could not select distinct hashes with different key order")
	}
	workerFixture := signed
	workerFixture.Runtime = runtime
	repository := newRevisionMutationAdmittedRepository(t, workerFixture)
	commands := make([]records.RevisionCommitCommand, 0, len(selected))
	for index := len(selected) - 1; index >= 0; index-- {
		commands = append(commands, revisionBirthBatchCommand(t, ctx, signed,
			recordsPostgresCompleteRevisionInput(t, "Idempotency rank"), selected[index].key.Key))
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].hash < selected[j].hash })

	holder, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID, workerPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock($1::integer, $2::integer)`, recordIdempotencyAdvisoryClassID, selected[1].hash); err != nil {
		t.Fatal(err)
	}
	if err := runtime.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := repository.CommitRevisions(ctx, commands)
		result <- err
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
	probe, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	var lowAcquired, highAcquired bool
	if err := probe.QueryRow(ctx, `select pg_catalog.pg_try_advisory_xact_lock($1::integer, $2::integer), pg_catalog.pg_try_advisory_xact_lock($1::integer, $3::integer)`, recordIdempotencyAdvisoryClassID, selected[0].hash, selected[2].hash).Scan(&lowAcquired, &highAcquired); err != nil {
		_ = probe.Rollback(ctx)
		t.Fatal(err)
	}
	_ = probe.Rollback(ctx)
	if lowAcquired || !highAcquired {
		t.Fatalf("physical locks while waiting: low acquired=%v high acquired=%v; want false/true", lowAcquired, highAcquired)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := waitForRecordPlatformResult(t, result, "ordered idempotency scope acquisition"); err != nil {
		t.Fatal(err)
	}
}
